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
// THIS FILE OWNS THE INFERENCE AND ITS ORACLE. P1 introduced the lattice and
// the monotone fixpoint but left tierConstraint returning S for every use, so
// the inference was the identity and nothing downstream could observe it. That
// identity is GONE: tierConstraint now answers the §3.1 table for real, the
// C-tier borrow views are seeded from the IR, and checkTierSoundness (§3.3,
// invariant I1) makes the partition a HARD BUILD GATE — a value the inference
// calls S or C while the IR treats it as an R object fails the build.
//
// What is still deliberately absent: nothing REWRITES the IR from the tiers.
// The R tier describes the mechanism the compiler already emits (the %task
// handle's retain/release, §2.4, plus the §4.2 b share gate); making the tiers
// PRESCRIPTIVE means moving the pass in front of insertBorrowRetains /
// emitAsyncRun, which is P2/P5 work. §4.2(c) records that boundary.
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
// This is the INSTRUCTION-LOCAL half of §3.1's table — the half whose answer
// depends only on the instruction. The two rows that need to see the CURRENT
// tier map (alias transitivity, and a store into an R-tier container) live in
// tierStep. The split is deliberate: this function is the directly testable
// statement of the design's table, and tierStep is the dataflow that closes it.
//
// §3.1's table, and where each row is implemented:
//
//	OpRun 的結果                       → R   here
//	OpRun 的參數（§4.2 b 閘門共享了）   → R   here
//	OpRun 的參數（深拷貝或 move）       → S   here (no reference survives)
//	OpTaskRetain / OpAwait 的運算元    → R   here
//	被 R 檔值別名（傳遞性）             → R   tierStep
//	存入 R 檔容器（I2）                → R   tierStep
//	借讀視圖且之後有寫入               → C   inferTiers (writtenThroughSet)
//	僅借讀且只讀                       → S   (the zero of the lattice)
func (m *Module) tierConstraint(f *Function, inst *Inst, v ValueID) Tier {
	if inst == nil || v <= NoVal {
		return TierS
	}
	switch inst.Op {
	case OpRun:
		// The handle a spawn produces crosses the concurrency boundary: the
		// task outlives the statement, and every copy of the handle is a second
		// reference that must carry its own count (§2.4). This is the ONLY
		// R source the language has today (§3.1).
		if v == inst.Dst {
			return TierR
		}
		// An ARGUMENT is R only when the §4.2(b) gate SHARED the caller's
		// buffer with the task. Otherwise the spawn deep-copied it (P0) or
		// moved it (P5-move), so the caller keeps no reference and the value
		// never crosses the boundary at all: S. Read back the RECORDED decision
		// (spawnArgRetains) rather than re-deriving it — §4.5's discipline, and
		// the reason the tier partition and the emitted retain cannot drift.
		if m.spawnArgRetains[v] {
			return TierR
		}
		return TierS
	case OpTaskRetain, OpAwait:
		// Both only ever take a task handle, and each is a second reference to
		// it that must be counted (§2.4 / §4.4). A missing retain on such a
		// value is the use-after-free checkRefBalance exists to catch, so the
		// tier must never be weaker than R here.
		return TierR
	}
	return TierS
}

