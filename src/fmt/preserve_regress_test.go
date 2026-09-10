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

// REGRESSION GUARD (block-scoped overflow annotation preserved + hoisted).
//
// `#{overflow=wrap}` is block-scoped: within a block that carries a single
// overflow mode, the mode applies bidirectionally to the whole block (see
// parser propagateBlockScopedOverflow). The formatter must therefore emit the
// annotation once, at the block top, without losing it. Previously the block
// mode detection only looked at standalone AnnotationStatements, so an
// annotation that got attached to the following statement became invisible and
// the whole block's wrap mode was dropped (integer arithmetic silently fell back
// to option<int>, which LLVM then rejected after truncation to a narrow type).
//
// HEAD also inserted a spurious blank line before the (non-hoisted) annotation.
func TestFormatBlockScopedOverflowHoistedAndPreserved(t *testing.T) {
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
	want := "f = (spec []i64, n i64) (out i64) {\n" +
		"    last-wi = 0\n" +
		"    wi <- [0..n): {\n" +
		"        #{overflow=wrap}\n" +
		"        c = spec[wi]\n" +
		"        c < 48 || c > 57 -> break\n" +
		"        width-v = width-v * 10 + c - 48\n" +
		"        last-wi = wi + 1\n" +
		"    }\n" +
		"    out = last-wi\n" +
		"}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if strings.Count(out, "#{overflow") == 0 {
		t.Fatalf("block-scoped overflow annotation was dropped:\n%q", out)
	}
	if out != want {
		t.Fatalf("overflow annotation not hoisted to block top:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
	// Idempotency of the hoisted form.
	out2, _ := FormatFileWithErrors(out)
	if out2 != out {
		t.Fatalf("not idempotent:\n--- pass1 ---\n%q\n--- pass2 ---\n%q", out, out2)
	}
}
