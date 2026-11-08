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

// lowerUnwrapAssignForTest drives ONE `?=` through the real desugarer and
// returns the resulting block's lets.
//
// The default pipeline (lowerProgram, skipSugarExpand=true) deliberately does
// NOT write the expansion back into the AST for `Name != nil` targets — H0
// (tohir.go lowerUnwrapAssignToHIR) calls the same lowerUnwrapAssign on the
// spot. So inspecting ParseProgram()'s AST cannot see `__unwrap_*`; tests must
// call the desugarer directly.
func lowerUnwrapAssignForTest(t *testing.T, src string) ([]*LetStatement, []string) {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	var fd *FunctionDefinition
	for _, s := range prog.Statements {
		if f, ok := s.(*FunctionDefinition); ok {
			fd = f
			break
		}
	}
	if fd == nil || fd.Body == nil {
		t.Fatalf("no function definition in source")
	}
	var uas *UnwrapAssignStatement
	for _, s := range fd.Body.Statements {
		if u, ok := s.(*UnwrapAssignStatement); ok {
			uas = u
			break
		}
	}
	if uas == nil {
		t.Fatalf("no UnwrapAssignStatement in function body")
	}
	p.curFuncName = fd.Name
	l := &lowerer{
		p:              p,
		visited:        map[uintptr]bool{},
		declaredLocals: map[string]bool{},
		curFuncDef:     fd,
		curFuncName:    fd.Name,
	}
	stmt := l.lowerUnwrapAssign(uas)
	if stmt == nil {
		return nil, p.Errors()
	}
	blk, ok := stmt.(*BlockStatement)
	if !ok {
		t.Fatalf("lowerUnwrapAssign returned %T, want *BlockStatement", stmt)
	}
	var lets []*LetStatement
	collectLets(blk.Statements, &lets)
	return lets, p.Errors()
}

func findUnwrapLet(lets []*LetStatement) *LetStatement {
	for _, ls := range lets {
		if ls.Name != nil && strings.HasPrefix(ls.Name.Value, "__unwrap_") {
			return ls
		}
	}
	return nil
}

// REGRESSION GUARD (2026-11-07): `v ?= .field[i]` (read side) must be accepted.
//
// lowerUnwrapAssign used to reject every RHS *IndexExpression whose Left was not
// a bare *Identifier with `gcvwum3g` ("safe-index ?= only supports direct
// array/vec variables, not struct fields"). That was already inconsistent with
// the rest of the language: isSafeIndexBase / indexBaseContainerRaw resolve
// DotExpression bases, and MIR lowers them as safe indexes. The guard is gone;
// the receiver type is now pushed for the whole `?=` desugar (not just the
// write-side branch), so `.field[i]` resolves inside a method.
func TestUnwrapAssignRHSFieldIndexSupported(t *testing.T) {
	src := `idx-rhs-field {
    keys [4]i64
}

idx-rhs-field.get = (i i64) (r ?i64) {
    v i64 = 0
    v ?= .keys[i]
    r = v
}`
	lets, errs := lowerUnwrapAssignForTest(t, src)
	if len(errs) > 0 {
		t.Fatalf("`v ?= .keys[i]` rejected: %v", errs)
	}
	unwrapLet := findUnwrapLet(lets)
	if unwrapLet == nil {
		t.Fatalf("no __unwrap_* binding emitted — `?=` did not desugar for a struct-field index RHS")
	}
	if _, ok := unwrapLet.Value.(*IndexExpression); !ok {
		t.Fatalf("__unwrap_* value is %T, want *IndexExpression", unwrapLet.Value)
	}
	nt, ok := unwrapLet.Type.(*NullableType)
	if !ok {
		t.Fatalf("__unwrap_* type is %T, want *NullableType", unwrapLet.Type)
	}
	if got := typeString(nt.Type); got != "i64" {
		t.Fatalf("__unwrap_* inner type is %q, want i64", got)
	}
}

