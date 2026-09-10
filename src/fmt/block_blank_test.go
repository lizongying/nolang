package fmt

import "testing"

// REGRESSION GUARD (two rules, deliberately asymmetric):
//
//  1. The formatter must NOT invent a blank line between '{' and the FIRST
//     statement of a block, even when that statement carries a doc comment
//     (function bodies pass openBraceLine=0, so the blank after `{` is always
//     collapsed by design).
//
//  2. A doc-commented statement that is NOT the first in its block DOES get a
//     separating blank line above it — this mirrors the top-level rule in
//     api.go (`|| f.hasDocComment(stmt)`) so that a comment reads as the title
//     of the code below it instead of being glued to the previous statement:
//
//     y = 2
//
//     ; inner comment
//     y = y + 1
//
//     Removing rule 2 from the `i > 0` gap logic makes `no fmt` stop
//     separating block-level comments (the regression this test now pins).
func TestFormatNoSpuriousBlankAfterOpenBraceBeforeComment(t *testing.T) {
	src := "main = () {\n    x = 1\n}\n\nhelper = () {\n    ; helper comment\n    y = 2\n    ; inner comment\n    y = y + 1\n}\n"

	want := "main = () {\n    x = 1\n}\n\nhelper = () {\n    ; helper comment\n    y = 2\n\n    ; inner comment\n    y = y + 1\n}\n"

	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != want {
		t.Fatalf("formatter output mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
}

// TestFormatBlankBeforeBlockCommentAfterBrace pins the real-world shape that
// triggered this fix in std/fs.no: a section comment glued directly under a
// block-closing line must be pushed down by one blank line.
func TestFormatBlankBeforeBlockCommentAfterBrace(t *testing.T) {
	src := "f = (total i64, size i64) () {\n    {\n        total >= size -> break\n    } (total < size)\n    ; 截斷到實際讀取長度\n    total = 0\n}\n"

	want := "f = (total i64, size i64) {\n    {\n        total >= size -> break\n    } (total < size)\n\n    ; 截斷到實際讀取長度\n    total = 0\n}\n"

	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != want {
		t.Fatalf("formatter output mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
}
