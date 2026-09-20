package mir

import (
	"testing"

	"github.com/lizongying/nolang/hir"
)

// TestFieldTagLayoutDecision pins the definition-site tag that Phase 1 will
// consume, with particular attention to the cases where the answer is
// counter-intuitive.
//
// The tag decides LAYOUT only: "does this field slot hold a pointer to a
// separately-allocated T, or the T itself?" It deliberately does NOT decide
// whether anything gets dropped -- ownership is TRANSITIVE, so `[16]str` is
// tagged Inline yet still owns 16 strings' worth of heap through its elements.
// Drop decisions must keep using ClassifyOwnership / Type.Owned. These cases
// exist so that confusion cannot be reintroduced silently.
func TestFieldTagLayoutDecision(t *testing.T) {
	// A module that knows one struct, `pt`, so isStructType can resolve it.
	newLowerer := func() *lowerer {
		m := NewModule("t")
		m.StructNames["pt"] = true
		m.StructFields["pt"] = []FieldInfo{
			{Name: "x", TypeRaw: "i64", Tag: FieldTagInline},
			{Name: "y", TypeRaw: "i64", Tag: FieldTagInline},
		}
		return &lowerer{pkg: &hir.Package{}, mod: m}
	}

	cases := []struct {
		raw  string
		want FieldTag
		why  string
	}{
		// --- the flip: struct-typed field defaults to a POINTER ---------------
		{"pt", FieldTagOwned, "struct-typed field => %T* (this is the Phase 1 flip)"},
		{"?pt", FieldTagOwned, "?T with T a struct => nullable pointer"},

		// --- container elements keep the ORIGINAL design (confirmed decision 3)
		{"[32]pt", FieldTagInline, "fixed-array ELEMENT stays inline; the slot is the array itself"},
		{"[16]str", FieldTagInline, "array of owned leaves: Inline tag, but elements still own heap"},
		{"[4][4]pt", FieldTagInline, "nested fixed array is still an inline array"},

		// --- owned leaves: already Owned today, layout unchanged ---------------
		{"str", FieldTagOwned, "str is an inline descriptor that owns its buffer"},
		{"vec", FieldTagOwned, "vec is an inline descriptor that owns its buffer"},
		{"[]pt", FieldTagOwned, "slice owns its backing block (but the ELEMENT is inline)"},
		{"?str", FieldTagOwned, "option of an owned leaf"},

		// --- everything else stays inline -------------------------------------
		{"i64", FieldTagInline, "scalar"},
		{"bool", FieldTagInline, "scalar"},
		{"txt", FieldTagInline, "txt is a plain 256-byte stack buffer that owns nothing"},
		{"", FieldTagInline, "empty raw"},

		// --- a qualified name that is NOT a registered struct -----------------
		{"nosuch.thing", FieldTagInline, "unknown qualified name must not become a pointer"},
		{"[32]nosuch.thing", FieldTagInline, "unknown qualified element type stays an inline array"},
	}

	for _, tc := range cases {
		l := newLowerer()
		if got := l.fieldTag(0, tc.raw); got != tc.want {
			t.Errorf("fieldTag(%q) = %v, want %v -- %s", tc.raw, got, tc.want, tc.why)
		}
	}
}

// TestIsStructTypeRejectsArraysAndBuiltins pins the two guards that keep the
// container-element decision correct.
//
// `isStructType` looks at the WHOLE raw string, so `[N]T` is never a struct --
// that is what makes `[32]pt` fall through to Inline. And str/vec/txt are
// excluded explicitly, because they are structs in the type registry (their
// descriptors are real struct layouts) but must not be treated as struct-typed
// fields: `str`'s ownership is ClassifyOwnership's job, and tagging `txt` Owned
// would emit a drop for a stack buffer.
func TestIsStructTypeRejectsArraysAndBuiltins(t *testing.T) {
	l := func() *lowerer {
		m := NewModule("t")
		m.StructNames["pt"] = true
		m.StructFields["pt"] = []FieldInfo{{Name: "x", TypeRaw: "i64"}}
		return &lowerer{pkg: &hir.Package{}, mod: m}
	}()

	cases := []struct {
		raw  string
		want bool
	}{
		{"pt", true},
		{"[32]pt", false},       // array: whole-string match fails => Inline
		{"[]pt", false},         // slice: not a bare struct name either
		{"str", false},          // builtin descriptor, excluded on purpose
		{"vec", false},          // builtin descriptor, excluded on purpose
		{"txt", false},          // builtin descriptor, excluded on purpose
		{"", false},             // empty
		{"nosuch.thing", false}, // qualified but unregistered => degrades to Inline
	}

	for _, tc := range cases {
		if got := l.isStructType(tc.raw); got != tc.want {
			t.Errorf("isStructType(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestClassifyOwnershipDoesNotOwnFixedArrays pins the parseMapTypes branch that
// makes `[N]T` a FIXED ARRAY rather than a map.
//
// `[32]net.conn` and `[str]i64` both start with `[`, and only the bracket
// CONTENT tells them apart: all-digits means a size, anything else means a key
// type. If `[32]pt` were ever mistaken for a map it would become Owned, and
// Phase 1 would then heap-allocate a struct field that decision 3 says must
// stay inline -- so this asymmetry is load-bearing.
func TestClassifyOwnershipDoesNotOwnFixedArrays(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		// fixed arrays: NOT owned as a whole (their elements may still own)
		{"[32]pt", false},
		{"[16]str", false},
		{"[512]byte", false},
		{"[0]pt", false},
		// maps: owned
		{"[str]i64", true},
		{"[pt]str", true},
		// slices: owned
		{"[]pt", true},
		{"[]byte", true},
		// descriptors: owned
		{"str", true},
		{"vec", true},
		// options recurse
		{"?str", true},
		{"?[]pt", true},
		{"?pt", false}, // ?T where T owns nothing is not an owned leaf
		// scalars
		{"i64", false},
		{"txt", false},
		{"", false},
	}

	for _, tc := range cases {
		if got := ClassifyOwnership(tc.raw); got != tc.want {
			t.Errorf("ClassifyOwnership(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}
