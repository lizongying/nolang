package fmt

import "testing"

// REGRESSION GUARD (annotation / comment ordering around a statement).
//
// A statement may carry BOTH an annotation (`#{...}`) and a comment. The
// formatter's canonical layout is fixed by position:
//
//	above the statement, top to bottom:   comment → annotation → statement
//	trailing on the same line, left→right: statement → annotation → comment
//
// and the whole group (comment + annotation + statement) is one *node*, kept
// apart from the code above it by at least one blank line.
//
// Before this, the two halves of the rule were violated in opposite directions:
//
//   - a comment sitting between an annotation line and its target statement was
//     left in the comment buffer and became the Doc of the NEXT statement, so the
//     node lost its comment (and the next statement gained a bogus one);
//   - a comment written after a trailing annotation (`stmt #{ann} ; c`) was
//     likewise orphaned onto the following line instead of staying put.
//
// Cases 1 and 2 below therefore have DIFFERENT inputs and the SAME expected
// output — the formatter normalizes the annotation/comment order rather than
// reproducing whatever the author wrote.
func TestFormatAnnotationCommentOrdering(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "comment above annotation (already canonical)",
			input: "f = () {\n" +
				"    a = 1\n" +
				"    ; 註釋\n" +
				"    #{mac-arm64}\n" +
				"    b = 2\n" +
				"    print(a + b)\n" +
				"}\n",
			expected: "f = () {\n" +
				"    a = 1\n" +
				"\n" +
				"    ; 註釋\n" +
				"    #{mac-arm64}\n" +
				"    b = 2\n" +
				"    print(a + b)\n" +
				"}\n",
		},
		{
			name: "annotation above comment is reordered to comment, annotation, statement",
			input: "f = () {\n" +
				"    a = 1\n" +
				"    #{mac-arm64}\n" +
				"    ; 註釋\n" +
				"    b = 2\n" +
				"    print(a + b)\n" +
				"}\n",
			expected: "f = () {\n" +
				"    a = 1\n" +
				"\n" +
				"    ; 註釋\n" +
				"    #{mac-arm64}\n" +
				"    b = 2\n" +
				"    print(a + b)\n" +
				"}\n",
		},
		{
			name: "trailing annotation keeps its same-line comment",
			input: "f = () {\n" +
				"    a = 1\n" +
				"    b = a #{mac-arm64} ; 尾隨\n" +
				"    print(b)\n" +
				"}\n",
			expected: "f = () {\n" +
				"    a = 1\n" +
				"    b = a #{mac-arm64}; 尾隨\n" +
				"    print(b)\n" +
				"}\n",
		},
		{
			name:     "top-level trailing annotation keeps its same-line comment",
			input:    "x = 1 #{mac-arm64} ; 尾隨\n",
			expected: "x = 1 #{mac-arm64}; 尾隨\n",
		},
		{
			name: "annotation-only node gets a blank line above",
			input: "f = () {\n" +
				"    a = 1\n" +
				"    #{mac-arm64}\n" +
				"    b = 2\n" +
				"}\n",
			expected: "f = () {\n" +
				"    a = 1\n" +
				"\n" +
				"    #{mac-arm64}\n" +
				"    b = 2\n" +
				"}\n",
		},
		{
			name: "node as the block's first statement gets a blank line after '{'",
			input: "f = () {\n" +
				"    ; 首條註釋\n" +
				"    a = 1\n" +
				"}\n",
			expected: "f = () {\n" +
				"\n" +
				"    ; 首條註釋\n" +
				"    a = 1\n" +
				"}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errs := FormatFileWithErrors(tt.input)
			if len(errs) > 0 {
				t.Fatalf("format errors: %v", errs)
			}
			if out != tt.expected {
				t.Errorf("FormatFileWithErrors\n--- got ---\n%s\n--- want ---\n%s", out, tt.expected)
			}
			// Idempotency: the reordering must be a fixed point, otherwise every
			// save would shuffle the annotation/comment pair again.
			out2, errs2 := FormatFileWithErrors(out)
			if len(errs2) > 0 {
				t.Fatalf("reformat errors: %v", errs2)
			}
			if out2 != out {
				t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
			}
		})
	}
}

