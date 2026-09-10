package fmt

import "testing"

// REGRESSION GUARD: the formatter must not invent a blank line between '{'
// and the first statement of a block when the source has none — even if that
// statement carries a doc comment. Previously formatBlockInner used
// `|| f.hasDocComment(stmt)`, injecting a blank after every function/match/if
// body whose first statement was commented (and re-emitting it idempotently).
//
// Note: function bodies pass openBraceLine=0 (blank after `{` always
// collapsed by design), so a doc-commented first statement is the ONLY thing
// that could produce the spurious blank there — this test pins that case.
func TestFormatNoSpuriousBlankAfterOpenBraceBeforeComment(t *testing.T) {
	src := "main = () {\n    x = 1\n}\n\nhelper = () {\n    ; helper comment\n    y = 2\n    ; inner comment\n    y = y + 1\n}\n"

	out, errs := FormatFileWithErrors(src)
	if len(errs) > 0 {
		t.Fatalf("FormatFileWithErrors errors: %v", errs)
	}
	if out != src {
		t.Fatalf("formatter altered source (spurious blank line after '{'?):\n--- want ---\n%s\n--- got ---\n%s", src, out)
	}
}
