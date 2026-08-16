//go:build !wasm

package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestVetFile_LightweightModuleCheck 验证 LSP 路径（VetFile）在 LightweightMode
// 下能检测到调用不存在的 std 模块函数（如 math.nonexistent_fn）。
//
// 回归保障：防止 LSP 路径因不做模块合并而漏报 module.fn 调用
// （即 no vet 报 unknown function，lsp vet 却 "No issues found"）。
func TestVetFile_LightweightModuleCheck(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.no")

	source := `main = () {
	math.nonexistent_fn(1.0)
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	results := VetFile(path)

	found := false
	for _, r := range results {
		if r.Severity == "error" && r.Line > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("期望至少一条 error 级别诊断（unknown module fn），实际: %+v", results)
	}
}

// TestVetFile_CleanFile 验证无错误的文件不产生诊断。
func TestVetFile_CleanFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "clean.no")

	source := `main = () {
	print('hello')
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	results := VetFile(path)

	// 过滤掉 hint 级别的（命名规范等），只看 error 和 warning
	var errs []VetResult
	for _, r := range results {
		if r.Severity == "error" || r.Severity == "warning" {
			errs = append(errs, r)
		}
	}
	if len(errs) > 0 {
		t.Errorf("期望无 error/warning 诊断，但得到: %+v", errs)
	}
}

// TestVetFile_UnusedVar 验证 LSP 路径能检测到未使用变量（hint 级别）。
func TestVetFile_UnusedVar(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "unused.no")

	source := `main = () {
	unused i64 = 42
	print('ok')
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	results := VetFile(path)

	found := false
	for _, r := range results {
		if r.Severity == "hint" && r.Line > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("期望至少一条 hint 级别诊断（未使用变量），实际: %+v", results)
	}
}

// TestVetDirVerbose_IgnoreList 验证 LSP 目录模式（VetDirVerbose）
// 会跳过 package.jsonc 中 ignore 列表匹配的文件。
//
// 回归保障：防止 LSP 目录模式跑出来的文件数量与 no vet 不一致
// （因 ignore 列表未对齐导致多跑或少跑文件）。
func TestVetDirVerbose_IgnoreList(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建 package.jsonc，忽略 *_skip.no 文件
	pkgJSON := `{
	"name": "test-pkg",
	"ignore": ["*_skip.no"]
}`
	pkgPath := filepath.Join(tmpDir, "package.jsonc")
	if err := os.WriteFile(pkgPath, []byte(pkgJSON), 0644); err != nil {
		t.Fatalf("写入 package.jsonc 失败: %v", err)
	}

	// 创建两个文件：一个正常，一个应被忽略
	normalSrc := `main = () {
	print('hello')
}
`
	normalPath := filepath.Join(tmpDir, "normal.no")
	if err := os.WriteFile(normalPath, []byte(normalSrc), 0644); err != nil {
		t.Fatalf("写入 normal.no 失败: %v", err)
	}

	skipSrc := `main = () {
	math.nonexistent_fn(1.0)
}
`
	skipPath := filepath.Join(tmpDir, "bad_skip.no")
	if err := os.WriteFile(skipPath, []byte(skipSrc), 0644); err != nil {
		t.Fatalf("写入 bad_skip.no 失败: %v", err)
	}

	// 运行 VetDirVerbose
	results := VetDirVerbose(tmpDir, nil)

	// 确认 bad_skip.no 不在结果中
	for _, r := range results {
		if filepath.Base(r.File) == "bad_skip.no" {
			t.Errorf("bad_skip.no 应被 ignore 列表跳过，但仍出现在结果中: %+v", r)
		}
	}

	// 确认 normal.no 至少被处理过（可能无诊断，但不应被跳过）
	// 由于 normal.no 无 error，results 可能为空——这里只验证 bad_skip.no 被跳过即可
}

// TestVetFile_StructuredFormat 验证 LSP 路径返回的 VetResult
// 包含文件路径、行号、列号、严重度、来源、消息——格式完整。
func TestVetFile_StructuredFormat(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "type_err.no")

	source := `x i64 = 'not a number'
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	results := VetFile(path)

	found := false
	for _, r := range results {
		if r.Severity == "error" {
			if r.Line == 0 {
				t.Errorf("error 诊断缺少行号: %+v", r)
			}
			if r.Source == "" {
				t.Errorf("error 诊断缺少来源: %+v", r)
			}
			if r.Message == "" {
				t.Errorf("error 诊断缺少消息: %+v", r)
			}
			if r.File == "" {
				t.Errorf("error 诊断缺少文件路径: %+v", r)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("期望至少一条 error 级别诊断（类型错误），实际: %+v", results)
	}
}
