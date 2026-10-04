package mir

import (
	"strings"
	"testing"
)

// strEmptyCloneSrc reads an owned `str` field, which emitGetField lowers with
// @str_clone, and builds an empty literal, which lowers with @str_from_const.
const strEmptyCloneSrc = `
box {
    name str
}

box.get = () (r str) {
    r = .name
}

main = () {
    b = box { name: '' }
    s = b.get()
    print(s)
}
`

// TestStrCloneAndFreeAgreeOnEmptyStrings pins the clone/free pairing for the
// EMPTY string.
//
// @str_free (and @vec_free) skip a value whose cap == 0, because a borrowed
// VIEW carries cap == 0 while aliasing storage the value does not own. That
// guard is correct — but it makes `cap == 0` mean "owns nothing", so nothing
// that ALLOCATES may hand back a value carrying cap == 0.
//
// @str_clone used to break that: for any non-null `data` it allocated len+1
// bytes and returned cap = len, so cloning '' produced
// `{0, 0, <fresh 16-byte block>}` — a heap block no @str_free would ever
// release. @str_from_const had the same shape for the '' literal itself:
// `@nolang_rc_alloc(0)` -> a 16-byte block (32-byte malloc class) that leaked
// once per evaluation in every program mentioning an empty string.
//
// Measured (leaks(1) on a 400k-iteration json parse loop): 25,867,365 leaked
// blocks, all 32-byte mallocs. The §4.3 tier-2 `json_` rollout turned this from
// a slow drip into the dominant cost, because every read of an owned `[]str`
// leaf deep-clones the vec and `json-pool.strs` holds nothing but '' values:
// json parse went 130.9 MB -> 170.2 MB @50k with the gate open. With the guard
// in both helpers the same run is 8.6 MB @50k / 14.7 MB @100k.
//
// The negative control matters: the guard must be an empty-string SHORT-CUT,
// not the removal of the copy — a non-empty clone still has to allocate.
func TestStrCloneAndFreeAgreeOnEmptyStrings(t *testing.T) {
	mod := lowerForTest(t, strEmptyCloneSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	clone := irFunc(ir, "str_clone")
	if clone == "" {
		t.Fatal("@str_clone not emitted")
	}
	if !strings.Contains(clone, "icmp eq i64 %len, 0") {
		t.Errorf("@str_clone does not short-circuit the empty string: an "+
			"allocated clone of '' carries cap == 0, which @str_free reads as "+
			"\"borrowed view, owns nothing\" and never releases\n%s", clone)
	}
	if !strings.Contains(clone, "call i8* @nolang_rc_alloc") {
		t.Errorf("@str_clone no longer allocates for non-empty strings — the "+
			"empty-string guard must not have deleted the copy itself\n%s", clone)
	}

	fromConst := irFunc(ir, "str_from_const")
	if fromConst == "" {
		t.Fatal("@str_from_const not emitted")
	}
	if !strings.Contains(fromConst, "icmp eq i64 %len, 0") {
		t.Errorf("@str_from_const allocates for the empty literal: '' becomes "+
			"a cap == 0 heap string that nothing frees\n%s", fromConst)
	}
}
