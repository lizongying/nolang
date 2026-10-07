package checker

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// TestRedundantTypeAnnotationSkipsInferredMapFromCall 釘住 `m2 = make-map()`
// 不應被回報為「可省略 [str]i64 標註」。
//
// make-map 的回傳（輸出參數）型別是 [str]i64；parser 在 parseLetStatement 的
// CallExpression 推斷分支會把這個 map 型別合成掛到 LetStatement.Type 上（源碼
// 根本沒寫標註）。過去 MapType 沒有 IsInferred 欄位、markInferred/isInferredType
// 也不認得它，於是冗餘檢查把這個「推斷而来的標註」當成用戶顯式寫的、且等於推斷
// 型別 → 回報可省略 → `no fmt` 預先移除據 Type.Pos()（指向變數名）掃描，把變數名
// `m2` 當成標註文本刪掉，產生 ` = make-map()` 亂碼。
//
// 修復：MapType 補 IsInferred；markInferred/isInferredType 認得它。本測試確保
// 推斷的 call-map 型別被標記為 inferred，從而不被回報。
func TestRedundantTypeAnnotationSkipsInferredMapFromCall(t *testing.T) {
	src := "make-map = () (out [str]i64) {\n    out [str]i64 = { 'x': 10 }\n}\nm2 = make-map()\n"
	prog := mustParse(t, src)

	// Sanity: m2's synthesized map type must be flagged inferred.
	var m2 *parser.LetStatement
	for _, s := range prog.Statements {
		if ls, ok := s.(*parser.LetStatement); ok && ls.Name != nil && ls.Name.Value == "m2" {
			m2 = ls
		}
	}
	if m2 == nil {
		t.Fatal("m2 LetStatement not found")
	}
	mt, ok := m2.Type.(*parser.MapType)
	if !ok {
		t.Fatalf("m2.Type = %T, want *parser.MapType", m2.Type)
	}
	if !mt.IsInferred {
		t.Fatal("inferred map type from call must be marked IsInferred=true")
	}

	// The m2 statement (line 4, where its name sits) must NOT be reported as an
	// omittable annotation. (The explicit `out [str]i64 = {..}` map-literal line
	// may still produce a cosmetic hint, but the fix tool refuses to remove
	// MapLiteral annotations — that is out of scope here.)
	for _, r := range ValidateRedundantTypeAnnotation(prog) {
		if r.Line == m2.Type.Pos().Line {
			t.Fatalf("m2's inferred map annotation wrongly reported omittable: %+v", r)
		}
	}
}
