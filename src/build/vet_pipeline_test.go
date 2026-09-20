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
//
// 回归保障（parser errors 规范化）：
//   - parser errors 也必须逐条给出位置，而不是把整个 "parser errors: [...]" 当成
//     一条无位置的 lint（曾如此，见 src/std/crypto/sha3.no）；
//   - 位置一旦提取，就不能再留在 Message 里，否则 `no vet` 会印两次位置
//     （"/f.no:1:9: [ERROR] nolang-compile: line 1, column 9: ..."）；
//   - 消息正文里的分号不能被当成诊断分隔符切开。
func TestParseCompileErrorToLints(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCount int
		wantFirst struct {
			line   int
			col    int
			source string
			file   string
			tid    string
			msg    string // 非空时精确断言（位置前缀必须已被剥离）
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
				file   string
				tid    string
				msg    string
			}{line: 10, col: 3, source: "nolang-compile", msg: "type mismatch"},
		},
		{
			name:      "check_error_single",
			err:       strErr("check error: line 5: unknown function 'math.bogus'"),
			wantCount: 1,
			wantFirst: struct {
				line   int
				col    int
				source string
				file   string
				tid    string
				msg    string
			}{line: 5, col: 0, source: "nolang-compile", msg: "unknown function 'math.bogus'"},
		},
		{
			name:      "validation_error_single",
			err:       strErr("validation error: line 1, column 1: something wrong"),
			wantCount: 1,
			wantFirst: struct {
				line   int
				col    int
				source string
				file   string
				tid    string
				msg    string
			}{line: 1, col: 1, source: "nolang-compile", msg: "something wrong"},
		},
		{
			// src/std/crypto/sha3.no 的真实形态：两条诊断曾被 Go 的 []string
			// 渲染成 "[a b]" 粘成一条无位置的长消息。
			name: "parser_errors_multi",
			err: strErr("parser errors: " +
				"line 24, column 23: expected comma or right parenthesis, got ASSIGN(=) instead [E_GENERAL]; " +
				"line 24, column 18: expected expression after operator '*' [E_GENERAL]"),
			wantCount: 2,
			wantFirst: struct {
				line   int
				col    int
				source string
				file   string
				tid    string
				msg    string
			}{
				line: 24, col: 23, source: "nolang-compile", tid: "E_GENERAL",
				msg: "expected comma or right parenthesis, got ASSIGN(=) instead",
			},
		},
		{
			// 消息正文里的分号不是分隔符（parser/stmt.go 的 export alias 提示）。
			name:      "semicolon_inside_message",
			err:       strErr("parser errors: line 5, column 3: export alias 'add' is the same as the function name; the alias can be omitted [E_GENERAL]"),
			wantCount: 1,
			wantFirst: struct {
				line   int
				col    int
				source string
				file   string
				tid    string
				msg    string
			}{
				line: 5, col: 3, source: "nolang-compile", tid: "E_GENERAL",
				msg: "export alias 'add' is the same as the function name; the alias can be omitted",
			},
		},
		{
			// 被汇入模块的语法错误：parseFile 用 "<path>: " 前缀标注来源，
			// 该路径必须成为 LintResult.File，而不是丢掉。
			name:      "module_file_prefix",
			err:       strErr("/w/src/std/vec.no: line 3, column 5: boom [E_GENERAL]"),
			wantCount: 1,
			wantFirst: struct {
				line   int
				col    int
				source string
				file   string
				tid    string
				msg    string
			}{line: 3, col: 5, source: "nolang-compile", file: "/w/src/std/vec.no", tid: "E_GENERAL", msg: "boom"},
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
			if tt.wantCount == 0 {
				return
			}
			got := results[0]
			if got.Line != tt.wantFirst.line {
				t.Errorf("行号: got %d, want %d", got.Line, tt.wantFirst.line)
			}
			if got.Column != tt.wantFirst.col {
				t.Errorf("列号: got %d, want %d", got.Column, tt.wantFirst.col)
			}
			if got.Source != tt.wantFirst.source {
				t.Errorf("来源: got %s, want %s", got.Source, tt.wantFirst.source)
			}
			if got.Severity != checker.LintError {
				t.Errorf("严重度: got %s, want %s", got.Severity, checker.LintError)
			}
			if tt.wantFirst.file != "" && got.File != tt.wantFirst.file {
				t.Errorf("文件: got %q, want %q", got.File, tt.wantFirst.file)
			}
			if tt.wantFirst.tid != "" && got.TraceID != tt.wantFirst.tid {
				t.Errorf("诊断码: got %q, want %q", got.TraceID, tt.wantFirst.tid)
			}
			if tt.wantFirst.msg != "" && got.Message != tt.wantFirst.msg {
				t.Errorf("消息: got %q, want %q", got.Message, tt.wantFirst.msg)
			}
			if strings.Contains(got.Message, "line ") && strings.Contains(got.Message, "column ") {
				t.Errorf("消息中不应残留位置前缀（会被调用方重复渲染）: %q", got.Message)
			}
		})
	}
}

