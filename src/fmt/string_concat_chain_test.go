package fmt

import "testing"

// TestFormatCLIStringConcatChain 固定 `no fmt` 的 checker-driven 自動替換：
// 鏈式拼接裡摻了非字面量運算元時，整條 `+` 鏈必須一致翻成 `-`。
// 舊口徑只翻「任一側是字串字面量」的節點，留下
// `'line ' - a - ', column ' + b - ': ' + c - ' [' + d - ']'`
// 這種混合 +/- 的壞碼（str + str 在 Nolang 裡根本不成立）。
// 走 formatCLISugar = 與 CLI 完全相同的FixStringConcatOperators → 格式化路徑。
func TestFormatCLIStringConcatChain(t *testing.T) {
	src := "f = () (r str) {\n    r = 'line ' + a + ', column ' + b + ': ' + c + ' [' + d + ']'\n}\n"
	got, perrs := formatCLISugar(src)
	if len(perrs) > 0 {
		t.Fatalf("parse errors: %v", perrs)
	}
	want := "f = () (r str) {\n    r = 'line ' - a - ', column ' - b - ': ' - c - ' [' - d - ']'\n}\n"
	if got != want {
		t.Fatalf("formatCLISugar:\ngot:\n%s\nwant:\n%s", got, want)
	}
	again, perrs2 := formatCLISugar(got)
	if len(perrs2) > 0 {
		t.Fatalf("formatted output no longer parses: %v", perrs2)
	}
	if again != got {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", got, again)
	}
}