// tierStep performs ONE fixpoint sweep over a single instruction: it raises the
// tier of every value that instruction constrains, and reports whether anything
// rose. It is monotone (raise only) and its input is the current tier map —
// which is what lets it express the two §3.1 rows tierConstraint cannot.
func (m *Module) tierStep(f *Function, inst *Inst, tiers map[ValueID]Tier) bool {
	if inst == nil {
		return false
	}
	changed := false
	raise := func(v ValueID, t Tier) {
		if v <= NoVal {
			return
		}
		if tiers[v] >= t {
			return
		}
		tiers[v] = t
		changed = true
	}

	// (1) The instruction-local constraints.
	for _, a := range inst.Args {
		raise(a, m.tierConstraint(f, inst, a))
	}
	if inst.Dst > NoVal {
		raise(inst.Dst, m.tierConstraint(f, inst, inst.Dst))
	}

	// (2) Alias transitivity — §3.1's 「被 R 檔值別名 → R」. An OpMove / OpCast /
	// OpPhi makes its endpoints the SAME reference: `h2 = h` is two names for
	// one task, so a tier on either end is a statement about the one object and
	// the join is taken in BOTH directions. OpClone is deliberately absent — a
	// clone is a fresh, independent value, which is exactly why §1.1 can call a
	// value-to-value assignment alias-free.
	switch inst.Op {
	case OpMove, OpCast, OpPhi:
		hi := TierS
		if inst.Dst > NoVal && tiers[inst.Dst] > hi {
			hi = tiers[inst.Dst]
		}
		for _, a := range inst.Args {
			if a > NoVal && tiers[a] > hi {
				hi = tiers[a]
			}
		}
		if hi > TierS {
			raise(inst.Dst, hi)
			for _, a := range inst.Args {
				raise(a, hi)
			}
		}
	}

	// (3) I2 — a value STORED INTO an R-tier container becomes a second
	// reference the container owns, so it is R as well. Monotone by
	// construction: the store can only raise the stored value once the
	// container itself has risen, and raising the container never lowers
	// anything. (Inert today — the only R object is the %task, which is not a
	// container — but it is the rule that keeps I2 true the moment one exists,
	// and it is why the validator must NOT try to check I2 today: §3.3.)
	if inst.Op == OpSetField || inst.Op == OpIndexStore {
		if len(inst.Args) >= 2 && inst.Args[0] > NoVal && tiers[inst.Args[0]] == TierR {
			for _, a := range inst.Args[1:] {
				raise(a, TierR)
			}
		}
	}
	return changed
}

// writtenThroughSet returns the values a function writes THROUGH: the container
// operand (Args[0]) of a field or element store.
//
// WHY THIS IS THE C-TIER TRIGGER (§3.1 「借讀視圖且之後有寫入 → C」). A borrow
// view — an element/field read that did not clone — normally costs nothing:
// reading through an alias is free, and the projection machinery (lvalueAddrOf)
// even hands the caller the SOURCE's address so a write lands where it belongs.
// The moment the view is used as a WRITE TARGET the two roles of "the same
// storage" diverge: the value is both a second name for the owner's buffer and
// a thing being mutated. That is precisely what "one clone before the alias's
// first write" (tier C, §1.4) describes, and it is the queryable write point
// §4.2(c) named as the precondition for checkTierSoundness.
//
// CONSERVATIVE IN ONE DIRECTION, and that is the safe one: the POSITION is not
// compared ("before" vs "after" the read), so a write anywhere in the function
// marks the view. Over-approximating C can only LOSE a report (a value wrongly
// called C is one the soundness check stops looking at), never manufacture one.
// The precise "strictly between read and use" test already exists as
// sourceWrittenBetween, but that is a codegen helper and this pass must not
// depend on codegen state.
func (m *Module) writtenThroughSet(f *Function) map[ValueID]bool {
	set := map[ValueID]bool{}
	if f == nil {
		return set
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
			if inst.Op != OpSetField && inst.Op != OpIndexStore {
				continue
			}
			if len(inst.Args) >= 1 && inst.Args[0] > NoVal {
				set[inst.Args[0]] = true
			}
		}
	}
	return set
}

