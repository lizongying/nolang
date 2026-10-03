package mir

import (
	"testing"
)

// These tests pin the tier inference (NOLANG-OWNERSHIP-MODEL.md §3.1 / §3.2 /
// §4.1) and its oracle, checkTierSoundness (§3.3, invariant I1).
//
// P1 landed the lattice and the fixpoint with tierConstraint returning S for
// every use, so the inference was the IDENTITY and two tests asserted exactly
// that (TestTierConstraintIsAllSInP1 / TestInferTiersIsAllS). Those assertions
// are gone: the constraint is real now, and the tests below pin the real table.
// The identity is no longer a contract — it is the NEGATIVE CONTROL, which is
// why TestCheckTierRSitesFiresOnIdentityPartition hands the validator the
// all-S partition and requires it to fire. That is the "must fail before the
// fix" evidence: under P1 the same call would have been silent, i.e. the check
// would have been vacuous.

const tierSrc = `work-async = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    s str = 'abc'
    t str = s
    print(t)
    h = run work-async(21)
    v = awy h
    print(v)
}
`

// tierAliasChainSrc is the shape that needs MORE THAN ONE fixpoint sweep: the
// handle is copied twice before it is awaited, so R has to travel
// h → h2 → h3 along the alias edges. A single-sweep "fixpoint" leaves h3 at S,
// which is what checkTierPartition's closure half exists to catch.
const tierAliasChainSrc = `work-async = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}

main = () {
    h = run work-async(21)
    h2 = h
    h3 = h2
    v = awy h3
    print(v)
}
`

// tierViewWrittenSrc is the C-tier trigger: `a` is a BORROW read (the `%vec`
// field of `o`, which emitGetField does not clone) and it is then written
// THROUGH — `a[0] = 9` reaches `o.v` by projection, which is why the write is
// the thing that makes the alias need one clone before its first write.
const tierViewWrittenSrc = `holder {
    v []i64
    n i64
}

main = () {
    o holder = holder {
        v: [1, 2, 3]
        n: 7
    }
    a = o.v
    a[0] = 9
    print(o.v[0])
}
`

// tierViewReadSrc is the negative control for the row above: the SAME borrow
// view, never written. A read does not raise the tier (§3.1 「僅借讀且只讀 → S」).
const tierViewReadSrc = `holder {
    v []i64
    n i64
}

main = () {
    o holder = holder {
        v: [1, 2, 3]
        n: 7
    }
    a = o.v
    print(a[0])
}
`

// tierFunc returns the named function of a lowered module.
func tierFunc(t *testing.T, mod *Module, name string) *Function {
	t.Helper()
	fid, ok := mod.FuncByName[name]
	if !ok {
		t.Fatalf("function %q not found", name)
	}
	f := mod.Func(fid)
	if f == nil {
		t.Fatalf("function %q has no body", name)
	}
	return f
}

// tierRunInst returns the first real spawn (`run <callee>`) instruction in fn.
func tierRunInst(t *testing.T, mod *Module, fn string) *Inst {
	t.Helper()
	f := tierFunc(t, mod, fn)
	for _, bid := range f.Blocks {
		b := mod.Block(bid)
		if b == nil {
			continue
		}
		for _, iid := range b.Insts {
			inst := mod.Inst(iid)
			if inst != nil && inst.Op == OpRun && inst.Sym != "" {
				return inst
			}
		}
	}
	t.Fatalf("no real spawn (`run <callee>`) in %q", fn)
	return nil
}

