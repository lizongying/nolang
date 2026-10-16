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

// toolAvailable 報告指定工具是否在本機「可執行」。
//
// 僅用 exec.LookPath 檢查存在是不夠的：一個存在於 PATH 但與本機指令集不相容的
// 二進位（例如 arm64 Mac 上未裝 Rosetta 時的 x86_64 wasmtime）會「存在但無法 spawn」。
// 這類失敗是環境缺陷，不是被测代碼的錯誤，依賴它的測試應該 t.Skip 而非 t.Fatal。
// 因此用一次 `--version` 探測真實可執行性：只有「無法啟動」（非 ExitError）才判定不可用；
// 工具正常啟動後即便以非零碼退出（不支援 --version）仍視為可用。
func toolAvailable(name string) bool {
	if _, err := exec.LookPath(name); err != nil {
		return false
	}
	cmd := exec.Command(name, "--version")
	if err := cmd.Run(); err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			return false // 無法 spawn：指令集／平台不相容
		}
	}
	return true
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

// TestZigTargetTriple 驗證 zig 的 Apple target 轉換：zig 連結已編譯好的
// arm64-apple-darwin object 時，若 --target 仍用 LLVM 的 "apple-darwin" 會報
// "unable to parse target query '...': UnknownOperatingSystem"；zig 的 OS 名是
// "macos"，故必須轉成 <arch>-macos。非 Apple 目標應原樣返回（冪等）。
func TestZigTargetTriple(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"aarch64-apple-darwin", "aarch64-macos"},
		{"x86_64-apple-darwin", "x86_64-macos"},
		{"aarch64-apple-darwin27.0.0", "aarch64-macos"},
		{"x86_64-apple-darwin22.5.0", "x86_64-macos"},
		{"wasm32-wasi", "wasm32-wasi"},
		{"wasm32-unknown-wasi", "wasm32-unknown-wasi"},
		{"x86_64-linux-gnu", "x86_64-linux-gnu"},
		{"", ""},
	}
	for _, c := range cases {
		if got := zigTargetTriple(c.in); got != c.want {
			t.Errorf("zigTargetTriple(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLLVMCodegenTriple 驗證 llvmCodegenTriple 把使用者 / CI 傳入的 -target
// （可能是 zig 風格方言）映射成 LLVM 規範 triple，以便進程內 LLVM C API 後端在
// 任何宿主上都選對 object 格式（darwin→Mach-O、windows→COFF、linux→ELF）。
// 這正是 CI（Linux runner + no-llvm）交叉編譯 darwin/windows 失敗的根因：
// 直接把 "x86_64-macos-none" / "aarch64-windows-gnu" 這類方言餵給 LLVM，OS 落在
// vendor 槽位 → 判成 UnknownOS → 退回宿主 ELF，連結階段才報 TBD / unknown file type。
func TestLLVMCodegenTriple(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// CI 的 6 個 zig 風格方言 → 規範 triple
		{"x86_64-macos-none", "x86_64-apple-macosx"},
		{"aarch64-macos-none", "aarch64-apple-macosx"},
		{"x86_64-windows-gnu", "x86_64-pc-windows-gnu"},
		{"aarch64-windows-gnu", "aarch64-pc-windows-gnu"},
		{"x86_64-linux-gnu", "x86_64-unknown-linux-gnu"},
		{"aarch64-linux-gnu", "aarch64-unknown-linux-gnu"},
		// 已規範化的 Apple/Go 拼寫也应收敛到同一 OS 槽位
		{"aarch64-apple-darwin", "aarch64-apple-macosx"},
		{"x86_64-apple-macos", "x86_64-apple-macosx"},
		// wasm / 空 target 原样返回（不干涉既有行为）
		{"wasm32-wasi", "wasm32-wasi"},
		{"wasm32-unknown-wasi", "wasm32-unknown-wasi"},
		{"", ""},
	}
	for _, c := range cases {
		if got := llvmCodegenTriple(c.in); got != c.want {
			t.Errorf("llvmCodegenTriple(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBuildLLVMInternalZigDarwinLinks 端到端驗證：以 cc="zig"、target 為
// aarch64-apple-darwin 呼叫 buildLLVMInternal，連結階段必須把 --target 翻成
// aarch64-macos，否則 zig 會報 UnknownOperatingSystem。
//
// 跳過條件：opt/llc/zig 任一不在 PATH，或 zig 不可用。
func TestBuildLLVMInternalZigDarwinLinks(t *testing.T) {
	for _, tool := range []string{"opt", "llc", "zig"} {
		if !toolAvailable(tool) {
			t.Skipf("skipping: %s not in PATH", tool)
		}
	}
	// 最小合法 LLVM IR：定義 main 回傳 0，object triple 為 arm64-apple-darwin。
	const code = `target triple = "arm64-apple-darwin"
define i32 @main() {
  ret i32 0
}
`
	// 走 verbose 路徑（opt + llc 產生 .s，再交 zig cc 組譯 + 連結），
	// 該路徑不會對 zig 附加 -x ir（zig 不支援 -x ir），
	// 正好用來驗證 darwin triple 在連結期的 zig 翻譯。
	out := t.TempDir()
	outBin := filepath.Join(out, "a.out")
	if err := buildLLVMInternal(code, "main", outBin, "zig", "aarch64-apple-darwin", true, nil, nil); err != nil {
		t.Fatalf("buildLLVMInternal (zig, aarch64-apple-darwin) failed: %v", err)
	}
	if _, statErr := os.Stat(outBin); statErr != nil {
		t.Fatalf("expected linked binary %s: %v", outBin, statErr)
	}
}

// TestBuildLLVMInternalZigDarwinLinksSingleShot 覆蓋 singleShot（非 verbose）路徑下
// cc="zig" 的連結：此路徑會對 clang 附加 -x ir 讓 clang 直接吃 .ll，但 zig 不認 -x ir
// （報 "language not recognized: \"ir\""）。修復後 zig 改靠 .ll 副檔名識別 IR（不加 -x ir），
// 本測試即為該回歸用例——對應 CI 的 embedded-fallback 步驟（NOLANG_LLVM_CAPI=0 + -cc zig）。
//
// 跳過條件：zig 不在 PATH。
func TestBuildLLVMInternalZigDarwinLinksSingleShot(t *testing.T) {
	if !toolAvailable("zig") {
		t.Skip("skipping: zig not in PATH")
	}
	// 與 CI embedded-fallback 一致：關閉 C API，強制走 singleShot 的 zig cc 路徑
	// （否則 no-llvm 會在進程內用 LLVM C API 產 .o，根本不會碰到 -x ir 分支）。
	t.Setenv("NOLANG_LLVM_CAPI", "0")
	const code = `target triple = "arm64-apple-darwin"
define i32 @main() {
  ret i32 0
}
`
	out := t.TempDir()
	outBin := filepath.Join(out, "a.out")
	// verbose=false ⇒ singleShot；cc="zig" 且關閉 C API ⇒ 走 -x ir 分支（修復前會掛）。
	if err := buildLLVMInternal(code, "main", outBin, "zig", "aarch64-apple-darwin", false, nil, nil); err != nil {
		t.Fatalf("buildLLVMInternal (zig, singleShot) failed: %v", err)
	}
	if _, statErr := os.Stat(outBin); statErr != nil {
		t.Fatalf("expected linked binary %s: %v", outBin, statErr)
	}
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
