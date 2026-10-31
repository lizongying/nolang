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
//  2. 運算符重載方法的結果參數書寫為 `?T`，簽名表 /
//     HIR KResult 一律**保留** `?T`（option ABI），呼叫端因而能 match 出
//     ok／err／nil 三分支；`?` 只在「巢狀運算子的接收者位置」
//     （resolveReceiverType）剝成 payload T，鏈式派發的查表鍵不受影響。
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

// TestOperatorOverloadSignatureKeepsOption 驗證運算符重載方法的簽名表保留
// 書寫的 `?T`（design B：結果型別在呼叫端是真實 option，可 match 三分支）。
// 注意存放去向的既有約定（見 semviews.go rebuildLocalSignatures 註釋）：
// `=`-形式方法（bigint._add = ...）落 funcSignatures[全名]，冒號形式才落
// methodSignatures[全名]。查表端（returnTypeFromCallExpr）兩張表都查，
// 這裡按形式斷言實際存放處。
func TestOperatorOverloadSignatureKeepsOption(t *testing.T) {
	p, _ := parseOLProg(t, olSrc)
	rets, ok := p.funcSignatures["bigint._add"]
	if !ok || len(rets) != 1 {
		t.Fatalf("funcSignatures[bigint._add] = %v, want [?T]", rets)
	}
	if rets[0] != "?bigint" {
		t.Fatalf("簽名結果型別 = %q, want %q", rets[0], "?bigint")
	}
	// 派發入口回報書寫型別原樣（binding 型別由 stmt.go 用它推斷）。
	inf := &InfixExpression{Operator: "+", Left: &Identifier{Value: "a"}, Right: &Identifier{Value: "b"}}
	if rt := p.OperatorOverloadReturnType(inf); rt != "?bigint" {
		t.Fatalf("OperatorOverloadReturnType = %q, want %q", rt, "?bigint")
	}
	// 唯一的剝 `?` 點：巢狀運算子的「接收者位置」——鏈式外層以 payload 型別
	// 作鍵查簽名，否則 `?bigint._add` 查不到而放棄改寫。
	inner := &CallExpression{
		Token: inf.Token,
		Function: &DotExpression{
			Token:    inf.Token,
			Receiver: &Identifier{Value: "a"},
			Property: "_add",
		},
		Arguments: []Expression{&Identifier{Value: "b"}},
	}
	if got := p.resolveReceiverType(inner); got != "bigint" {
		t.Fatalf("resolveReceiverType(呼叫接收者) = %q, want %q（僅此處剝 ?）", got, "bigint")
	}
}

// TestOperatorOverloadHirResultKeepsOption 驗證 HIR 層 KResult 型別保留
// `?bigint`（option ABI）—— callee 結果槽為 %option*，呼叫端才拿得到
// tag，`m: { ok -> … ; err -> … ; nil -> … }` 才可能成立。
// 代價：鏈式中間值經過 heap 包裝，生命週期需另行處理（design B 已知風險）。
func TestOperatorOverloadHirResultKeepsOption(t *testing.T) {
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
		var optionResults int
		for _, c := range pkg.Children(int32(id)) {
			cn := pkg.Node(c)
			if cn == nil || cn.Kind != hir.KResult {
				continue
			}
			if rt := pkg.Type(cn.Type); strings.HasPrefix(rt, "?") {
				optionResults++
			}
		}
		if optionResults == 0 {
			t.Errorf("HIR 中 _add 的結果 KResult 未保留 option 型別，want >=1")
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

// TestOperatorOverloadReassignKeepsDeclaredPayload 驗證「重新賦值給已宣告為
// payload（非 option）的變數」不會把該變數的型別表登記退成 `?T`。
//
// 背景（n1 design B）：運算符重載結果保留 `?T` 之後，`x = x._add(y)` 的 RHS
// 推斷型別是 `?bigint`。parseLetStatement 的 CallExpression 分支對 `?` 前綴
// 一律覆寫型別表（那是 option match 型別窄化需要的規則），於是宣告為
// `x bigint` 的累加器被打成 `?bigint`，後續 `x * 256` 的接收者查表落到
// 預設 i64 → 運算符不改寫 → MIR 產生 bogus `i64._add`
// （實測 src/std/crypto/chacha20-poly1305.no poly1305 的 `c`）。
//
// 不變量：**已宣告非 option** 的變數，被運算符重載結果重新賦值時維持原宣告型別。
// （全新綁定 `hc = h._add(c)` 仍保留 `?T`，那是 design B 的語義。）
func TestOperatorOverloadReassignKeepsDeclaredPayload(t *testing.T) {
	src := `
bigint {
    sign i64
}

bigint._add = (b bigint) (c ?bigint) {
    c = b
}

f = () (out bigint) {
    x bigint = bigint{}
    y bigint = bigint{}
    x = x._add(y)
    out = x
}
`
	p, _ := parseOLProg(t, src)
	got, ok := p.sem.FuncVarType("f", "x")
	if !ok || got != "bigint" {
		t.Fatalf(`已宣告 'x bigint' 被運算符結果重新賦值後型別 = %q (ok=%v)，want "bigint"`, got, ok)
	}
}

// TestOperatorOverloadResultParamKeepsOptionInBody 驗證運算符重載方法「方法體內」
// 的結果名也登記為書寫的 `?T`（不剝）——與簽名表、HIR KResult 同一口徑，callee 的
// option ABI 才自洽：結果名照常是 option 槽，body 末的返回走 option 包裝。
// 舊實作在此剝 `?` 換取純 payload 結果槽，代價是呼叫端拿不到 tag（n1）。
func TestOperatorOverloadResultParamKeepsOptionInBody(t *testing.T) {
	p, _ := parseOLProg(t, olSrc)
	got, ok := p.sem.FuncVarType("bigint._add", "c")
	if !ok || got != "?bigint" {
		t.Fatalf(`方法體內結果參數 'c' 型別 = %q (ok=%v)，want "?bigint"`, got, ok)
	}
}
