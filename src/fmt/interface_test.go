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

// TestFormatInterfaceMergeItems 鎖定接口合併項的規範輸出形：體內、每行一個，
// 排在方法之前；且二次格式化冪等。
func TestFormatInterfaceMergeItems(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "body merge items stay one per line",
			input: `serialize {
    json
    noml
}
`,
			expected: `serialize {
    json
    noml
}`,
		},
		{
			name: "merge items precede methods",
			input: `db {
    enter
    leave
    fs.close() (ok bool)
}
`,
			expected: `db {
    enter
    leave
    fs.close() (ok bool)
}`,
		},
		{
			name: "dotted merge item survives",
			input: `stmt-mysql {
    sql.stmt
    op() (ok bool)
}
`,
			expected: `stmt-mysql {
    sql.stmt
    op() (ok bool)
}`,
		},
		{
			// 前置列表（兼容形）被改寫為體內形——這是 `no fmt` 的規範化職責。
			name:     "leading list is normalised to body form",
			input:    "db enter, leave {\n    fs.close() (ok bool)\n}\n",
			expected: "db {\n    enter\n    leave\n    fs.close() (ok bool)\n}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Format(tt.input)
			if got != tt.expected {
				t.Errorf("mismatch\ninput:    %q\nwant:     %q\ngot:      %q", tt.input, tt.expected, got)
			}
			// 冪等：再格式化一次必須不變。
			if again := Format(got); again != got {
				t.Errorf("not idempotent\nfirst:  %q\nsecond: %q", got, again)
			}
		})
	}
}
