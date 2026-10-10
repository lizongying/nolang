package mir

import (
	"strings"
	"testing"
)

// strIndexGrowSrc writes past a string's capacity with a dynamic index. A
// `%str-long` built by `with-cap(4)` owns a 4-byte buffer (cap = 4). Storing at
// index 5 must GROW the buffer before the store, otherwise the byte lands one
// past the allocation — a heap-buffer-overflow / adjacent-heap corruption. This
// is the "%str-long 下標寫無增長護欄" memory-safety gap.
//
// The runtime smoke test (tests/mem-safety/str-index-grow.no) proves the guard
// is actually taken and keeps the program memory-safe; this Go test pins the
// codegen structure so the guard cannot be silently removed.
const strIndexGrowSrc = `
main = () {
    s str = with-cap(4)
    n i64 = 5
    s[n] = 'e'
}
`

// TestStrIndexStoreGrowsBeyondCap pins the growth guard on the `%str-long`
// index-store path. As with %vec (ensureVecBuffer), a `s[i] = c` store whose
// index reaches past the owned buffer's cap must reallocate and free the old
// buffer. The observable is the emitted IR: a grow block (`slbG`) that frees the
// previous data pointer (`call void @nolang_free(i8* %slbdi...)`) and copies the
// live bytes (`@llvm.memcpy`).
//
// Before the fix, ensureStrLongBuffer only handled the null-data case and fell
// straight through to the store on any non-null buffer, emitting NO grow, NO
// free, and NO copy — the store wrote past the allocated bytes. After the fix
// the grow block (realloc + memcpy + free) is emitted for the owned-string
// beyond-cap write.
func TestStrIndexStoreGrowsBeyondCap(t *testing.T) {
	mod := lowerForTest(t, strIndexGrowSrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	if !strings.Contains(ir, "slbG") {
		t.Errorf("str index store beyond cap did not emit a growth guard: "+
			"the store writes one byte past the owned buffer (heap overflow). "+
			"expected a grow block (slbG) in the IR\n%s", ir)
	}
	// The grow must release the old buffer (not just branch): the only place a
	// `%slbdi` (loaded data pointer) is freed is the grow block. The grow must
	// also copy the live bytes forward so existing content survives the realloc.
	if !strings.Contains(ir, "call void @nolang_free(i8* %slbdi") {
		t.Errorf("str grow block does not free the old buffer: a grow that leaks "+
			"the previous allocation is not a real grow\n%s", ir)
	}
	if !strings.Contains(ir, "@llvm.memcpy.p0i8.p0i8.i64") {
		t.Errorf("str grow block does not copy the live bytes forward: "+
			"existing string content would be lost on grow\n%s", ir)
	}
}
