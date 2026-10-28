package mir

import (
	"testing"
)

// opConstType returns the raw MIR type of an OpConst carrying the integer
// `val`, or "" if none exists. It is what lets these tests prove the comptime
// fold preserved the char TYPE — not merely that a constant appeared. A char
// value folded as i64 (the pre-fix behaviour) would show up here as "i64",
// which `.to-str()` then resolves to `i64.to-str` and prints the code point
// number instead of the character.
//
// NOTE on Nolang syntax: a char literal is DOUBLE-quoted ("A"); a single-quoted
// literal ('A') is a (single-character) STRING. The two are unrelated types,
// so the comptime evaluator must read the node's kind (KCharLit) to know which
// constant form to build.
func opConstType(m *Module, val int64) string {
	for i := range m.Insts {
		if m.Insts[i].Op == OpConst && m.Insts[i].Int == val {
			if t := m.Type(m.Insts[i].Type); t != nil {
				return t.Raw
			}
			return "<notype>"
		}
	}
	return ""
}

// TestComptimeFoldsCharResult pins the char type-loss fix. `to-next` returns a
// char and computes it with char arithmetic (`c + 1`). With comptime ON the
// call must fold away and the result must be a CHAR-typed constant (so a later
// `.to-str()` still yields "B", not "66"). With comptime OFF the call survives.
//
// This is the exact case the v1 guards refused: before the fix the evaluator
// modelled char arithmetic as int, folded the call to an i64 constant, and
// `.to-str()` resolved to `i64.to-str` -> "66".
func TestComptimeFoldsCharResult(t *testing.T) {
	src := `to-next = (c char) (r char) {
    r = c + 1
}
main = () () {
    r = to-next("A")
    print(r)
}
`
	on, off := comptimePair(t, src)
	if hasCallSym(on, "to-next") {
		t.Errorf("to-next(\"A\") kept its call instead of folding; syms=%v", callSyms(on))
	}
	if typ := opConstType(on, 66); typ != "char" {
		t.Errorf("folded char result must be emitted as type char, got %q", typ)
	}
	if !hasCallSym(off, "to-next") {
		t.Errorf("to-next(\"A\") was folded even with comptime off; syms=%v", callSyms(off))
	}
}

// TestComptimeFoldsCharExpr is the expression-position counterpart: a char
// arithmetic expression used directly (not via a function call) folds to a
// char-typed constant. `"A" + 1` must materialise as a char constant 66, not an
// i64; otherwise `print("A" + 1)` would print "66" with comptime on but "B"
// off.
func TestComptimeFoldsCharExpr(t *testing.T) {
	on, _ := comptimePair(t, `main = () () {
    r char = "A" + 1
    print(r)
}
`)
	if typ := opConstType(on, 66); typ != "char" {
		t.Errorf("folded char expression must be emitted as type char, got %q", typ)
	}
}

// TestComptimeFoldsMultiResultChar pins that multi-result folding accepts a
// char out-parameter now that char arithmetic preserves the char kind. Both
// out-params are char and are derived by char arithmetic, so the whole call
// folds and the destinations receive char-typed constants.
//
// This exercises the v1 `KindChar` rejection that foldMultiResultCall used to
// apply: it refused any call with a char result, so this exact shape would
// have kept its call.
func TestComptimeFoldsMultiResultChar(t *testing.T) {
	src := `next-char = (c char) (n char, prev char) {
    n = c + 1
    prev = c - 1
}
main = () () {
    a char = 0
    b char = 0
    next-char("A", a, b)
    print(a)
    print(b)
}
`
	on, off := comptimePair(t, src)
	if hasCallSym(on, "next-char") {
		t.Errorf("next-char(\"A\", a, b) kept its call instead of folding; syms=%v", callSyms(on))
	}
	if typ := opConstType(on, 66); typ != "char" {
		t.Errorf("multi-result char out-param must be emitted as type char, got %q", typ)
	}
	if typ := opConstType(on, 64); typ != "char" {
		t.Errorf("multi-result char out-param must be emitted as type char, got %q", typ)
	}
	if !hasCallSym(off, "next-char") {
		t.Errorf("next-char was folded even with comptime off; syms=%v", callSyms(off))
	}
}

// TestComptimeLeavesCharRuntimeArgAlone is the reverse guard: a char-returning
// function called with a RUNTIME char argument must keep its call. Otherwise
// folding would invent a value for the unknown argument.
func TestComptimeLeavesCharRuntimeArgAlone(t *testing.T) {
	on, _ := comptimePair(t, `to-next = (c char) (r char) {
    r = c + 1
}
main = () () {
    i <- [0..3) {
        c char = i + 65
        r = to-next(c)
        print(r)
    }
}
`)
	if !hasCallSym(on, "to-next") {
		t.Errorf("char-returning call with a runtime argument was folded away; syms=%v", callSyms(on))
	}
}
