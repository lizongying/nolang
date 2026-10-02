package mir

import (
	"strings"
	"testing"
)

// p4HeaderABISrc exercises the owned shapes P4 ("full") routes through the RC
// allocator: a heap str (built by concat) and a vec literal, plus their frees.
const p4HeaderABISrc = `
mk = (a str, b str) (r str) {
    r = a + b
}

main = () {
    s = mk('hello', ' world')
    print(s)
    v = [1, 2, 3]
    print(len(v))
}
`

// TestP4OwnedBuffersUseTheHeaderAllocator pins the "full" P4 ABI (§3.4): the
// language's OWN value buffers are allocated by @nolang_rc_alloc and released by
// @nolang_free, never by raw libc @malloc/@free. Without this the buffers carry
// no header and therefore cannot be retained — which is the entire point of P4,
// and the prerequisite for the "source still live ⇒ retain" half of §1.6.
func TestP4OwnedBuffersUseTheHeaderAllocator(t *testing.T) {
	mod := lowerForTest(t, p4HeaderABISrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	if !strings.Contains(ir, "call void @nolang_free(i8* %data)") {
		t.Errorf("@str_free does not route its free through @nolang_free")
	}
	if !strings.Contains(ir, "call void @nolang_free(i8* %ptr)") {
		t.Errorf("@vec_free does not route its free through @nolang_free")
	}
	// The allocator writes the header tag...
	if !strings.Contains(ir, "store i64 7957698232306827265, i64* %fp") {
		t.Errorf("@nolang_rc_alloc does not write the header tag into flags")
	}
	// ...and @nolang_free verifies it before touching the header.
	free := irFunc(ir, "nolang_free")
	if free == "" {
		t.Fatalf("@nolang_free is not defined in the emitted IR")
	}
	if !strings.Contains(free, "icmp eq i64 %flags, 7957698232306827265") {
		t.Errorf("@nolang_free does not verify the header tag before touching the header")
	}
	if !strings.Contains(free, "call void @free(i8* %p)") {
		t.Errorf("@nolang_free lost its raw fallback for pointers that are not header blocks")
	}
	if !strings.Contains(free, "call void @free(i8* %base)") {
		t.Errorf("@nolang_free does not free the block base when rc reaches zero")
	}
}

// TestP4NoRawFreeOnOwnedBuffers is the negative half of the same invariant: every
// remaining call to raw libc @free must live inside a helper that legitimately
// owns the raw pointer. A raw @free anywhere else is a site the swap missed —
// i.e. a buffer that gets freed 16 bytes off its base, which is heap corruption
// rather than a test failure, so it must be caught statically.
func TestP4NoRawFreeOnOwnedBuffers(t *testing.T) {
	mod := lowerForTest(t, p4HeaderABISrc)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// How many raw frees each allowed helper is expected to contain:
	//   @nolang_free       -> the guarded raw fallback (%p) + the base free
	//   @nolang_rc_release -> the base free
	allowed := map[string]int{"nolang_free": 2, "nolang_rc_release": 1}
	for _, part := range strings.Split(ir, "\ndefine ") {
		n := strings.Count(part, "call void @free(i8*")
		if n == 0 {
			continue
		}
		head := part
		if i := strings.IndexByte(head, '\n'); i >= 0 {
			head = head[:i]
		}
		matched := ""
		for name := range allowed {
			if strings.Contains(head, "@"+name+"(") {
				matched = name
			}
		}
		if matched == "" {
			t.Errorf("raw @free outside the RC helpers in %q — a swap site was missed", head)
			continue
		}
		if n != allowed[matched] {
			t.Errorf("%s contains %d raw @free call(s), want %d", matched, n, allowed[matched])
		}
	}
}
