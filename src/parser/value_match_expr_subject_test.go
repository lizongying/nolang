package parser

// value_match_expr_subject_test.go — 值位 match 的主語放寬到任意表達式。
//
// 使用者語法（2026-11-08）：
//
//	r = a % b: { ok -> it  -> 0 }
//
// `a % b: { … }` 的優先級必須**大於** `r =`，也就是 `: {` 要附著在 RHS
// 上成為 SurfaceMatch，而不是讓 `r = a % b` 先結成一條陳述、後面再跟一條
// 主體為空的裸 match（那時 `-> 0` 臂形同不存在，且 r 仍是被硬報
// 15w45dqk 的 ?i64）。
//
// 舊實作的 `: {` 附加點全在 Pratt 中綴迴圈**之前**（IDENT 分支、
// Index/Call 分支），中綴運算結果要等迴圈跑完才成形 ⇒ 必然漏掉。
// 判別力：關掉 expr.go 中綴迴圈之後那段附加，本檔第一、二個測試立刻
// 失敗（LetStatement.Value 會是 *InfixExpression，且 main body 會多一條
// ExpressionStatement）；第三個測試（HIR）會因為沒有 __match_subj
// wrapper、it 無型別而失敗。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

const valueMatchExprSubjectSrc = `f = (a i64, b i64) (out i64) {
    out = a % b: {
        ok -> it

        -> 0
    }
}
`

const valueMatchExprSubjectTypedSrc = `f = (a i64, b i64) (out i64) {
    r i64 = a % b: {
        ok -> it

        -> 0
    }
    out = r
}
`

// 連續兩段值位 match：第一段結果變數會被第二段拿去算 option 算術。
// 若第一段結果沒登記型別，第二段 `let it` 無型別、MIR 零填，結果恆為 0。
const valueMatchExprSubjectChainSrc = `two = (a i64, b i64) (out i64) {
    x-opt = a % b
    x = x-opt: {
        ok -> it

        -> 0
    }
    y = x % b: {
        ok -> it

        -> 0
    }
    out = y
}
`

// 運算元型別「只能從 AST 顯式標註取得」的一段式值位 match。
// src/std/number.no 的 f64.to-str-general 就是這個形狀：`e` 的來源是深層
// 方法鏈（ax.log10 → lg10.floor → flr.to-i64），parser 的 AST 回放推不出
// 接收者型別、語義副表也沒有 e 的條目。少了 `e i64 = …` 的標註，
// `precision - 1 - e: { … }` 就不會歸一化成 `__match_subj ?i64`，ok 臂
// 沒有 `let it` ⇒ MIR 零填 ⇒ dec-places 恆為 0 ⇒ fmt.fmt-f64(2.5, '')
// 從 '2.5' 退化成 '3'（test/std/fmt.no 的 fmt-f64 default）。
const valueMatchExprSubjectASTAnnotatedSrc = `f = (precision i64) (out i64) {
    e i64 = flr.to-i64()
    dec-places i64 = precision - 1 - e: {
        ok -> it

        -> 0
    }
    out = dec-places
}
`

