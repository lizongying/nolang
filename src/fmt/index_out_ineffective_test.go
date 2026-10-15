package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD (#{index-out=...} removal under D7 final decision, two-layer
// separation, 2026-10-15).
//
// User side: even when the compiler statically proves a container index read is
// in-bounds, the read STILL produces an option (`?elem`) and MUST be unpacked.
// The `#{index-out = DEF}` annotation is therefore NOT dead for such reads — it
// triggers maybeIndexOutAssign's DEF-match desugaring (the consumption path).
// `no fmt` must NOT strip it.
//
// fmt only removes #{index-out} when it is genuinely dead:
//   - pure-write statements (no read-position index at all);
//   - orphan standalone annotations (no governed statement follows).
//
// The decision is shared with codegen and `no vet` via parser/bounds.go, so that
// "fmt deletes the annotation" never causes "vet/compile immediately errors".
// These tests pin that three-way consistency.

// parseFull runs the same pipeline `no vet` uses (default parse, with safe-index
// lowering ON) so the checker-side validators see the lowered program.
func parseFull(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

// TestFormatPreservesProvablyInBoundsIndexOut: a fixed-array read whose index is a
// for-range loop variable provably in `[0, N)` KEEPS its `#{index-out=zero}` — under
// D7 final decision the read still produces an option on the user side, and the
// annotation carries the DEF-match consumption. fmt must not strip it.
func TestFormatPreservesProvablyInBoundsIndexOut(t *testing.T) {
	input := "main = () {\n" +
		"    p-tmp [32]byte\n" +
		"    p []byte = with-len(32)\n" +
		"    i <- [0..32): {\n" +
		"        #{index-out=zero}\n" +
		"        p[i] = p-tmp[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "index-out") {
		t.Errorf("provably-in-bounds #{index-out} was wrongly removed (still carries DEF-match):\n%s", out)
	}
	// Idempotency: a second pass must not change anything.
	program2 := parseForTest(out)
	if program2 == nil {
		t.Fatal("failed to re-parse formatted output")
	}
	if out2 := FormatProgramWithOverflow(program2, out, nil, nil); out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// TestFormatPreservesAttachedInBoundsIndexOut covers the *attached* (trailing on the
// same line) annotation path — must also be preserved under D7 final decision.
func TestFormatPreservesAttachedInBoundsIndexOut(t *testing.T) {
	input := "get = () (res i64) {\n" +
		"    a [4]i64 = [10, 20, 30, 40]\n" +
		"    i <- [0..4): {\n" +
		"        res = a[i] #{index-out=zero}\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "index-out") {
		t.Errorf("attached provably-in-bounds #{index-out} was wrongly removed:\n%s", out)
	}
}

// TestFormatPreservesIndexOutForSlice: a slice read (`[]T`, runtime length unknown)
// is NOT provably in-bounds, so its `#{index-out=zero}` must be preserved.
func TestFormatPreservesIndexOutForSlice(t *testing.T) {
	input := "get = (v []i64) (res i64) {\n" +
		"    i <- [0..4): {\n" +
		"        #{index-out=zero}\n" +
		"        res = v[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "#{index-out=zero}") {
		t.Errorf("slice (non-provable) #{index-out} was wrongly stripped:\n%s", out)
	}
}

// TestFormatPreservesIndexOutForParamBound: for-range bound is a non-literal
// parameter, so the read is not provable and the annotation is preserved.
func TestFormatPreservesIndexOutForParamBound(t *testing.T) {
	input := "get = (n i64) (res i64) {\n" +
		"    a [4]i64 = [10, 20, 30, 40]\n" +
		"    i <- [0..n): {\n" +
		"        #{index-out=zero}\n" +
		"        res = a[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "#{index-out=zero}") {
		t.Errorf("non-literal-bound #{index-out} was wrongly stripped:\n%s", out)
	}
}

