package mir

import (
	"strings"
	"testing"
)

// ownedVecLeafReadSrc reads an owned `[]i64` field out of a struct. This is the
// ordinary (non-option) field read that crashed the first activation of the
// owned-%vec leaf classifier: isBorrowRead handed the value to the drop walker
// while emitGetField returned a bitwise alias of the struct's buffer, so the
// drop freed a buffer the struct still owned (`hashmap_i64_i64_put`, SIGTRAP in
// mfm_free). The clone below is what makes the two sides agree.
const ownedVecLeafReadSrc = `
box {
    data []i64
}

main = () {
    b box
    b.data.push(1)
    d = b.data
    print(d.len())
}
`

// ownedVecLeafCopySrc is the same struct through a struct copy: `c = b` must
// deep-copy the leaf (so the two structs do not share one buffer) and the drop
// of each must free its own buffer.
const ownedVecLeafCopySrc = `
box {
    data []i64
}

main = () {
    b box
    b.data.push(1)
    c = b
    c.data.push(2)
    print(b.data.len())
    print(c.data.len())
}
`

// emitWithOwnedVecTiers lowers and emits src with the tier allowlist temporarily
// set to tiers. The allowlist is a package-level gate (see ownedVecLeafTiers),
// so the test can exercise the landed and the un-landed shape of the same
// program from one source of truth.
func emitWithOwnedVecTiers(t *testing.T, src string, tiers []string) string {
	t.Helper()
	old := ownedVecLeafTiers
	ownedVecLeafTiers = tiers
	defer func() { ownedVecLeafTiers = old }()
	return mustEmit(t, src)
}

// TestOwnedVecLeafFieldReadIsCloned pins the clone half of the read path, and
// the negative half is the control: with the gate closed the read must stay a
// plain alias, otherwise the test would also pass if every field read were made
// deep. A missing clone here is not a leak you can see on stdout — it is a
// heap corruption that shows up as a crash in some other function.
func TestOwnedVecLeafFieldReadIsCloned(t *testing.T) {
	off := emitWithOwnedVecTiers(t, ownedVecLeafReadSrc, nil)
	if strings.Contains(off, "@__nolang_vec_clone_") {
		t.Fatalf("gate closed, but the field read was routed through a deep clone:\n%s", irFunc(off, "_nolang_main"))
	}

	on := emitWithOwnedVecTiers(t, ownedVecLeafReadSrc, []string{""})
	if !strings.Contains(on, "@__nolang_vec_clone_") {
		t.Fatalf("an owned %s field read that the drop walker will release was not cloned:\n%s", "%vec", irFunc(on, "_nolang_main"))
	}
}

// TestOwnedVecLeafStructCopyClonesAndFrees is the lockstep half: the deep CLONE
// and the deep FREE are emitted from the same classifier, so a struct that gets
// a private leaf buffer on copy must also release it, and a struct that is not
// opted in must get neither. Shipping either side alone is strictly worse than
// shipping nothing — clone without free leaks, free without clone double-frees.
func TestOwnedVecLeafStructCopyClonesAndFrees(t *testing.T) {
	off := emitWithOwnedVecTiers(t, ownedVecLeafCopySrc, nil)
	if strings.Contains(off, "@__nolang_vec_clone_") {
		t.Fatalf("gate closed, but the struct copy deep-cloned a leaf:\n%s", irFunc(off, "_nolang_main"))
	}
	if strings.Contains(off, "@__nolang_vec_free_") {
		t.Fatalf("gate closed, but a drop deep-freed a leaf:\n%s", irFunc(off, "_nolang_main"))
	}

	on := emitWithOwnedVecTiers(t, ownedVecLeafCopySrc, []string{""})
	for _, want := range []string{
		"@__nolang_vec_clone_", // `c = b` deep-copies the leaf
		"@__nolang_vec_free_",  // and the drop of each struct releases its own
	} {
		if !strings.Contains(on, want) {
			t.Errorf("opted-in struct copy is missing %q — clone and drop must land together:\n%s", want, irFunc(on, "_nolang_main"))
		}
	}
}

// TestOwnedVecLeafTiersMatchDottedKeys covers the key spelling trap: a struct
// declared inside a module is keyed `module.name` (`json.json-pool`), so a tier
// written as `json_` matches nothing unless the dot is normalised too. It fails
// silently — the tier is simply opt-in for zero structs — which is the one
// failure mode this whole allowlist is most likely to hit next time.
func TestOwnedVecLeafTiersMatchDottedKeys(t *testing.T) {
	old := ownedVecLeafTiers
	ownedVecLeafTiers = []string{"json_"}
	defer func() { ownedVecLeafTiers = old }()

	m := &Module{}
	for _, key := range []string{"json.json-pool", "json.json-value"} {
		if !m.ownedVecLeafAllowed(key) {
			t.Errorf("tier %q did not match dotted struct key %q", ownedVecLeafTiers[0], key)
		}
	}
	if m.ownedVecLeafAllowed("hashmap-str-i64") {
		t.Errorf("tier %q matched an unrelated container", ownedVecLeafTiers[0])
	}
}

// TestOwnedVecLeafTiersMatchLandedSet pins the rollout state: a struct key opts
// in only if its tier has a clean corpus compile+run sweep behind it. Adding a
// tier is a one-line change to ownedVecLeafTiers — and it has to change this
// list in the same commit, which is what stops a tier from shipping on the
// strength of "it looked fine on the tests I ran".
func TestOwnedVecLeafTiersMatchLandedSet(t *testing.T) {
	want := []string{"hashmap_", "static_hashmap_", "json_"} // tiers 1-2; see ownedVecLeafTiers
	if len(ownedVecLeafTiers) != len(want) {
		t.Fatalf("ownedVecLeafTiers = %v, want %v — a tier changed without its sweep record", ownedVecLeafTiers, want)
	}
	for i := range want {
		if ownedVecLeafTiers[i] != want[i] {
			t.Fatalf("ownedVecLeafTiers[%d] = %q, want %q", i, ownedVecLeafTiers[i], want[i])
		}
	}
}
