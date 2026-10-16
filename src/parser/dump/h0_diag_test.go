package dump

// ─────────────────────────────────────────────────────────────────────────────
// S2 follow-up: the ERROR half of the H0 diagnostic channel.
//
// Desugaring a SurfaceMatch inside H0 can emit ERRORS, not just warnings —
// buildMatchDesugar's "empty match arm cannot determine a value" is the live
// example. Until this gate existed those errors had no reader at all:
//
//	shipped  ParseProgram()            → LowerSurface → p.saveError → p.Errors()
//	                                        ✓ the build fails
//	target   ParseProgram(SkipLowering) → ASTToHIR     → p.saveError → hirConv.h0Diags
//	                                        ✗ nothing reads h0Diags; HIR is produced
//
// The *Parser that owns diags is discarded when ParseProgram returns, so the
// target shape needs a channel that lives on the Program. That is what
// Program.Errors is (see its block comment in src/parser/ast.go), and
// ASTToHIRWithMap folds SeverityError into it while SeverityWarning keeps going
// to Program.Warnings.
//
// This gate pins three properties, each with its own non-vacuity check:
//
//	A. the error is VISIBLE on the target shape  (prog.Errors is non-empty)
//	B. it is NOT laundered into Warnings          (prog.Warnings stays empty)
//	C. warnings still take the warning channel   (a warning-only input stays in
//	   Warnings and leaves Errors empty)
//
// Plus a corpus-wide sanity count: the shipped corpus must not suddenly start
// producing H0 errors, because that would mean H0 rejects something the parser
// accepted.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// h0DiagErrorSrc is the measured witness. `s` is a str, so buildMatchDesugar
// cannot derive an element type for the empty `ok ->` arm and fires
// "empty match arm cannot determine a value".
//
// Verified both ways before the fix landed:
//   - shipped shape (default ParseProgram, lowering inside the parser):
//     p.Errors() = ["… empty match arm cannot determine a value …"]
//   - target shape (SkipLowering + ASTToHIR): p.Errors() empty,
//     prog.Warnings empty, HIR produced — the error was gone.
const h0DiagErrorSrc = `s = 'abc'
main = () () {
    k str = s: {
        ok ->
        err -> 'e'
        nil -> 'f'
    }
    bb = k
}
`

// h0DiagWarningSrc exercises the warning half so the two severities cannot be
// conflated. `match` is a deprecated keyword, so the parser emits a warning
// before any desugaring happens.
const h0DiagWarningSrc = `s = 'abc'
main = () () {
    match s {
        ok -> bb = 1
        err -> bb = 2
        nil -> bb = 3
    }
}
`

// h0DiagCleanSrc is the control: a well-formed match that desugars without any
// diagnostic, so "Errors is empty" cannot be satisfied by a desugar that never
// runs.
const h0DiagCleanSrc = `aa = () (r ?i64) {
    r = nil
}
main = () () {
    v0 = aa()
    k = v0: {
        ok -> 1
        err -> 2
        nil -> 3
    }
    bb = k
}
`

// h0DiagTargetShape runs the target pipeline — the parser stops at sugar, H0
// desugars — and returns whatever the Program ended up carrying.
func h0DiagTargetShape(t *testing.T, name, src string) (errs, warns []string) {
	t.Helper()
	p := parser.New(lexer.NewCached(name, src))
	p.Filename = name
	p.SkipLowering = true
	prog := p.ParseProgram()
	if got := p.Errors(); len(got) > 0 {
		// The parser itself rejected the source. That is not what this gate is
		// about, and it would make every later assertion vacuous.
		t.Fatalf("%s: parser rejected the source before any desugaring: %v", name, got)
	}
	parser.ASTToHIR(prog)
	return prog.Errors, prog.Warnings
}

// TestH0DesugarErrorsReachProgramErrors is property A + B.
func TestH0DesugarErrorsReachProgramErrors(t *testing.T) {
	errs, warns := h0DiagTargetShape(t, "h0-diag-error.no", h0DiagErrorSrc)

	if len(errs) == 0 {
		t.Fatalf("H0-side desugaring emitted an error and NOTHING read it: " +
			"prog.Errors is empty. Either Program.Errors is no longer folded in by " +
			"ASTToHIRWithMap, or buildMatchDesugar stopped firing on this input " +
			"(check by running the shipped shape and looking at p.Errors())")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "empty match arm cannot determine a value") {
		t.Errorf("prog.Errors does not carry the expected diagnostic: %v", errs)
	}

	// B: severity must be preserved. Folding the error into Warnings would have
	// made a program with valueless match arms compile.
	for _, w := range warns {
		if strings.Contains(w, "empty match arm cannot determine a value") {
			t.Errorf("the desugaring ERROR was laundered into prog.Warnings: %v", warns)
		}
	}
}

// TestH0DesugarWarningsStillReachProgramWarnings is property C. It fails if the
// error/warning split regresses in EITHER direction: warnings that start landing
// in Errors, or errors that start landing in Warnings.
func TestH0DesugarWarningsStillReachProgramWarnings(t *testing.T) {
	errs, warns := h0DiagTargetShape(t, "h0-diag-warning.no", h0DiagWarningSrc)

	if len(warns) == 0 {
		t.Fatalf("expected the deprecated `match` keyword warning in prog.Warnings, got none")
	}
	if len(errs) > 0 {
		t.Errorf("a warning-only input produced prog.Errors: %v", errs)
	}
}

// TestH0DesugarCleanInputHasNoDiagnostics is the control for the two tests
// above. Without it, "Errors is non-empty" could be satisfied by a desugar that
// always reports, and "Errors is empty" by one that never runs.
func TestH0DesugarCleanInputHasNoDiagnostics(t *testing.T) {
	errs, warns := h0DiagTargetShape(t, "h0-diag-clean.no", h0DiagCleanSrc)
	if len(errs) > 0 {
		t.Errorf("clean input produced prog.Errors: %v", errs)
	}
	if len(warns) > 0 {
		t.Errorf("clean input produced prog.Warnings: %v", warns)
	}
}

// TestShippedCorpusProducesNoH0Errors is the corpus-wide guard. The shipped
// corpus is clean, so H0 must not be rejecting anything the parser accepted. A
// hit here means H0's desugar is applying a rule the parser's desugar does not —
// i.e. the S2 equivalence gate would also be reporting it, but this runs first
// and names the file.
func TestShippedCorpusProducesNoH0Errors(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}
	var offenders []string
	walkErr := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		p := parser.New(lexer.NewCached(path, string(raw)))
		p.Filename = path
		p.SkipLowering = true
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil
		}
		parser.ASTToHIR(prog)
		if len(prog.Errors) > 0 {
			offenders = append(offenders, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", corpusRoot, walkErr)
	}
	if len(offenders) > 0 {
		const show = 10
		t.Errorf("%d corpus files produce H0 desugaring ERRORS that the parser "+
			"did not; the first %d: %v",
			len(offenders), min(show, len(offenders)), offenders[:min(show, len(offenders))])
	}
}