// TestVetFileWithLints_ParserErrorIsStructured 是 no vet 端到端的回归：
// 一个语法错误的文件必须产出带行/列的错误 lint，而不是一条无位置的
// "parser errors: [...]" 塌缩消息。
func TestVetFileWithLints_ParserErrorIsStructured(t *testing.T) {
	dir := t.TempDir()
	// 第 3 行故意缺右操作数（与 src/std/crypto/sha3.no 的失败形态同类）。
	src := "main = () {\n    a i64 = 1\n    b i64 = 1 + \n}\n"
	path := filepath.Join(dir, "parser-err.no")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	lints, err := VetFileWithLints(path, BuildOptions{})
	if err != nil {
		t.Fatalf("VetFileWithLints 返回 error: %v", err)
	}

	var found *checker.LintResult
	for i := range lints {
		if lints[i].Severity == checker.LintError && lints[i].Line > 0 &&
			strings.Contains(lints[i].Message, "expected expression after operator") {
			found = &lints[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("未找到带行号的 parser error lint，实际: %+v", lints)
	}
	if found.Line != 3 {
		t.Errorf("行号: got %d, want 3", found.Line)
	}
	if strings.Contains(found.Message, "line 3, column") {
		t.Errorf("消息中不应残留位置前缀: %q", found.Message)
	}
	if strings.Contains(found.Message, "[E_GENERAL]") {
		t.Errorf("诊断码应落在 TraceID 字段而非消息正文: %q", found.Message)
	}
	if found.TraceID != "E_GENERAL" {
		t.Errorf("诊断码: got %q, want %q", found.TraceID, "E_GENERAL")
	}
}

// TestSplitCompileErrParts pins the splitting rule: cut only where a location
// prefix follows the "; " separator.
func TestSplitCompileErrParts(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want []string
	}{
		{"empty", "", nil},
		{"blank", "   ", nil},
		{
			name: "two_located",
			msg:  "line 10, column 3: a; line 20, column 1: b",
			want: []string{"line 10, column 3: a", "line 20, column 1: b"},
		},
		{
			// 分号在消息正文里，不是分隔符。
			name: "semicolon_in_message",
			msg:  "line 5, column 3: first; then more [E_GENERAL]",
			want: []string{"line 5, column 3: first; then more [E_GENERAL]"},
		},
		{
			name: "column_less_location",
			msg:  "line 5: a; line 6: b",
			want: []string{"line 5: a", "line 6: b"},
		},
		{
			name: "file_prefix_on_first_only",
			msg:  "/w/a.no: line 1, column 2: x; line 3, column 4: y",
			want: []string{"/w/a.no: line 1, column 2: x", "line 3, column 4: y"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCompileErrParts(tt.msg)
			if len(got) != len(tt.want) {
				t.Fatalf("split(%q) = %q, want %q", tt.msg, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

type strErr string

func (e strErr) Error() string { return string(e) }
