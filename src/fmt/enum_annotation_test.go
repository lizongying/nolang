package fmt

import "testing"

// TestFormatEnumAnnotations pins how enum annotations round-trip through the
// formatter. Both legal placements are supported: definition-level (above the
// whole enum), per-member own-line, and per-member trailing. The formatter
// normalizes the own-line per-member form to the trailing spelling, and must be
// idempotent for every case.
func TestFormatEnumAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "definition-level annotation stays above",
			input:    "#{inline}\ncolor {\n    red,\n    green,\n    blue,\n}\n",
			expected: "#{inline}\ncolor {\n    red,\n    green,\n    blue,\n}\n",
		},
		{
			name:     "enum value own-line annotation becomes trailing",
			input:    "color {\n    #{a}\n    red,\n    green,\n}\n",
			expected: "color {\n    red #{a},\n    green,\n}\n",
		},
		{
			name:     "enum value trailing annotation on first member is preserved",
			input:    "color {\n    red #{a},\n    green,\n}\n",
			expected: "color {\n    red #{a},\n    green,\n}\n",
		},
		{
			name:     "explicit enum value keeps trailing annotation",
			input:    "color {\n    red = 5 #{a},\n    green,\n}\n",
			expected: "color {\n    red = 5 #{a},\n    green,\n}\n",
		},
		{
			name:     "tagged variant own-line annotation becomes trailing",
			input:    "option {\n    #{a}\n    ok(v i64),\n    nil,\n}\n",
			expected: "option {\n    ok(v i64) #{a},\n    nil,\n}\n",
		},
		{
			name:     "tagged first member trailing annotation is preserved",
			input:    "option {\n    nil #{x},\n    ok(v i64),\n}\n",
			expected: "option {\n    nil #{x},\n    ok(v i64),\n}\n",
		},
		{
			name:     "tagged definition-level annotation stays above",
			input:    "#{inline}\noption {\n    ok(v i64),\n    nil,\n}\n",
			expected: "#{inline}\noption {\n    ok(v i64),\n    nil,\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatFile(tt.input)
			if got != tt.expected {
				t.Errorf("FormatFile(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			if again := FormatFile(got); again != got {
				t.Errorf("not idempotent for %q: second pass = %q", tt.input, again)
			}
		})
	}
}

// TestFormatInlineBoolSpellings pins that the explicit boolean forms survive
// formatting unchanged — in particular that `#{inline=false}` is NOT rewritten
// into `#{inline=true}`.
//
// That corruption was real before the value was stored on the AST node: the
// parser built the same value object for `true` and `false`, and its String()
// hard-coded "true". `no fmt -w` would therefore have silently flipped the
// meaning of every `#{inline=false}` in the tree.
func TestFormatInlineBoolSpellings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "definition-level inline=true stays",
			input:    "#{inline=true}\ncolor {\n    red,\n    green,\n}\n",
			expected: "#{inline=true}\ncolor {\n    red,\n    green,\n}\n",
		},
		{
			name:     "definition-level inline=false is not flipped to true",
			input:    "#{inline=false}\ncolor {\n    red,\n    green,\n}\n",
			expected: "#{inline=false}\ncolor {\n    red,\n    green,\n}\n",
		},
		{
			name:     "tagged enum inline=false is not flipped to true",
			input:    "#{inline=false}\noption {\n    ok(v i64),\n    nil,\n}\n",
			expected: "#{inline=false}\noption {\n    ok(v i64),\n    nil,\n}\n",
		},
		{
			name:     "struct field inline=false is not flipped to true",
			input:    "holder {\n    p pt #{inline=false}\n}\n",
			expected: "holder {\n    p pt #{inline=false}\n}\n",
		},
		{
			name:     "struct field inline=true stays",
			input:    "holder {\n    p pt #{inline=true}\n}\n",
			expected: "holder {\n    p pt #{inline=true}\n}\n",
		},
		{
			name:     "bare shorthand stays bare",
			input:    "holder {\n    p pt #{inline}\n}\n",
			expected: "holder {\n    p pt #{inline}\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatFile(tt.input)
			if got != tt.expected {
				t.Errorf("FormatFile(%q) = %q, want %q", tt.input, got, tt.expected)
			}
			if again := FormatFile(got); again != got {
				t.Errorf("not idempotent for %q: second pass = %q", tt.input, again)
			}
		})
	}
}
