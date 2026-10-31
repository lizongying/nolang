package parser

import "testing"

// TestParseDocLinkLine pins the shapes that count as a doc link and the ones that
// must not. The negative cases carry the weight: an ordinary external link or a
// link in the middle of a sentence must never be mistaken for a translation key,
// or a doc comment would silently change meaning.
func TestParseDocLinkLine(t *testing.T) {
	cases := []struct {
		in     string
		label  string
		anchor string
		ok     bool
	}{
		// Positive.
		{"[get](#args.get)", "get", "args.get", true},
		{"[](#args.get)", "", "args.get", true},
		{"  [count](#args.count)  ", "count", "args.count", true},
		{"[a b](#x-y_z.1)", "a b", "x-y_z.1", true},

		// Negative: no `#` — an external or relative link, not a catalog entry.
		{"[docs](https://example.com)", "", "", false},
		{"[readme](README.md)", "", "", false},
		// Negative: empty anchor.
		{"[get](#)", "", "", false},
		// Negative: not a standalone line.
		{"see [get](#args.get) for details", "", "", false},
		{"- [get](#args.get)", "", "", false},
		// Negative: anchor with characters that would make the boundary fuzzy.
		{"[get](#args get)", "", "", false},
		{"[get](#args#get)", "", "", false},
		// Negative: reference-style link.
		{"[a][b](#c)", "", "", false},
		// Negative: plain prose.
		{"get: 獲取第 i 個參數", "", "", false},
		{"", "", "", false},
		{"[get](#args.get) trailing", "", "", false},
	}
	for _, c := range cases {
		label, anchor, ok := ParseDocLinkLine(c.in)
		if ok != c.ok || anchor != c.anchor || (ok && label != c.label) {
			t.Errorf("ParseDocLinkLine(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.in, label, anchor, ok, c.label, c.anchor, c.ok)
		}
	}
}

// TestSplitDocBlock covers the group-level rule: only the LAST line may be the
// link, and the text is everything above it.
func TestSplitDocBlock(t *testing.T) {
	cases := []struct {
		name   string
		in     []string
		block  string
		anchor string
		has    bool
	}{
		{
			name:   "two prose lines plus link",
			in:     []string{" get: 獲取第 i 個參數", " 委派至 os.arg(i)", " [get](#args.get)"},
			block:  "get: 獲取第 i 個參數\n委派至 os.arg(i)",
			anchor: "args.get",
			has:    true,
		},
		{
			name:  "no link",
			in:    []string{" just a comment", " and another"},
			block: "just a comment\nand another",
		},
		{
			name:   "link only, no prose",
			in:     []string{"[get](#args.get)"},
			block:  "",
			anchor: "args.get",
			has:    true,
		},
		{
			name:  "link not last",
			in:    []string{"[get](#args.get)", " still prose"},
			block: "[get](#args.get)\nstill prose",
		},
		{
			name:  "trailing blank lines trimmed",
			in:    []string{" prose", "", ""},
			block: "prose",
		},
		{name: "empty group", in: nil, block: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			block, anchor, has := SplitDocBlock(c.in)
			if block != c.block || anchor != c.anchor || has != c.has {
				t.Fatalf("SplitDocBlock(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.in, block, anchor, has, c.block, c.anchor, c.has)
			}
		})
	}
}

// TestCommentCarriesDocLink pins the marker kinds that are eligible. `;;` and
// `;;block` are excluded on purpose: the generator reads raw source, where a block
// comment's interior lines carry no `;` prefix, so honouring them would make the
// two sides see different blocks.
func TestCommentCarriesDocLink(t *testing.T) {
	if !CommentCarriesDocLink(";") {
		t.Error(`marker ";" must be eligible`)
	}
	for _, marker := range []string{"", ";;", ";;block", "//"} {
		if CommentCarriesDocLink(marker) {
			t.Errorf("marker %q must NOT be eligible", marker)
		}
	}
}
