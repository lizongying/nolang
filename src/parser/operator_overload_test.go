package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
)

// operator_overload_test.go — 運算符重載（`_add` 等）的 parser 端不变量：
//
//  1. lowering（build 路徑）把 `a + b` 重寫為 `a._add(b)`（CallExpression）。
//  2. 運算符重載方法的結果參數書寫為 `?T`，但簽名表 / HIR KResult 均為
//     payload T（不產生 option ABI）——鏈式 `a + b + c` 的中間值因此
//     不經過 heap 包裝，不會被淺複製後的生命週期問題打穿（讀到 0）。
//  3. analysisOnly（fmt/LSP）路徑不改寫 AST，`a + b` 原樣保留。

const olSrc = `
bigint {
    sign i64
    limbs []i64
}

bigint._add = (b bigint) (c ?bigint) {
    c = b
}

a bigint = bigint{}
b bigint = bigint{}
r = a + b
print(r.sign)
`

func parseOLProg(t *testing.T, src string) (*Parser, *Program) {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	return p, prog
}

// findLet 回傳具名頂層 LetStatement。
func findTopLet(t *testing.T, prog *Program, name string) *LetStatement {
	t.Helper()
	for _, s := range prog.Statements {
		if ls, ok := s.(*LetStatement); ok && ls.Name != nil && ls.Name.Value == name {
			return ls
		}
	}
	t.Fatalf("no top-level let %q", name)
	return nil
}

// TestOperatorOverloadDesugarsToMethodCall 驗證 build 路徑把 `a + b`
// 重寫為 `a._add(b)` 的方法呼叫。
func TestOperatorOverloadDesugarsToMethodCall(t *testing.T) {
	_, prog := parseOLProg(t, olSrc)
	ls := findTopLet(t, prog, "r")
	call, ok := ls.Value.(*CallExpression)
	if !ok {
		t.Fatalf("`a + b` 未被重寫為方法呼叫，got %T", ls.Value)
	}
	dot, ok := call.Function.(*DotExpression)
	if !ok {
		t.Fatalf("callee 不是 DotExpression，got %T", call.Function)
	}
	if dot.Property != "_add" {
		t.Fatalf("callee property = %q, want %q", dot.Property, "_add")
	}
	if len(call.Arguments) != 1 {
		t.Fatalf("call args = %d, want 1", len(call.Arguments))
	}
}

// TestOperatorOverloadChainDesugars 驗證左嵌套鏈式 `a + b + c` 的內層
// （post-order 先降）與外層都重寫完成：外層接收者是 `a._add(b)` 呼叫。
func TestOperatorOverloadChainDesugars(t *testing.T) {
	src := `
bigint {
    sign i64
    limbs []i64
}

bigint._add = (b bigint) (c ?bigint) {
    c = b
}

a bigint = bigint{}
b bigint = bigint{}
c bigint = bigint{}
r = a + b + c
print(r.sign)
`
	_, prog := parseOLProg(t, src)
	ls := findTopLet(t, prog, "r")
	outer, ok := ls.Value.(*CallExpression)
	if !ok {
		t.Fatalf("outer `+` 未重寫，got %T", ls.Value)
	}
	outerDot, ok := outer.Function.(*DotExpression)
	if !ok || outerDot.Property != "_add" {
		t.Fatalf("outer callee = %#v, want _add dot-call", outer.Function)
	}
	inner, ok := outerDot.Receiver.(*CallExpression)
	if !ok {
		t.Fatalf("inner `a + b` 未重寫為呼叫，got %T", outerDot.Receiver)
	}
	innerDot, ok := inner.Function.(*DotExpression)
	if !ok || innerDot.Property != "_add" {
		t.Fatalf("inner callee = %#v, want _add dot-call", inner.Function)
	}
}

// TestOperatorOverloadSignatureIsPayload 驗證運算符重載方法的簽名表把
// 書寫的 `?T` 存成 payload T（結果型別不帶 option）。
// 注意存放去向的既有約定（見 semviews.go rebuildLocalSignatures 註釋）：
// `=`-形式方法（bigint._add = ...）落 funcSignatures[全名]，冒號形式才落
// methodSignatures[全名]。查表端（returnTypeFromCallExpr）兩張表都查，
// 這裡按形式斷言實際存放處。
func TestOperatorOverloadSignatureIsPayload(t *testing.T) {
	p, _ := parseOLProg(t, olSrc)
	rets, ok := p.funcSignatures["bigint._add"]
	if !ok || len(rets) != 1 {
		t.Fatalf("funcSignatures[bigint._add] = %v, want [T]", rets)
	}
	if strings.HasPrefix(rets[0], "?") {
		t.Fatalf("運算符重載簽名仍帶 option：得 %q，want %q", rets[0], "bigint")
	}
	if rets[0] != "bigint" {
		t.Fatalf("簽名結果型別 = %q, want %q", rets[0], "bigint")
	}
	// 探測入口的一致性：運算符重載回傳型別必須是純 payload（鏈式派發依賴它）。
	inf := &InfixExpression{Operator: "+", Left: &Identifier{Value: "a"}, Right: &Identifier{Value: "b"}}
	if rt := p.OperatorOverloadReturnType(inf); rt != "bigint" {
		t.Fatalf("OperatorOverloadReturnType = %q, want %q", rt, "bigint")
	}
}

// TestOperatorOverloadHirResultPayload 驗證 HIR 層 KResult 型別為純
// payload（`bigint` 而非 `?bigint`），callee ABI 不產生 %option* 結果槽。
// 這是鏈式運算子中間值不被 heap 包裝的根基（否則淺複製 vec 欄位隨臨時
// 值釋放，第二個運算子讀到 0）。
func TestOperatorOverloadHirResultPayload(t *testing.T) {
	_, prog := parseOLProg(t, olSrc)
	pkg := ASTToHIR(prog)
	var found bool
	for id := range pkg.Nodes {
		n := pkg.Node(int32(id))
		if n == nil || n.Kind != hir.KFuncDef {
			continue
		}
		if !strings.HasSuffix(pkg.Str(n.S), "_add") {
			continue
		}
		found = true
		for _, c := range pkg.Children(int32(id)) {
			cn := pkg.Node(c)
			if cn == nil || cn.Kind != hir.KResult {
				continue
			}
			if rt := pkg.Type(cn.Type); strings.HasPrefix(rt, "?") {
				t.Errorf("HIR KResult %q 型別仍為 option：%q，want payload", pkg.Str(cn.S), rt)
			}
		}
	}
	if !found {
		t.Fatal("HIR 中找不到 _add 的 KFuncDef")
	}
}

// TestOperatorOverloadAnalysisKeepsInfix 驗證 analysisOnly（推斷/fmt/LSP）
// 路徑不改寫 AST：`a + b` 必須仍是 InfixExpression。
func TestOperatorOverloadAnalysisKeepsInfix(t *testing.T) {
	p := New(lexer.New(olSrc))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	p.prepareInferredTypes(prog)
	ls := findTopLet(t, prog, "r")
	if _, ok := ls.Value.(*InfixExpression); !ok {
		t.Fatalf("analysisOnly 路徑改寫了 AST，got %T，want *InfixExpression", ls.Value)
	}
}
