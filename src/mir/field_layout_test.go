package mir

import "testing"

// TestFieldIsPointerInlineOverride pins the one thing `#{inline=...}` is for:
// a field can be laid out AGAINST the module-wide default that
// NOLANG_FIELD_PTR selects.
//
// The matrix that must hold:
//
//	annotation        FieldPtrLayout=off   FieldPtrLayout=on
//	(none)            inline               pointer
//	#{inline=true}    inline               inline
//	#{inline=false}   pointer              pointer
//
// The bottom-left cell is the one that used to be wrong: `#{inline=false}` fell
// through to the default, so in the default state it stayed inline and could
// not express "make this field a pointer" at all. That also meant a recursive
// type could only be written by flipping the global switch for the whole
// program.
//
// Layout is decided here and ownership is decided by the tag; this test is only
// about the former. A forced pointer is still FieldTagOwned (see fieldTag), so
// it is dropped like any other owning field.
func TestFieldIsPointerInlineOverride(t *testing.T) {
	newMod := func() *Module {
		m := NewModule("t")
		m.StructNames["pt"] = true
		m.StructFields["pt"] = []FieldInfo{
			{Name: "x", TypeRaw: "i64"},
		}
		return m
	}

	// tag is what fieldTag() produces for the same declaration. It is set here
	// because FieldIsPointer still consults it for the DEFAULT case (guard 1):
	// a FieldInfo that never went through tag analysis keeps the by-value
	// layout, which is what makes the flip safe to land incrementally.
	cases := []struct {
		name string
		raw  string
		lay  FieldLayout
		tag  FieldTag
		off  bool // FieldPtrLayout unset
		on   bool // FieldPtrLayout set
		why  string
	}{
		{"no annotation follows the default", "pt", FieldLayoutDefault, FieldTagOwned,
			false, true,
			"unannotated struct field: inline by default, pointer under the flip"},
		{"inline=true is inline in both states", "pt", FieldLayoutInline, FieldTagInline,
			false, false,
			"#{inline=true} opts out of the pointer layout"},
		{"inline=false is a pointer in both states", "pt", FieldLayoutPointer, FieldTagOwned,
			true, true,
			"#{inline=false} forces the pointer layout even without the global flip"},
		{"inline=false on a scalar stays inline", "i64", FieldLayoutPointer, FieldTagInline,
			false, false,
			"only a struct can be stored behind a pointer; the checker rejects this spelling on a scalar"},
		{"inline=false on str stays inline", "str", FieldLayoutPointer, FieldTagOwned,
			false, false,
			"str is an owned leaf: its descriptor must stay inline or the drop walk breaks"},
	}

	orig := FieldPtrLayout
	defer func() { FieldPtrLayout = orig }()

	for _, c := range cases {
		f := FieldInfo{Name: "f", TypeRaw: c.raw, Layout: c.lay, Tag: c.tag}

		FieldPtrLayout = false
		if got := newMod().FieldIsPointer(f); got != c.off {
			t.Errorf("%s: with FieldPtrLayout off, FieldIsPointer(%s, %s) = %v, want %v",
				c.name, c.raw, c.lay, got, c.off)
		}
		FieldPtrLayout = true
		if got := newMod().FieldIsPointer(f); got != c.on {
			t.Errorf("%s: with FieldPtrLayout on, FieldIsPointer(%s, %s) = %v, want %v",
				c.name, c.raw, c.lay, got, c.on)
		}
	}
}

// TestFieldLayoutString keeps the String output aligned with the spelling the
// language uses, because it shows up in MIR dumps people read while debugging.
func TestFieldLayoutString(t *testing.T) {
	cases := map[FieldLayout]string{
		FieldLayoutDefault: "default",
		FieldLayoutInline:  "inline",
		FieldLayoutPointer: "pointer",
	}
	for l, want := range cases {
		if got := l.String(); got != want {
			t.Errorf("FieldLayout(%d).String() = %q, want %q", l, got, want)
		}
	}
}
