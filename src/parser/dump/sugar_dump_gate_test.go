package dump

// ─────────────────────────────────────────────────────────────────────────────
// C8: the sugar shape must be dumpable, and the non-mutation claim must have
// coverage over it.
//
// Two blind spots lived here, and both had the same shape: the only corpus
// parser (parseCorpus, dump_completeness_test.go) uses the DEFAULT
// ParseProgram, i.e. the already-lowered tree, which contains no SurfaceMatch.
// So every dump-completeness gate in this package was structurally unable to
// see the one node kind that only exists before lowering.
//
// 1. dump.go had no `case *parser.SurfaceMatch`, so on the sugar shape 168
//    corpus files produced 626 `<unknown *parser.SurfaceMatch>` lines and every
//    arm subtree was invisible to `no ast` — the L2 self-host oracle. The gap
//    was recorded as an exemption in TestSugarASTIsDumpable with the reason
//    "unreachable today because lowering.go always expands SurfaceMatch before
//    `no ast` runs", which is true and also the whole problem: it was invisible
//    *because* nothing looked. Now closed (the case + a matchArm helper, and
//    the exemption deleted); these gates keep it closed.
//
// 2. TestASTIsFrozenAcrossHIRLowering (ast_freeze_test.go) is gate G1's C6
//    half — "H0 must not mutate the AST". desugarSurfaceMatch's own comment
//    claimed it "runs over the sugar shape", but it does not: it walks the
//    lowered corpus, where the match nodes are already gone. The non-mutation
//    claim for the ONE desugar that runs inside H0 therefore had NO coverage at
//    all — and before fix 1 it could not have had any, because a mutated arm
//    body would have dumped as `<unknown *parser.SurfaceMatch>` both before and
//    after. TestASTIsFrozenAcrossHIRLoweringSugar is the missing half.
//
// Both gates re-parse the corpus with SkipLowering, which is the sugar shape.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// parseCorpusSugar parses every *.no under corpusRoot with SkipLowering, i.e.
// the sugar shape: no sugar node has been expanded, so SurfaceMatch (and
// everything else) survives to the dump. Files the parser rejects are skipped,
// matching parseCorpus's contract.
//
// This exists because parseCorpus cannot serve these gates: its default
// ParseProgram lowers the tree first, so the corpus it hands over contains no
// sugar nodes at all and every assertion below would pass vacuously.
func parseCorpusSugar(t *testing.T) map[string]*parser.Program {
	t.Helper()
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("corpus not found at %s: %v", corpusRoot, err)
	}
	out := map[string]*parser.Program{}
	err := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		l := lexer.NewCached(path, string(src))
		p := parser.New(l)
		p.Filename = filepath.Base(path)
		p.SkipLowering = true
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil
		}
		out[path] = prog
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", corpusRoot, err)
	}
	return out
}

// TestSugarShapeCorpusHasNoUnknownNodes is the corpus-wide form of dump rule 1
// on the SUGAR shape. It is the gate that was missing when dump.go had no
// SurfaceMatch case.
//
// Non-vacuity, both directions:
//   - it requires a minimum number of programs, so "no unknowns" cannot be
//     satisfied by a corpus that parsed nothing;
//   - it requires that the corpus actually CONTAINS SurfaceMatch nodes. Before
//     the case was added this gate reported 626 `<unknown *parser.SurfaceMatch>`
//     lines across 168 files; asserting the positive count means deleting the
//     case fails the test even though the unknown count would drop to zero.
func TestSugarShapeCorpusHasNoUnknownNodes(t *testing.T) {
	programs := parseCorpusSugar(t)
	if len(programs) < 100 {
		t.Fatalf("only %d programs parsed in the sugar shape; too few for this gate to "+
			"mean anything", len(programs))
	}

	const minSurfaceMatchFiles = 100
	var offenders []string
	badTypes := map[string]int{}
	var smFiles int
	for path, prog := range programs {
		out := Dump(prog)
		if strings.Contains(out, "SurfaceMatch{") {
			smFiles++
		}
		if !strings.Contains(out, "<unknown ") {
			continue
		}
		offenders = append(offenders, path)
		for _, line := range strings.Split(out, "\n") {
			if i := strings.Index(line, "<unknown "); i >= 0 {
				badTypes[strings.TrimSpace(line[i:])]++
			}
		}
	}

	// Positive non-vacuity: the sugar shape must actually contain the node kind
	// this gate exists for. If SurfaceMatch disappears from the corpus the gate
	// would report "0 unknowns" for the wrong reason.
	if smFiles < minSurfaceMatchFiles {
		t.Fatalf("only %d of %d programs contain a SurfaceMatch in the sugar shape; this "+
			"gate exists to cover that kind and would otherwise pass vacuously",
			smFiles, len(programs))
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		types := make([]string, 0, len(badTypes))
		for k, n := range badTypes {
			types = append(types, k+" ("+itoa(n)+")")
		}
		sort.Strings(types)
		shown := offenders
		if len(shown) > 10 {
			shown = shown[:10]
		}
		t.Errorf("dumpNode fell through to `default` on %d file(s) in the SUGAR shape; "+
			"those nodes' entire subtrees are invisible to `no ast` and to the L2 "+
			"self-host oracle.\ndistinct types: %s\nfirst offenders:\n  %s",
			len(offenders), strings.Join(types, ", "), strings.Join(shown, "\n  "))
	}
	t.Logf("sugar shape: %d programs, %d contain SurfaceMatch, 0 unknown node types",
		len(programs), smFiles)
}

