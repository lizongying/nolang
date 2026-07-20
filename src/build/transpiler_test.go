package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateDuplicateVarsPlatformExemption 驗證複合 key 豁免機制：
// 不同平台變體不應觸發重名偵測；同平台重複應報錯；平台通用 vs 平台特定不衝突。
// 使用顯式型別註記（如 `O-EXCL i64 = 1024`）以觸發「實型別註記」分支的重名檢查。
func TestValidateDuplicateVarsPlatformExemption(t *testing.T) {
	// 多平台變體不應觸發重名錯誤
	t.Run("different_platforms_no_conflict", func(t *testing.T) {
		src := `#{mac-amd64}
O-EXCL i64 = 2048
#{mac-arm64}
O-EXCL i64 = 2048
#{linux-amd64}
O-EXCL i64 = 128
#{win-arm64}
O-EXCL i64 = 1024
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateDuplicateVars(prog)
		// 不應有任何 duplicate variable 錯誤
		for _, r := range results {
			if strings.Contains(r.Message, "duplicate") || strings.Contains(r.Message, "already declared") {
				t.Errorf("unexpected duplicate error: %s", r.Message)
			}
		}
	})

	// 同平台重複應報錯
	t.Run("same_platform_conflict", func(t *testing.T) {
		src := `#{win-arm64}
O-EXCL i64 = 1024
#{win-arm64}
O-EXCL i64 = 1024
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateDuplicateVars(prog)
		foundDup := false
		for _, r := range results {
			if strings.Contains(r.Message, "already declared") {
				foundDup = true
				break
			}
		}
		if !foundDup {
			t.Errorf("expected duplicate variable error, got: %v", results)
		}
	})

	// 平台通用 vs 平台特定不衝突
	t.Run("generic_vs_platform_no_conflict", func(t *testing.T) {
		src := `O-RDONLY i64 = 0
#{win-arm64}
O-RDONLY i64 = 1
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateDuplicateVars(prog)
		for _, r := range results {
			if strings.Contains(r.Message, "already declared") {
				t.Errorf("unexpected duplicate error for generic vs platform variant: %s", r.Message)
			}
		}
	})
}

// TestValidateTypesPlatformExemption 驗證函式簽名重複檢查的複合 key 豁免機制：
// 同簽名但不同平台的函式定義不應觸發 "duplicate function" 錯誤。
func TestValidateTypesPlatformExemption(t *testing.T) {
	t.Run("function_variants_no_conflict", func(t *testing.T) {
		src := `#{mac-amd64, mac-arm64, linux-amd64, linux-arm64}
list-dir = (dirpath str) (entries []str) {
    entries = []
}

#{win-amd64, win-arm64}
list-dir = (dirpath str) (entries []str) {
    entries = []
}
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateTypes(prog)
		for _, r := range results {
			if strings.Contains(r.Message, "duplicate function") {
				t.Errorf("unexpected duplicate function error: %s", r.Message)
			}
		}
	})
}

// TestValidateDuplicateUppercaseConstants 驗證大寫常數（無顯式型別註記）也應觸發重複偵測。
// Nolang 命名規範：首字元為大寫 ASCII 字母且無小寫字母者為常數（如 A, SBOX, FNV-OFFSET），
// 重複賦值應報「already declared」錯誤；小寫變數（如 i）允許後續賦值。
func TestValidateDuplicateUppercaseConstants(t *testing.T) {
	// 大寫常數重複賦值應報錯（LSP 路徑：ValidateDuplicateVars）
	t.Run("uppercase_constant_conflict_lsp", func(t *testing.T) {
		src := `A = 0
A = 1
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateDuplicateVars(prog)
		foundDup := false
		for _, r := range results {
			if strings.Contains(r.Message, "already declared") {
				foundDup = true
				break
			}
		}
		if !foundDup {
			t.Errorf("expected duplicate variable error for uppercase constant, got: %v", results)
		}
	})

	// 大寫常數重複賦值應報錯（no vet 路徑：validateDuplicates）
	t.Run("uppercase_constant_conflict_vet", func(t *testing.T) {
		src := `A = 0
A = 1
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		err := validateDuplicates(prog)
		if err == nil {
			t.Errorf("expected duplicate variable error, got nil")
		} else if !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("expected duplicate error, got: %v", err)
		}
	})

	// 小寫變數允許後續賦值（不報重複）
	t.Run("lowercase_variable_no_conflict", func(t *testing.T) {
		src := `i = 0
i = 1
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateDuplicateVars(prog)
		for _, r := range results {
			if strings.Contains(r.Message, "already declared") {
				t.Errorf("unexpected duplicate error for lowercase variable: %s", r.Message)
			}
		}
		err := validateDuplicates(prog)
		if err != nil {
			t.Errorf("unexpected duplicate error for lowercase variable: %v", err)
		}
	})

	// 大寫常數與小寫變數同名不衝突（複合 key 不同）
	t.Run("uppercase_and_lowercase_no_conflict", func(t *testing.T) {
		src := `A = 0
a = 1
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateDuplicateVars(prog)
		for _, r := range results {
			if strings.Contains(r.Message, "already declared") {
				t.Errorf("unexpected duplicate error for A vs a: %s", r.Message)
			}
		}
	})

	// 多字元大寫常數也應觸發（如 SBOX）
	t.Run("multi_char_uppercase_constant", func(t *testing.T) {
		src := `SBOX = 0
SBOX = 1
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		err := validateDuplicates(prog)
		if err == nil {
			t.Errorf("expected duplicate error for SBOX, got nil")
		}
	})

	// 混合大小寫名稱（如 Foo）不視為常數，允許後續賦值
	t.Run("mixed_case_not_constant", func(t *testing.T) {
		src := `Foo = 0
Foo = 1
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		err := validateDuplicates(prog)
		if err != nil {
			t.Errorf("mixed-case name should not be treated as constant, but got error: %v", err)
		}
	})
}
