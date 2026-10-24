package mir

import (
	"regexp"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// `match` used as an expression value: where the captured value lives.
//
// A match in value position (`r = subject: { arms }`, `out = subject: { arms }`,
// `b i64 = subject: { arms }`) is lowered by running the if-chain as a
// STATEMENT and funnelling every arm's final value into one shared slot
// (l.exprSink); the binding then aliases — or receives — that slot.
//
// Two defects sat in that funnel, and both were SILENT: the program compiled,
// printed a plausible number, and the number was wrong. They are pinned here
// because the documented two-stage option unwrap
// (docs/design/option-unified.md §11.5) is written in exactly this shape, so
// every `?T` unwrap in std depends on it.
//
//  1. The seed of the capture slot was materialised in the LAST ARM's block
//     instead of the block that dominates both arms. Its defining `store` and
//     the `load` that consumed it therefore sat in different blocks, and the
//     dominating block read a slot nothing had written yet — uninitialised
//     stack — on every path that produced no value.
//
//  2. The `let` path left the statement's value at NoVal after capturing, so
//     the "declaration with no initializer" zero-fill fired (it only needs the
//     DECLARED type, which `b i64 = <match>` has) and the rebind path moved
//     that zero into the very slot the arms had just filled:
//
//       b i64 = o: { ok -> it  -> }   ; b == 0     (WRONG)
//       b     = o: { ok -> it  -> }   ; b == it    (no declared type,
//                                   ;              so the zero-fill never fired)
//
//     The same shape hit a result parameter (`out = <match>`): aliasing the
//     sink repointed the NAME and left the out-parameter write-back unwritten.
//
// The IR invariants below are deliberately structural rather than numeric:
// a test that only checked "0 vs 98" would pass again the moment some other
// layer papered over the wrong value.
// ─────────────────────────────────────────────────────────────────────────────

// matchExprValueSrc covers the three binding shapes side by side: annotated
// local, unannotated local (the shape that always worked), and result
// parameter.
const matchExprValueSrc = `mk = (n i64) (out ?i64) {
    out = n
}

annotated = (n i64) (out i64) {
    o = mk(n)
    b i64 = o: {
        ok -> it
        ->
    }
    out = b
}

plain = (n i64) (out i64) {
    o = mk(n)
    b = o: {
        ok -> it
        ->
    }
    out = b
}

to-out = (n i64) (out i64) {
    o = mk(n)
    out = o: {
        ok -> it
        ->
    }
}

a = annotated(98)
b = plain(98)
c = to-out(98)
`

// irBlocks splits a `define ... { }` body into its basic blocks, keyed by
// label, in emission order. The entry block (everything before the first
// label) is keyed "".
func irBlocks(fn string) (order []string, body map[string][]string) {
	body = map[string][]string{}
	labelRE := regexp.MustCompile(`^([\w.]+):$`)
	cur := ""
	order = append(order, "")
	for _, line := range strings.Split(fn, "\n") {
		if m := labelRE.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			cur = m[1]
			order = append(order, cur)
			continue
		}
		body[cur] = append(body[cur], line)
	}
	return order, body
}

// loadedSlotsWithoutStore returns the `%x.s` slots that a block LOADS but never
// stores to. In the block that dominates a capture slot's consumers, that list
// must be empty: anything else is a read of uninitialised stack.
func loadedSlotsWithoutStore(lines []string) []string {
	stored := map[string]bool{}
	var loaded []string
	storeRE := regexp.MustCompile(`store [^,]+,\s*[^ ]*\* (%[\w.]+)`)
	loadRE := regexp.MustCompile(`load [^,]+,\s*[^ ]*\* (%[\w.]+)`)
	for _, l := range lines {
		if m := storeRE.FindStringSubmatch(l); m != nil {
			stored[m[1]] = true
		}
		if m := loadRE.FindStringSubmatch(l); m != nil {
			loaded = append(loaded, m[1])
		}
	}
	var out []string
	for _, s := range loaded {
		if !stored[s] {
			out = append(out, s)
		}
	}
	return out
}

