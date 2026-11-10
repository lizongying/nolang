package fmt

import "testing"

// TestFormatStringSliceAlwaysOnePerLine 固定 2026-11-10 定案的規範：
// **字串陣列無條件每行一個元素**。不看元素個數、不看原始碼是單行還是同行
// 塞了好幾個 —— 只要陣列元素全都是字串字面量，就展开成
//
//	v = [
//	    'a',
//	    'b',
//	]
//
// 起初只修了「原始碼本來跨多行卻被壓成單行」這個缺陷（len > 8 門檻），
// 使用者随后要求把單行寫法也一體拆開，故改成無條件規則。
// 邊界：空陣列維持 []；混合型別 / 含運算式的陣列不算字串陣列，仍走
// 「原始碼跨多行才保留分行」；純量陣列維持 ≤8 單行、>8 每行 8 個。
func TestFormatStringSliceAlwaysOnePerLine(t *testing.T) {
	for _, tt := range []struct{ name, input, want string }{
		{
			name:  "three strings on one line expand",
			input: "f = () {\n    t.evk = ['code', 'file-mode', 'file-perm']\n}\n",
			want:  "f = () {\n    t.evk = [\n        'code',\n        'file-mode',\n        'file-perm',\n    ]\n}",
		},
		{
			name:  "two strings on one line expand",
			input: "f = () {\n    v = ['a', 'b']\n}\n",
			want:  "f = () {\n    v = [\n        'a',\n        'b',\n    ]\n}",
		},
		{
			name:  "single string expands too",
			input: "f = () {\n    v = ['a']\n}\n",
			want:  "f = () {\n    v = [\n        'a',\n    ]\n}",
		},
		{
			name:  "elements sharing a source line are still split",
			input: "f = () {\n    v = ['a', 'b',\n        'c']\n}\n",
			want:  "f = () {\n    v = [\n        'a',\n        'b',\n        'c',\n    ]\n}",
		},
		{
			name:  "already one-per-line stays one-per-line",
			input: "f = () {\n    t.evk = [\n        'code',\n        'file-mode',\n        'file-perm'\n    ]\n}\n",
			want:  "f = () {\n    t.evk = [\n        'code',\n        'file-mode',\n        'file-perm',\n    ]\n}",
		},
		{
			name:  "long string table stays one per line",
			input: "f = () {\n    t.evt = [\n        'a,b,c',\n        'd,e',\n        'f,g,h'\n    ]\n}\n",
			want:  "f = () {\n    t.evt = [\n        'a,b,c',\n        'd,e',\n        'f,g,h',\n    ]\n}",
		},
		{
			name:  "nine strings on one line split one per line",
			input: "f = () {\n    v = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i']\n}\n",
			want:  "f = () {\n    v = [\n        'a',\n        'b',\n        'c',\n        'd',\n        'e',\n        'f',\n        'g',\n        'h',\n        'i',\n    ]\n}",
		},
		{
			name:  "empty slice stays inline",
			input: "f = () {\n    v = []\n}\n",
			want:  "f = () {\n    v = []\n}",
		},
		{
			name:  "mixed string and number on one line stays single line",
			input: "f = () {\n    v = ['a', 1]\n}\n",
			want:  "f = () {\n    v = ['a', 1]\n}",
		},
		{
			name:  "mixed string and number multi-line keeps source lines",
			input: "f = () {\n    v = [\n        'a',\n        1\n    ]\n}\n",
			want:  "f = () {\n    v = [\n        'a',\n        1,\n    ]\n}",
		},
		{
			name:  "scalar slice with <=8 elements still collapses to one line",
			input: "f = () {\n    v = [\n        1,\n        2,\n        3\n    ]\n}\n",
			want:  "f = () {\n    v = [1, 2, 3]\n}",
		},
		{
			name:  "scalar slice with >8 elements still uses 8-per-line",
			input: "v []i64 = [0, 1, 2, 3, 4, 5, 6, 7, 8]\n",
			want:  "v []i64 = [\n    0, 1, 2, 3, 4, 5, 6, 7,\n    8,\n]",
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