// dumpNodeTree dumps prog and drops the `Program{...}` header line.
//
// The header renders only `warnings: N`, and ASTToHIRWithMap legitimately folds
// H0-side desugaring WARNINGS into prog.Warnings — a side-channel write that
// §1.4 of the design doc explicitly permits ("允许被写的只有与节点解耦的副表 …
// Warnings"). Comparing the raw dump would therefore report a C6 violation for
// every program whose match desugaring emits a warning, which is not a mutation
// of the AST at all.
//
// ⚠️ The lowered-shape twin (TestASTIsFrozenAcrossHIRLowering in
// ast_freeze_test.go) compares the raw dump and gets away with it only because
// its shape cannot trigger H0 diagnostics: on the lowered corpus the
// SurfaceMatch nodes are already gone before ASTToHIR runs, so nothing can emit
// an H0 warning. That is luck, not design — the sugar shape is where the
// diagnostics appear, and this comparator is what makes the gate measure the
// node tree rather than the side channel.
func dumpNodeTree(prog *parser.Program) string {
	out := Dump(prog)
	if i := strings.IndexByte(out, '\n'); i >= 0 && strings.HasPrefix(out, "Program{") {
		return out[i+1:]
	}
	return out
}

// TestASTIsFrozenAcrossHIRLoweringSugar is gate G1's C6 half over the SUGAR
// shape: ASTToHIR must not mutate the tree when the tree still carries sugar.
//
// The lowered-shape twin in ast_freeze_test.go cannot cover this. On the
// lowered corpus the SurfaceMatch nodes are already gone, so the one desugar
// that runs INSIDE H0 (desugarSurfaceMatch) is never exercised by it. And
// before dump.go could render SurfaceMatch, this gate could not have existed
// even in principle: a mutated arm body dumps as `<unknown *parser.SurfaceMatch>`
// on both sides, so before == after and the mutation is invisible.
func TestASTIsFrozenAcrossHIRLoweringSugar(t *testing.T) {
	programs := parseCorpusSugar(t)
	if len(programs) < 100 {
		t.Fatalf("only %d programs parsed in the sugar shape; too few for this gate to "+
			"mean anything", len(programs))
	}

	paths := make([]string, 0, len(programs))
	for p := range programs {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var checked, withSugar int
	for _, path := range paths {
		prog := programs[path]
		before := dumpNodeTree(prog)
		if strings.Contains(before, "SurfaceMatch{") {
			withSugar++
		}
		parser.ASTToHIR(prog)
		after := dumpNodeTree(prog)
		if before != after {
			t.Errorf("%s: the AST changed across ASTToHIR in the SUGAR shape "+
				"(C6 — H0 must not mutate the tree, and desugarSurfaceMatch must "+
				"lower on copies)\n%s", path, firstDiff(before, after))
			continue
		}
		checked++
	}

	if withSugar < minSurfaceMatchFiles {
		t.Fatalf("only %d of %d programs carried a SurfaceMatch before lowering; the "+
			"non-mutation claim for desugarSurfaceMatch would be untested",
			withSugar, checked)
	}
	t.Logf("sugar shape: %d programs AST-identical across ASTToHIR (%d carried a "+
		"SurfaceMatch, so desugarSurfaceMatch's copy-on-desugar claim is covered)",
		checked, withSugar)
}

const minSurfaceMatchFiles = 100

// itoa is a local strconv.Itoa to keep this file's imports to what it uses.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