// exprSubjectRHS 斷言函式主體第一條 let 的右值就是 SurfaceMatch，
// 且主語是 InfixExpression（不是裸識別字／索引／呼叫）。
func exprSubjectRHS(t *testing.T, src string) *SurfaceMatch {
	t.Helper()
	prog, errs := parseSugar(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	stmts := bodyOf(t, prog)
	if len(stmts) == 0 {
		t.Fatal("empty function body")
	}
	ls, ok := stmts[0].(*LetStatement)
	if !ok {
		t.Fatalf("body[0] is %T, want *LetStatement", stmts[0])
	}
	sm, ok := ls.Value.(*SurfaceMatch)
	if !ok {
		t.Fatalf("let value is %T, want *SurfaceMatch — the `: { … }` tail detached from the RHS", ls.Value)
	}
	if _, ok := sm.Matched.(*InfixExpression); !ok {
		t.Fatalf("match subject is %T, want *InfixExpression", sm.Matched)
	}
	if len(sm.Arms) != 2 {
		t.Fatalf("want 2 arms, got %d", len(sm.Arms))
	}
	return sm
}

// TestValueMatchExprSubjectIsAssignmentRHS 是主形狀門禁。
func TestValueMatchExprSubjectIsAssignmentRHS(t *testing.T) {
	exprSubjectRHS(t, valueMatchExprSubjectSrc)
}

// TestValueMatchExprSubjectTypedFirstAssign 覆蓋使用者點名的「首次賦值可帶
// 顯式型別」：`r i64 = a % b: { … }` 同樣必須是單一值位 match。
func TestValueMatchExprSubjectTypedFirstAssign(t *testing.T) {
	prog, errs := parseSugar(t, valueMatchExprSubjectTypedSrc)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	stmts := bodyOf(t, prog)
	if len(stmts) != 2 {
		t.Fatalf("want 2 body statements (match-let + out), got %d", len(stmts))
	}
	ls, ok := stmts[0].(*LetStatement)
	if !ok {
		t.Fatalf("body[0] is %T, want *LetStatement", stmts[0])
	}
	if ls.Type == nil {
		t.Fatal("explicit `i64` annotation was lost")
	}
	if _, ok := ls.Value.(*SurfaceMatch); !ok {
		t.Fatalf("typed let value is %T, want *SurfaceMatch", ls.Value)
	}
}

// TestValueMatchExprSubjectKeepsBlockMatch 保證既有 `{r ?= a % b}: { … }`
// 形態沒有被新的附加點吃掉。
func TestValueMatchExprSubjectKeepsBlockMatch(t *testing.T) {
	src := `f = (a i64, b i64) (out i64) {
    {r ?= a % b}: {
        ok -> it

        -> 0
    }
    out = r
}
`
	prog, errs := parseSugar(t, src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	stmts := bodyOf(t, prog)
	if len(stmts) < 1 {
		t.Fatal("empty function body")
	}
	if _, ok := stmts[0].(*BlockMatchStatement); !ok {
		t.Fatalf("body[0] is %T, want *BlockMatchStatement", stmts[0])
	}
}

// TestValueMatchExprSubjectHIRBindsIt：option 算術主語必須歸一化成
// `__match_subj_* ?i64`，ok 臂的 `let it` 必須帶型別；連續兩段時
// 兩段的 it 都要有型別（第二段丟 payload 是本次修掉的另一個缺陷）。
func TestValueMatchExprSubjectHIRBindsIt(t *testing.T) {
	dump := dumpHIR(t, valueMatchExprSubjectSrc)
	if !strings.Contains(dump, "let s=__match_subj_") {
		t.Fatalf("infix subject was not normalised into a `__match_subj_*` temp; "+
			"the ok arm would have no `it` binding.\n--- HIR ---\n%s", dump)
	}
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("ok arm `it` binding has no i64 type; MIR will zero-fill it.\n--- HIR ---\n%s", dump)
	}

	chain := dumpHIR(t, valueMatchExprSubjectChainSrc)
	if got := strings.Count(chain, "let s=it t=i64"); got < 2 {
		t.Fatalf("want both value matches to carry a typed `it`, got %d typed bindings.\n--- HIR ---\n%s", got, chain)
	}
}

// TestValueMatchExprSubjectASTAnnotatedOperand 釘住「顯式標註是深層方法鏈
// 運算元唯一的型別來源」這條邊界。判別力：把來源常數裡的 `e i64 = …`
// 改成 `e = …`（標註拿掉、簽名表又查不到 f64.to-i64），本測試會因
// HIR 缺少 `__match_subj`/typed `it` 而失敗 —— 正是 fmt-f64 default 的成因。
func TestValueMatchExprSubjectASTAnnotatedOperand(t *testing.T) {
	dump := dumpHIR(t, valueMatchExprSubjectASTAnnotatedSrc)
	if !strings.Contains(dump, "let s=__match_subj_") {
		t.Fatalf("expression subject whose operand type comes only from an AST "+
			"annotation must still be normalised.\n--- HIR ---\n%s", dump)
	}
	if !strings.Contains(dump, "let s=it t=i64") {
		t.Fatalf("ok arm `it` binding has no i64 type; MIR will zero-fill it.\n--- HIR ---\n%s", dump)
	}
}

// TestOptionArithSubjectOperandIntFallsBackToAST 釘住 std/markdown.no 折疊後
// 逾時的根因：`close = s.find-byte-from(...)` 這類由呼叫回傳宣告的區域變數，
// 在語義副表可能被全域同名殘值／option 捕捉升級成 `?i64`。舊實作只在副表
// 「查無」時才走 AST 回放，於是被非空的 `?i64` 卡死 ⇒ option 算術主語推不出
// `?i64` ⇒ ok 臂的 `it` 沒綁定 ⇒ 迴圈索引恆為 0 ⇒ markdown.inline 無窮迴圈。
//
// 判別力：把 optionArithSubjectOperandInt 還原成「只有 typ == \"\" 才回退」，
// 本測試第一個斷言立刻失敗。
func TestOptionArithSubjectOperandIntFallsBackToAST(t *testing.T) {
	src := `f = (a i64) (out i64) {
    close = g(a)
    label = h(a)
    out = close + 1
}
`
	p := New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	p.prog = prog
	p.curFuncName = "f"
	// 呼叫簽名在 AST 回放路徑上用；副表刻意塞入「不是有號整數」的舊值。
	p.funcSignatures = map[string][]string{
		"g": {"i64"},
		"h": {"str"},
	}
	p.sem.SetFuncVarType("f", "close", "?i64")
	p.sem.SetFuncVarType("f", "label", "?i64")

	if got, ok := p.optionArithSubjectOperandInt(&Identifier{Value: "close"}); !ok || got != "i64" {
		t.Fatalf("call-declared int operand must fall back to the AST declaration (got %q, ok=%v)", got, ok)
	}
	// 對照組：回退不是「放行一切」，真正的非整數宣告仍須拒絕。
	if got, ok := p.optionArithSubjectOperandInt(&Identifier{Value: "label"}); ok || got != "" {
		t.Fatalf("a str-typed operand must still be rejected (got %q, ok=%v)", got, ok)
	}
}
