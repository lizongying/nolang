package checker

import (
	"strings"
	"testing"
)

// 定長陣列標註 `[N]T` 不是冗餘標註：它是決定表示形式（固定棧陣列 vs 堆切片）的開關。
// 寫了 `[3]char`，parser 才會把右值的 SliceLiteral 轉成 ArrayLiteral，
// inferExprType 才回報 `[3]char` —— 這個「相等」是標註自己造出來的循環論證。
// 一旦把標註刪掉重新解析，同一行就變成 `v = [...]`，型別是堆上可增長的
// `[]char`，語意隨之改變（實測 `[3]char` 的 `.push()` 後 `.len()` 仍為 3，
// `[]char` 則變成 4）。`no fmt` 預設的「先去冗餘型別標註再格式化」因此會
// 悄悄把 test/std/char.no 的 `fixed [3]char = ["x", "y", "z"]` 降級為切片。
// 本測試釘住：固定長度陣列（含 `[?]T`、省略元素型別的 `[N]`）一律不報冗餘。
func TestRedundantTypeAnnotationKeepsFixedArray(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantOmit bool
	}{
		// 定長陣列：標註決定表示形式，不可省略
		{"fixed char array", "    fixed [3]char = [\"x\", \"y\", \"z\"]", false},
		{"fixed i64 array", "    a [3]i64 = [1, 2, 3]", false},
		{"fixed array without elem", "    a [3] = [1, 2, 3]", false},
		{"inferred-size array", "    a [?]char = [\"x\", \"y\"]", false},
		// 切片與純量：標註確實可從字面量推斷
		{"slice of char", "    arr []char = [\"a\", \"b\"]", true},
		{"scalar i64", "    n i64 = 42", true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			prog := mustParse(t, "f = () {\n"+tt.line+"\n}\n")
			results := ValidateRedundantTypeAnnotation(prog)
			got := len(results) > 0
			if got != tt.wantOmit {
				t.Fatalf("omittable = %v, want %v; results: %+v", got, tt.wantOmit, results)
			}
			if got && !strings.Contains(results[0].Message, "can be omitted") {
				t.Errorf("unexpected message %q", results[0].Message)
			}
		})
	}
}
