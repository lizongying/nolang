//go:build !wasm

package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestCloneUnionVariantInheritsSourceFileDeep 是單態化克隆體丟失來源檔的迴歸測試。
//
// 复現場景：std/math.no 的 `div = (a int, b int) (r int)` 參數是 union 別名 int，
// 單態化會為每個成員型別複製一份具體實例（`div__u8` / `div__u16` / …）。clone 是
// 淺拷貝 + 逐欄位搬運（cloneStmtForUnion），它搬了 TypeOverride、OverflowMode，
// 卻不知道 NodeFacts 裡還有 SourceFile —— 於是克隆體整棵子樹的 SourceFile 都是空。
//
// 為什麼這很惡性：checker 的 lint 把「SourceFile 為空」當成「這是主檔語句」
// （LintIneffectiveOverflow 的 isImportedCopy）。克隆體因此被貼上用戶主檔的檔名，
// 卻帶著 std 的行號，報出 `main.no:250` 這種主檔根本沒有的位置。實測
// TestVetImportedModuleDiagnosticsNotAttributedToMainFile 與
// TestVetCrossModulePositionReliability 兩條不變量同時被它打破。
//
// 為什麼到 2026-10-26 才暴露：在此之前 `#{...}` 註解存在各 Program 自己的
// `SemanticContext.anns`（節點指針鍵）裡，克隆體是新指針、查不到條目 ⇒ std 的
// `#{overflow=...}` 從不觸發 lint，這條歸因路徑沒被走到。註解改為住在節點上之後
// 它們隨淺拷貝進了克隆體，缺陷才顯形。本測試不依賴註解路徑，直接釘住克隆契約。
//
// 反向保障（避免測試空洞）：斷言必須**真的**在克隆體裡看到語句，否則「每條語句
// 都有來源檔」對空集合恆真。
func TestCloneUnionVariantInheritsSourceFileDeep(t *testing.T) {
	const src = "div = (a int, b int) (r int) {\n" +
		"    #{overflow=wrap}\n" +
		"    r = a / b\n" +
		"}\n"
	p := parser.New(lexer.NewCached("mod.no", src))
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	var fd *parser.FunctionDefinition
	for _, s := range prog.Statements {
		if x, ok := s.(*parser.FunctionDefinition); ok && x.Name == "div" {
			fd = x
		}
	}
	if fd == nil {
		t.Fatalf("fixture did not parse to a FunctionDefinition named div; statements=%d", len(prog.Statements))
	}
	prog.Sem.SetSourceFileDeep(fd, "mod.no")

	clone := cloneUnionVariant(fd, "u8", nil, prog.Sem)
	if clone == nil || clone.Body == nil {
		t.Fatal("cloneUnionVariant returned a nil body")
	}
	if got := prog.Sem.SourceFileOf(clone); got != "mod.no" {
		t.Errorf("clone SourceFile = %q, want %q (the clone is the same source code)", got, "mod.no")
	}

	// 每個子節點都必須帶來源檔，不只是頂層：cloneStmtForUnion 為每條語句新建指標，
	// 只標頂層會讓 lint 從子節點回退到主檔。
	seen := 0
	var walk func(stmts []parser.Statement)
	walk = func(stmts []parser.Statement) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			seen++
			if got := prog.Sem.SourceFileOf(s); got != "mod.no" {
				t.Errorf("cloned %T at %d:%d has SourceFile %q, want %q",
					s, s.Pos().Line, s.Pos().Column, got, "mod.no")
			}
			switch v := s.(type) {
			case *parser.FunctionDefinition:
				if v.Body != nil {
					walk(v.Body.Statements)
				}
			case *parser.BlockStatement:
				walk(v.Statements)
			case *parser.LetStatement:
				if fl, ok := v.Value.(*parser.FunctionLiteral); ok && fl.Body != nil {
					walk(fl.Body.Statements)
				}
			}
		}
	}
	walk(clone.Body.Statements)
	if seen == 0 {
		t.Fatal("walked zero statements in the clone — the per-statement assertion above would be vacuous")
	}
}
