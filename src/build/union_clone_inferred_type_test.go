package build

import (
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// findLetByName 在 prog 裡找 name 為 funcName 的函式，回傳其函式體中
// 第一個名為 varName 的 LetStatement（找不到回 nil）。
func findLetByName(prog *parser.Program, funcName, varName string) *parser.LetStatement {
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok || fd.Name != funcName || fd.Body == nil {
			continue
		}
		for _, bs := range fd.Body.Statements {
			ls, ok := bs.(*parser.LetStatement)
			if !ok || ls.Name == nil || ls.Name.Value != varName {
				continue
			}
			return ls
		}
	}
	return nil
}

// TestUnionCloneCarriesInferredType 是本輪 bug 的守門測試。
//
// monomorphizeUnions 把方法體逐節點複製成單態化 clone，clone 全是新的節點
// 指標；而 Program 級 TypeFacts（InferredType / TypeOverride）以節點為鍵，
// 不會自動跟過去。漏搬 InferredType 時 clone 內的 `r = a + 1` 退回 i64——
// 值槽只留 payload、沒有 option 標籤，後面的 `r: { ok … -> … }` 分派整個
// 失效（三臂全不執行、兩臂恆落 catch-all）。
//
// 外顯症狀：int.to-str（union 方法）對 i64 以外的整數型別輸出空字串——
// 位數靠 `digits-opt = digits + 1` 累加，clone 內恆為 0。test/std/number.no
// 的 "u64-to-str 255 len/content" 兩條失敗即此。
func TestUnionCloneCarriesInferredType(t *testing.T) {
	src := `myu = u8 | u64

myu.f = () (n i64) {
    a = 0
    r = a + 1
    r: {
        ok -> n = it
        nil -> n = 1
        err -> n = 2
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if prog.Sem == nil {
		t.Fatalf("program has no SemanticContext")
	}

	// 反空洞：模板自身必須已記錄 `r` 的推斷型別，否則 clone 的斷言會恆真。
	tmpl := findLetByName(prog, "myu.f", "r")
	if tmpl == nil {
		t.Fatalf("template let `r` not found in myu.f")
	}
	if got := prog.Sem.InferredType(tmpl); got != "?i64" {
		t.Fatalf("template `r` inferred type = %q, want %q "+
			"(lowering 不再登記 ?T 的話本測試失去意義)", got, "?i64")
	}

	checker.ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	clone := findLetByName(prog, "myu.f__u64", "r")
	if clone == nil {
		t.Fatalf("monomorphized clone myu.f__u64 (let `r`) not found; "+
			"statements present: %v", funcNames(prog))
	}
	if clone == tmpl {
		t.Fatalf("clone shares the template's *LetStatement node; " +
			"cloneStmtForUnion must deep-copy it")
	}
	if got := prog.Sem.InferredType(clone); got != "?i64" {
		t.Errorf("clone `r` inferred type = %q, want %q: "+
			"cloneStmtForUnion 必須把 InferredType 一併搬到 clone，否則 "+
			"clone 內 `r = a + 1` 退回 i64，option 分派失效", got, "?i64")
	}
}

// funcNames 收集程式中所有函式名，僅供失敗訊息定位。
func funcNames(prog *parser.Program) []string {
	var names []string
	for _, s := range prog.Statements {
		if fd, ok := s.(*parser.FunctionDefinition); ok {
			names = append(names, fd.Name)
		}
	}
	return names
}
