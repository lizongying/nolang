package fmt

import "testing"

// The native-call policy (`#{bind = ffi}` / `#{bind = wasm, wasm = ...}`) is
// declared on the import line. `no fmt` used to drop it silently: the
// formatter's attached-annotation whitelist did not include UseStatement, so
// both legal placements (own line above the `# import`, and trailing on its
// line) vanished — the program still built, but fell back to the plain ESM
// path and the koffi/wasm wrapper was never emitted. Both placements must
// round-trip verbatim and be idempotent.
func TestFormatUseNativeBindAnnotation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "own line above the import",
			input:    "#{bind = ffi}\n# /src/sqlite\n",
			expected: "#{bind=ffi}\n# /src/sqlite\n",
		},
		{
			name:     "trailing on the import line",
			input:    "# /src/sqlite #{bind = ffi}\n",
			expected: "# /src/sqlite #{bind=ffi}\n",
		},
		{
			name:     "wasm mode with a quoted specifier",
			input:    "#{bind = wasm, wasm = '@sqlite.org/sqlite-wasm'}\n# /src/sqlite\n",
			expected: "#{bind=wasm, wasm='@sqlite.org/sqlite-wasm'}\n# /src/sqlite\n",
		},
		{
			name: "between code",
			input: "x = 1\n" +
				"#{bind = ffi}\n" +
				"# /src/sqlite\n" +
				"print(x)\n",
			expected: "x = 1\n" +
				"\n" +
				"#{bind=ffi}\n" +
				"# /src/sqlite\n" +
				"\n" +
				"print(x)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errs := FormatFileWithErrors(tt.input)
			if len(errs) > 0 {
				t.Fatalf("format errors: %v", errs)
			}
			if out != tt.expected {
				t.Fatalf("first pass mismatch:\n got %q\nwant %q", out, tt.expected)
			}
			again, errs := FormatFileWithErrors(out)
			if len(errs) > 0 {
				t.Fatalf("format errors on second pass: %v", errs)
			}
			if again != out {
				t.Fatalf("not idempotent:\nfirst  %q\nsecond %q", out, again)
			}
		})
	}
}
