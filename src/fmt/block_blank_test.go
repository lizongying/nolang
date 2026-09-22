package fmt

import "testing"

// REGRESSION GUARD (the 「註釋 + 註解 + 陳述」node rule):
//
//  1. A block's FIRST statement is separated from `{` by a blank line **when that
//     statement carries a node head** — a doc comment and/or an annotation line
//     (`#{...}`). Such a statement is a node of its own (see nodeHeadWillEmit);
//     gluing it to `{` reads as if the comment belongs to the block opening.
//
//  2. A first statement with NO node head still gets no invented blank line:
//     `{` and the body stay adjacent unless the source had a blank there.
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
