package fmt

import "testing"

// REGRESSION GUARD (the 「註釋 + 註解 + 陳述」node rule):
//
//  1. A block's FIRST statement is separated from `{` by a blank line **when that
//     statement carries a node head** — a doc comment and/or an annotation line
//     (`#{...}`). Such a statement is a node of its own (see nodeHeadWillEmit);
//     gluing it to `{` reads as if the comment belongs to the block opening.
//
//  2. A first statement with NO node head gets NO blank line at all: an
//     authored blank between `{` and a plain first statement is REMOVED on
//     format (see TestFormatBlockTopBlankOnlyForNodeHead).
//
//  3. A node that is NOT first in its block always gets a separating blank line
//     above it, mirroring the top-level rule in api.go, so a comment reads as the
//     title of the code below it instead of being glued to the previous statement:
//
//     y = 2
//
//     ; inner comment
//     y = y + 1
func TestFormatBlankAfterOpenBraceBeforeNodeHead(t *testing.T) {
	src := "main = () {\n    x = 1\n}\n\nhelper = () {\n    ; helper comment\n    y = 2\n    ; inner comment\n    y = y + 1\n}\n"

	want := "main = () {\n    x = 1\n}\n\nhelper = () {\n\n    ; helper comment\n    y = 2\n\n    ; inner comment\n    y = y + 1\n}\n"

	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != want {
		t.Fatalf("formatter output mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
	// Idempotency: the injected blank must not multiply on a second pass.
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%s\n--- pass2 ---\n%s", out, out2)
	}
}

// TestFormatNoBlankAfterOpenBraceWithoutNodeHead pins the other half of rule 1:
// an ordinary first statement (no comment, no annotation) must NOT get a blank
// line after `{` — the formatter would otherwise inflate every block.
func TestFormatNoBlankAfterOpenBraceWithoutNodeHead(t *testing.T) {
	src := "main = () {\n    x = 1\n    y = 2\n}\n"
	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != src {
		t.Fatalf("spurious blank inserted after '{':\n--- want ---\n%q\n--- got ---\n%q", src, out)
	}
}

// TestFormatBlankBeforeBlockCommentAfterBrace pins the real-world shape that
// triggered this fix in std/fs.no: a section comment glued directly under a
// block-closing line must be pushed down by one blank line.
func TestFormatBlankBeforeBlockCommentAfterBrace(t *testing.T) {
	src := "f = (total i64, size i64) () {\n    {\n        total >= size -> break\n    } (total < size)\n    ; 截斷到實際讀取長度\n    total = 0\n}\n"

	want := "f = (total i64, size i64) {\n    (total < size) {\n        total >= size -> break\n    }\n\n    ; 截斷到實際讀取長度\n    total = 0\n}\n"

	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != want {
		t.Fatalf("formatter output mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
}

// REGRESSION GUARD (match/if 組 block 顶部空行保留): a blank line the author
// wrote between a match block's `{` and its FIRST arm must survive formatting —
// the same source-blank rule other block forms apply via formatBlockInner
// (e.g. the loop bodies in std/archive/gzip.no). Previously formatSurfaceMatch
// emitted no blank for i==0 regardless of source, so the top blank in a bare
// if-group `{ cond -> ... }` was silently stripped and could not be kept.
// Without a source blank, nothing is invented (arm-to-arm separation is a
// separate pre-existing rule and must stay untouched).
func TestFormatBlankAfterMatchOpenBracePreserved(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bare if-group keeps top blank",
			in:   "f = (sym i64) {\n    {\n\n        sym < 16 -> print('a')\n\n        -> _\n    }\n}\n",
			want: "f = (sym i64) {\n    {\n\n        sym < 16 -> print('a')\n\n        -> _\n    }\n}\n",
		},
		{
			name: "bare if-group without top blank stays compact",
			in:   "f = (sym i64) {\n    {\n        sym < 16 -> print('a')\n        -> _\n    }\n}\n",
			want: "f = (sym i64) {\n    {\n        sym < 16 -> print('a')\n\n        -> _\n    }\n}\n",
		},
		{
			name: "option match keeps top blank",
			in:   "f = (v []i64) {\n    x ?i64 = v[0]\n    x: {\n\n        ok -> print(it)\n\n        -> _\n    }\n}\n",
			want: "f = (v []i64) {\n    x ?i64 = v[0]\n    x: {\n\n        ok -> print(it)\n\n        -> _\n    }\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errs := FormatFileWithErrors(tc.in)
			if len(errs) > 0 {
				t.Fatalf("FormatFileWithErrors errors: %v", errs)
			}
			if out != tc.want {
				t.Fatalf("match top-blank handling wrong:\n--- want ---\n%q\n--- got ---\n%q", tc.want, out)
			}
			out2, _ := FormatFileWithErrors(out)
			if out2 != out {
				t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
			}
		})
	}
}

// REGRESSION GUARD (區塊頂部空行規範 —— 只有 node 頭才配得上空行):
// a blank line between a block's `{` and its FIRST statement is only
// meaningful when that statement carries a node head (doc comment and/or an
// `#{...}` annotation) — the blank keeps the annotation from being misread as
// belonging to the code ABOVE the block (bzip2.no: `i <- [0..alpha-size): {`
// + `#{index-out=zero}` + `l = lengths[i]`). A plain first statement has no
// such ambiguity, so an authored blank there must be REMOVED on format:
//   - loop body `.limit[i] = 0` (bzip2.no:104-106)  → blank removed
//   - loop body starting with a bare match `e: {`  → blank removed
//   - function body `sym = -1` (bzip2.no:211-213)  → blank removed
//   - loop body starting with `#{index-out=zero}` (bzip2.no:127-130) → kept
func TestFormatBlockTopBlankOnlyForNodeHead(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain first stmt: loop-top blank removed",
			in:   "f = (limit []i64) {\n    i <- [0..24): {\n\n        limit[i] = 0\n    }\n}\n",
			want: "f = (limit []i64) {\n    i <- [0..24): {\n        limit[i] = 0\n    }\n}\n",
		},
		{
			name: "plain first stmt: function-top blank removed",
			in:   "f = () (sym i64) {\n\n    sym = -1\n}\n",
			want: "f = () (sym i64) {\n    sym = -1\n}\n",
		},
		{
			name: "bare match first stmt: loop-top blank removed",
			in:   "f = (xs []i64) {\n    e <- xs: {\n\n        e: {\n            ok -> it = 0\n\n            -> _\n        }\n    }\n}\n",
			want: "f = (xs []i64) {\n    e <- xs: {\n        e: {\n            ok -> it = 0\n\n            -> _\n        }\n    }\n}\n",
		},
		{
			name: "annotation first stmt: loop-top blank kept",
			in:   "f = (lengths []i64) {\n    i <- [0..3): {\n\n        #{index-out=zero}\n        l = lengths[i]\n    }\n}\n",
			want: "f = (lengths []i64) {\n    i <- [0..3): {\n\n        #{index-out=zero}\n        l = lengths[i]\n    }\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errs := FormatFileWithErrors(tc.in)
			if len(errs) > 0 {
				t.Fatalf("FormatFileWithErrors errors: %v", errs)
			}
			if out != tc.want {
				t.Fatalf("block-top blank rule wrong:\n--- want ---\n%q\n--- got ---\n%q", tc.want, out)
			}
			out2, _ := FormatFileWithErrors(out)
			if out2 != out {
				t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
			}
		})
	}
}