// inferTiers computes the tier of every value in f by the monotone fixpoint of
// §3.1: start every value at S (the zero of the lattice), raise the C-tier
// borrow views, then repeatedly raise each value to the join of the constraints
// its uses impose until nothing rises.
//
// Termination: the lattice height is 3, every step is monotone (raise only) and
// no value's tier ever falls, so the loop runs at most twice past the first
// sweep. The all-S partition of P1 converged on the first sweep; the real
// constraints need the extra sweeps to carry R along an alias CHAIN
// (`h → h2 → h3` needs three), which is exactly the property
// checkTierSoundness's closure half pins.
//
// The map is SEEDED with every value the function mentions rather than relying
// on map's zero value: S is the lattice zero, so a value that never rises would
// otherwise be ABSENT instead of recorded as S — len(tiers) would read 0 and
// every consumer would silently see an empty partition. (That was P1's one real
// bug; checkTierSoundness now guards it, see checkTierPartition.)
//
// inferTiers READS the module and never writes it: no instruction is inserted,
// rewritten or reordered. That is what keeps the corpus A/B a clean baseline.
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
	written := m.writtenThroughSet(f)
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
			// C tier: a BORROW view that is written through. Both halves come
			// from the IR — isBorrowRead is the SAME predicate insertDrops uses
			// to decide whether the read borrows (so the tier and the drop
			// cannot disagree), and writtenThroughSet is the write point.
			if inst.Dst > NoVal && written[inst.Dst] && m.isBorrowRead(f, inst) {
				tiers[inst.Dst] = TierC
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
				if m.tierStep(f, inst, tiers) {
					changed = true
				}
			}
		}
	}
	return tiers
}

// ─────────────────────────────────────────────────────────────────────────────
// checkTierSoundness — the ORACLE for the partition (invariant I1)
// ─────────────────────────────────────────────────────────────────────────────

// checkTierSoundness enforces invariant I1 of NOLANG-OWNERSHIP-MODEL.md §1.5 —
// "every value has exactly one tier" — in the form that is both TRUE and
// load-bearing: a value the inference calls S or C must not be an object the IR
// treats as R.
//
// WHY THIS IS NOT SELF-JUSTIFYING — the reason P1 refused to write it (§4.2 c).
// The two halves are built from OPPOSITE directions:
//
//   - the CONSTRAINT (tierConstraint / tierStep) is the design's table: "what
//     tier does this use demand of its operand";
//   - the VALIDATOR reads the IR's own ground truth for "this is an R object":
//     a real spawn's handle and every alias of it (handleAliasSet — the
//     predicate spawn_graph.go and checkRefBalance already share), the §4.2(b)
//     shared spawn arguments, and every OpTaskRetain / OpAwait operand.
//
// Both read the same RECORDED decisions rather than recomputing them (§4.5), so
// a drift between "what the inference concluded" and "what the IR assumes" is a
// hard build failure instead of a silent mis-tier. Under P1's identity
// constraint this fires immediately — which is exactly the non-vacuity property
// the unit tests pin by handing checkTierRSites the identity partition.
//
// WHAT IT DELIBERATELY DOES NOT CHECK. I2 (an S/C value must not be stored into
// an R container) is VACUOUS today: the only R object is the %task, which is
// not a container. §3.3's verdict stands — an empty check is worse than no
// check, because it turns "a validator exists" into a false assurance. I5/I6
// live inside @nolang_rc_release's function body, which MIR cannot see, and the
// ValidateTypes chain the doc names is an AST-level hook that cannot host MIR
// concepts.
//
// A false positive here is a HARD COMPILE FAILURE (Analyze's report gates
// codegen, §4.5), so the load-bearing evidence for this pass is a whole-corpus
// COMPILE sweep — `no build` needs no execution, so it has no timeouts and
// costs minutes — not the unit tests alone.
func (m *Module) checkTierSoundness(f *Function, rep *Report) {
	tiers := m.inferTiers(f)
	m.checkTierPartition(f, tiers, rep)
	m.checkTierRSites(f, tiers, rep)
}

