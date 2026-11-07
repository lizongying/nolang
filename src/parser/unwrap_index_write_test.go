package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// collectLets 收集 stmts 及其巢狀區塊 / if 分支裡的所有 *LetStatement。
// 只用於測試斷言（?= 降糖產出的 __idx / __match_subj 綁定都藏在
// `if 1 { … }` 的 Consequence 裡，需要下鑽才看得到）。
func collectLets(stmts []Statement, out *[]*LetStatement) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *LetStatement:
			*out = append(*out, v)
		case *BlockStatement:
			collectLets(v.Statements, out)
		case *FunctionDefinition:
			if v.Body != nil {
				collectLets(v.Body.Statements, out)
			}
		case *ExpressionStatement:
			collectLetsInExpr(v.Expression, out)
		}
	}
}

func collectLetsInExpr(e Expression, out *[]*LetStatement) {
	switch v := e.(type) {
	case *IfExpression:
		if v.Consequence != nil {
			collectLets(v.Consequence.Statements, out)
		}
		if v.Alternative != nil {
			collectLets(v.Alternative.Statements, out)
		}
	}
}

func parseClean(t *testing.T, src string) []Statement {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if prog == nil {
		t.Fatalf("nil program")
	}
	return prog.Statements
}

// findIdxTempLet returns the `let __idx_L_C = …` binding produced by the D9
// place-write desugar, or nil.
func findIdxTempLet(lets []*LetStatement) *LetStatement {
	for _, ls := range lets {
		if ls.Name != nil && strings.HasPrefix(ls.Name.Value, "__idx_") {
			return ls
		}
	}
	return nil
}

// REGRESSION GUARD (2026-11-07): the index temp must be initialised from the
// user's index expression — never from itself.
//
// emitSubjectMatchPlain used to rewrite the index IN PLACE (`idx.Index =
// __idx_L_C`) on the user's *IndexExpression, the same node H0 later re-reads.
// On the second lowering pass the capture of `origIdxExpr` therefore yielded
// the temp itself and the binding became `let __idx_L_C = __idx_L_C` — an
// uninitialised temp used as BOTH the bounds-check index and the write-back
// index, so `a[i] ?= v` compiled and silently wrote nothing (probe x8 printed 0
// instead of 99). H0 already builds a fresh node (tohir.go:1922 "H0 must not
// touch the AST"); the AST mirror must do the same.
func TestUnwrapAssignIndexTargetKeepsOriginalIndexExpr(t *testing.T) {
	// 只測「陳述層級的裸 `?=`」：塊 match 內的 `?=` 在 parser 走查時**不展開**
	//（skipSugarExpand 對 BlockMatchStatement 直接 return，見 lowering.go:360），
	// 展開全權交給 H0 的 lowerBlockMatchToHIR，故 parser AST 上沒有 __idx_ 綁定。
	// 該形態由 src/build/unwrap_index_write_test.go 的 IR 斷言端到端守住。
	src := `unwrap-idx-bare = (i i64, v i64) (r ?i64) {
    a [4]i64 = [0, 0, 0, 0]
    a[i] ?= v
    r = 0
}`
	var lets []*LetStatement
	collectLets(parseClean(t, src), &lets)
	idxLet := findIdxTempLet(lets)
	if idxLet == nil {
		t.Fatalf("no __idx_* binding emitted — the place-write desugar did not fire " +
			"(isSafeIndexBase missed the base, so `?=` fell back to the generic path)")
	}
	ident, ok := idxLet.Value.(*Identifier)
	if !ok {
		t.Fatalf("__idx_* binding value is %T, want *Identifier", idxLet.Value)
	}
	if ident.Value == idxLet.Name.Value {
		t.Fatalf("self-referential index temp `%s = %s`: the desugar mutated the subject "+
			"index instead of building a fresh node", idxLet.Name.Value, ident.Value)
	}
	if ident.Value != "i" {
		t.Fatalf("__idx_* binding value is %q, want the original index `i`", ident.Value)
	}
}

// REGRESSION GUARD (2026-11-07): `.field[i] ?= v` inside a method must be
// recognised as a safe-index write target.
//
// Parser.methodStructStack is pushed/popped inside parseFunctionBody, so by the
// time the post-parse lowering walk runs the stack is empty and
// indexBaseContainerRaw could not resolve the implicit `self` receiver. `?=`
// then fell through to the generic branch, whose rhsDefinitelyNonOption check
// rejected the (perfectly plain) RHS with `5l952ztl` — "`?=` RHS is not an
// option — use `=` instead", which is actively misleading. The walk now pushes
// the receiver type derived from the synthesised `self` result param, exactly
// like H0's scratchParser does.
func TestUnwrapAssignIndexTargetMethodFieldRecognised(t *testing.T) {
	src := `idx-field { 
    keys [4]i64
}

idx-field.set = (i i64, v i64) (r ?i64) {
    .keys[i] ?= v
    r = 0
}`
	var lets []*LetStatement
	collectLets(parseClean(t, src), &lets)
	if findIdxTempLet(lets) == nil {
		t.Fatalf("`.keys[i] ?= v` inside a method produced no __idx_* binding — " +
			"the receiver field was not resolved as a container (methodStructStack empty during lowering?)")
	}
}