// REGRESSION GUARD (2026-11-07): `h ?= .field[i].to-lower()` must propagate
// the option from the CHAIN ROOT index, then apply the method to `it` — with
// no `h str = ''` pre-declaration. SplitIndexReceiverChain splits the chain;
// lowerUnwrapAssign uses the root as the option RHS and rebuilds the ok arm as
// `it.to-lower()`.
func TestUnwrapAssignIndexReceiverChain(t *testing.T) {
	src := `idx-chain {
    names [4]str
}

idx-chain.get = (i i64) (r ?str) {
    h ?= .names[i].to-lower()
    r = h
}`
	lets, errs := lowerUnwrapAssignForTest(t, src)
	if len(errs) > 0 {
		t.Fatalf("`h ?= .names[i].to-lower()` rejected: %v", errs)
	}
	unwrapLet := findUnwrapLet(lets)
	if unwrapLet == nil {
		t.Fatalf("no __unwrap_* binding emitted")
	}
	if _, ok := unwrapLet.Value.(*IndexExpression); !ok {
		t.Fatalf("__unwrap_* value is %T, want the chain ROOT *IndexExpression", unwrapLet.Value)
	}
	nt, ok := unwrapLet.Type.(*NullableType)
	if !ok {
		t.Fatalf("__unwrap_* type is %T, want *NullableType", unwrapLet.Type)
	}
	if got := typeString(nt.Type); got != "str" {
		t.Fatalf("__unwrap_* inner type is %q, want str", got)
	}
	// ok 臂必須是 `h = it.to-lower()`，不是 `h = it`：遍歷降糖結果裡的
	// AssignExpression，找出以 `it.method(...)` 為值的那一條。
	var assigns []*AssignExpression
	collectAssigns(testChainBlock(t, src).Statements, &assigns)
	if len(assigns) == 0 {
		t.Fatalf("no assignment to the ?= target in the desugared block")
	}
	foundOK := false
	for _, ae := range assigns {
		call, isCall := ae.Value.(*CallExpression)
		if !isCall {
			continue
		}
		dot, isDot := call.Function.(*DotExpression)
		if !isDot || dot.Property != "to-lower" {
			continue
		}
		id, isIdent := dot.Receiver.(*Identifier)
		if isIdent && id.Value == "it" {
			foundOK = true
		}
	}
	if !foundOK {
		t.Fatalf("ok arm does not assign `it.to-lower()` — the chain was dropped")
	}
}

// testChainBlock re-runs the desugar and returns the whole block (the
// shared helper only returns its lets, so the arm-shape assertion needs the
// statements too).
func testChainBlock(t *testing.T, src string) *BlockStatement {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	var fd *FunctionDefinition
	for _, s := range prog.Statements {
		if f, ok := s.(*FunctionDefinition); ok {
			fd = f
			break
		}
	}
	var uas *UnwrapAssignStatement
	for _, s := range fd.Body.Statements {
		if u, ok := s.(*UnwrapAssignStatement); ok {
			uas = u
			break
		}
	}
	p.curFuncName = fd.Name
	l := &lowerer{p: p, visited: map[uintptr]bool{}, declaredLocals: map[string]bool{}, curFuncDef: fd, curFuncName: fd.Name}
	blk, _ := l.lowerUnwrapAssign(uas).(*BlockStatement)
	return blk
}

func collectAssigns(stmts []Statement, out *[]*AssignExpression) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *ExpressionStatement:
			if ae, ok := v.Expression.(*AssignExpression); ok {
				*out = append(*out, ae)
			}
			if ie, ok := v.Expression.(*IfExpression); ok {
				if ie.Consequence != nil {
					collectAssigns(ie.Consequence.Statements, out)
				}
				if ie.Alternative != nil {
					collectAssigns(ie.Alternative.Statements, out)
				}
			}
		case *BlockStatement:
			collectAssigns(v.Statements, out)
		}
	}
}

// REGRESSION GUARD (2026-11-07): `[N]?T` / `[]?T` element reads yield ONE
// option (?T), not a nested ??T. inferIndexElemType returns the declared
// element text (`?i64`), so the temp used to be typed `??i64` and the ok arm
// read the whole option struct as a scalar (runtime garbage).
func TestUnwrapAssignOptionElemIndexNotNested(t *testing.T) {
	src := `opt-elem-get = (i i64) (r ?i64) {
    a [4]?i64 = [1, nil, 3, nil]
    v i64 = 0
    v ?= a[i]
    r = v
}`
	lets, errs := lowerUnwrapAssignForTest(t, src)
	if len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	unwrapLet := findUnwrapLet(lets)
	if unwrapLet == nil {
		t.Fatalf("no __unwrap_* binding emitted")
	}
	nt, ok := unwrapLet.Type.(*NullableType)
	if !ok {
		t.Fatalf("__unwrap_* type is %T, want *NullableType", unwrapLet.Type)
	}
	if got := typeString(nt.Type); got != "i64" {
		t.Fatalf("__unwrap_* inner type is %q, want i64 (nested option?)", got)
	}
}