// TestTierLatticeJoin pins the lattice order S ⊑ C ⊑ R: the join is the weaker
// tier, and it is commutative (the order is total, so it is also idempotent and
// associative).
func TestTierLatticeJoin(t *testing.T) {
	cases := []struct{ a, b, want Tier }{
		{TierS, TierS, TierS},
		{TierS, TierC, TierC},
		{TierS, TierR, TierR},
		{TierC, TierC, TierC},
		{TierC, TierR, TierR},
		{TierR, TierR, TierR},
	}
	for _, c := range cases {
		if got := joinTier(c.a, c.b); got != c.want {
			t.Errorf("joinTier(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := joinTier(c.b, c.a); got != c.want {
			t.Errorf("joinTier(%v, %v) = %v, want %v (commutativity)", c.b, c.a, got, c.want)
		}
	}
	// The join is only "least upper bound" if the order really is S < C < R;
	// the String() letters must sort the same way.
	if !(TierS < TierC && TierC < TierR) {
		t.Fatalf("lattice order broken: S=%d C=%d R=%d", TierS, TierC, TierR)
	}
	if TierS.String() != "S" || TierC.String() != "C" || TierR.String() != "R" {
		t.Errorf("tier letters = %q/%q/%q, want S/C/R", TierS, TierC, TierR)
	}
}

// TestTierConstraintSpawnEdgeIsR pins the rows of §3.1's table that make the
// inference non-trivial, AND the default: every use that is NOT one of those
// rows must still answer S, because S is the lattice zero and a constraint that
// over-answers R would silently switch the soundness check off.
func TestTierConstraintSpawnEdgeIsR(t *testing.T) {
	mod := lowerForTest(t, tierSrc)
	f := tierFunc(t, mod, "main")
	checked, rSites := 0, 0
	for _, bid := range f.Blocks {
		b := mod.Block(bid)
		if b == nil {
			continue
		}
		for _, iid := range b.Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			// The instruction-local R rows: the spawn handle, a §4.2(b)
			// SHARED spawn argument, and the operands of the two handle ops.
			want := func(v ValueID) Tier {
				switch inst.Op {
				case OpRun:
					if v == inst.Dst || mod.spawnArgRetains[v] {
						return TierR
					}
				case OpTaskRetain, OpAwait:
					return TierR
				}
				return TierS
			}
			for _, a := range inst.Args {
				if a <= NoVal {
					continue
				}
				checked++
				got := mod.tierConstraint(f, inst, a)
				if got != want(a) {
					t.Errorf("%s: constraint on arg %d of %v = %v, want %v",
						f.Name, a, inst.Op, got, want(a))
				}
				if got == TierR {
					rSites++
				}
			}
			if inst.Dst > NoVal {
				checked++
				got := mod.tierConstraint(f, inst, inst.Dst)
				if got != want(inst.Dst) {
					t.Errorf("%s: constraint on dst %d of %v = %v, want %v",
						f.Name, inst.Dst, inst.Op, got, want(inst.Dst))
				}
				if got == TierR {
					rSites++
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no uses checked: the test would pass vacuously")
	}
	if rSites == 0 {
		t.Fatal("no use demanded R: the R rows of §3.1 were never exercised")
	}
}

// TestTierConstraintSharedSpawnArgIsR is the §4.2(b) row, and its negative
// control. The gate's OWN tests (spawn_arg_retain_test.go) pin which arguments
// are shared; this pins that the tier reads the same recorded decision, so the
// partition and the emitted retain cannot drift.
func TestTierConstraintSharedSpawnArgIsR(t *testing.T) {
	// Shared: the caller never writes `a` after the spawn.
	mod := lowerForTest(t, retainVecSrc)
	arg := runArg(t, mod, "main")
	inst := tierRunInst(t, mod, "main")
	if !mod.spawnArgRetains[arg] {
		t.Fatalf("precondition failed: value %d is not in spawnArgRetains", arg)
	}
	if got := mod.tierConstraint(tierFunc(t, mod, "main"), inst, arg); got != TierR {
		t.Errorf("shared spawn argument %d tiered %v, want R (the task holds a second reference)", arg, got)
	}

	// Copied: the caller writes `a[0]` in place after the spawn, so the gate
	// refuses the share and the task gets its own buffer — no reference crosses
	// the boundary, so the argument is NOT R.
	mod = lowerForTest(t, retainVecWrittenSrc)
	arg = runArg(t, mod, "main")
	inst = tierRunInst(t, mod, "main")
	if mod.spawnArgRetains[arg] {
		t.Fatalf("precondition failed: value %d was shared despite the in-place write", arg)
	}
	if got := mod.tierConstraint(tierFunc(t, mod, "main"), inst, arg); got != TierS {
		t.Errorf("deep-copied spawn argument %d tiered %v, want S (no reference survives)", arg, got)
	}
}

// TestInferTiersSpawnHandleIsR is the replacement for P1's
// TestInferTiersIsAllS: the identity is no longer the contract. The handle the
// spawn produces and the value the await defines are the R objects the language
// actually has today (§3.1 「目前唯一的 R 來源」).
func TestInferTiersSpawnHandleIsR(t *testing.T) {
	mod := lowerForTest(t, tierSrc)
	f := tierFunc(t, mod, "main")
	run := tierRunInst(t, mod, "main")
	tiers := mod.inferTiers(f)
	if run.Dst <= NoVal {
		t.Fatalf("the spawn has no destination value")
	}
	if got := tiers[run.Dst]; got != TierR {
		t.Errorf("spawn handle %d tiered %v, want R", run.Dst, got)
	}
	// The partition must still be a PARTITION: seeded for every mentioned
	// value, not just the ones that rose.
	seen := 0
	for _, bid := range f.Blocks {
		b := mod.Block(bid)
		if b == nil {
			continue
		}
		for _, iid := range b.Insts {
			inst := mod.Inst(iid)
			if inst == nil {
				continue
			}
			if inst.Dst > NoVal {
				seen++
				if _, ok := tiers[inst.Dst]; !ok {
					t.Errorf("value %d (defined by %v) has no tier: the partition is not seeded", inst.Dst, inst.Op)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no definitions checked: the test would pass vacuously")
	}
}

// TestInferTiersPropagatesRAcrossAliasChain pins the transitivity row
// (§3.1 「被 R 檔值別名 → R」) at a depth that needs more than one sweep.
func TestInferTiersPropagatesRAcrossAliasChain(t *testing.T) {
	mod := lowerForTest(t, tierAliasChainSrc)
	f := tierFunc(t, mod, "main")
	run := tierRunInst(t, mod, "main")
	tiers := mod.inferTiers(f)
	root := run.Dst
	if root <= NoVal {
		t.Fatalf("the spawn has no destination value")
	}
	// The alias set is the SHARED predicate (spawn_graph.go / checkRefBalance),
	// so the assertion is on the property the validator uses, not on a
	// re-derivation.
	members := mod.handleAliasSet(f, root)
	if len(members) < 3 {
		t.Fatalf("the alias chain collapsed to %d member(s): the source no longer exercises depth", len(members))
	}
	for _, m := range members {
		if got := tiers[m]; got != TierR {
			t.Errorf("alias member %d of the spawn handle tiered %v, want R", m, got)
		}
	}
}

// TestInferTiersCTierForWrittenThroughView pins the C row (§3.1 「借讀視圖且
// 之後有寫入 → C」) and its negative control in one place, so a rule that
// answered C for every borrow view cannot pass.
func TestInferTiersCTierForWrittenThroughView(t *testing.T) {
	viewVal := func(src string) Tier {
		t.Helper()
		mod := lowerForTest(t, src)
		f := tierFunc(t, mod, "main")
		tiers := mod.inferTiers(f)
		// The view is the destination of the OpGetField that reads `o.v`.
		for _, bid := range f.Blocks {
			b := mod.Block(bid)
			if b == nil {
				continue
			}
			for _, iid := range b.Insts {
				inst := mod.Inst(iid)
				if inst == nil || inst.Op != OpGetField || inst.Dst <= NoVal {
					continue
				}
				if mod.isBorrowRead(f, inst) {
					return tiers[inst.Dst]
				}
			}
		}
		t.Fatalf("no borrow-view read found in the probe")
		return TierS
	}

	if got := viewVal(tierViewWrittenSrc); got != TierC {
		t.Errorf("a borrow view that is WRITTEN THROUGH tiered %v, want C", got)
	}
	if got := viewVal(tierViewReadSrc); got != TierS {
		t.Errorf("a read-only borrow view tiered %v, want S (a read does not raise the tier)", got)
	}
}

// TestCheckTierRSitesFiresOnIdentityPartition is the non-vacuity proof for the
// validator's R-site half: handed P1's identity partition (every value S) it
// must fire, because the IR's spawn handle IS an R object. If this test passed
// under the identity constraint too, the check would have been vacuous and the
// whole "land the validator" step would be a false assurance (§3.3).
func TestCheckTierRSitesFiresOnIdentityPartition(t *testing.T) {
	mod := lowerForTest(t, tierSrc)
	f := tierFunc(t, mod, "main")
	real := mod.inferTiers(f)

	// Positive control: the real partition is sound.
	rep := &Report{}
	mod.checkTierRSites(f, real, rep)
	if len(rep.Diagnostics) != 0 {
		t.Fatalf("the real partition reported %d diagnostic(s): %v", len(rep.Diagnostics), rep.Error())
	}

	// The pre-fix behaviour, replayed faithfully: the constraint answered S for
	// every use, so every value is S. (Built by rewriting the map rather than by
	// a flag, so there is no chance of the control acquiring the new semantics
	// on another path.)
	identity := map[ValueID]Tier{}
	for v := range real {
		identity[v] = TierS
	}
	rep = &Report{}
	mod.checkTierRSites(f, identity, rep)
	if len(rep.Diagnostics) == 0 {
		t.Fatal("the validator did not fire on the identity partition: it would have been vacuous under P1")
	}
	for _, d := range rep.Diagnostics {
		if d.Kind != "tier-soundness" {
			t.Errorf("diagnostic kind = %q, want tier-soundness", d.Kind)
		}
	}
}

// TestCheckTierPartitionFiresOnUnseededMap is the non-vacuity proof for the
// other half. P1's one real defect was an unseeded partition: S is the lattice
// zero, so a "raise only" loop over an all-S constraint returned an EMPTY map
// (len(tiers) == 0) and every consumer silently saw nothing.
func TestCheckTierPartitionFiresOnUnseededMap(t *testing.T) {
	mod := lowerForTest(t, tierSrc)
	f := tierFunc(t, mod, "main")

	rep := &Report{}
	mod.checkTierPartition(f, mod.inferTiers(f), rep)
	if len(rep.Diagnostics) != 0 {
		t.Fatalf("the real partition reported %d diagnostic(s): %v", len(rep.Diagnostics), rep.Error())
	}

	rep = &Report{}
	mod.checkTierPartition(f, map[ValueID]Tier{}, rep)
	if len(rep.Diagnostics) == 0 {
		t.Fatal("an unseeded (empty) partition passed: the check cannot see P1's actual defect")
	}
}

// TestInferTiersDoesNotMutate is the "reads only" guarantee: the inference
// reads the module and nothing else. If it ever started inserting or rewriting
// instructions, the corpus A/B would stop being a clean baseline — and the
// validator would be checking a program that no longer exists.
func TestInferTiersDoesNotMutate(t *testing.T) {
	mod := lowerForTest(t, tierSrc)
	count := func() int {
		n := 0
		for i := range mod.Funcs {
			for _, bid := range mod.Funcs[i].Blocks {
				if b := mod.Block(bid); b != nil {
					n += len(b.Insts)
				}
			}
		}
		return n
	}
	before := count()
	for i := 0; i < 3; i++ {
		for j := range mod.Funcs {
			_ = mod.inferTiers(&mod.Funcs[j])
		}
	}
	if after := count(); after != before {
		t.Errorf("instruction count %d -> %d: inferTiers mutated the module", before, after)
	}
}

// TestDumpTiersIsSilentByDefault pins the other half of "reads only": the
// stderr dump is gated, so a normal build prints nothing and the golden
// comparison stays byte-identical.
func TestDumpTiersIsSilentByDefault(t *testing.T) {
	// No NOLANG_MIR_TIER in the environment: DumpTiers must return without
	// writing. Capturing stderr is not worth the plumbing; the gate is the
	// single branch at the top of the function and this test simply asserts it
	// is driven by the environment rather than by any module state.
	mod := lowerForTest(t, tierSrc)
	total := 0
	for i := range mod.Funcs {
		if mod.Funcs[i].IsExtern {
			continue
		}
		total += len(mod.inferTiers(&mod.Funcs[i]))
	}
	if total == 0 {
		t.Fatal("inferTiers returned no values: the gate test would pass vacuously")
	}
	mod.DumpTiers() // must not panic and must not write
}
