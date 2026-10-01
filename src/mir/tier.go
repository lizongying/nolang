package mir

import (
	"fmt"
	"os"
)

// ─────────────────────────────────────────────────────────────────────────────
// P1 — tier inference (NOLANG-OWNERSHIP-MODEL.md §3.1 / §3.2 / §4.1)
//
// The hybrid ownership model assigns every value a TIER:
//
//	S  statically unique   — bare pointer, no clone, no RC
//	C  static CoW          — one clone inserted before the alias's first write
//	R  refcounted          — 16-byte header + inc/dec at every copy/release
//
// S and C share the identical bare-pointer ABI; only R carries a header. That
// is what makes "a statically-determinable value pays nothing at run time"
// true — there is no runtime tag and no shared-bit branch.
//
// THIS FILE IS THE FRAMEWORK ONLY. Per the roadmap, P1 introduces the lattice
// and the monotone fixpoint but leaves tierConstraint returning S for every
// use, so the inference is the identity and the pipeline is byte-for-byte
// unchanged. P2 (first-write clone) and P5 (escape → R) fill in the constraint
// function; the fixpoint below does not change.
// ─────────────────────────────────────────────────────────────────────────────

// Tier is a value's position in the ownership lattice S ⊑ C ⊑ R.
type Tier uint8

const (
	// TierS — statically unique: a bare pointer, no clone, no RC.
	TierS Tier = iota
	// TierC — static CoW: one clone before the alias's first write.
	TierC
	// TierR — refcounted: header + inc/dec at every copy and release.
	TierR
)

// String renders the tier as the single letter used in the design doc.
func (t Tier) String() string {
	switch t {
	case TierS:
		return "S"
	case TierC:
		return "C"
	case TierR:
		return "R"
	}
	return fmt.Sprintf("Tier(%d)", uint8(t))
}

// joinTier returns the least upper bound of two tiers. The lattice is a total
// order S ⊑ C ⊑ R, so the join is simply the weaker of the two: a value takes
// the join of the constraints every one of its uses imposes (§3.2). Because the
// order is total, joinTier is idempotent, commutative and associative.
func joinTier(a, b Tier) Tier {
	if a > b {
		return a
	}
	return b
}

// tierConstraint returns the tier that ONE use of v demands of v, where the use
// is the instruction inst and v is either one of its operands or (when
// v == inst.Dst) the value it defines.
//
// §4.1 lists the intended rules:
//
//	OpRun 的參數/結果 · 存入 R 檔容器 · 被 R 檔值別名   → R
//	別名且別名後有寫入                                  → C
//	僅別名且只讀                                        → S   (讀不提升檔位)
//	其他                                                → S
//
// P1 (roadmap): every case returns S. That makes the fixpoint converge after a
// single sweep and the tier of every value S, so nothing downstream can observe
// a difference. The signature is the one P2/P5 will fill in.
func (m *Module) tierConstraint(f *Function, inst *Inst, v ValueID) Tier {
	return TierS
}

// inferTiers computes the tier of every value in f by the monotone fixpoint of
// §4.1: start every value at S (the zero of the lattice), then raise each to
// the join of the constraints its uses impose, repeating until nothing rises.
//
// Termination: the lattice height is 3, every constraint is monotone, and a
// value's tier only ever rises, so after the first sweep the loop runs at most
// twice more. P1's all-S constraint converges on the first sweep.
//
// The map is SEEDED with every value the function mentions rather than relying
// on map's zero value: S is the lattice zero, so a value that never rises would
// otherwise be absent instead of recorded as S (and len(tiers) would read 0).
//
// The result is REPORT ONLY in P1 — no pass consults it yet. See DumpTiers.
func (m *Module) inferTiers(f *Function) map[ValueID]Tier {
	tiers := map[ValueID]Tier{}
	if f == nil {
		return tiers
	}
	seed := func(v ValueID) {
		if v <= NoVal {
			return
		}
		if _, ok := tiers[v]; !ok {
			tiers[v] = TierS
		}
	}
	for _, bid := range f.Blocks {
		b := m.Block(bid)
		if b == nil {
			continue
		}
		for _, iid := range b.Insts {
			inst := m.Inst(iid)
			if inst == nil {
				continue
			}
			seed(inst.Dst)
			for _, a := range inst.Args {
				seed(a)
			}
		}
	}
	for _, rp := range f.ResultParams {
		seed(rp)
	}
	for changed := true; changed; {
		changed = false
		for _, bid := range f.Blocks {
			b := m.Block(bid)
			if b == nil {
				continue
			}
			for _, iid := range b.Insts {
				inst := m.Inst(iid)
				if inst == nil {
					continue
				}
				for _, a := range inst.Args {
					if a <= NoVal {
						continue
					}
					if t := joinTier(tiers[a], m.tierConstraint(f, inst, a)); t != tiers[a] {
						tiers[a] = t
						changed = true
					}
				}
				if inst.Dst > NoVal {
					if t := joinTier(tiers[inst.Dst], m.tierConstraint(f, inst, inst.Dst)); t != tiers[inst.Dst] {
						tiers[inst.Dst] = t
						changed = true
					}
				}
			}
		}
	}
	return tiers
}

// DumpTiers writes the P1 tier report to stderr. Gated by NOLANG_MIR_TIER=1 —
// off by default, so a normal build prints nothing and emits byte-identical
// output (the same contract as DumpSpawnGraph). It is the observable that shows
// P1 is wired into the pipeline without changing it.
func (m *Module) DumpTiers() {
	if os.Getenv("NOLANG_MIR_TIER") == "" {
		return
	}
	counts := map[Tier]int{}
	total := 0
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.IsExtern {
			continue
		}
		tiers := m.inferTiers(f)
		if len(tiers) == 0 {
			continue
		}
		fc := map[Tier]int{}
		for _, t := range tiers {
			fc[t]++
			counts[t]++
			total++
		}
		fmt.Fprintf(os.Stderr, "[tier] %s: %d value(s) (S=%d C=%d R=%d)\n",
			f.Name, len(tiers), fc[TierS], fc[TierC], fc[TierR])
	}
	fmt.Fprintf(os.Stderr, "[tier] total %d value(s): S=%d C=%d R=%d\n",
		total, counts[TierS], counts[TierC], counts[TierR])
}
