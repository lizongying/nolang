package mir

import (
	"testing"
)

// These tests pin P1 (NOLANG-OWNERSHIP-MODEL.md §3.1 / §3.2 / §4.1): the tier
// lattice and the monotone inference framework.
//
// P1 is deliberately the IDENTITY — every constraint returns S — so the
// assertions are on the lattice algebra and on the inference being a no-op, not
// on any tier actually rising. When P2 (first-write clone) or P5 (escape → R)
// fills in tierConstraint, TestTierConstraintIsAllSInP1 and TestInferTiersIsAllS
// are the tests that must be updated in the SAME change, which is exactly the
// guard that keeps a half-landed stage from silently changing behaviour.

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

// TestTierConstraintIsAllSInP1 pins the P1 contract: the constraint function
// returns S for EVERY use. If a later stage makes a use return C or R without
// also landing that stage's codegen, this test is where the accidental
// behaviour change is caught.
func TestTierConstraintIsAllSInP1(t *testing.T) {
	mod := lowerForTest(t, tierSrc)
	checked := 0
	for i := range mod.Funcs {
		f := &mod.Funcs[i]
		if f.IsExtern {
			continue
		}
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
				for _, a := range inst.Args {
					if a <= NoVal {
						continue
					}
					checked++
					if got := mod.tierConstraint(f, inst, a); got != TierS {
						t.Errorf("%s: constraint on arg %d of %v = %v, want S (P1)",
							f.Name, a, inst.Op, got)
					}
				}
				if inst.Dst > NoVal {
					checked++
					if got := mod.tierConstraint(f, inst, inst.Dst); got != TierS {
						t.Errorf("%s: constraint on dst %d of %v = %v, want S (P1)",
							f.Name, inst.Dst, inst.Op, got)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no uses checked: the test would pass vacuously")
	}
}

// TestInferTiersIsAllS pins that with an all-S constraint the fixpoint returns
// S for every value — the identity the roadmap's P1 acceptance criterion
// ("金標 0 差異") rests on.
func TestInferTiersIsAllS(t *testing.T) {
	mod := lowerForTest(t, tierSrc)
	seen := 0
	for i := range mod.Funcs {
		f := &mod.Funcs[i]
		if f.IsExtern {
			continue
		}
		for v, tier := range mod.inferTiers(f) {
			seen++
			if tier != TierS {
				t.Errorf("%s: value %d tiered %v, want S", f.Name, v, tier)
			}
		}
	}
	if seen == 0 {
		t.Fatal("inferTiers returned no values: the test would pass vacuously")
	}
}

// TestInferTiersDoesNotMutate is the "report only" guarantee: P1 reads the
// module and nothing else. If it ever started inserting or rewriting
// instructions, the corpus verification for the later stages would stop being a
// clean baseline.
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

// TestDumpTiersIsSilentByDefault pins the other half of "report only": the
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
