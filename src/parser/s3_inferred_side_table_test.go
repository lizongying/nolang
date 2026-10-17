package parser

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestInferredTypeLivesInSemanticSideTable pins the S3 invariant: parser-side
// type inference facts (`?T` for `x = a + b`, `x = arr[i]`, etc.) live in
// Program.Sem.InferredType and NEVER mutate the AST's LetStatement.Type.
//
// The regression guard has four shapes, all needed:
//
//  1. LetStatement.Type stays nil for inferred cases (no AST mutation,
//     P1 invariant).
//  2. Program.Sem publishes the inferred ?T string and `HasInferredType`
//     answers the right question.
//  3. The InferredByOptionArith flag distinguishes the two WRITE SOURCES:
//     inferOptionArith (root arithmetic) sets it true, synthesizeNullableType
//     (structural capture) sets it false. A flag collapse here lets the
//     option-capture pass re-desugar `a = b + c` into `__cap_N`, silently
//     dropping the `?i64` annotation the user would otherwise see.
//  4. The cache mirror (detNodeSem) round-trips through gob, so an
//     incremental-build AST cache hit produces identical checker / formatter
//     output to a fresh parse.
//
// S3 was the third "lowerer mutates AST for ?T" removal (after S0 for safe
// index and S1 for option-capture); without this test, a future lowering pass
// could regress by writing `s.Type = &NullableType{IsInferred: true}` again
// and only a behaviour change in the formatter would expose it.
func TestInferredTypeLivesInSemanticSideTable(t *testing.T) {
	// 1. AST-level guard: LetStatement must not carry inferred type state.
	//    P1 forbids writing AST after construction; using a field name
	//    (instead of value comparison) catches any future re-introduction.
	for _, name := range []string{"InferredType", "IsInferred"} {
		if _, ok := reflect.TypeOf(LetStatement{}).FieldByName(name); ok {
			t.Fatalf("LetStatement unexpectedly carries a %q field; "+
				"S3 forbids AST mutation for inferred types", name)
		}
	}

	// 2. Side-table publication: `x = a + b` publishes `?i64` to Program.Sem
	//    and leaves `x.Type` empty.
	arithProg := parseProg(t, `
f = () () {
    a i64 = 1
    b i64 = 2
    x = a + b
    print(x)
}
`)
	x := findLetByName(t, arithProg, "x")
	if x.Type != nil {
		t.Fatalf("`x = a + b` mutated LetStatement.Type (got %s); "+
			"inferred types must live in Program.Sem, not on the AST",
			typeString(x.Type))
	}
	if !arithProg.Sem.HasInferredType(x) {
		t.Fatal("`x = a + b` did not publish an inferred type to Program.Sem")
	}
	if got := arithProg.Sem.InferredType(x); got != "?i64" {
		t.Fatalf("`x = a + b` published %q to Program.Sem, want \"?i64\"", got)
	}
	// Root-arithmetic inference must set InferredByOptionArith=true; this is
	// what stops maybeCaptureOptionAssign from re-desugaring into __cap_N.
	if !arithProg.Sem.InferredByOptionArith(x) {
		t.Fatal("inferOptionArith source must set InferredByOptionArith=true")
	}

	// 3. Two-source distinction: structural-capture cases (where
	//    synthesizeNullableType is the writer) leave the flag false, so the
	//    option-capture pass is allowed to run.
	captureProg := parseProg(t, `
f = () () {
    p []i64 = [1, 2, 3]
    i i64 = 0
    x = p[i]
    print(x)
}
`)
	cx := findLetByName(t, captureProg, "x")
	if cx.Type != nil {
		t.Fatalf("`x = p[i]` mutated LetStatement.Type (got %s); "+
			"structural inference must live in Program.Sem", typeString(cx.Type))
	}
	if !captureProg.Sem.HasInferredType(cx) {
		t.Fatal("`x = p[i]` did not publish an inferred type to Program.Sem")
	}
	if got := captureProg.Sem.InferredType(cx); got != "?i64" {
		t.Fatalf("`x = p[i]` published %q, want \"?i64\"", got)
	}
	// Structural-capture cases: InferredByOptionArith must be false so
	// option-capture can still re-desugar (the `__cap_N` rewrites
	// OUT of `prepareInferredTypes`, in the structural pass).
	if captureProg.Sem.InferredByOptionArith(cx) {
		t.Fatal("synthesizeNullableType must leave InferredByOptionArith=false")
	}

	// 4. Two-form mechanism: the surface path (SkipUnwrapLowering +
	//    SkipSafeIndexLowering — what `no fmt` / `no vet` use) must NOT mutate
	//    the AST either. The formatter path is precisely the one that must
	//    remain read-only so a re-format does not introduce synthetic __cap_N
	//    bindings or change the user's source layout.
	surfaceProg := skipLoweringParse(t, `
f = (a i64, b i64) () {
    x = a + b
}
`)
	sx := findLetByName(t, surfaceProg, "x")
	if sx.Type != nil {
		t.Fatalf("surface path mutated LetStatement.Type (got %s); "+
			"`no fmt` must not add synthetic annotations", typeString(sx.Type))
	}
	// The Program.Sem side-table is intentionally NOT populated in surface
	// mode for arithmetic roots — the formatter has no consumer for it and
	// `prepareInferredTypes` skips it under `SkipUnwrapLowering`. This is
	// why the gate is "AST untouched" not "side-table populated".

	// 5. Cache round-trip: the detNodeSem mirror must carry both fields, so a
	//    Tier-1 cache hit restores the side-table byte-for-byte (paralleling
	//    the InBoundsProofLivesInSemanticSideTable round-trip).
	cp := &CachedProgram{Prog: arithProg, NodeSem: SerializeNodeSem(arithProg)}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cp); err != nil {
		t.Fatalf("gob encode: %v", err)
	}
	var dec CachedProgram
	if err := gob.NewDecoder(&buf).Decode(&dec); err != nil {
		t.Fatalf("gob decode: %v", err)
	}
	RestoreNodeSem(dec.Prog, dec.NodeSem)

	dx := findLetByName(t, dec.Prog, "x")
	if dx == nil {
		t.Fatal("decoded program has no `x` binding")
	}
	if !dec.Prog.Sem.HasInferredType(dx) {
		t.Fatal("gob round-trip lost InferredType")
	}
	if got := dec.Prog.Sem.InferredType(dx); got != "?i64" {
		t.Fatalf("gob round-trip restored InferredType=%q, want \"?i64\"", got)
	}
	if !dec.Prog.Sem.InferredByOptionArith(dx) {
		t.Fatal("gob round-trip lost InferredByOptionArith")
	}
}