// TestIndexOutPreservedConsistentWithChecker is the crux of three-way agreement:
// under D7 final decision, `#{index-out}` on a provably-in-bounds read is PRESERVED
// by fmt (it carries the DEF-match consumption). The checker (which `no vet` runs
// on the lowered program) must NOT then report the read as an unhandled
// out-of-bounds index — maybeIndexOutAssign's match consumes it. If fmt were to
// strip the annotation, the read would become an unconsumed option and vet would
// newly error — the exact regression class shared with #{overflow} removal.
func TestIndexOutPreservedConsistentWithChecker(t *testing.T) {
	input := "main = () {\n" +
		"    p-tmp [32]byte\n" +
		"    p []byte = with-len(32)\n" +
		"    i <- [0..32): {\n" +
		"        #{index-out=zero}\n" +
		"        p[i] = p-tmp[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "index-out") {
		t.Fatalf("precondition: annotation should have been preserved:\n%s", out)
	}
	// Feed the (still-annotated) output through the checker pipeline.
	res := checker.ValidateUnhandledIndex(parseFull(t, out), "src/app.no")
	for _, r := range res {
		if strings.Contains(r.Message, "越界") || r.TraceID == "idxhndld" {
			t.Fatalf("annotation preserved but checker still reports unhandled index: %+v\n(source after fmt)\n%s", r, out)
		}
	}
}

// TestFormatPreservesWithLenSliceFixedLenRead: a slice base whose length is fixed by a
// top-level `buf []T = with-len(<literal>)` and never reassigned has a statically known
// length, so a literal index within it is provably in-bounds. Under D7 final decision
// the read STILL produces an option on the user side, so the `#{index-out=zero}` (which
// carries the DEF-match consumption) must be PRESERVED.
func TestFormatPreservesWithLenSliceFixedLenRead(t *testing.T) {
	input := "get = () (res i64) {\n" +
		"    buf []i64 = with-len(4)\n" +
		"    #{index-out=zero}\n" +
		"    res = buf[0]\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "index-out") {
		t.Errorf("with-len literal slice in-bounds read #{index-out} was wrongly stripped:\n%s", out)
	}
	// Idempotency.
	if out2 := FormatProgramWithOverflow(parseForTest(out), out, nil, nil); out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
	// Three-way consistency: with the annotation preserved, the checker must NOT report the read.
	for _, r := range checker.ValidateUnhandledIndex(parseFull(t, out), "src/app.no") {
		if r.TraceID == "idxhndld" {
			t.Fatalf("annotation preserved but checker reports unhandled index: %+v\n%s", r, out)
		}
	}
}

// TestFormatPreservesIndexOutForReassignedSlice: the fixed-length guarantee is void the
// moment the variable is reassigned — `buf` may then hold a shorter slice, so a literal
// index is no longer provably in-bounds and the annotation must be PRESERVED.
func TestFormatPreservesIndexOutForReassignedSlice(t *testing.T) {
	input := "get = () (res i64) {\n" +
		"    buf []i64 = with-len(4)\n" +
		"    buf = with-len(2)\n" +
		"    #{index-out=zero}\n" +
		"    res = buf[0]\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "#{index-out=zero}") {
		t.Errorf("reassigned slice #{index-out} was wrongly stripped:\n%s", out)
	}
}

