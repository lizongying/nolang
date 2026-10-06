package mir

import (
	"strings"
	"testing"
)

// TestGenericSliceMethodCallResolves pins that calling a user-defined method on a
// GENERIC slice receiver ([]i64 / []t, as opposed to the concrete []byte buffer)
// resolves to the CONCRETE registered function rather than being canonicalised
// to the non-existent generic stub `[]t.<method>`.
//
// The front end lowers `v.test-len()` (v: []i64) to a KIdent callee already named
// `[]i64.test-len`, which IS a registered function. resolveCallee's KIdent
// fallback used to run canonSliceRecv on it unconditionally, rewriting the valid
// concrete name to `[]t.test-len`. That function is never defined, so codegen
// died with "unknown callee []t.test-len in func main".
//
// The fix keeps a registered concrete name verbatim and only canonicalises the
// truly-generic fallback. The regression guard: EmitLLVM must succeed AND emit
// the concrete method as a real definition.
func TestGenericSliceMethodCallResolves(t *testing.T) {
	src := "[]i64.test-len = () (n i64) {\n    n = .len()\n}\n" +
		"main = () {\n    v []i64 = [1, 2, 3]\n    n = v.test-len()\n    print(n)\n}\n"
	mod, _ := lowerSrcWithDiags(t, src)
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed (generic-slice method call must resolve to the concrete definition): %v", err)
	}
	// The callee must NOT be the phantom generic stub.
	if strings.Contains(ll, "[]t.test-len") {
		t.Fatalf("callee leaked the unregistered generic form []t.test-len:\n%s", ll)
	}
	// The concrete method must be emitted (sanitized name contains test_len).
	if !strings.Contains(ll, "test_len") {
		t.Fatalf("concrete []i64.test-len definition was not emitted:\n%s", ll)
	}
}
