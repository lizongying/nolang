package fmt

import "testing"

// REGRESSION GUARD: a blank line in the *source* between the preceding code and a
// comment must survive formatting, including for "trailing" comments (comments
// with no statement after them, i.e. right before the closing brace or at EOF).
// Previously formatTrailingComments / the file-end trailing-comment loop emitted
// exactly one newline per comment with no blank-line detection, so any blank the
// user put above such a comment was silently collapsed — you simply could not
// keep `stmt\n\n; comment` when the comment had nothing after it.
func TestFormatTrailingCommentBlankPreserved(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "trailing comment keeps blank above",
			in:   "f = (a i64) (out i64) {\n    out = a\n\n    ; trailing\n}\n",
			want: "f = (a i64) (out i64) {\n    out = a\n\n    ; trailing\n}\n",
		},
		{
			name: "trailing comment without blank stays glued",
			in:   "f = (a i64) (out i64) {\n    out = a\n    ; noop\n}\n",
			want: "f = (a i64) (out i64) {\n    out = a\n    ; noop\n}\n",
		},
		{
			name: "blank between two trailing comments preserved",
			in:   "f = (a i64) (out i64) {\n    out = a\n\n    ; c1\n\n    ; c2\n}\n",
			want: "f = (a i64) (out i64) {\n    out = a\n\n    ; c1\n\n    ; c2\n}\n",
		},
		{
			name: "file-end trailing comment keeps blank above",
			in:   "f = (a i64) (out i64) {\n    out = a\n}\n\n; file-end\n",
			want: "f = (a i64) (out i64) {\n    out = a\n}\n\n; file-end\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errs := FormatFileWithErrors(tc.in)
			if len(errs) > 0 {
				t.Fatalf("format errors: %v", errs)
			}
			if out != tc.want {
				t.Fatalf("blank line before trailing comment not preserved:\n--- want ---\n%q\n--- got ---\n%q", tc.want, out)
			}
		})
	}
}

// REGRESSION GUARD: consecutive `# std/...` imports must each be emitted on their
// own line (previously the `prevIsUse && currIsUse` branch emitted nothing, so a
// run of imports was glued into a single `# a# b# c` line), and there must be a
// separating blank line between the import block and the following statement.
func TestFormatImportSeparationAndBlank(t *testing.T) {
	in := "# std/a\n# std/b\nf = () (out i64) {\n    out = 1\n}\n"
	want := "# std/a\n# std/b\n\nf = () (out i64) {\n    out = 1\n}\n"
	out, errs := FormatFileWithErrors(in)
	if len(errs) > 0 {
		t.Fatalf("format errors: %v", errs)
	}
	if out != want {
		t.Fatalf("imports not separated / no blank before body:\n--- want ---\n%q\n--- got ---\n%q", want, out)
	}
}
