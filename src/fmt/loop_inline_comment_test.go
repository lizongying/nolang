package fmt

import (
	"strings"
	"testing"
)

// TestFormatLoopInlineCommentIsIdempotent guards that a trailing `; comment`
// written on the SAME line as a bare range-for's inline single-statement body
// (`i <- [0..100): out.push(i); comment`) stays bound to that statement.
//
// Bug: for an inline body the block's `Token` is the NEWLINE *after* the
// statement (one line too far), so `stmtTokenEndLine(*ForStatement)` returned
// the wrong line. The same-line test in `attachInlineComment` then failed, the
// comment leaked to the enclosing block's trailing-comment list, and the
// formatter emitted it on its own line — flipping joined<->split on every pass
// (non-idempotent), losing the author's intended binding to `out.push(i)`.
func TestFormatLoopInlineCommentIsIdempotent(t *testing.T) {
	joined := "grow-vec = () (out []i64) {\n" +
		"    out = []\n" +
		"    i <- [0..100): out.push(i); 100 ci push triggers growth\n" +
		"    ; each growth leaks the old buffer\n" +
		"}\n"

	// Same-line comment must remain on the body's line (joined) — not split.
	out := FormatFile(joined)
	if out != joined {
		t.Errorf("loop inline comment was rewritten\n--- want (joined) ---\n%s\n--- got ---\n%s", joined, out)
	}

	// Idempotent across repeated passes (this is what previously oscillated).
	acc := joined
	for i := 0; i < 4; i++ {
		next := FormatFile(acc)
		if next != acc {
			t.Fatalf("formatting not idempotent on pass %d\n--- before ---\n%s\n--- after ---\n%s", i+1, acc, next)
		}
		acc = next
	}
	if !strings.Contains(acc, "out.push(i); 100 ci push triggers growth") {
		t.Errorf("inline comment lost its binding to out.push(i)\n--- output ---\n%s", acc)
	}

	// The standalone comment on its own line must stay on its own line.
	split := "grow-vec = () (out []i64) {\n" +
		"    out = []\n" +
		"    i <- [0..100): out.push(i)\n" +
		"    ; standalone note\n" +
		"}\n"
	if out := FormatFile(split); out != split {
		t.Errorf("standalone comment under a loop body should stay on its own line\n--- want ---\n%s\n--- got ---\n%s", split, out)
	}
}
