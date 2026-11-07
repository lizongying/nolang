package fmt

import "testing"

// REGRESSION GUARD (duplicate-key annotation merge: later value wins).
//
// When the same annotation key appears more than once on one statement (e.g.
// two trailing `#{...}` on the same line), the entries must collapse to a
// single key whose value is the LAST one written. Before this, both values
// survived and the formatter emitted `#{overflow=wrap, overflow=wrap}`.
func TestFormatDuplicateAnnotationKeyLastWins(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "duplicate overflow value collapses to one (idempotent)",
			input: "f = (a i64, b i64) (c i64) {\n" +
				"    #{overflow=wrap} #{overflow=wrap}\n" +
				"    c = a + b\n" +
				"}\n",
			expected: "f = (a i64, b i64) (c i64) {\n" +
				"\n" +
				"    #{overflow=wrap}\n" +
				"    c = a + b\n" +
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
				t.Errorf("Format()\n--- got ---\n%s\n--- want ---\n%s", out, tt.expected)
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
