package fmt

import "testing"

// TestFormatInterfaceGenericMethod verifies the formatter round-trips
// the generic-receiver interface method form
// `t.method(params) (results)`. Before this test, the formatter was
// not exercised for this syntax.
func TestFormatInterfaceGenericMethod(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "interface with generic-receiver method",
			input: `ord {
    t.gt(b t) (res bool)
}
`,
			expected: `ord {
    t.gt(b t) (res bool)
}`,
		},
		{
			name: "nullable slice method preserves [?] prefix",
			input: `[?]ord.ast = () (res [?]ord) {
}
`,
			expected: `[?]ord.ast = () (res [?]ord) {
}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Format(tt.input)
			if got != tt.expected {
				t.Errorf("mismatch\ninput:    %q\nwant:     %q\ngot:      %q", tt.input, tt.expected, got)
			}
		})
	}
}
