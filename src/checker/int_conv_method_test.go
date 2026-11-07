package checker

import "testing"

// IntConvMethodName is the checker half of the "assignment target type is
// known ⇒ look up `<src>.to-<dst>`" rule: its verdict is what lets a NARROWING
// binding through the `15w45dqk` gate, so it must be exact. A wrong "" here
// means a legal conversion is rejected; a wrong hit means a value the backend
// never converts is silently accepted.
func TestIntConvMethodName(t *testing.T) {
	cases := []struct {
		name      string
		src, dst  string
		funcNames map[string]bool
		want      string
	}{
		{"builtin byte→i64", "byte", "i64", nil, "byte.to-i64"},
		{"builtin u8→u64", "u8", "u64", nil, "u8.to-u64"},
		{"builtin i16→i64", "i16", "i64", nil, "i16.to-i64"},
		{"builtin u32→u64", "u32", "u64", nil, "u32.to-u64"},
		{"no such method", "u64", "i32", nil, ""},
		{"identity needs no conversion", "i64", "i64", nil, ""},
		{"non-integer source", "str", "i64", nil, ""},
		{"non-integer target", "i64", "str", nil, ""},
		{"empty raw", "", "i64", nil, ""},
		{
			"user-defined method",
			"u16", "i32",
			map[string]bool{"u16.to-i32": true},
			"u16.to-i32",
		},
		{
			"user method for another pair does not leak",
			"u16", "i32",
			map[string]bool{"u16.to-i64": true},
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IntConvMethodName(c.src, c.dst, c.funcNames); got != c.want {
				t.Errorf("IntConvMethodName(%q, %q) = %q, want %q", c.src, c.dst, got, c.want)
			}
		})
	}
}
