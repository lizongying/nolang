package parser

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"strings"
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
//  3. The type-fact entry carries ONLY reconstructible program facts. The
//     former `ByOptionArith` bit — which told `maybeCaptureOptionAssign`
//     whether root-arithmetic inference had already fired — is gone: it is a
//     local decision made and consumed inside one `LowerSurface` walk, passed
//     by value instead. The behavioural half of that invariant is pinned here
//     too: `x = a + b` must NOT be re-desugared into `__cap_N`, while
//     `x = p[i] + 1` (where inference genuinely fails) MUST be.
//  4. The cache mirror (detCached.TypeFacts) round-trips through gob, so an
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
	// 3a. The entry type must carry no walk-local bookkeeping. A future
	//     re-introduction of `ByOptionArith` (or any sibling "who wrote this"
	//     bit) would silently re-add a non-reconstructible field to the blob.
	//     Reflection with an exact field set — a subset check would let a new
	//     field slip in unnoticed.
	wantFactFields := []string{"InferredType", "TypeOverride"}
	var gotFactFields []string
	ft := reflect.TypeOf(TypeFact{})
	for i := 0; i < ft.NumField(); i++ {
		gotFactFields = append(gotFactFields, ft.Field(i).Name)
	}
	if len(gotFactFields) == 0 {
		t.Fatal("reflection saw zero fields on TypeFact — the gate would be vacuous")
	}
	if !reflect.DeepEqual(gotFactFields, wantFactFields) {
		t.Fatalf("TypeFact fields = %v, want %v\n"+
			"Every field here is gob-serialized into the Tier-0/Tier-1 blob. "+
			"Walk-local decisions (e.g. the removed ByOptionArith bit) must be "+
			"passed by value at the consumption point, not persisted.",
			gotFactFields, wantFactFields)
	}

	// 3b. Behavioural half: root arithmetic is NOT structurally re-desugared.
	//     Without this, deleting the bit could silently start emitting `__cap_N`
	//     for `x = a + b`, which changes the HIR the cache must reproduce.
	if n := countCapTemps(t, arithProg); n != 0 {
		t.Fatalf("`x = a + b` produced %d __cap_ temp(s); root-arithmetic "+
			"inference must short-circuit the structural capture pass", n)
	}

	// 3c. Anti-vacuity for 3b: a root arithmetic expression whose operand type
	//     is NOT inferable (safe index) must still be captured. If this fixture
	//     also produced zero temps, 3b would be proving nothing.
	//
	//     Note: the captured LetStatement itself leaves the tree (it is replaced
	//     by the capture chain), so `findLetByName` would find the *synthesised*
	//     ok-arm binding, not the original — assert on the temp count only.
	captureProg := parseProg(t, `
f = () () {
    p []i64 = [1, 2, 3]
    i i64 = 0
    x = p[i] + 1
    print(x)
}
`)
	if n := countCapTemps(t, captureProg); n == 0 {
		t.Fatal("`x = p[i] + 1` produced no __cap_ temp; the structural capture " +
			"pass is dead, so the 3b assertion above is vacuous")
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

	// 5. Cache round-trip: since 2026-10-26 the blob is the syntax tree only, so
	//    the `?T` fact is NOT on the wire — it is regenerated by BuildSemViews.
	//    Both halves matter: the first proves the envelope really carries no
	//    derived data, the second proves the rebuild brings the fact back on the
	//    SAME node. (A Tier-1 hit that skipped the rebuild would leave `x`
	//    untyped, which is exactly the failure this step guards.)
	cp := &CachedProgram{Prog: arithProg}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cp); err != nil {
		t.Fatalf("gob encode: %v", err)
	}
	var dec CachedProgram
	if err := gob.NewDecoder(&buf).Decode(&dec); err != nil {
		t.Fatalf("gob decode: %v", err)
	}
	if n := dec.Prog.TypeFacts().Len(); n != 0 {
		t.Fatalf("blob carried %d type facts; CachedProgram must hold the syntax tree only", n)
	}

	BuildSemViews(dec.Prog, SemRebuildConfig{Filename: "arith.no"})
	dx := findLetByName(t, dec.Prog, "x")
	if dx == nil {
		t.Fatal("decoded program has no `x` binding")
	}
	if !dec.Prog.Sem.HasInferredType(dx) {
		t.Fatal("decode+rebuild lost InferredType")
	}
	if got := dec.Prog.Sem.InferredType(dx); got != "?i64" {
		t.Fatalf("decode+rebuild restored InferredType=%q, want \"?i64\"", got)
	}
}

// countCapTemps counts identifiers the option-capture pass synthesised
// (`__cap_N`). It is the observable footprint of the structural desugar: zero
// means the capture pass declined to rewrite the statement.
func countCapTemps(t *testing.T, prog *Program) int {
	t.Helper()
	n := 0
	walkNodes(prog, func(v interface{}) {
		switch x := v.(type) {
		case *Identifier:
			if x != nil && strings.HasPrefix(x.Value, "__cap_") {
				n++
			}
		case *LetStatement:
			if x != nil && x.Name != nil && strings.HasPrefix(x.Name.Value, "__cap_") {
				n++
			}
		}
	})
	return n
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
	p.SkipLowering = true
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
