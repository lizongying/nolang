package parser

// ─────────────────────────────────────────────────────────────────────────────
// S1 seam instrumentation for docs/design/nolang-parser-lowering-invariants.md.
//
// Parser.SkipLowering + (*Parser).LowerSurface exist so that lowering can be
// moved out of ParseProgram (step S1) without changing what it does. Two claims
// have to hold for that to be worth anything:
//
//	1. the sugar really is preserved on the deferred route (otherwise there is
//	   nothing for the checker to inspect, and S1's premise evaporates), and
//	2. the two routes are genuinely distinguishable, so the equivalence gate in
//	   src/parser/dump/lowering_seam_test.go is not proving a tautology.
//
// Measuring (2) turns out to require a distinction the design doc's §4.1 table
// makes only in prose: the sugar nodes split into TWO tiers by *who removes
// them*.
//
//	removedByParser — the parser's lowering pass expands it before ParseProgram
//	                  returns. These are the P1 violations (V1): the AST the
//	                  caller sees no longer mirrors the source.
//	removedByH0     — it survives the parser intact and is only erased by H0
//	                  (step S2). These do not violate P1; they violate P2 by
//	                  surviving into HIR (KGrouped, KMultiAssign).
//
// Getting that wrong is easy — the first version of this file asserted "the
// default route contains zero sugar" and failed immediately on GroupedExpression
// and MultiAssignStatement, which are supposed to survive. The tier table below
// makes the distinction explicit and testable.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// sugarTier records which stage removes a sugar type today.
type sugarTier int

const (
	removedByParser sugarTier = iota // the parser's lowering pass expands it (V1)
	removedByH0                      // it survives the parser; H0 must erase it (S2)
)

func (t sugarTier) String() string {
	if t == removedByParser {
		return "parser-lowering"
	}
	return "H0"
}

// sugarTiers is keyed by the same AST type names as sugarASTTypes. Keeping the
// two in lockstep is enforced by TestSugarTierClassificationIsComplete: a new
// sugar node must declare who removes it, so it cannot be added without someone
// deciding whether it is a P1 violation or an H0 obligation.
var sugarTiers = map[string]sugarTier{
	"SurfaceMatch":          removedByParser,
	"UnwrapAssignStatement": removedByParser,
	"GroupedExpression":     removedByH0,
	"MultiAssignStatement":  removedByH0,
}

// TestSugarTierClassificationIsComplete keeps sugarTiers and sugarASTTypes in
// step, in both directions.
func TestSugarTierClassificationIsComplete(t *testing.T) {
	if len(sugarTiers) == 0 {
		t.Fatal("sugarTiers is empty, so this gate cannot fail")
	}
	var missing, stale []string
	for name := range sugarASTTypes {
		if _, ok := sugarTiers[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range sugarTiers {
		if _, ok := sugarASTTypes[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("sugar node type(s) with no tier in sugarTiers: %v\n"+
			"decide whether the parser's lowering pass expands it (removedByParser, a P1 "+
			"violation) or whether it survives to H0 (removedByH0)", missing)
	}
	if len(stale) > 0 {
		t.Errorf("sugarTiers names type(s) that are not registered sugar: %v", stale)
	}
}

var programReflectType = reflect.TypeOf(Program{})

// sugarNodeCensus walks an AST and tallies every node type that sugarASTTypes
// classifies as sugar.
//
// The walk is reflective rather than hand-written for two reasons: the AST is a
// DAG with shared nodes (SurfaceMatch.Matched is also reachable from the tree —
// the same sharing that forces lowerer.visited in lowering.go), and whole
// subtrees hang off *unexported* fields (a SurfaceMatch's bodies live in
// MatchArm.body). A hand-written walker would miss them; reflection reaches them
// even from a read-only field.
func sugarNodeCensus(prog *Program) map[string]int {
	counts := map[string]int{}
	seen := map[uintptr]bool{}

	var walk func(v reflect.Value, depth int)
	walk = func(v reflect.Value, depth int) {
		if !v.IsValid() || depth > 400 {
			return
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			addr := v.Pointer()
			if seen[addr] {
				return
			}
			seen[addr] = true
			if name := v.Elem().Type().Name(); sugarASTTypes[name] != "" {
				counts[name]++
			}
			walk(v.Elem(), depth+1)
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), depth+1)
			}
		case reflect.Struct:
			isProgram := v.Type() == programReflectType
			for i := 0; i < v.NumField(); i++ {
				if isProgram {
					// Side tables, not part of the node tree. Sem in particular
					// is a map keyed by Node and would dominate the walk.
					switch v.Type().Field(i).Name {
					case "Sem", "Warnings", "BuiltinFuncNames":
						continue
					}
				}
				walk(v.Field(i), depth+1)
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				walk(it.Value(), depth+1)
			}
		}
	}

	walk(reflect.ValueOf(prog), 0)
	return counts
}

func sugarCensusTotal(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

func sugarCensusString(counts map[string]int) string {
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+"="+strconv.Itoa(counts[n]))
	}
	return strings.Join(parts, " ")
}

