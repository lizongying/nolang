package fmt

import "testing"

// TestFormatStructLiteralBraceTight 固定 2026-11-10 定下的規範：結構體
// **實例化**的 `{` 必須緊貼型別名（`zip{}` / `net.conn{...}`），fmt 不得在
// 中間補空白。舊版 formatStructLiteral 寫出 `zip {}`、`zip {`，與結構體
// **定義**（`zip { ... }`）以及舊式 brace-match 區塊（parser 會發
// W_DEPRECATED_BRACE）在視覺上無從區分。
//
// 每條用例同時檢查三件事：輸出形態、可重新解析、冪等。少了後兩者，
// 「把空格拿掉」很容易產出 fmt 自己再也讀不回去的程式碼。
func TestFormatStructLiteralBraceTight(t *testing.T) {
	for _, tt := range []struct{ name, input, want string }{
		{
			name:  "empty literal written tight stays tight",
			input: "f = () {\n    z = zip{}\n}\n",
			want:  "f = () {\n    z = zip{}\n}",
		},
		{
			name:  "empty literal written with a space is normalised",
			input: "f = () {\n    z = zip {}\n}\n",
			want:  "f = () {\n    z = zip{}\n}",
		},
		{
			name:  "typed declaration normalised",
			input: "f = () {\n    z zip = zip {}\n}\n",
			want:  "f = () {\n    z zip = zip{}\n}",
		},
		{
			name:  "field-bearing literal keeps brace tight",
			input: "f = () {\n    u = zip {\n        name: 'a'\n    }\n}\n",
			want:  "f = () {\n    u = zip{\n        name: 'a'\n    }\n}",
		},
		{
			name:  "module-qualified type stays tight",
			input: "f = () {\n    v = net.conn{}\n}\n",
			want:  "f = () {\n    v = net.conn{}\n}",
		},
		{
			name:  "module-qualified type normalised",
			input: "f = () {\n    v = net.conn {}\n}\n",
			want:  "f = () {\n    v = net.conn{}\n}",
		},
		{
			name:  "struct definition still uses a space",
			input: "point {\n    x i64\n    y i64\n}\n",
			want:  "point {\n    x i64\n    y i64\n}",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Format(tt.input)
			if got != tt.want {
				t.Fatalf("Format:\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
			if _, errs := FormatFileWithErrors(got + "\n"); len(errs) > 0 {
				t.Errorf("formatted output no longer parses: %v\n%s", errs, got)
			}
			if again := Format(got); again != got {
				t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", got, again)
			}
		})
	}
}