func TestMatchExprCaptureSeedIsMaterialisedInTheDominatingBlock(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, matchExprValueSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	for _, sym := range []string{"@annotated", "@plain", "@to_out"} {
		t.Run(sym, func(t *testing.T) {
			fn := extractDefineBlock(t, ll, sym)
			order, body := irBlocks(fn)
			entry := order[0]

			// (1) The entry block dominates both arms, so every slot it reads
			// must already have been written there. The capture seed used to
			// violate this: its `store i64 0` landed in the else arm while the
			// `load` stayed in the entry block.
			if bad := loadedSlotsWithoutStore(body[entry]); len(bad) > 0 {
				t.Errorf("entry block reads slots it never stores to (uninitialised stack): %v\n---\n%s",
					bad, strings.Join(body[entry], "\n"))
			}

			// (2) The merge block must not zero the capture slot: the arms have
			// already filled it, and a zero here silently replaces the match's
			// value (the `b i64 = <match>` / `out = <match>` defect).
			merge := order[len(order)-1]
			if merge == entry {
				t.Fatalf("%s: expected a merge block after the arms\n%s", sym, fn)
			}
			for _, l := range body[merge] {
				if strings.HasPrefix(strings.TrimSpace(l), "store i64 0,") {
					t.Errorf("merge block zeroes a slot after the arms filled it:\n  %s\n---\n%s",
						strings.TrimSpace(l), strings.Join(body[merge], "\n"))
				}
			}
		})
	}
}

// TestMatchExprValueReachesEveryBindingShape proves the captured value is
// actually routed to the binding, not merely that no zero is stored: the merge
// block must read the ONE slot that BOTH arms write (the capture slot) and
// carry it to the target. A merge block that reads a slot only one arm wrote is
// reading a leftover — that is exactly how `out = <match>` returned 0 while the
// identical local-binding form worked: the merge loaded a constant that the
// else arm had just stored, not the value the ok arm produced.
func TestMatchExprValueReachesEveryBindingShape(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, matchExprValueSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	storeRE := regexp.MustCompile(`store [^,]+,\s*[^ ]*\* (%[\w.]+)`)
	loadRE := regexp.MustCompile(`load [^,]+,\s*[^ ]*\* (%[\w.]+)`)
	storedIn := func(lines []string) map[string]bool {
		set := map[string]bool{}
		for _, l := range lines {
			if m := storeRE.FindStringSubmatch(l); m != nil {
				set[m[1]] = true
			}
		}
		return set
	}

	for _, sym := range []string{"@annotated", "@plain", "@to_out"} {
		t.Run(sym, func(t *testing.T) {
			fn := extractDefineBlock(t, ll, sym)
			if !regexp.MustCompile(`define void ` + regexp.QuoteMeta(sym) + `\(i64 %p0, i64\* %p1\)`).MatchString(fn) {
				t.Fatalf("%s: unexpected signature\n%s", sym, strings.SplitN(fn, "\n", 2)[0])
			}
			order, body := irBlocks(fn)
			if len(order) < 4 {
				t.Fatalf("%s: expected entry + two arms + merge, got %d blocks\n%s", sym, len(order), fn)
			}
			merge := order[len(order)-1]
			arms := order[1 : len(order)-1]

			// The capture slot: written by every arm.
			common := storedIn(body[arms[0]])
			for _, a := range arms[1:] {
				next := map[string]bool{}
				for s := range storedIn(body[a]) {
					if common[s] {
						next[s] = true
					}
				}
				common = next
			}
			if len(common) == 0 {
				t.Fatalf("%s: the arms share no written slot — there is no capture slot\n%s", sym, fn)
			}

			// The merge must read one of them, not a per-arm leftover.
			mergeText := strings.Join(body[merge], "\n")
			read := false
			for _, l := range body[merge] {
				m := loadRE.FindStringSubmatch(l)
				if m == nil {
					continue
				}
				if common[m[1]] {
					read = true
					break
				}
			}
			if !read {
				t.Errorf("%s: the merge block never reads the slot both arms wrote (it reads a per-arm leftover)\n---\n%s",
					sym, mergeText)
			}

			// ... and the value must reach the out parameter.
			if !regexp.MustCompile(`store i64 %[\w.]+, i64\* %p1`).MatchString(mergeText) {
				t.Errorf("%s: the captured value never reaches the out parameter %%p1\n---\n%s", sym, mergeText)
			}
			if regexp.MustCompile(`store i64 0, i64\* %p1`).MatchString(mergeText) {
				t.Errorf("%s: the out parameter receives a literal 0\n---\n%s", sym, mergeText)
			}
		})
	}
}
