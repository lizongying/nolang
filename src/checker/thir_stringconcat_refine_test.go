package checker

import "testing"

// 本文件锁定 6C-optional 第四批「StringConcat 型別精化補報」的行為變更語義。
//
// 背景（摸底結論）：Nolang 字串拼接用 `-`，`+` 搭 str 是誤用（lint 提示改 `-`）。
// 精化前只認「任一侧是 StringLiteral」⇒ `str_var + str_var` **漏報**。
// 精化後两侧都確定為字串（str/txt，走 THIRView.TypeOf 表達式表）時同樣報。
//
// 行為變更性質：**只增不減**（`str+str` 由漏報轉補報，其餘用例報告集合不變）。
// 保守口徑：任一側型別未知即不補報。

func TestStringConcatRefine(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		wantRefine int // 精化後（生產路徑）應有的報告數
		wantLegacy int // view=nil（純語法、只認字面量）應有的報告數
	}{
		// 字面量一側觸發：精化不改變，兩側都報
		{"str-lit-concat", "f = (a str) {\n\tb = a + 'x'\n}\n", 1, 1},
		// `str_var + str_var`：精化補報（本批行為變更）
		{"str-var-concat", "f = (a str) (r str) {\n\tb str = 'y'\n\tr = a + b\n}\n", 1, 0},
		// 兩側都 str 的巢狀鏈：每個 `+` 都補報
		{"str-chain-concat", "f = (a str) (r str) {\n\tb str = 'y'\n\tc str = 'z'\n\tr = a + b + c\n}\n", 2, 0},
		// 一侧 str、一侧 i64：不都確定為字串 ⇒ 保守不補報（也無字面量）
		{"str-plus-num", "f = (a str) (n i64) {\n\tb = a + n\n}\n", 0, 0},
		// 純數值加法：不報
		{"num-plus-num", "f = (a i64, b i64) {\n\tc = a + b\n}\n", 0, 0},
	}
	for _, c := range cases {
		// view 與 program 必須同源（IdMap 以 AST 指針為鍵），故同一 prog/view 雙跑。
		prog, view := shadowBuildView(t, c.src)
		if got := len(ValidateStringConcat(prog, view)); got != c.wantRefine {
			t.Errorf("[%s] refined string-concat reports = %d, want %d", c.name, got, c.wantRefine)
		}
		if got := len(ValidateStringConcat(prog, nil)); got != c.wantLegacy {
			t.Errorf("[%s] legacy (view=nil) string-concat reports = %d, want %d", c.name, got, c.wantLegacy)
		}
	}
}

// TestStringConcatRefineOnlyAddsReports 斷言精化的**只增不減**性質：影子語料上，
// legacy（view=nil）的報告多重集必須是 refined（view）的子集（計數逐鍵 <=）。
func TestStringConcatRefineOnlyAddsReports(t *testing.T) {
	for _, tc := range shadowSynth {
		prog, view := shadowBuildView(t, tc.src)
		refined := ValidateStringConcat(prog, view)
		legacy := ValidateStringConcat(prog, nil)
		type key struct {
			file  string
			line  int
			col   int
			trace string
		}
		count := func(rs []ValidateResult) map[key]int {
			m := map[key]int{}
			for _, r := range rs {
				m[key{r.File, r.Line, r.Column, r.TraceID}]++
			}
			return m
		}
		rc, lc := count(refined), count(legacy)
		for k, n := range lc {
			if n > rc[k] {
				t.Errorf("%s: legacy report %v count %d exceeds refined %d — refine must only add", tc.name, k, n, rc[k])
			}
		}
	}
}
