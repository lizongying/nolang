package dump

// ─────────────────────────────────────────────────────────────────────────────
// S0 instrumentation for docs/design/nolang-parser-lowering-invariants.md.
//
// Gate G1 has two halves:
//
//	C6  H0 must not modify the AST — it lowers *while* emitting HIR. The AST
//	    dump must be byte-identical before and after AST -> HIR. This holds
//	    today, so the test runs.
//	C1  no stage may write AST node fields after ParseProgram returns. This does
//	    NOT hold today (the checker records inferred types by assigning into
//	    node fields — violation V9 / risk R6), so its test is skipped until
//	    S1/S5 move that information into a side table.
//
// Both halves share one comparator: a canonical dump taken before and after a
// stage. The comparator is only trustworthy if it can *see* a change, so
// TestASTDumpComparatorDetectsMutation is its anti-vacuity control — without it,
// a comparator that returned "" for every input would pass both gates.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"sort"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func freezeSortedPaths(corpus map[string]*parser.Program) []string {
	out := make([]string, 0, len(corpus))
	for path := range corpus {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// TestASTIsFrozenAcrossHIRLowering is gate G1's C6 half: the AST -> HIR pass
// lowers while it walks and never writes back into the tree.
//
// The dump is canonical (TestDumpIsDeterministic pins that), so a byte
// comparison is a sound oracle for "did any node field change".
func TestASTIsFrozenAcrossHIRLowering(t *testing.T) {
	corpus := parseCorpus(t)
	if len(corpus) < 100 {
		t.Fatalf("only %d corpus files parsed cleanly; too few for this gate to mean "+
			"anything", len(corpus))
	}

	var checked, nonEmpty int
	for _, path := range freezeSortedPaths(corpus) {
		prog := corpus[path]
		before := Dump(prog)
		parser.ASTToHIR(prog)
		after := Dump(prog)
		if before != after {
			t.Errorf("%s: the AST changed across ASTToHIR (C6 — H0 must not mutate the "+
				"tree)\n%s", path, firstDiff(before, after))
			continue
		}
		checked++
		if before != "" {
			nonEmpty++
		}
	}

	// An empty-vs-empty comparison passes for free, so require that the corpus
	// actually produced dumps to compare.
	if nonEmpty < 100 {
		t.Fatalf("only %d of %d programs produced a non-empty dump; the comparison "+
			"would be vacuous", nonEmpty, checked)
	}
	t.Logf("%d programs: AST dump byte-identical across ASTToHIR (%d non-empty)",
		checked, nonEmpty)
}

// TestASTDumpComparatorDetectsMutation is the anti-vacuity control for the gate
// above: it proves the comparator can see a node field change, so the gate's
// "identical" result is evidence rather than an artifact.
func TestASTDumpComparatorDetectsMutation(t *testing.T) {
	p := parser.New(lexer.NewCached("mut.no", "abc = 1\n"))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("the probe source no longer parses: %v", errs)
	}

	before := Dump(prog)
	if before == "" {
		t.Fatal("the probe program dumped to nothing, so the comparator cannot be tested")
	}

	var target *parser.LetStatement
	for _, stmt := range prog.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok {
			target = ls
			break
		}
	}
	if target == nil {
		t.Fatalf("no LetStatement in the probe program; first statement is %T",
			prog.Statements[0])
	}

	target.Name.Value = "xyz"
	if after := Dump(prog); after == before {
		t.Fatal("mutating LetStatement.Name.Value did not change the dump; the freeze " +
			"gate above would pass even against a mutating stage")
	}
}

// TestCheckerASTMutationCensus quantifies violation V9 / risk R6 before anyone
// tries to fix it: how many corpus programs does the checker actually mutate?
//
// The design doc's strict scope (§1.4) says no stage may write AST node fields
// after ParseProgram returns, and rates the checker's habit of recording
// inferred types in node fields as the expensive part of that choice. Sizing that
// work needs a number, not an adjective — and if the number turns out to be zero,
// the claim is wrong and the plan gets cheaper. Either way this is worth knowing
// before S5 is scheduled.
//
// It is a census, not an assertion of correctness: it logs what it finds and only
// fails if the measurement itself is degenerate.
func TestCheckerASTMutationCensus(t *testing.T) {
	// The checker hard-fails when the std signature key was not injected, which
	// only `make test` (and the release build) do. Skipping keeps a bare
	// `go test ./parser/dump/` usable — this package's other tests do not need
	// the key, and before this census existed neither did it.
	if checker.StdSigKey == "" {
		t.Skip("StdSigKey not injected (bare `go test`); the checker hard-fails without " +
			"it. Run `make test`, or pass " +
			`-ldflags "-X github.com/lizongying/nolang/checker.StdSigKey=$KEY"`)
	}

	corpus := parseCorpus(t)
	if len(corpus) < 100 {
		t.Fatalf("only %d corpus files parsed; the census would be vacuous", len(corpus))
	}

	var checked, mutated int
	for _, path := range freezeSortedPaths(corpus) {
		prog := corpus[path]
		before := Dump(prog)
		checker.ValidateTypes(prog)
		after := Dump(prog)
		checked++
		if before == after {
			continue
		}
		mutated++
		if mutated <= 3 {
			t.Logf("example of a checker-mutated tree: %s\n%s", path, firstDiff(before, after))
		}
	}

	if checked < 100 {
		t.Fatalf("only %d programs checked; the census would be vacuous", checked)
	}
	t.Logf("checker.ValidateTypes mutated the AST of %d of %d programs "+
		"(%d of the corpus's files)", mutated, checked, len(corpus))
	if mutated == 0 {
		t.Logf("no mutation observed — either the checker does not write node fields " +
			"for this corpus, or the mutation happens in a stage this census does not run")
	}
}

// TestASTIsFrozenAfterChecker is gate G1's C1 half: the strict form of the census
// above, which requires the count to be ZERO.
//
// SKIPPED because it fails today by construction. The census above measures the
// gap; this gate is what closes it. S1 moves the checker ahead of lowering and S5
// turns any node-field write into a returned type map; enable this then, and
// delete the census if the two would duplicate each other.
func TestASTIsFrozenAfterChecker(t *testing.T) {
	t.Skip("S1/S5 (docs/design/nolang-parser-lowering-invariants.md §8): the checker " +
		"still writes into AST node fields (V9/R6); enable once it returns a type map " +
		"instead of mutating the tree")
}
