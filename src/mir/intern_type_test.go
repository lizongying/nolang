package mir

import "testing"

// TestInternTypeNestedElemSurvivesRealloc pins the fix for a dangling-pointer
// write in Module.internType.
//
// internType used to keep `t := &m.Types[tid]` and then call m.internType(elemRaw)
// to resolve a composite type's element. That nested call APPENDS to m.Types and
// can therefore REALLOCATE the backing array, leaving `t` pointing into the freed
// one — so `t.Elem = ...` (and `t.Func = ...`) stored into the old array and
// vanished. The `m.Types[tid].Elem = m.internType(...)` spelling is no better:
// Go evaluates the left-hand index before the right-hand call.
//
// The failure was order-dependent, which is why it survived so long. m.TypeMap's
// lookup turns every LATER intern of the same raw into an early no-append return,
// so only the FIRST composite type to reference a not-yet-interned element lost
// it. `[512]byte` is exactly that case in std regexp (`cls [512]byte`): Elem came
// out NoType, llvmTypeOf's KindArray fallback rendered it as the ZERO-BYTE
// `[0 x i64]`, and the enclosing struct was laid out 512 bytes too small. Every
// `cls[i] = c` then wrote past the allocation and aborted with SIGBUS
// (tests/dump2.no, chain-copy.no, regexp.no, ...).
//
// Each case below interns its composite type on a FRESH module so the element raw
// is still unknown at that moment; that is the ordering the bug required.
func TestInternTypeNestedElemSurvivesRealloc(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		m := NewModule("t")
		tid := m.internType("[512]byte")
		ty := m.Type(tid)
		if ty == nil {
			t.Fatal("internType returned a nil type")
		}
		if ty.Kind != KindArray {
			t.Fatalf("kind = %v, want KindArray", ty.Kind)
		}
		if len(ty.Sizes) != 1 || ty.Sizes[0] != 512 {
			t.Fatalf("Sizes = %v, want [512]", ty.Sizes)
		}
		if ty.Elem == NoType {
			t.Fatal("Elem = NoType: the element write was lost (stale slice pointer)")
		}
		if elem := m.Type(ty.Elem); elem == nil || elem.Raw != "byte" {
			t.Fatalf("Elem = %v, want the interned \"byte\" type", elem)
		}
		cg := &codegen{mod: m}
		if got := cg.llvmTypeOf(ty); got != "[512 x i8]" {
			t.Fatalf("llvmTypeOf = %q, want \"[512 x i8]\" (the buggy path yields the zero-byte fallback \"[0 x i64]\")", got)
		}
		if again := m.internType("[512]byte"); again != tid {
			t.Fatalf("re-intern gave tid %d, want %d", again, tid)
		}
	})

	t.Run("slice", func(t *testing.T) {
		m := NewModule("t")
		ty := m.Type(m.internType("[]zz-newtype"))
		if ty == nil || ty.Kind != KindSlice {
			t.Fatalf("kind = %v, want KindSlice", ty)
		}
		if ty.Elem == NoType {
			t.Fatal("Elem = NoType: the element write was lost (stale slice pointer)")
		}
	})

	t.Run("option", func(t *testing.T) {
		m := NewModule("t")
		ty := m.Type(m.internType("?zz-newtype"))
		if ty == nil || ty.Kind != KindOption {
			t.Fatalf("kind = %v, want KindOption", ty)
		}
		if ty.Elem == NoType {
			t.Fatal("Elem = NoType: the element write was lost (stale slice pointer)")
		}
	})

	t.Run("func", func(t *testing.T) {
		m := NewModule("t")
		ty := m.Type(m.internType("fn(i64)(i64)"))
		if ty == nil || ty.Kind != KindFunc {
			t.Fatalf("kind = %v, want KindFunc", ty)
		}
		if ty.Func == nil {
			t.Fatal("Func = nil: the signature write was lost (stale slice pointer)")
		}
		if len(ty.Func.Params) != 1 || len(ty.Func.Results) != 1 {
			t.Fatalf("Func = %+v, want 1 param and 1 result", ty.Func)
		}
	})
}
