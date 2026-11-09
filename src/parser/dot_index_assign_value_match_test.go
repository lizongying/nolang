package parser

// dot_index_assign_value_match_test.go — 欄位／索引目標賦值的右值也是值位置。
//
// 使用者語法（2026-11-09）：
//
//	b.n = b.n + 1: { ok -> it  -> }
//
// 期望 `: {` 綁在**右值**上，即 `b.n = (b.n + 1: { … })`；舊實作綁在整個
// 賦值外層，解析成 `match(主語 = (b.n = b.n + 1))`，條件變成
// `(b.n = b.n + 1) == ok` —— 欄位永遠拿不到臂值，且賦值語義被挪進 match 主語。
//
// 成因是不對稱：`x = …`（識別字 LHS）走 parseLetStatement，右值在 CTX_EXPR
// 下解析；而 `b.n = …` / `c[i] = …`（Dot / Index LHS）由 parseExpression 尾端
// 自行解析右值，沒推 CTX_EXPR。pratt 中綴迴圈之後那個「任意表達式主語」的
// `: {` 附加塊以 CTX_EXPR 為閘門（識別字／索引／呼叫主語另有不帶閘門的分支，
// 所以不受影響），於是只有算術主語會歪。
//
// 判別力：把 expr.go 的 parseAssignValueExpr 改回 parseExpression(LOWEST)
// （不推 CTX_EXPR），本檔三個形狀測試全紅、HIR 測試也會因 `if` 出現在
// `assign` 之前而紅。

import (
	"strings"
	"testing"
)

const dotAssignValueMatchSrc = `f = (b box) (out i64) {
    b.n = b.n + 1: {
        ok -> it

        -> 999
    }
    out = b.n
}
`

const indexAssignValueMatchSrc = `f = (a i64) (out i64) {
    c = [1, 2, 3]
    c[0] = c[1] + a: {
        ok -> it

        -> 999
    }
    out = c[0]
}
`

const dotUnwrapAssignValueMatchSrc = `f = (b box) (out i64) {
    b.n ?= b.n + 1: {
        ok -> it

        -> 999
    }
    out = b.n
}
`

// assignStmtValue 回傳函式主體中第 idx 條陳述的右值（Let / Assign / UnwrapAssign
// 三種容器都認），並要求它就是 SurfaceMatch、主語是 InfixExpression。
func assignStmtValue(t *testing.T, src string, idx int) *SurfaceMatch {
	t.Helper()
	prog, errs := parseSugar(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	stmts := bodyOf(t, prog)
	if len(stmts) <= idx {
		t.Fatalf("want at least %d body statements, got %d", idx+1, len(stmts))
	}

	var val Expression
	switch s := stmts[idx].(type) {
	case *LetStatement:
		val = s.Value
	case *UnwrapAssignStatement:
		val = s.Value
	case *ExpressionStatement:
		ae, ok := s.Expression.(*AssignExpression)
		if !ok {
			t.Fatalf("body[%d] expression is %T, want *AssignExpression "+
				"(the `: { … }` tail detached from the RHS and became its own match)",
				idx, s.Expression)
		}
		val = ae.Value
	default:
		t.Fatalf("body[%d] is %T, want *ExpressionStatement", idx, stmts[idx])
	}

	sm, ok := val.(*SurfaceMatch)
	if !ok {
		t.Fatalf("body[%d] right-hand side is %T, want *SurfaceMatch — "+
			"`: { … }` must bind to the RHS, not to the whole assignment", idx, val)
	}
	if _, ok := sm.Matched.(*InfixExpression); !ok {
		t.Fatalf("body[%d] match subject is %T, want *InfixExpression", idx, sm.Matched)
	}
	if len(sm.Arms) != 2 {
		t.Fatalf("body[%d] want 2 arms, got %d", idx, len(sm.Arms))
	}
	return sm
}

// TestDotAssignValueMatchBindsToRHS 是主形狀門禁：欄位 LHS。
func TestDotAssignValueMatchBindsToRHS(t *testing.T) {
	assignStmtValue(t, dotAssignValueMatchSrc, 0)
}

// TestIndexAssignValueMatchBindsToRHS 覆蓋索引 LHS（另一條 LHS 分支，同樣漏推）。
//
// 注意：checker 的 D9 規則本來就**禁止**下標直寫（`a[i] = v` 要寫成 place-match
// 或 `?=`），所以這個形狀在 checker 之後到不了 codegen。此處只釘 AST 形狀——
// 它決定 `no fmt` 能否原樣回寫、以及 idxwrite 診斷裡渲染出的右值是不是
// `<*parser.SurfaceMatch>` 這種 Go 型別名。
func TestIndexAssignValueMatchBindsToRHS(t *testing.T) {
	assignStmtValue(t, indexAssignValueMatchSrc, 1)
}

// TestDotUnwrapAssignValueMatchBindsToRHS 覆蓋欄位 `?=`：解箱對象必須是整段
// 值位 match 的結果，而不是「先賦值再解箱」。
func TestDotUnwrapAssignValueMatchBindsToRHS(t *testing.T) {
	assignStmtValue(t, dotUnwrapAssignValueMatchSrc, 0)
}

// TestDotAssignValueMatchHIRShape：HIR 必須是「先算 match、再把結果寫進欄位」，
// 即 assign 在 if 之前，而不是 if 套在 assign 外面（舊形狀的條件是
// `(b.n = b.n + 1) == ok`）。
func TestDotAssignValueMatchHIRShape(t *testing.T) {
	dump := dumpHIR(t, dotAssignValueMatchSrc)
	iAssign := strings.Index(dump, "assign @")
	iIf := strings.Index(dump, "if @")
	if iAssign < 0 || iIf < 0 {
		t.Fatalf("HIR lost the assignment or the match chain\n--- HIR ---\n%s", dump)
	}
	if iIf < iAssign {
		t.Fatalf("the match is still wrapped around the assignment (`(b.n = b.n + 1) == ok`); "+
			"the assignment must consume the match as its value\n--- HIR ---\n%s", dump)
	}
}

// TestDotAssignIdentifierSubjectStillValueMatch 是對照組：識別字主語本來就
// 走不帶閘門的附加分支，修復不得改變它。
func TestDotAssignIdentifierSubjectStillValueMatch(t *testing.T) {
	src := `f = (b box, k ?i64) (out i64) {
    b.n = k: {
        ok -> it

        -> 999
    }
    out = b.n
}
`
	prog, errs := parseSugar(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	stmts := bodyOf(t, prog)
	if len(stmts) != 2 {
		t.Fatalf("want 2 body statements, got %d", len(stmts))
	}
	es, ok := stmts[0].(*ExpressionStatement)
	if !ok {
		t.Fatalf("body[0] is %T, want *ExpressionStatement", stmts[0])
	}
	ae, ok := es.Expression.(*AssignExpression)
	if !ok {
		t.Fatalf("body[0] expression is %T, want *AssignExpression", es.Expression)
	}
	sm, ok := ae.Value.(*SurfaceMatch)
	if !ok {
		t.Fatalf("assign value is %T, want *SurfaceMatch", ae.Value)
	}
	if _, ok := sm.Matched.(*Identifier); !ok {
		t.Fatalf("match subject is %T, want *Identifier", sm.Matched)
	}
}
