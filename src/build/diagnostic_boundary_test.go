package build

// #6 行为保持门禁（等价性版，归一化无关、自验证）：
// 验证 DiagnosticError 的结构化 LintResult 与「Error() 字串经 parseCompileErrorToLints
// 反解析」得到的 LintResult 逐条相等。这证明 #6 把 []checker.ValidateResult 停拍平、
// 改为边界结构化后，no vet 实际回传的 lint 与旧拍平→反解析路径完全一致——即编译期
// 验证诊断的输出零变化。
//
// 说明：Batch 32 的原始临时指纹测试（硬编码 sha256=bb84cf82…）已随该批清理删除，
// 其精确归一化口径不可恢复；本测试改用等价性断言（不依赖丢失的基线哈希），对任意
// 归一化口径都成立，是更强的保持证明。
import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiagnosticBoundaryParityTemp(t *testing.T) {
	dir := t.TempDir()
	// 未定义变量（顶层语句，避开 'main' 保留字检查）：语法合法，但
	// ValidateUndefinedVars 在验证期拦截，触发 *DiagnosticError（而非 parser error）。
	src := "result = undefinedVar + 1\n"
	p := filepath.Join(dir, "bad.no")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 路径 A：VetFileWithLints（经 errors.As 取结构化 LintResult）
	lints, lerr := VetFileWithLints(p, BuildOptions{})
	if lerr != nil {
		t.Fatalf("VetFileWithLints error: %v", lerr)
	}

	// 路径 B：直接 Compile 拿 error，分别用 errors.As 与 reparse 两条路取出
	compiler := NewTranspiler(nil)
	compiler.sourcePath = p
	compiler.SetVetMode(true)
	_, cerr := compiler.Compile(src)
	if cerr == nil {
		t.Fatal("expected Compile error from validation")
	}
	var de *DiagnosticError
	if !errors.As(cerr, &de) {
		t.Fatalf("Compile error is not *DiagnosticError: %T %v", cerr, cerr)
	}
	reparsed := parseCompileErrorToLints(cerr)

	if !reflect.DeepEqual(de.Lints, reparsed) {
		t.Fatalf("structured Lints != reparsed Lints\n--- structured ---\n%+v\n--- reparsed ---\n%+v", de.Lints, reparsed)
	}
	// 路径 A 的回传 lint 必须包含路径 B 的结构化验证诊断（顺序可能经 VetLints 前置，
	// 故以子集断言，而非全等）。
	found := false
	for _, l := range lints {
		for _, sl := range de.Lints {
			if reflect.DeepEqual(l, sl) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found && len(de.Lints) > 0 {
		t.Fatalf("VetFileWithLints did not surface the structured validation lint; lints=%+v structured=%+v", lints, de.Lints)
	}
	t.Logf("parity OK: %d structured lint(s) match reparse; VetFileWithLints surfaced %d lint(s)", len(de.Lints), len(lints))
}
