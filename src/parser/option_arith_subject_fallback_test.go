package parser

// option_arith_subject_fallback_test.go — 值位 match 的 option 算術主語在
// 「語義副表缺條目」時仍必須推出 ?T（2026-11-09）。
//
// 使用者指定的寫法：
//
//	zn = zn + 1: { ok -> it  -> }
//
// `zn + 1` 本身就是 `?i64`，不需要中繼變數、也不需要 `?i64` 標註。但 std
// 烘焙成 AST blob 後，函式內以欄位／索引／呼叫建立的局部變數常沒有副表
// 條目；舊的 inferOptionArithSubjectElem 要求兩側運算元都查得到有號整數，
// 整段放棄 __match_subj 歸一化 ⇒ ok 臂的 it 沒有型別、值語境空臂又因
// elemType 未知而報 hcdpwnpp。
//
// 判別力：把 inferOptionArithSubjectElem 尾端的 partialArithElem 兜底改回
// `return ""`，本檔前兩個測試立刻失敗（回 "" 就不會歸一化）；第三個對照組
// 則保證「回退不等於放行一切」——明確宣告成 str 的運算元仍須否決。
// 第四／五個測試針對一元負號：deque 的 `.cap - ((-idx) % .cap): { … }`
// 過去因 PrefixExpression 不在推斷鏈而報 s8gh6opt；對照組確保前綴不是
// 無條件放行（str 運算元仍要否決）。

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// newArithSubjectProbeParser 建立一個保留 SurfaceMatch、且已設好函式作用域的
// Parser；不跑 H0，只讓待測函式查得到 p.prog / p.curFuncName。
func newArithSubjectProbeParser(t *testing.T, src string) *Parser {
	t.Helper()
	p := New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	p.prog = prog
	p.curFuncName = "f"
	return p
}

// firstValueMatchSubject 取出函式主體裡第一個值位 match 的主語表達式。
func firstValueMatchSubject(t *testing.T, p *Parser) Expression {
	t.Helper()
	for _, st := range p.prog.Statements {
		fd, ok := st.(*FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		for _, bs := range fd.Body.Statements {
			ls, ok := bs.(*LetStatement)
			if !ok {
				continue
			}
			if sm, ok := ls.Value.(*SurfaceMatch); ok {
				return sm.Matched
			}
		}
	}
	t.Fatal("no value-position match found in function body")
	return nil
}

// 兩側運算元都查不到型別（呼叫回傳宣告、副表與 AST 回放都沒有條目）。
// src/std/archive/zip.no 的 `idx = start - i: { … }` 就是這個形狀。
func TestInferOptionArithSubjectElemBothOperandsUnknown(t *testing.T) {
	p := newArithSubjectProbeParser(t, `f = () (out i64) {
    start = g()
    i = h()
    idx = start - i: {
        ok -> it
        ->
    }
    out = idx
}
`)
	subj := firstValueMatchSubject(t, p)
	if got := p.inferOptionArithSubjectElem(subj); got != "i64" {
		t.Fatalf("both operands type-less still yields an integer option subject, got %q, want i64", got)
	}
}

// 混合欄位讀取與整數字面量的深層算術（src/std/time.no 的 date-to-unix）。
func TestInferOptionArithSubjectElemDotOperands(t *testing.T) {
	p := newArithSubjectProbeParser(t, `f = (d i64) (out i64) {
    days = g()
    ts = days * 86400 + d.hour * 3600 + d.min * 60: {
        ok -> it
        ->
    }
    out = ts
}
`)
	subj := firstValueMatchSubject(t, p)
	if got := p.inferOptionArithSubjectElem(subj); got != "i64" {
		t.Fatalf("field-read operands must not veto integer arithmetic, got %q, want i64", got)
	}
}

// 對照組：明確宣告成 str 的運算元不是整數算術；回退不可把它誤當 ?i64。
func TestInferOptionArithSubjectElemRejectsKnownNonInt(t *testing.T) {
	p := newArithSubjectProbeParser(t, `f = (s str) (out str) {
    a = g()
    r = s - a: {
        ok -> it
        ->
    }
    out = r
}
`)
	subj := firstValueMatchSubject(t, p)
	if got := p.inferOptionArithSubjectElem(subj); got != "" {
		t.Fatalf("a str operand must veto the fallback, got %q, want empty", got)
	}
}

// 一元負號包住運算元（src/std/collection/deque.no 的 pop/push 索引正規化）。
// PrefixExpression 過去不在推斷鏈 ⇒ 整棵樹回 "" ⇒ 空臂 s8gh6opt。
func TestInferOptionArithSubjectElemUnaryMinus(t *testing.T) {
	p := newArithSubjectProbeParser(t, `f = (v i64) (out i64) {
    d = g()
    idx = v
    idx = d.cap - ((-idx) % d.cap): {
        ok -> it
        ->
    }
    out = idx
}
`)
	subj := firstValueMatchSubject(t, p)
	if got := p.inferOptionArithSubjectElem(subj); got != "i64" {
		t.Fatalf("unary minus must pass through the integer operand type, got %q, want i64", got)
	}
}

// 對照組：加了一元負號也不能把明確的 str 運算元洗成整數。
func TestInferOptionArithSubjectElemUnaryMinusRejectsNonInt(t *testing.T) {
	p := newArithSubjectProbeParser(t, `f = (s str) (out i64) {
    r = (-s) % 2: {
        ok -> it
        ->
    }
    out = r
}
`)
	subj := firstValueMatchSubject(t, p)
	if got := p.inferOptionArithSubjectElem(subj); got != "" {
		t.Fatalf("unary minus over a str operand must still veto, got %q, want empty", got)
	}
}