// seamProbes each contain exactly one kind of sugar. They are deliberately tiny:
// the point is to show the two routes differ in a controlled way, not to cover
// the language.
var seamProbes = map[string]struct {
	src   string
	sugar string
}{
	"match": {
		src:   "v = 1\nv: {\n    1 -> print('one')\n    -> print('other')\n}\n",
		sugar: "SurfaceMatch",
	},
	"parens": {
		src:   "x = (1 + 2)\n",
		sugar: "GroupedExpression",
	},
	"multi-assign": {
		src:   "f = () (a i64, b i64) {\n    a = 1\n    b = 2\n}\nx, y = f()\n",
		sugar: "MultiAssignStatement",
	},
	"unwrap": {
		src:   "g = () (r ?i64) {\n    x ?= h()\n    r = 1\n}\n",
		sugar: "UnwrapAssignStatement",
	},
}

// TestSeamProbesAreControlled checks the probe corpus itself before it is used as
// evidence: on the deferred route each probe must contain its declared sugar
// node, and on the default route it must be gone or retained according to its
// tier.
func TestSeamProbesAreControlled(t *testing.T) {
	for name, probe := range seamProbes {
		tier, ok := sugarTiers[probe.sugar]
		if !ok {
			t.Fatalf("probe %q declares sugar %q, which has no tier", name, probe.sugar)
		}

		deferred := New(lexer.NewCached(name+".no", probe.src))
		deferred.SkipLowering = true
		before := sugarNodeCensus(deferred.ParseProgram())
		if errs := deferred.Errors(); len(errs) > 0 {
			t.Fatalf("probe %q no longer parses: %v", name, errs)
		}
		if before[probe.sugar] == 0 {
			t.Errorf("probe %q: SkipLowering preserved no %s node (%s); the probe no "+
				"longer exercises what it claims", name, probe.sugar, sugarCensusString(before))
		}

		def := New(lexer.NewCached(name+".no", probe.src))
		after := sugarNodeCensus(def.ParseProgram())
		if errs := def.Errors(); len(errs) > 0 {
			t.Fatalf("probe %q no longer parses on the default route: %v", name, errs)
		}

		switch tier {
		case removedByParser:
			if after[probe.sugar] != 0 {
				t.Errorf("probe %q: %s is tiered removedByParser but the default route "+
					"still holds %d of them (%s)", name, probe.sugar, after[probe.sugar],
					sugarCensusString(after))
			}
		case removedByH0:
			if after[probe.sugar] == 0 {
				t.Errorf("probe %q: %s is tiered removedByH0 but the default route already "+
					"removed it; either the tier is wrong or the parser started expanding it",
					name, probe.sugar)
			}
		}
		t.Logf("probe %q (%s, removed by %s): deferred %s → default %s",
			name, probe.sugar, tier, sugarCensusString(before), sugarCensusString(after))
	}
}

// TestCorpusSugarCensus is the P1-side counterpart of the HIR census in
// src/hir/sugar_kinds_test.go. It puts a hard number on the sugar the corpus
// contains before lowering, and splits it by tier:
//
//   - removedByParser types must be present before lowering and absent after —
//     that is the P1 violation, quantified.
//   - removedByH0 types must be present both before AND after, because the
//     parser does not touch them — that is the P2 obligation still outstanding.
//
// The second half is the AST-side evidence for the HIR census's `grouped=11021`:
// the parentheses are not created by lowering, they are simply passed through.
func TestCorpusSugarCensus(t *testing.T) {
	root := filepath.Join("..", "..", "tests")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("corpus not found at %s: %v", root, err)
	}

	before := map[string]int{}
	after := map[string]int{}
	var files int
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		p := New(lexer.NewCached(path, string(src)))
		p.SkipLowering = true
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			return nil // deliberate error cases
		}
		for k, n := range sugarNodeCensus(prog) {
			before[k] += n
		}

		q := New(lexer.NewCached(path, string(src)))
		lowered := q.ParseProgram()
		if len(q.Errors()) > 0 {
			return nil
		}
		for k, n := range sugarNodeCensus(lowered) {
			after[k] += n
		}

		files++
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", root, walkErr)
	}
	if files < 200 {
		t.Fatalf("only %d corpus files parsed cleanly; the census would be vacuous", files)
	}

	var parserTotal, h0Total int
	for name, tier := range sugarTiers {
		switch tier {
		case removedByParser:
			if before[name] == 0 {
				t.Errorf("%s is tiered removedByParser but the corpus contains none before "+
					"lowering; the entry is dead", name)
			}
			if after[name] != 0 {
				t.Errorf("%s is tiered removedByParser but %d survived the parser's "+
					"lowering pass", name, after[name])
			}
			parserTotal += before[name]
		case removedByH0:
			if before[name] == 0 {
				t.Errorf("%s is tiered removedByH0 but the corpus contains none before "+
					"lowering; the entry is dead", name)
			}
			if after[name] == 0 {
				t.Errorf("%s is tiered removedByH0 but the parser's lowering pass removed "+
					"all of them; either the tier is wrong or the parser started expanding it",
					name)
			}
			h0Total += after[name]
		}
	}
	if parserTotal == 0 || h0Total == 0 {
		t.Fatalf("census split is degenerate (parser=%d, h0=%d); the corpus no longer "+
			"exercises both tiers", parserTotal, h0Total)
	}

	t.Logf("%d files; before lowering: %s | after the parser's lowering pass: %s "+
		"| parser-expanded %d, left for H0 %d",
		files, sugarCensusString(before), sugarCensusString(after), parserTotal, h0Total)
}
