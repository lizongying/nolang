package checker

import "testing"

// TestValidateFieldTags covers the field-level `#{inline}` rules: placement
// (only on a field that can be a struct) and acyclicity (an inlined struct is
// stored inside its host, so a cycle has no finite size).
func TestValidateFieldTags(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "inline on a struct-typed field is accepted",
			src: `pt {
    x i64
}
holder {
    #{inline} p pt
}`,
			want: 0,
		},
		{
			name: "unannotated struct-typed field is the default and accepted",
			src: `pt {
    x i64
}
holder {
    p pt
}`,
			want: 0,
		},
		{
			name: "inline on a scalar field is rejected",
			src: `holder {
    #{inline} n i64
}`,
			want: 1,
		},
		{
			name: "inline on a str field is rejected",
			src: `holder {
    #{inline} s str
}`,
			want: 1,
		},
		{
			name: "inline on an option field is rejected",
			src: `pt {
    x i64
}
holder {
    #{inline} p ?pt
}`,
			want: 1,
		},
		{
			name: "inline self reference is rejected",
			src: `node {
    val i64
    #{inline} next node
}`,
			want: 1,
		},
		{
			name: "inline cycle between two structs is rejected",
			src: `a {
    #{inline} b b
}
b {
    #{inline} a a
}`,
			want: 1,
		},
		{
			name: "self reference through an owned pointer field is fine",
			src: `node {
    val i64
    next node
}`,
			want: 0,
		},
		{
			name: "inline through a fixed array still counts as a cycle",
			src: `a {
    #{inline} b [2]b
}
b {
    #{inline} a a
}`,
			want: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ValidateFieldTags(parseProg(t, c.src))
			if len(res) != c.want {
				t.Fatalf("expected %d error(s), got %d: %+v", c.want, len(res), res)
			}
			for _, r := range res {
				if r.TraceID != "fieldtag1" {
					t.Fatalf("unexpected TraceID %q: %+v", r.TraceID, r)
				}
			}
		})
	}
}
