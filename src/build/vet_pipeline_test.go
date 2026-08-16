//go:build !wasm

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// TestVetFileWithLints_StructuredOutput 验证 no vet 路径（VetFileWithLints）
// 对已知文件返回结构化 LintResult（含行号/严重度/来源），而非塌缩的单行 error。
//
// 测试矩阵：
//   - type_err.no：类型错误 → LintError，行号 > 0
//   - unused_var.no：未使用变量 → LintHint
//   - unknown_modfn.no：调用不存在的模块函数 → LintError（来自 compile error 解析）
//
// 注：no vet 路径会合并 std 模块，产生来自 std 内部的诊断（属正常行为）。
// 测试只断言用户代码相关的诊断，忽略 std 模块噪音。
//
// 回归保障：防止将来把 compile error 重新塌缩成 "validation error: ..." 单行，
// 丢失行号/严重度，无法与 LSP 逐条对。
func TestVetFileWithLints_StructuredOutput(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		source  string
		wantErr bool // VetFileWithLints 返回 error（I/O 级别）
		checkFn func(t *testing.T, lints []checker.LintResult)
	}{
		{
			name: "type_error",
			source: `x i64 = 'not a number'
`,
			wantErr: false, // compile error 已解析为 lints，不再返回 error
			checkFn: func(t *testing.T, lints []checker.LintResult) {
				found := false
				for _, l := range lints {
					if l.Severity == checker.LintError && l.Line > 0 {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("期望至少一条带行号的 LintError，实际 lints: %+v", lints)
				}
			},
		},
		{
			name: "unused_var",
			source: `unused i64 = 42
`,
			wantErr: false,
			checkFn: func(t *testing.T, lints []checker.LintResult) {
				found := false
				for _, l := range lints {
					if l.Severity == checker.LintHint && l.Line > 0 && l.Line <= 2 {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("期望在行 1-2 找到 LintHint（未使用变量），实际 lints: %+v", lints)
				}
			},
		},
		{
			name: "unknown_modfn",
			source: `math.nonexistent_fn(1.0)
`,
			wantErr: false, // compile error 已解析为 lints，不再返回 error
			checkFn: func(t *testing.T, lints []checker.LintResult) {
				found := false
				for _, l := range lints {
					if l.Severity == checker.LintError && l.Line > 0 &&
						(strings.Contains(l.Message, "nonexistent_fn") ||
							strings.Contains(l.Message, "unknown")) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("期望一条含 'nonexistent_fn' 的 LintError，实际 lints: %+v", lints)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.name+".no")
			if err := os.WriteFile(path, []byte(tt.source), 0644); err != nil {
				t.Fatalf("写入测试文件失败: %v", err)
			}

			lints, err := VetFileWithLints(path, BuildOptions{})
			if (err != nil) != tt.wantErr {
				t.Errorf("VetFileWithLints error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, lints)
			}
		})
	}
}

// TestParseCompileErrorToLints 验证 compile error 字符串解析逻辑
// 能正确提取行号/列号，而非塌缩成一行。
func TestParseCompileErrorToLints(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCount int
		wantFirst struct {
			line   int
			col    int
			source string
		}
	}{
		{
			name:      "validation_errors_multi",
			err:       strErr("validation errors: line 10, column 3: type mismatch; line 20, column 1: undefined var"),
			wantCount: 2,
			wantFirst: struct {
				line   int
				col    int
				source string
			}{line: 10, col: 3, source: "nolang-compile"},
		},
		{
			name:      "check_error_single",
			err:       strErr("check error: line 5: unknown function 'math.bogus'"),
			wantCount: 1,
			wantFirst: struct {
				line   int
				col    int
				source string
			}{line: 5, col: 0, source: "nolang-compile"},
		},
		{
			name:      "validation_error_single",
			err:       strErr("validation error: line 1, column 1: something wrong"),
			wantCount: 1,
			wantFirst: struct {
				line   int
				col    int
				source string
			}{line: 1, col: 1, source: "nolang-compile"},
		},
		{
			name:      "nil_error",
			err:       nil,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := parseCompileErrorToLints(tt.err)
			if len(results) != tt.wantCount {
				t.Fatalf("期望 %d 条结果，得到 %d: %+v", tt.wantCount, len(results), results)
			}
			if tt.wantCount > 0 {
				if results[0].Line != tt.wantFirst.line {
					t.Errorf("行号: got %d, want %d", results[0].Line, tt.wantFirst.line)
				}
				if results[0].Column != tt.wantFirst.col {
					t.Errorf("列号: got %d, want %d", results[0].Column, tt.wantFirst.col)
				}
				if results[0].Source != tt.wantFirst.source {
					t.Errorf("来源: got %s, want %s", results[0].Source, tt.wantFirst.source)
				}
				if results[0].Severity != checker.LintError {
					t.Errorf("严重度: got %s, want %s", results[0].Severity, checker.LintError)
				}
			}
		})
	}
}

type strErr string

func (e strErr) Error() string { return string(e) }
