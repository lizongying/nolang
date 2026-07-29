package checker

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

// TestValidateUnusedVarsFormatString 驗證具名格式字串中的變數引用
// 不會被誤報為未使用。例如 `pi = 3.14` 在 `print('pi = {pi:.2f}')` 中
// 通過 {pi:.2f} 字段引用，不應觸發 "'pi' is defined but never used"。
func TestValidateUnusedVarsFormatString(t *testing.T) {
	t.Run("var_used_in_format_spec", func(t *testing.T) {
		src := `pi = 3.14
print('pi = {pi:.2f}')
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateUnusedVars(prog)
		for _, r := range results {
			if strings.Contains(r.Message, "'pi'") {
				t.Errorf("expected pi to be marked used by format string, got: %s", r.Message)
			}
		}
	})

	t.Run("var_used_in_plain_field", func(t *testing.T) {
		src := `name = 'world'
print('hello {name}')
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateUnusedVars(prog)
		for _, r := range results {
			if strings.Contains(r.Message, "'name'") {
				t.Errorf("expected name to be marked used by format string, got: %s", r.Message)
			}
		}
	})

	t.Run("truly_unused_var_still_reported", func(t *testing.T) {
		src := `unused = 42
print('hello')
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		results := ValidateUnusedVars(prog)
		found := false
		for _, r := range results {
			if strings.Contains(r.Message, "'unused'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected unused variable 'unused' to be reported, got: %v", results)
		}
	})
}
