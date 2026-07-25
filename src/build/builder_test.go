//go:build !wasm

package build

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// toolAvailable 報告指定工具是否在 $PATH 中可用。
func toolAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// TestBuildLLVMInternalWasiMissingSysroot 驗證：當 target 為 wasm32-wasi 且
// $WASI_SYSROOT 未設定時，buildLLVMInternal 應回傳包含安裝提示的清晰錯誤訊息。
//
// 測試流程：
//  1. 跳過條件：若 opt/llc/clang 任一不在 $PATH 中，則 t.Skip
//  2. 顯式將 $WASI_SYSROOT 設為空字串
//  3. 以最小合法 LLVM IR 呼叫 buildLLVMInternal，target=wasm32-wasi
//  4. 驗證回傳錯誤包含 "WASI sysroot not found" 與 macOS/Ubuntu 安裝提示
//
// 註：buildLLVMInternal 為未匯出函式，故測試需在 package build 內。
func TestBuildLLVMInternalWasiMissingSysroot(t *testing.T) {
	// 跳過條件：LLVM 工具鏈必須可用才能抵達連結階段（WASI sysroot 檢查發生處）。
	for _, tool := range []string{"opt", "llc", "clang"} {
		if !toolAvailable(tool) {
			t.Skipf("skipping: %s not in PATH", tool)
		}
	}

	// 儲存並還原 $WASI_SYSROOT
	orig := os.Getenv("WASI_SYSROOT")
	defer os.Setenv("WASI_SYSROOT", orig)
	if err := os.Setenv("WASI_SYSROOT", ""); err != nil {
		t.Fatalf("failed to unset WASI_SYSROOT: %v", err)
	}

	// 最小合法 LLVM IR：main 函數返回 0。
	// target triple 由 llc 的 -mtriple=wasm32-wasi 旗標指定，不需在 IR 中聲明。
	const code = `; ModuleID = 'test'
source_filename = "test"

define i32 @main() {
entry:
  ret i32 0
}
`

	// 輸出路徑不會被使用（WASI sysroot 檢查在 clangCmd.Run() 之前發生），
	// 但仍使用 t.TempDir() 以避免污染工作目錄。
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.wasm")

	// sink 用來吸收 opt/llc 的輸出，避免測試日誌噪音。
	sink := &bytes.Buffer{}
	err := buildLLVMInternal(code, "wasi_missing_sysroot_test", outPath, "clang", "wasm32-wasi", false, nil, sink)
	if err == nil {
		t.Fatal("expected error for missing $WASI_SYSROOT, got nil")
	}

	if !strings.Contains(err.Error(), "WASI sysroot not found") {
		t.Errorf("error message should contain 'WASI sysroot not found', got: %v", err)
	}

	// 驗證安裝提示涵蓋 macOS / Ubuntu / 原始碼建構三種路徑。
	wantSubstrings := []string{
		"brew install wasi-libc",       // macOS
		"wasi-sdk",                    // Ubuntu
		"WASI_SYSROOT",                // 環境變數提示
		"git clone",                   // 原始碼建構
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error message should contain %q, got: %v", s, err)
		}
	}
}

// --- Task 9: Direct WASM 後端整合測試 ---

// runDirectWasmWithWasmtime 以 Direct WASM 後端編譯源碼並以 wasmtime 執行。
// 回傳 (stdout, true) 成功；若 wasmtime 不在 $PATH 則回傳 ("", false) 供呼叫端 t.Skip。
// 編譯或執行失敗時直接 t.Fatalf。
func runDirectWasmWithWasmtime(t *testing.T, src string) (string, bool) {
	t.Helper()
	if !toolAvailable("wasmtime") {
		return "", false
	}
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.no")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	wasmBytes, err := BuildDirectWasm(srcPath, BuildOptions{
		Target:        "wasm32-wasi",
		UseDirectWasm: true,
	})
	if err != nil {
		t.Fatalf("BuildDirectWasm: %v", err)
	}
	outPath := filepath.Join(tmpDir, "out.wasm")
	if err := os.WriteFile(outPath, wasmBytes, 0644); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	cmd := exec.Command("wasmtime", "run", outPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("wasmtime exited %d\nstdout: %s\nstderr: %s",
				exitErr.ExitCode(), stdout.String(), stderr.String())
		}
		t.Fatalf("wasmtime run failed: %v\nstderr: %s", err, stderr.String())
	}
	return stdout.String(), true
}

// TestBuildDirectWasmHelloWorld 驗證 Direct WASM 後端能編譯 `print('Hello, World!')`
// 並產出可被 wasmtime 執行的 .wasm，stdout 應包含 "Hello, World!"。
// 若 wasmtime 不在 $PATH 則 t.Skip。
func TestBuildDirectWasmHelloWorld(t *testing.T) {
	stdout, ok := runDirectWasmWithWasmtime(t, "print('Hello, World!')")
	if !ok {
		t.Skip("wasmtime not in PATH")
	}
	if !strings.Contains(stdout, "Hello, World!") {
		t.Errorf("stdout = %q, want contains 'Hello, World!'", stdout)
	}
}

// TestBuildDirectWasmFib 驗證 Direct WASM 後端能編譯 fibonacci 迴圈版本，
// 產出可被 wasmtime 執行的 .wasm，stdout 應為 "55\n"。
// 若 wasmtime 不在 $PATH 則 t.Skip。
func TestBuildDirectWasmFib(t *testing.T) {
	src := `fib = (n i64) (r i64) {
	a = 0
	b = 1
	i <- [2..n]: {
		c = a + b
		a = b
		b = c
	}
	r = b
}
result = fib(10)
print(result)`
	stdout, ok := runDirectWasmWithWasmtime(t, src)
	if !ok {
		t.Skip("wasmtime not in PATH")
	}
	want := "55\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}
