package fmt

import (
	"strings"
	"testing"
)

// REGRESSION GUARD (in-as-identifier statement boundary).
//
// `in` is a lexer keyword but is commonly used as a parameter / variable name
// (e.g. `aes-128-cbc-enc = (in []byte, …)`). When a statement is followed by a
// line whose first token is `in`, skipToStatementEnd must treat `in` as a
// statement boundary. Previously parser/stmt.go isStatementBoundary lacked
// lexer.IN, so the preceding statement's skipToStatementEnd swallowed the
// leading `in` of the NEXT statement and the source was corrupted:
//
//	in.len() == 0 -> out = 1     ⇒     .len() == 0 -> out = 1
//
// (also made the file non-idempotent). See parser/annotation.go's attach
// condition, which likewise had to accept lexer.IN.
func TestFormatInIdentifierNotSwallowed(t *testing.T) {
	in := "f = (in []byte) (out i64) {\n    #{overflow=wrap}\n    in.len() == 0 -> out = 1\n    out = 0\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if out != in {
		t.Fatalf("`in` identifier corrupted by formatter:\n--- want ---\n%q\n--- got ---\n%q", in, out)
	}
}

// REGRESSION GUARD (auto-propagated index assignment must render as `=`).
//
// parser/lowering.go maybeAutoPropagateIndex rewrites `x = v[i]` (an index
// expression yielding an option) into UnwrapAssignStatement{IsAutoPropagated:
// true} even in format-only mode (SkipUnwrapLowering gates the *expansion*, not
// the promotion). fmt/stmt.go used to render every UnwrapAssignStatement as
// `?=`, ignoring IsAutoPropagated, so `no fmt` silently rewrote `=` into `?=`
// on every save (semantically equivalent but a lossy, noisy diff).
func TestFormatAutoPropagatedIndexKeepsAssign(t *testing.T) {
	in := "f = (v []i64) (result ?i64) {\n    result = nil\n    x = v[0]\n    result = x\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if strings.Contains(out, "?=") {
		t.Fatalf("auto-propagated index assignment rewritten to `?=`:\n%q", out)
	}
	if out != in {
		t.Fatalf("output not stable:\n--- want ---\n%q\n--- got ---\n%q", in, out)
	}
}

