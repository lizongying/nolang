package hir_test

// ─────────────────────────────────────────────────────────────────────────────
// S0 instrumentation for docs/design/nolang-parser-lowering-invariants.md.
//
// Invariant P2 / constraint C4: HIR must contain zero sugar. Gate G3 declares
// the sugar kinds and requires their corpus-wide count to be 0, with an
// anti-vacuity half (design doc §7): each declared kind must be *produced* by
// the pre-H1 snapshot — a kind that is never produced means the exemption is
// dead and the entry must be deleted.
//
// Today neither half of C4 holds, so the gate is skipped and replaced by a
// census that PINS the pre-migration state. The census is a tripwire, not a
// formality: when S2 lands, KGrouped drops to 0 and the census fails, which is
// the signal to delete the assertion and enable the real gate.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"sort"
	"strconv"
	"testing"

	"github.com/lizongying/nolang/hir"
)

// sugarHIRKinds names the HIR node kinds the design doc rules SUGAR (design doc
// §5). Each must be consumed by H0 and therefore reach 0 in HIR.
var sugarHIRKinds = map[hir.Kind]string{
	hir.KUnwrapAssign: "§5 — `x ?= e`; the kind is deleted from HIR once H0 consumes the sugar node",
	hir.KGrouped:      "§5 — parentheses; pure syntax with no semantics, so H0 must erase it (§10.2)",
}

func sugarKindNames() []hir.Kind {
	out := make([]hir.Kind, 0, len(sugarHIRKinds))
	for k := range sugarHIRKinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// TestSugarHIRKindCensus pins the PRE-migration census over the std corpus and
// documents the two violations the migration has to remove (design doc §2.3):
//
//	W5  KGrouped IS produced — pure-paren sugar survives into HIR
//	W4  KUnwrapAssign is NOT produced — the kind is already dead, because the
//	    parser desugars `?=` before ParseProgram returns
//
// Both facts change at S2; this test is what forces that update to be
// deliberate rather than silent.
func TestSugarHIRKindCensus(t *testing.T) {
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
		t.Fatalf("only %d HIR nodes built from the std corpus; the census would be "+
			"vacuous", nodes)
	}

	// W5 — the violation the migration must remove.
	if total[hir.KGrouped] == 0 {
		t.Errorf("KGrouped count is 0 over the std corpus. Either the corpus stopped "+
			"producing parentheses, or S2 landed and erased the kind: if it landed, "+
			"delete this assertion and enable TestSugarHIRKindsAbsentAfterH0")
	}
	// W4 — the dead kind.
	if n := total[hir.KUnwrapAssign]; n != 0 {
		t.Errorf("KUnwrapAssign count is %d over the std corpus; it is supposed to be "+
			"desugared inside ParseProgram, before H0 ever sees it", n)
	}

	census := make([]string, 0, len(sugarHIRKinds))
	for _, k := range sugarKindNames() {
		census = append(census, k.String()+"="+strconv.Itoa(total[k]))
	}
	t.Logf("%d nodes across %d std modules; sugar census: %v", nodes, len(mods), census)
}

// TestSugarHIRKindsAbsentAfterH0 is gate G3 (constraint C4).
//
// SKIPPED until S2. When enabled it must assert, over the corpus:
//
//	for each kind in sugarHIRKinds: total[kind] == 0
//
// and carry the anti-vacuity half from design doc §7: on a snapshot taken AFTER
// H0 and BEFORE H1, every declared kind must be non-zero — otherwise the
// exemption is dead and the entry has to go. hir/golden_test.go's KUnwrapAssign
// exemption is already an instance of this pattern, and it is currently
// *stale*: that entry claims the kind never reaches HIR, which is true, but the
// kind is then unreachable by any corpus and the entry cannot be validated.
func TestSugarHIRKindsAbsentAfterH0(t *testing.T) {
	t.Skip("S2 (docs/design/nolang-parser-lowering-invariants.md §8): enable once H0 " +
		"consumes every sugar node and the kind set is genuinely empty in HIR")
}
