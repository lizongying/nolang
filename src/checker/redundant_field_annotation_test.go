package checker

import (
	"testing"
)

// 欄位讀取右值上的型別標註是 D1 option 算術的語意開關，不是可省的風格提示。
//
// src/std/yaml.no:1328 `parser.looks-like-map-entry` 的 `i i64 = .pos`：
// 刪掉 i64 後下游 `i-optN = i + N` 不再產生 `?i64`，兩段式解包的 ok 臂永不命中，
// `i` 原地不動 ⇒ `yaml.parse('abc')` 無限迴圈（實測被 SIGTERM 殺掉）。
// 舊版 `no vet` 對這行報 tcpoxtfd、`no fmt -fix=redundant` 更會照提示把標註刪掉；
// 本測試釘住：欄位讀取（含隱式 self、括號/前綴包裹）一律不報冗餘。
//
// 每條案例都跑兩種上下文：validationStructFields 為 nil（預設 no fmt 的單檔
// 管道）與已填表（合併 vet 管道）。兩邊必須一致，否則又會出現「vet 有提示、
// fmt 沒 diff」的工具鏈分歧。
func TestRedundantTypeAnnotationKeepsFieldAccess(t *testing.T) {
	const prefix = "box {\n" +
		"    pos i64\n" +
		"    name str\n" +
		"}\n" +
		"h box = box { pos: 0, name: 'x' }\n" +
		"box.walk = () {\n"
	// 目標陳述固定在第 7 行。
	const targetLine = 7

	cases := []struct {
		name          string
		line          string
		wantRedundant bool
	}{
		{"implicit self field", "    i i64 = .pos", false},
		{"explicit self field", "    i i64 = self.pos", false},
		{"explicit receiver field", "    i i64 = h.pos", false},
		{"grouped field", "    i i64 = (.pos)", false},
		{"prefix field", "    i i64 = - .pos", false},
		{"string field", "    s str = .name", false},
		// 尺子：非欄位讀取的純量標註仍須照常報冗餘，證明推斷鏈沒有整體失效。
		{"scalar i64 literal control", "    i i64 = 42", true},
		{"scalar str literal control", "    s str = 'x'", true},
	}

	for _, c := range cases {
		for _, ctx := range []struct {
			name    string
			vetLike bool
		}{{"standalone", false}, {"vetlike", true}} {
			t.Run(c.name+"/"+ctx.name, func(t *testing.T) {
				prog := mustParse(t, prefix+c.line+"\n}\n")
				validationMu.Lock()
				if ctx.vetLike {
					validationStructFields = collectStructFields(prog)
				} else {
					validationStructFields = nil
				}
				validationMu.Unlock()

				got := false
				for _, r := range ValidateRedundantTypeAnnotation(prog) {
					if r.TraceID == "tcpoxtfd" && r.Line == targetLine {
						got = true
					}
				}

				validationMu.Lock()
				validationStructFields = nil
				validationMu.Unlock()

				if got != c.wantRedundant {
					t.Fatalf("tcpoxtfd on line %d = %v, want %v (%q, %s)",
						targetLine, got, c.wantRedundant, c.line, ctx.name)
				}
			})
		}
	}
}