// checkTierPartition is the seeding/closure half of I1: every value the
// function mentions must HAVE a tier, and no value may sit BELOW what its own
// uses demand.
//
// The two halves catch two different bugs, and both are real:
//
//   - a MISSING entry is P1's one actual defect — S is the lattice zero, so an
//     all-S partition written by a "raise only" loop comes back as an EMPTY map
//     (len(tiers) == 0), and every consumer then silently sees nothing;
//   - a value BELOW its own uses' join is a fixpoint that did not close. One
//     sweep is not enough for an alias CHAIN (`h → h2 → h3` needs three), and
//     the alias edges are exactly where the R tier travels.
//
// It takes the partition as a PARAMETER so a test can hand it a stale or
// identity map and watch it fire; the "must fail before the fix" evidence is
// that call, not a claim.
func (m *Module) checkTierPartition(f *Function, tiers map[ValueID]Tier, rep *Report) {
	report := func(bid BlockID, iid InstID, v ValueID, msg string) {
		rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
			Kind: "tier-soundness", Func: f.Name, Block: bid, Inst: iid,
			Msg: fmt.Sprintf("value %d %s", v, msg),
		})
	}
	if f == nil {
		return
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
			check := func(v ValueID, want Tier) {
				if v <= NoVal {
					return
				}
				got, ok := tiers[v]
				if !ok {
					report(bid, iid, v, "has no tier: the partition is not seeded (I1)")
					return
				}
				if got < want {
					report(bid, iid, v, fmt.Sprintf(
						"is tiered %v but a use demands %v: the fixpoint did not close (I1)", got, want))
				}
			}
			for _, a := range inst.Args {
				check(a, m.tierConstraint(f, inst, a))
			}
			if inst.Dst > NoVal {
				check(inst.Dst, m.tierConstraint(f, inst, inst.Dst))
			}
			// An alias edge carries ONE tier, so its endpoints must agree —
			// the property a one-sweep fixpoint breaks on a chain.
			switch inst.Op {
			case OpMove, OpCast, OpPhi:
				hi := TierS
				for _, a := range inst.Args {
					if a > NoVal && tiers[a] > hi {
						hi = tiers[a]
					}
				}
				if inst.Dst > NoVal && tiers[inst.Dst] > hi {
					hi = tiers[inst.Dst]
				}
				check(inst.Dst, hi)
				for _, a := range inst.Args {
					check(a, hi)
				}
			}
		}
	}
}

// checkTierRSites is the I1 half that is NOT vacuous today: every object the IR
// treats as an R object must be tiered R. Three routes establish that, and they
// are checked independently so a gap in one is not masked by another.
func (m *Module) checkTierRSites(f *Function, tiers map[ValueID]Tier, rep *Report) {
	report := func(bid BlockID, iid InstID, v ValueID, why string) {
		rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
			Kind: "tier-soundness", Func: f.Name, Block: bid, Inst: iid,
			Msg: fmt.Sprintf("value %d is an R object (%s) but the inference tiered it %v (I1)",
				v, why, tiers[v]),
		})
	}
	if f == nil {
		return
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
			switch inst.Op {
			case OpRun:
				// A real spawn (Sym != "") establishes rc = 1, so its handle and
				// every ALIAS of it is an R object. `run <handle-var>` (Sym == "")
				// forwards an existing handle — the same object, already checked
				// from the site that created it, so it is skipped here exactly as
				// checkRefBalance skips it.
				if inst.Sym == "" || inst.Dst <= NoVal {
					continue
				}
				for _, mem := range m.handleAliasSet(f, inst.Dst) {
					if tiers[mem] != TierR {
						report(bid, iid, mem, "spawn handle or an alias of it")
					}
				}
				// The §4.2(b) share gate: the caller's buffer is a SECOND
				// reference held by the task, which is what R means.
				for _, a := range inst.Args {
					if a > NoVal && m.spawnArgRetains[a] && tiers[a] != TierR {
						report(bid, iid, a, "argument shared with the task (§4.2 b)")
					}
				}
			case OpTaskRetain, OpAwait:
				// Both only ever take a task handle, and each is a second
				// reference that must carry its own count (§2.4 / §4.4).
				for _, a := range inst.Args {
					if a > NoVal && tiers[a] != TierR {
						report(bid, iid, a, "operand of a handle reference")
					}
				}
			}
		}
	}
}

// DumpTiers writes the tier report to stderr. Gated by NOLANG_MIR_TIER=1 — off
// by default, so a normal build prints nothing and emits byte-identical output
// (the same contract as DumpSpawnGraph). It is the measurement surface for the
// partition: the S/C/R histogram per function is how "did the constraint
// actually fire?" is answered without instrumenting the pass.
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
