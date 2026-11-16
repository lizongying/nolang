package mir

import (
	"strings"
	"testing"
)

// These tests pin the ownership rule for moving a BORROWED input parameter
// whose type is a STRUCT with owned (heap) fields into an owned slot — e.g.
// `r = ctx` where `ctx` is a struct parameter and `r` is the out-param / a
// local that the callee later drops.
//
// emitMove already clones borrowed `str` and `[]T` parameters (see the guard at
// the top of emitMove): the callee does NOT own a parameter's heap, so a
// bitwise copy would hand the destination a pointer the caller still frees.
// Structs were missing from that guard. `bpctx { r str }` passed by value and
// re-bound into a result param (`drv.bp-walk`'s `r = ctx`) bitwise-copied the
// owned `str` field; the callee's scope-exit drop of `r` then freed the
// caller's buffer -> SIGTRAP double-free in cov-measure.no.
//
// The observable is on the emitted IR: the borrowed struct param move must
// deep-copy the owned field (@str_clone), exactly like the borrowed `str`
// case. A bitwise-only store leaves the field alias shared and double-frees.

const borrowedStructParamSrc = `bx {
    s str
    n i64
}

id = (ctx bx) (r bx) {
    r = ctx
}

v = bx { s: 'a-heap-string-long-enough-to-matter', n: 7 }
w = id(v)
print(w.s)
`

func TestBorrowedStructParamMoveClonesOwnedField(t *testing.T) {
	ir := mustEmit(t, borrowedStructParamSrc)
	body := irFunc(ir, "id")
	if body == "" {
		t.Fatalf("could not locate @id in the emitted IR")
	}
	// The struct param `ctx` is borrowed; `r = ctx` must give the destination
	// its own copy of the owned str field. Without the clone the field buffer
	// is shared with the caller and double-freed at scope exit.
	if !strings.Contains(body, "@str_clone(") {
		t.Errorf("borrowed struct param move into @id's result did not clone the owned str field; "+
			"the destination aliases the caller's buffer and will double-free:\n%s", body)
	}
}

// recursiveBorrowedStructSrc is the shape that actually crashed cov-measure.no:
// a struct param re-bound into the out-param (`r = ctx`), then passed as the
// ctx argument of a recursive call in a loop, then the recursion result re-bound
// into the same out-param (`r = sub`). Before the fix this double-freed the
// buffer at 6/6 runs (rc 133); after it, 6/6 rc 0 and cov-measure no longer
// aborts. Both `r = ctx` and `r = sub` are OpMove into the out-param and must
// each give the destination an owned copy.
const recursiveBorrowedStructSrc = `bx {
    s str
    n i64
}

walk = (ctx bx, depth i64) (r bx) {
    r = ctx
    i <- [0..depth) {
        sub = walk(r, 0)
        r = sub
    }
}

v = bx { s: 'a-heap-string-long-enough-to-matter', n: 7 }
w = walk(v, 2)
print(w.s)
`

func TestRecursiveBorrowedStructParamClones(t *testing.T) {
	ir := mustEmit(t, recursiveBorrowedStructSrc)
	body := irFunc(ir, "walk")
	if body == "" {
		t.Fatalf("could not locate @walk in the emitted IR")
	}
	if !strings.Contains(body, "@str_clone(") {
		t.Errorf("recursive struct rebind did not clone the owned field; the out-param "+
			"aliases the borrowed param buffer across recursion and double-frees:\n%s", body)
	}
}

// podStructParamSrc is the NEGATIVE CONTROL: a struct with NO owned fields.
// Moving a borrowed POD parameter is a plain bitwise copy (correct — nothing
// to alias), and must NOT gain a spurious clone. This guards the fix from
// over-reaching into structs that own no heap.
const podStructParamSrc = `pt {
    x i64
    y i64
}

id = (ctx pt) (r pt) {
    r = ctx
}

v = pt { x: 3, y: 7 }
w = id(v)
print(w.x)
`

func TestPodStructParamMoveDoesNotClone(t *testing.T) {
	ir := mustEmit(t, podStructParamSrc)
	body := irFunc(ir, "id")
	if body == "" {
		t.Fatalf("could not locate @id in the emitted IR")
	}
	if strings.Contains(body, "@str_clone(") || strings.Contains(body, "@vec_clone(") {
		t.Errorf("a POD struct param move gained a spurious clone; nothing is owned, "+
			"so a deep copy is wasted work:\n%s", body)
	}
}
