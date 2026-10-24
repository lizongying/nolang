package hir_test

// ─────────────────────────────────────────────────────────────────────────────
// S0 instrumentation for docs/design/nolang-parser-lowering-invariants.md.
//
// Invariant P2 / constraint C4: HIR must contain zero sugar. Gate G3 declares
// the sugar kinds and requires their corpus-wide count to be 0, with an
// anti-vacuity half (design doc §7): the erasure has to be demonstrated, not
// assumed — a kind whose sugar is never even fed into H0 would make the zero
// count meaningless.
//
// History: this file originally held a census TRIPWIRE pinning the
// pre-migration state (KGrouped > 0 = W5, KUnwrapAssign == 0 = W4) plus a
// skipped placeholder for the real gate. W5 landed 2026-10-21 (H0 erases
// parentheses at tohir, §10.2), which drove the census's KGrouped count to 0.
// Per the tripwire's own instruction the census is deleted and the real gate
// below is now active; the census's residual checks are folded into it.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// sugarHIRKinds names the HIR node kinds the design doc rules SUGAR (design doc
// §5). Each must be consumed by H0 and therefore reach 0 in HIR. Both entries
// are tombstone guards today: the erasure already happened, so these rows pin
// the zero and name where the erasure proof lives.
var sugarHIRKinds = map[hir.Kind]string{
	hir.KUnwrapAssign: "§5 — `x ?= e`; the parser's lowering pass expands it before " +
		"ParseProgram returns (W4), so nothing survives for H0. Its erasure is " +
		"proven parser-side (parser/sugar_seam_test.go seamProbes[\"unwrap\"]); " +
		"here it is a keep-at-zero tombstone guard",
	hir.KGrouped: "§5 — parentheses are pure syntax with no semantics (§10.2); H0 " +
		"erases them via a transparent passthrough in tohir (W5 landed " +
		"2026-10-21). The kind stays a tombstone to avoid renumbering KindNames",
}

func sugarKindNames() []hir.Kind {
	out := make([]hir.Kind, 0, len(sugarHIRKinds))
	for k := range sugarHIRKinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

var programReflectType = reflect.TypeOf(parser.Program{})

// countASTNodeType counts AST nodes of the given type name by reflective walk.
// The AST is a DAG with shared nodes and whole subtrees hanging off unexported
// fields, so the walk mirrors parser's sugarNodeCensus (sugar_seam_test.go):
// reflection reaches them read-only. Used only by the G3 anti-vacuity half to
// prove the sugar was fed INTO H0 before the zero count means anything.
func countASTNodeType(prog *parser.Program, want string) int {
	seen := map[uintptr]bool{}
	n := 0
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
			if v.Elem().Type().Name() == want {
				n++
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
					case "Sem", "Warnings", "BuiltinFuncNames", "TrailingComments", "FmtTrivia":
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
	return n
}

// TestSugarHIRKindsAbsentAfterH0 is gate G3 (constraint C4), active since the
// W5 erasure landed (2026-10-21). Two halves:
//
//	G3.1 — over the std corpus, every declared sugar kind has count 0.
//	G3.2 — anti-vacuity (§7): prove ERASURE rather than ABSENCE. A
//	       parenthesized probe must parse into GroupedExpression AST nodes
//	       (the sugar really is fed into H0) and still lower to zero KGrouped.
//	       Without this half the gate would also pass on a corpus with no
//	       parentheses at all, which would prove nothing.
func TestSugarHIRKindsAbsentAfterH0(t *testing.T) {
	// G3.1 — corpus-wide absence.
	mods := stdModules(t)
	total := map[hir.Kind]int{}
	var nodes int
	for _, m := range mods {
		for k, n := range m.Pkg.KindCounts() {
			total[k] += n
			nodes += n
		}
	}
	if nodes < 100000 {
		t.Fatalf("only %d HIR nodes built from the std corpus; the gate would be "+
			"vacuous", nodes)
	}
	for _, k := range sugarKindNames() {
		if n := total[k]; n != 0 {
			t.Errorf("G3: %s: %d node(s) over the std corpus; P2/C4 requires HIR to "+
				"contain zero sugar — H0 stopped consuming it", k, n)
		}
	}

	// G3.2 — anti-vacuity: the erasure must be visible on a controlled probe.
	const (
		probeSrc = "x = (1 + 2) * 3\n"
		probe    = "g3-parens"
	)
	p := parser.New(lexer.NewCached(probe+".no", probeSrc))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("probe %q no longer parses: %v", probe, errs)
	}
	if got := countASTNodeType(prog, "GroupedExpression"); got == 0 {
		t.Fatalf("anti-vacuity: the probe's AST holds no GroupedExpression, so the " +
			"erasure half of this gate cannot fail; fix the probe (it must feed " +
			"paren sugar into H0)")
	}
	pkg := parser.ASTToHIR(prog)
	probeCount := pkg.KindCounts()[hir.KGrouped]
	if probeCount != 0 {
		t.Errorf("anti-vacuity: H0 emitted %d KGrouped from an AST that contains "+
			"GroupedExpression; the §10.2/W5 erasure regressed (tohir must pass the "+
			"inner expression through)", probeCount)
	}

	census := make([]string, 0, len(sugarHIRKinds))
	for _, k := range sugarKindNames() {
		census = append(census, k.String()+"="+strconv.Itoa(total[k]))
	}
	t.Logf("%d nodes across %d std modules; sugar census: %v; probe: AST "+
		"GroupedExpression → %d KGrouped", nodes, len(mods), census, probeCount)
}
