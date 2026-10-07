package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestNullableSliceTypePosIsQuestionMark guards the position of a `?T` option
// annotation whose inner type is a slice/array/map (the `p ?[]i64 = o` form).
//
// Such a type is wrapped into NullableType inside parseLetStatement. Its Token
// — and therefore NullableType.Pos() — must point at the `?`, NOT the variable
// name. The redundant-annotation remover (checker report, findRedundantTypeNode,
// computeRedundantTypeRemoval) all scan from Type.Pos(); when Pos() landed on the
// name, removing `?[]i64` deleted the variable name instead, producing garbage
// (`    p ?[]i64 = o` -> ` ?[]i64 = o`). The scalar `?str` form went through
// buildType (Token = the type token) and was already correct — this test pins the
// two paths to the same invariant.
func TestNullableSliceTypePosIsQuestionMark(t *testing.T) {
	// `    p ?[]i64 = o` on line 2: p=col5, space=col6, ?=col7.
	src := "f = () {\n    p ?[]i64 = o\n}\n"
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}

	fn, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok || fn.Body == nil || len(fn.Body.Statements) == 0 {
		t.Fatalf("expected function with a body statement")
	}
	ls, ok := fn.Body.Statements[0].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", fn.Body.Statements[0])
	}

	nt, ok := ls.Type.(*NullableType)
	if !ok {
		t.Fatalf("Type = %T, want *NullableType", ls.Type)
	}
	if _, ok := nt.Type.(*SliceType); !ok {
		t.Fatalf("inner type = %T, want *SliceType", nt.Type)
	}
	if nt.Type.Pos().Line != 2 {
		t.Errorf("NullableType.Pos().Line = %d, want 2", nt.Type.Pos().Line)
	}
	if got := ls.Type.Pos().Column; got != 7 {
		t.Errorf("NullableType.Pos().Column = %d, want 7 (the `?`), NOT the name", got)
	}
}