// REGRESSION GUARD (trailing annotation after a MULTI-LINE statement).
//
// A trailing `#{...}` belongs to the statement whose last token it follows on the
// SAME LINE. The parser used to test that with stmt.Pos().Line — the statement's
// FIRST line — so any statement that ended on a later line had its trailing
// annotation silently reinterpreted as a *leading* annotation of the NEXT
// statement. The shapes that actually end on a later line are the ones whose
// EndPos() is imprecise:
//
//   - a call whose `)` sits on its own line (CallExpression.EndPos returns the
//     last ARGUMENT, not the paren), and
//   - a slice/array literal whose `]` sits on its own line.
//
// For `#{index-out=…}` this is not cosmetic: the annotation stops guarding the
// index it was written for (the out-of-range index is treated as unhandled), and
// the LSP quickfix that appends `#{index-out = 0}` at end-of-line does nothing.
//
// The test is now "the previous non-comment token is on the same line as `#{`"
// (annotationImmediatelyTrails), which is exactly the language's own rule: a
// `#{...}` is legal only standalone-above or trailing on the same line.
func TestFormatTrailingAnnotationAfterMultilineStatement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "multi-line call, closing paren on its own line",
			input: "f = () {\n" +
				"    v = pick(\n" +
				"        1,\n" +
				"        2\n" +
				"    ) #{index-out=0}\n" +
				"    print(v)\n" +
				"}\n",
			expected: "f = () {\n" +
				"    v = pick(1, 2) #{index-out=0}\n" +
				"    print(v)\n" +
				"}\n",
		},
		{
			name: "multi-line slice literal, closing bracket on its own line",
			input: "f = () {\n" +
				"    v = [\n" +
				"        1,\n" +
				"        2\n" +
				"    ] #{overflow=wrap}\n" +
				"    print(v)\n" +
				"}\n",
			expected: "f = () {\n" +
				"    v = [1, 2] #{overflow=wrap}\n" +
				"    print(v)\n" +
				"}\n",
		},
		{
			// Over-reach guard: here the annotation is on its OWN line, so it is a
			// leading annotation of the following statement — it must never be
			// absorbed backwards into the multi-line statement above it.
			name: "annotation on its own line stays a leading annotation",
			input: "f = () {\n" +
				"    v = pick(\n" +
				"        1,\n" +
				"        2\n" +
				"    )\n" +
				"    #{index-out=0}\n" +
				"    print(v)\n" +
				"}\n",
			expected: "f = () {\n" +
				"    v = pick(1, 2)\n" +
				"\n" +
				"    #{index-out=0}\n" +
				"    print(v)\n" +
				"}\n",
		},
		{
			// Top level goes through ParseProgram, a DIFFERENT interception site
			// from the in-function cases above — keep one case per path so a
			// future edit to either site cannot silently pass.
			name: "top-level multi-line statement keeps its trailing annotation",
			input: "v = max(\n" +
				"    1,\n" +
				"    2\n" +
				") #{overflow=wrap}\n" +
				"print(v)\n",
			expected: "v = max(1, 2) #{overflow=wrap}\n" +
				"print(v)\n",
		},
		{
			// Top-level trailing annotation + same-line comment: the comment must
			// stay on that line. Detaching the annotation also orphaned the
			// comment onto the NEXT line, splitting the pair apart.
			name: "top-level multi-line statement keeps its trailing annotation and comment",
			input: "v = max(\n" +
				"    1,\n" +
				"    2\n" +
				") #{overflow=wrap} ; 尾隨註釋\n" +
				"print(v)\n",
			expected: "v = max(1, 2) #{overflow=wrap}; 尾隨註釋\n" +
				"print(v)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errs := FormatFileWithErrors(tt.input)
			if len(errs) > 0 {
				t.Fatalf("format errors: %v", errs)
			}
			if out != tt.expected {
				t.Errorf("FormatFileWithErrors\n--- got ---\n%s\n--- want ---\n%s", out, tt.expected)
			}
			out2, errs2 := FormatFileWithErrors(out)
			if len(errs2) > 0 {
				t.Fatalf("reformat errors: %v", errs2)
			}
			if out2 != out {
				t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
			}
		})
	}
}