// REGRESSION GUARD (doc comment above an auto-promoted index assignment).
//
// The auto-promotion above used to drop the statement's CommentedNode, so a doc
// comment sitting directly above `x = v[i]` vanished on format. The comment must
// survive, and a blank line must be inserted between the preceding statement and
// the comment (the block-inner doc-comment blank-line rule).
func TestFormatDocCommentBeforeAutoPromotedIndex(t *testing.T) {
	in := "f = (v []i64) (result ?i64) {\n    result = nil\n    ; read the first element\n    x = v[0]\n    result = x\n}\n"
	want := "f = (v []i64) (result ?i64) {\n    result = nil\n\n    ; read the first element\n    x = v[0]\n    result = x\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if !strings.Contains(out, "; read the first element") {
		t.Fatalf("doc comment before auto-promoted index assignment was dropped:\n%q", out)
	}
	if out != want {
		t.Fatalf("doc-comment blank line not inserted:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
}

// REGRESSION GUARD (standalone comment + line overflow annotation before a bare
// `{` match block must not be merged or dropped).
//
// A bare `{` match block may be preceded by an ordinary standalone comment line
// and a line-scoped `#{overflow=wrap}` annotation:
//
//	pos = pos + 1
//	; 名稱長度（varint，高位=0 表示 Huffman 關閉）
//	#{overflow=wrap}
//	{
//
// Previously an (older) formatter corrupted this shape in two ways:
//   1. it merged the standalone comment onto the preceding statement as an
//      inline trailing comment — `pos = pos + 1; 名稱長度…` — destroying the
//      comment's association with the `{` block;
//   2. it silently dropped the `#{overflow=wrap}` annotation, leaving integer
//      arithmetic to fall back to option<int>.
//
// The corrected formatter keeps the comment on its own line (separated from the
// previous statement by a blank line, since it is the `{` block's doc comment),
// preserves the `#{overflow=wrap}` line verbatim, and is idempotent.
func TestFormatStandaloneCommentPlusOverflowBeforeBareMatch(t *testing.T) {
	in := "encode-header = (name str, value str) (buf []byte, n i64) {\n" +
		"    pos = 0\n" +
		"    pos = pos + 1\n" +
		"    ; 名稱長度（varint，高位=0 表示 Huffman 關閉）\n" +
		"    #{overflow=wrap}\n" +
		"    {\n" +
		"        x < 64 -> {\n" +
		"            pos = pos + 1\n" +
		"        }\n" +
		"\n" +
		"        -> {\n" +
		"            pos = pos + 2\n" +
		"        }\n" +
		"    }\n" +
		"    return\n" +
		"}\n"
	want := "encode-header = (name str, value str) (buf []byte, n i64) {\n" +
		"    pos = 0\n" +
		"    pos = pos + 1\n" +
		"\n" +
		"    ; 名稱長度（varint，高位=0 表示 Huffman 關閉）\n" +
		"    #{overflow=wrap}\n" +
		"    {\n" +
		"        x < 64 -> {\n" +
		"            pos = pos + 1\n" +
		"        }\n" +
		"\n" +
		"        -> {\n" +
		"            pos = pos + 2\n" +
		"        }\n" +
		"    }\n" +
		"    return\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	// The standalone comment must NOT be merged into the previous statement.
	if strings.Contains(out, "pos = pos + 1; 名稱長度") {
		t.Fatalf("standalone comment merged into preceding statement:\n%q", out)
	}
	// The line-scoped overflow annotation must survive, exactly once.
	if got := strings.Count(out, "#{overflow"); got != 1 {
		t.Fatalf("expected exactly 1 overflow annotation, got %d:\n%q", got, out)
	}
	if !strings.Contains(out, "; 名稱長度（varint，高位=0 表示 Huffman 關閉）") {
		t.Fatalf("standalone comment was dropped:\n%q", out)
	}
	if out != want {
		t.Fatalf("output not stable:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	// Idempotency: a second pass must reproduce the same output.
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}

// REGRESSION GUARD (line-scoped overflow annotation preserved in place).
//
// `#{overflow=wrap}` is a *line* annotation: it applies only to the single
// statement on the line immediately below it (see parser
// applyLineOverflowAnnotations), and is no longer hoisted to the top of a
// block. The formatter must therefore emit each occurrence verbatim, on its own
// line, without dropping or collapsing it. This prevents the prior silent
// loss: reformatting used to fold away block-scoped overflow annotations, which
// left integer arithmetic to silently fall back to option<int> (LLVM then
// rejected the value after truncation to a narrow type).
//
// With line-scoped annotations, the source keeps one `#{overflow=wrap}` directly
// above every statement that needs wrap mode, and `no fmt` reproduces it
// byte-for-byte (idempotent).
func TestFormatLineScopedOverflowPreservedInPlace(t *testing.T) {
	in := "f = (spec []i64, n i64) (out i64) {\n" +
		"    last-wi = 0\n" +
		"    wi <- [0..n): {\n" +
		"        c = spec[wi]\n" +
		"        c < 48 || c > 57 -> break\n" +
		"        #{overflow=wrap}\n" +
		"        width-v = width-v * 10 + c - 48\n" +
		"        #{overflow=wrap}\n" +
		"        last-wi = wi + 1\n" +
		"    }\n" +
		"    out = last-wi\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if strings.Count(out, "#{overflow") != 2 {
		t.Fatalf("expected exactly 2 line-scoped overflow annotations, got:\n%q", out)
	}
	if out != in {
		t.Fatalf("line-scoped overflow annotation not preserved in place:\n--- want ---\n%q\n--- got ---\n%q", in, out)
	}
	// Idempotency: a second pass must reproduce the same output.
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}
