package mir

import "testing"

// OpSetField.MovesArg is what tells insertDrops to suppress the RHS
// temporary's drop. It is only sound when the codegen MOVES the RHS into the
// field. emitSetField deep-copies an owned leaf (a `str` always; a `%vec` leaf
// opted in by ownedVecLeafAllowed) and every struct/pointer field, so for those
// the RHS still owns its buffer and must be dropped. A container field the
// codegen merely bitwise-stores (`%vec` outside the tier allowlist, a map, an
// `%option`) shares the RHS's buffer, so the field really does take ownership
// and the RHS must NOT be freed.
//
// The predicate is invisible on stdout — a leak-only defect never changes a
// program's output — so it needs a pin on the MIR itself.
//
// Measured on `box { name str }` with `s = i.to-str(); b.name = s` in a loop:
// control 8.16 MB @200k / 27.44 MB @800k (slope 32 B/iter, linear), fixed
// 1.72 MB / 1.72 MB (flat, equal to the no-struct negative control). The
// struct-literal site is the same defect one level in: `inner { name: s }`
// leaked another 32 B/iter, so `outer { p inner }` + `o.p = inner { name: s }`
// leaked 64 B/iter before and is flat now.
//
// The probe shape matters: the string must DEPEND on the loop variable and be
// READ, or opt -O3 deletes the whole loop and both sides measure flat.

const setFieldMovesArgSrc = `
inner { name str }
outer { p inner }
vecbox { v []str }
strbox { name str }

main = () {
    s str = 'seed'
    b strbox
    b.name = s
    vb vecbox
    vb.v = [s]
    o outer
    o.p = inner { name: s }
    print(b.name)
    print(vb.v.len())
    print(o.p.name)
}
`

// setFieldDecisions lowers src and returns, for every OpSetField, the answer
// SetFieldConsumesRHS gives for that exact receiver+field. Keyed by field name
// (the MIR carries only the field name, not the struct key).
func setFieldDecisions(t *testing.T, src string) map[string][]bool {
	t.Helper()
	mod := lowerForTest(t, src)
	out := map[string][]bool{}
	for i := range mod.Insts {
		inst := &mod.Insts[i]
		if inst.Op != OpSetField || inst.Str == "" || len(inst.Args) < 2 {
			continue
		}
		out[inst.Str] = append(out[inst.Str], mod.SetFieldConsumesRHS(inst.Args[0], inst.Str))
	}
	return out
}

// requireAll asserts every decision recorded for field name is want.
func requireAll(t *testing.T, got map[string][]bool, name string, want bool) {
	t.Helper()
	vs, ok := got[name]
	if !ok || len(vs) == 0 {
		t.Fatalf("no OpSetField found for field %q — the probe did not lower as expected (decisions=%v)", name, got)
	}
	for _, v := range vs {
		if v != want {
			t.Fatalf("field %q: SetFieldConsumesRHS=%v, want %v (all=%v)", name, v, want, vs)
		}
	}
}

// TestSetFieldConsumesRHSFollowsCodegen pins the predicate against the codegen's
// copy/share decision, in both directions, from one lowered program:
//
//   - `str` leaf (`strbox.name`, `inner.name`): codegen CLONES ->
//     MovesArg must be false so the RHS temporary is still dropped.
//   - struct field (`outer.p`): codegen DEEP-COPIES via cloneStructFieldLeaves
//     -> false.
//   - `%vec` field outside the tier allowlist (`vecbox.v`): codegen
//     BITWISE-STORES -> true, the historical "consumes" answer, so the field's
//     buffer is not freed out from under it (the `.keys = with-len(n)` case).
//
// The `vecbox.v` arm is the negative control: a predicate that simply returned
// false everywhere would fix the leak and turn every shared container field
// into a use-after-free.
func TestSetFieldConsumesRHSFollowsCodegen(t *testing.T) {
	got := setFieldDecisions(t, setFieldMovesArgSrc)
	requireAll(t, got, "name", false) // str leaf, both sites (strbox + inner literal)
	requireAll(t, got, "p", false)    // struct field: deep-copied
	requireAll(t, got, "v", true)     // %vec outside the allowlist: shared
}

// TestSetFieldConsumesRHSDefaultsToConsumeOnUnknownTarget pins the fallback.
// A receiver whose type the predicate cannot resolve (here a value id that does
// not exist) must keep the historical "consumes" answer: a missed fix is a
// leak, a wrong fix is a use-after-free.
func TestSetFieldConsumesRHSDefaultsToConsumeOnUnknownTarget(t *testing.T) {
	mod := lowerForTest(t, setFieldMovesArgSrc)
	if !mod.SetFieldConsumesRHS(NoVal, "name") {
		t.Fatal("NoVal receiver: want the conservative `consumes` default (true), got false")
	}
	if !mod.SetFieldConsumesRHS(NoVal, "") {
		t.Fatal("empty field name: want the conservative `consumes` default (true), got false")
	}
}

// TestSetFieldConsumesRHSOptionReceiverIsPeeled pins the `?T.field = v` path.
// emitSetField peels the option and then runs the SAME owned-leaf clone, so the
// predicate must peel too — otherwise an option-typed receiver falls through to
// the conservative default and the str leak comes back through that spelling.
func TestSetFieldConsumesRHSOptionReceiverIsPeeled(t *testing.T) {
	const src = `
box { name str }
main = () {
    b ?box = ok(box { name: 'seed' })
    s str = 'x'
    b.name = s
    print(s)
}
`
	requireAll(t, setFieldDecisions(t, src), "name", false)
}

// TestSetFieldConsumesRHSOwnedVecLeafIsNotConsumed pins the tier-allowlist half:
// once a `%vec` leaf is opted in by ownedVecLeafAllowed, emitOwnedLeafFieldStore
// CLONES it, so the RHS is no longer consumed. The allowlist is a package-level
// gate, so the test flips it to exercise both shapes of the SAME program — the
// control (gate closed -> shared -> true) and the landed one (gate open ->
// cloned -> false).
func TestSetFieldConsumesRHSOwnedVecLeafIsNotConsumed(t *testing.T) {
	const src = `
vecbox { v []str }
main = () {
    s str = 'seed'
    vb vecbox
    vb.v = [s]
    print(vb.v.len())
}
`
	old := ownedVecLeafTiers
	defer func() { ownedVecLeafTiers = old }()

	ownedVecLeafTiers = nil
	if got := setFieldDecisions(t, src); !got["v"][0] {
		t.Fatalf("with the tier allowlist empty the %%vec leaf is shared, so it must stay consumed; got %v", got)
	}

	// The allowlist normalises `-`/`.` to `_`; a bare struct key like `vecbox`
	// normalises to itself, so the prefix has no trailing separator.
	ownedVecLeafTiers = []string{"vecbox"}
	got := setFieldDecisions(t, src)
	requireAll(t, got, "v", false)
}
