package build

// char_arith_gate_test.go — 「char 與非數值運算元的算術」必須**阻斷 no build**。
//
// checker 側的單元測試（checker/char_arith_test.go）只證明 validator 會產出診斷；
// 這裡證明它真的接到硬錯誤鏈上：`validateEntryProgram` 把 char-arith 的結果併進
// lints，`len(lints) > 0` 就回 *DiagnosticError ⇒ `no build` rc≠0。
//
// 這道閘門容易被誤接成「只走 vet」：vet 路徑本來就會把 lint 顯示出來，
// 所以 `no vet` 有訊息 ≠ `no build` 會失敗。兩條路徑都必須驗。
//
// 語料一律用**頂層語句**：`main` 是保留字（見 main_reserved_test.go），
// 顯式定義 `main` 會被另一條規則先攔下，測試就會「因為錯誤的原因而通過」。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const charArithTraceID = "k2m9xq4t"

// compileSrc 把源碼寫到臨時檔並跑一次完整 Compile（非 vet 模式 = no build 口徑）。
func compileSrc(t *testing.T, src string) error {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "app.no")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := NewTranspiler(nil)
	c.sourcePath = p
	_, err := c.Compile(src)
	return err
}

// TestCharArithBlocksBuild 正例：bool / f64 運算元必須讓編譯失敗，且診斷帶 k2m9xq4t。
func TestCharArithBlocksBuild(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"char-minus-bool", "flag = true\nprint(\"x\" - flag)\n"},
		{"char-minus-float", "r = 1.5\nprint(\"x\" - r)\n"},
		{"bool-minus-char", "flag = true\nprint(flag - \"x\")\n"},
		{"char-times-float", "r = 1.5\nprint(\"x\" * r)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileSrc(t, tc.src)
			if err == nil {
				t.Fatalf("expected the build to fail, it succeeded")
			}
			var de *DiagnosticError
			if !errors.As(err, &de) {
				t.Fatalf("error is not *DiagnosticError: %T %v", err, err)
			}
			found := false
			for _, l := range de.Lints {
				if l.TraceID == charArithTraceID {
					found = true
				}
			}
			if !found {
				t.Fatalf("no %s among %d diagnostics: %v", charArithTraceID, len(de.Lints), err)
			}
			// 訊息本身也要帶著 ID —— `no build` 的 stderr 是使用者唯一看得到的東西。
			if !strings.Contains(err.Error(), charArithTraceID) {
				t.Errorf("rendered error does not carry the trace id:\n%s", err.Error())
			}
		})
	}
}

// TestCharArithAllowsLegalCombos 負例：整數 / char / str 是 char 的合法同伴，
// 這些必須能編譯過（否則 `c - '0'`、`"x" - s` 這類慣用寫法全被打死）。
func TestCharArithAllowsLegalCombos(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"char-minus-int", "print(\"x\" - 1)\n"},
		{"char-minus-char-literal", "print(\"x\" - \"y\")\n"},
		{"char-minus-char-var", "c char = \"z\"\nprint(\"x\" - c)\n"},
		{"char-minus-str-var", "s str = 'hi'\nprint(\"x\" - s)\n"},
		{"char-times-int", "print(\"x\" * 3)\n"},
		// `'...'` 是 str 字面量 ⇒ 走 to-str 拼接，不是 char 算術。
		{"str-literal-minus-bool", "b = true\nprint('x' - b)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := compileSrc(t, tc.src); err != nil {
				t.Fatalf("expected a clean build, got: %v", err)
			}
		})
	}
}
