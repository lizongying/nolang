package fmt

import "testing"

// REGRESSION GUARD (`#{index-out = ...}` safety-index annotation preserved in place).
//
// `#{index-out = DEF}` is a *line* annotation: it applies only to the single
// statement on the line immediately below it (see parser
// applyLineIndexOutAnnotations), marking an out-of-range index read to yield
// DEF instead of faulting. `no fmt` used to DROP it for the common spelling
//
//	#{index-out=0}
//	ac = arg[j]
//
// because the parser attaches a leading annotation directly to the following
// IDENT-initial statement (no standalone AnnotationStatement node survives),
// while fmt/attachedAnnotations filtered out every non-trailing `index-out`
// key under the assumption a standalone node would print it. The result: the
// only printable representation was removed, silently deleting out-of-range
// protection on every `no fmt -w`.
//
// The formatter must therefore reproduce each `#{index-out = DEF}` line
// byte-for-byte, on its own line directly above the annotated statement, and
// stay idempotent. The annotation line is a node head, so it is separated from
// the code above it (including the block's `{`) by one blank line — see
// nodeHeadWillEmit.
func TestFormatIndexOutPreservedInPlace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			// Case A: annotation attached to an IDENT-initial let statement
			// (parser consumes the standalone node) — the historically-broken
			// spelling.
			name: "index-out above ident-initial let is preserved",
			input: "f = (arg str, flag str, flagn i64) (yes bool) {\n" +
				"    j <- [0..flagn): {\n" +
				"        #{index-out=0}\n" +
				"        ac = arg[j]\n" +
				"        #{index-out=0}\n" +
				"        fc = flag[j]\n" +
				"        ac != fc -> {\n" +
				"            yes = false\n" +
				"            break\n" +
				"        }\n" +
				"    }\n" +
				"}\n",
			expected: "f = (arg str, flag str, flagn i64) (yes bool) {\n" +
				"    j <- [0..flagn): {\n" +
				"\n" +
				"        #{index-out=0}\n" +
				"        ac = arg[j]\n" +
				"\n" +
				"        #{index-out=0}\n" +
				"        fc = flag[j]\n" +
				"        ac != fc -> {\n" +
				"            yes = false\n" +
				"            break\n" +
				"        }\n" +
				"    }\n" +
				"}\n",
		},
		{
			// index-write spelling: the annotation attaches to the whole
			// assignment statement; the propagated copy must NOT double print.
			name: "index-out above an index-write is printed once",
			input: "f = (arr []i64, i i64, v i64) {\n" +
				"    #{index-out=0}\n" +
				"    arr[i] = v\n" +
				"}\n",
			expected: "f = (arr []i64, i i64, v i64) {\n" +
				"\n" +
				"    #{index-out=0}\n" +
				"    arr[i] = v\n" +
				"}\n",
		},
		{
			// Trailing spelling stays on the same line (already worked, guard
			// against the filter change regressing it).
			name:     "trailing index-out stays on the same line",
			input:    "f = (arr []i64, i i64) (x i64) {\n    x = arr[i] #{index-out=0}\n}\n",
			expected: "f = (arr []i64, i i64) (x i64) {\n    x = arr[i] #{index-out=0}\n}\n",
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
			// Idempotency: a second pass must not change anything.
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

// REGRESSION GUARD (duplicate-key annotation merge: later value wins).
//
// When the same annotation key appears more than once on one statement — either
// as two trailing `#{...}` on the same line (`x = v[i] #{index-out=0} #{index-out=1}`)
// or as consecutive line annotations that the parser merges into a single node
// (`#{index-out=0}` / `#{index-out=1}`) — the entries must collapse to a single
// key whose value is the LAST one written. Before this, both values survived and
// the formatter emitted `#{index-out=0, index-out=1}`, and desugar picked the
// first (wrong) default.
func TestFormatDuplicateAnnotationKeyLastWins(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "two trailing index-out collapse to the last value",
			input: "f = (arr []i64, i i64) (x i64) {\n" +
				"    x = arr[i] #{index-out=0} #{index-out=1}\n" +
				"}\n",
			expected: "f = (arr []i64, i i64) (x i64) {\n" +
				"    x = arr[i] #{index-out=1}\n" +
				"}\n",
		},
		{
			name: "consecutive line index-out collapse to the last value",
			input: "f = (arr []i64, i i64) (x i64) {\n" +
				"    #{index-out=0}\n" +
				"    #{index-out=1}\n" +
				"    x = arr[i]\n" +
				"}\n",
			expected: "f = (arr []i64, i i64) (x i64) {\n" +
				"\n" +
				"    #{index-out=1}\n" +
				"    x = arr[i]\n" +
				"}\n",
		},
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
