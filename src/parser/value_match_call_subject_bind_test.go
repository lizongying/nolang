package parser

// value_match_call_subject_bind_test.go — 值位 match 的主語是**方法**呼叫時，
// ok 臂必須真的拿到合成的 `let it`（而不只是「it 有型別」）。
//
// 兩個獨立缺陷會讓 `it` 在臂內完全沒有綁定，觀測結果是 0：
//
//	缺陷 A（H0 scratch parser 的簽名表）
//	  scratchParser 用 fd.Results 重建 funcSignatures，而方法定義的
//	  Results[0] 是 parser 插入的隱含接收者 ⇒ `box.to-i` 得到
//	  ["box", "?i64"]，returnTypeFromCallExpr 的 `len(rets) == 1` 守衛整條
//	  略過。正式註冊路徑（decl.go）用的是 DeclaredResults + IsMethodDef
//	  分流，兩邊不同口徑。
//
//	缺陷 B（LLVM 路的 callee 形態）
//	  merged program 在 H0 之前已把 `b.to-i()` 改寫成 `box.to-i(b)`——callee
//	  是**帶點的 Identifier**。returnTypeFromCallExpr 的 Identifier 分支只
//	  查 funcSignatures，方法簽名在 methodSignatures 裡 ⇒ 依然落空。
//
// 兩者都讓主語的 `?T` 回傳型別推不出 ⇒ 值位 match 主語不歸一化成
// `__match_subj` 臨時變數 ⇒ 臂內沒有 `let it = <subj>` ⇒ MIR 的 KLet 落到
// 「無初始化式」分支，把 `it` 零填成 OpConst 0，臂體就讀到 0。
//
// 目標：HIR 必須同時出現歸一化 wrapper（`__match_subj_*`）與 ok 臂的
// `let it t=i64`。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// 使用者檔案形態：方法在前、值位 match 在後（與 q.no / str-to-f64 同構）。
const valueMatchMethodSubjectSrc = `box {
    n i64
}

box.to-i = () (r ?i64) {
    r = .n
}

m1 = (b box) (v i64) {
    bv = b.to-i(): {
        ok -> 111

        -> 999
    }
    v = bv
}

m2 = (b box) (v i64) {
    bv = b.to-i(): {
        ok -> it

        -> 999
    }
    v = bv
}

m3 = (b box) (v i64) {
    o ?i64 = b.to-i()
    r = o: {
        ok -> it

        -> 999
    }
    v = r
}`

// TestValueMatchMethodSubjectBindsIt 是主回歸門禁：主語是方法呼叫的 ok 臂
// 必須有 `let it`，且主語要先歸一化成 `__match_subj_*` 臨時變數（保證恰好
// 求值一次，也讓 `it` 的綁定有識別符可依）。
func TestValueMatchMethodSubjectBindsIt(t *testing.T) {
	dump := dumpHIR(t, valueMatchMethodSubjectSrc)

	if !strings.Contains(dump, "let s=__match_subj_") {
		t.Fatalf("method-call subject was not normalised into a `__match_subj_*` temp; "+
			"without it the ok arm has no `it` binding at all.\n--- HIR ---\n%s", dump)
	}
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("ok-arm `it` binding is missing or untyped; the arm body then reads 0.\n--- HIR ---\n%s", dump)
	}
	// 對照組（m3：主語是識別符）本來就必須有 `let it` —— 若它也消失，說明
	// 斷言命中的不是本缺陷修好的那條路徑，門禁就是空的。
	if n := strings.Count(dump, "let s=it t=i64"); n < 3 {
		t.Fatalf("expected an i64 `it` binding in every ok arm (m2 value-match + m3 "+
			"identifier match + the dot-val arm copy), got %d.\n--- HIR ---\n%s", n, dump)
	}
}

// TestReturnTypeFromCallExprResolvesDesugaredMethodCall 釘住缺陷 B：
// 接收者糖降糖後 callee 是帶點的 Identifier，方法簽名在 methodSignatures。
func TestReturnTypeFromCallExprResolvesDesugaredMethodCall(t *testing.T) {
	p := New(lexer.New(""))
	p.methodSignatures = map[string][]string{"box.to-i": []string{"?i64"}}
	// 對照：同名的裸函數鍵（無點）不得被當成方法查詢，也不得在兩值結果時
	// 冒充「單回傳」。
	p.funcSignatures = map[string][]string{"plain": {"i64", "str"}}

	call := &CallExpression{
		Function: &Identifier{Value: "box.to-i"},
		Arguments: []Expression{
			&Identifier{Value: "b"},
		},
	}
	if got := p.returnTypeFromCallExpr(call); got != "?i64" {
		t.Fatalf("desugared method call `box.to-i(b)` should resolve to ?i64, got %q", got)
	}

	// 雙回傳的普通函數仍必須拒絕（不得把 2 個結果讀成「接收者+結果」）。
	plain := &CallExpression{Function: &Identifier{Value: "plain"}}
	if got := p.returnTypeFromCallExpr(plain); got != "" {
		t.Fatalf("a two-result plain function must not be read as a method with a "+
			"receiver, got %q", got)
	}

	// 未知的帶點名字（沒有簽名）不得誤命中。
	unknown := &CallExpression{Function: &Identifier{Value: "nope.missing"}}
	if got := p.returnTypeFromCallExpr(unknown); got != "" {
		t.Fatalf("an unknown dotted callee must not resolve, got %q", got)
	}
}

// TestScratchParserStripsImplicitSelfFromMethodSigs 釘住缺陷 A：H0 的
// scratch parser 重建簽名表時必須與 decl.go 同口徑——方法進 methodSignatures
// 且剔除隱含接收者。
func TestScratchParserStripsImplicitSelfFromMethodSigs(t *testing.T) {
	p := New(lexer.New(valueMatchMethodSubjectSrc))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	c := &hirConv{prog: prog, sem: prog.Sem}
	sp := c.scratchParser()

	if got := sp.methodSignatures["box.to-i"]; len(got) != 1 || got[0] != "?i64" {
		t.Fatalf("methodSignatures[box.to-i] = %v, want [?i64] (implicit `self` result "+
			"must be stripped, mirroring decl.go)", got)
	}
	for k, v := range sp.funcSignatures {
		if k == "box.to-i" {
			t.Fatalf("funcSignatures must not carry method `box.to-i` (=%v); methods are "+
				"routed to methodSignatures by IsMethodDef", v)
		}
	}
}
