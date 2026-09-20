package checker

import "testing"

// TestValidateInlineBoolSpellings covers the explicit boolean forms of the
// `#{inline}` tag:
//
//	#{inline}        shorthand for `#{inline=true}`
//	#{inline=true}   by-value layout
//	#{inline=false}  explicitly the DEFAULT pointer layout
//
// Two things must hold. First, a non-boolean value is a diagnostic rather than a
// silent "true". Second, `#{inline=false}` must NOT create an inline embedding
// edge — that is the whole difference between the two values, and it is what
// makes a mutually recursive pair of structs legal when both sides say "pointer".
func TestValidateInlineBoolSpellings(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "inline=true on a struct-typed field is accepted",
			src: `pt {
    x i64
}
holder {
    p pt #{inline=true}
}`,
			want: 0,
		},
		{
			name: "inline=false on a struct-typed field is accepted",
			src: `pt {
    x i64
}
holder {
    p pt #{inline=false}
}`,
			want: 0,
		},
		{
			name: "inline=1 is accepted as true",
			src: `pt {
    x i64
}
holder {
    p pt #{inline=1}
}`,
			want: 0,
		},
		{
			name: "inline=0 is accepted as false",
			src: `pt {
    x i64
}
holder {
    p pt #{inline=0}
}`,
			want: 0,
		},
		{
			name: "inline=foo is rejected, not read as true",
			src: `pt {
    x i64
}
holder {
    p pt #{inline=foo}
}`,
			want: 1,
		},
		{
			name: "inline='true' (a string) is rejected",
			src: `pt {
    x i64
}
holder {
    p pt #{inline='true'}
}`,
			want: 1,
		},
		{
			name: "inline=false on a scalar field is rejected: a scalar is ALWAYS inline",
			src: `holder {
    n i64 #{inline=false}
}`,
			want: 1,
		},
		{
			name: "inline=false on an enum definition is accepted",
			src: `#{inline=false}
color {
    red,
    green,
}`,
			want: 0,
		},
		{
			name: "inline=true on an enum definition is accepted",
			src: `#{inline=true}
color {
    red,
    green,
}`,
			want: 0,
		},
		{
			name: "inline=foo on an enum definition is rejected",
			src: `#{inline=foo}
color {
    red,
    green,
}`,
			want: 1,
		},
		{
			name: "mutual inline=true recursion is still a cycle",
			src: `a {
    b b #{inline=true}
}
b {
    a a #{inline=true}
}`,
			want: 1,
		},
		{
			name: "mutual inline=false is NOT a cycle: both fields are pointers",
			src: `a {
    b b #{inline=false}
}
b {
    a a #{inline=false}
}`,
			want: 0,
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