// TestFormatRemovesOrphanIndexOutAnnotation: a standalone `#{index-out=zero}` with no
// following statement — it sits at the end of a block, governing nothing (the orphan
// case in src/std/collection/map.no). checker never reads it (no indexed read statement
// carries it) and codegen never applies it (maybeIndexOutAssign fires only for an
// annotation attached to an index-read) → dead code, `no fmt` must strip it.
// (The final `r = x[0] #{index-out=zero}` read is annotated separately so it stays
// consumed under D7 final decision — the test isolates orphan removal only.)
func TestFormatRemovesOrphanIndexOutAnnotation(t *testing.T) {
	input := "rebuild = (n i64) (r i64) {\n" +
		"    x []i64 = with-len(8)\n" +
		"    i <- [0..n): {\n" +
		"        x[i] = i\n" +
		"        #{index-out=zero}\n" +
		"    }\n" +
		"    r = x[0] #{index-out=zero}\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	// The orphan (end-of-loop-body, governing nothing) must be gone; the attached
	// one on `r = x[0]` must remain.
	if strings.Count(out, "index-out") != 1 {
		t.Errorf("orphan #{index-out} not removed (or attached one wrongly stripped):\n%s", out)
	}
	// Orphan removal must not create a checker unhandled-index error.
	for _, r := range checker.ValidateUnhandledIndex(parseFull(t, out), "src/app.no") {
		if r.TraceID == "idxhndld" {
			t.Fatalf("orphan removal introduced unhandled index: %+v\n%s", r, out)
		}
	}
	// Idempotency: a second format must be stable (no dangling blank line left behind).
	if out2 := FormatProgramWithOverflow(parseForTest(out), out, nil, nil); out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// TestFormatPreservesIndexOutForWithLenVarArg: `with-len(n)` (runtime length) is not a
// literal, so the slice length is unknown and the annotation must be PRESERVED.
func TestFormatPreservesIndexOutForWithLenVarArg(t *testing.T) {
	input := "get = (n i64) (res i64) {\n" +
		"    buf []i64 = with-len(n)\n" +
		"    #{index-out=zero}\n" +
		"    res = buf[0]\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "#{index-out=zero}") {
		t.Errorf("with-len(non-literal) #{index-out} was wrongly stripped:\n%s", out)
	}
}

// TestFormatPreservesIndexOutForUnresolvedBaseRead: `names = <cross-module call>()`
// gives `names` a container type that the single-file fmt analysis cannot resolve, so
// the OLD removable check (keyed on *recognized* container reads) saw an empty read set
// — indistinguishable from a pure write — and stripped the annotation. commit 23038595
// ran `no fmt -w src/std` with that bug and removed the needed `#{index-out=zero}` from
// `name = names[i]` (process.no) / `lines[i]` (toml.no), making `no vet` fail with
// idxhndld / fxxoptarg. enumReadIndexAll now counts ANY read-position index (regardless
// of base resolution), so this annotation must be PRESERVED.
func TestFormatPreservesIndexOutForUnresolvedBaseRead(t *testing.T) {
	// `probe` is an undefined (cross-module) call → `names`'s type is not a resolvable
	// container in this single-file analysis.
	input := "scan = () (r str) {\n" +
		"    names = probe-list()\n" +
		"    i <- [0..names.len()): {\n" +
		"        #{index-out=zero}\n" +
		"        r = names[i]\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if !strings.Contains(out, "#{index-out=zero}") {
		t.Errorf("read with unresolved container base: #{index-out} was wrongly stripped:\n%s", out)
	}
}

// TestFormatRemovesIndexOutOnPureWrite: `#{index-out = DEF}` only ever affects an index
// READ (the desugar fires when the assignment RHS is an IndexExpression). On a pure
// write — `nl[0] = 10`, RHS is a literal — the annotation is dead code: writes go through
// the bounds_check path (no option) and the checker never reports writes. `no fmt` must
// strip it, and removal must not introduce a vet error (three-way consistency).
func TestFormatRemovesIndexOutOnPureWrite(t *testing.T) {
	input := "errln = (s str) (n i64) {\n" +
		"    nl []byte = with-len(1)\n" +
		"    #{index-out=zero}\n" +
		"    nl[0] = 10\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	out := FormatProgramWithOverflow(program, input, nil, nil)
	if strings.Contains(out, "index-out") {
		t.Errorf("#{index-out} on a pure index write (dead annotation) was not removed:\n%s", out)
	}
	if !strings.Contains(out, "nl[0] = 10") {
		t.Errorf("write statement changed unexpectedly:\n%s", out)
	}
	// Removing it must not make the checker report an unhandled index.
	for _, r := range checker.ValidateUnhandledIndex(parseFull(t, out), "src/app.no") {
		if r.TraceID == "idxhndld" {
			t.Fatalf("fmt removed write annotation but checker reports unhandled index: %+v\n%s", r, out)
		}
	}
	// Idempotency.
	if out2 := FormatProgramWithOverflow(parseForTest(out), out, nil, nil); out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}