// parseProg parses src under the same flags ParseProgram uses by default;
// failures are fatal so call sites read straight.
func parseProg(t *testing.T, src string) *Program {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse failed: %v", p.Errors())
	}
	return prog
}

// skipLoweringParse parses src with both safe-index and unwrap lowering
// disabled, mimicking the `no fmt` / `no vet` path that must not produce a
// divergent side-table.
func skipLoweringParse(t *testing.T, src string) *Program {
	t.Helper()
	p := New(lexer.New(src))
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("skipLowering parse failed: %v", p.Errors())
	}
	return prog
}

// findLetByName locates the LetStatement named name anywhere in prog. It
// exists only for the S3 regression: assertions need a pointer to compare
// HasInferredType / InferredType against, and the test fixtures define names
// uniquely (the parser stores them in parse order, so the first match is
// always the right one).
func findLetByName(t *testing.T, prog *Program, name string) *LetStatement {
	t.Helper()
	var found *LetStatement
	walkNodes(prog, func(v interface{}) {
		if found != nil {
			return
		}
		ls, ok := v.(*LetStatement)
		if !ok || ls == nil || ls.Name == nil {
			return
		}
		if ls.Name.Value == name {
			found = ls
		}
	})
	if found == nil {
		t.Fatalf("no LetStatement named %q in program", name)
	}
	return found
}
