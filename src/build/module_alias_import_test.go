package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModuleAliasImportLLVM 驗證「模組別名導入」：`# /util/helper hx` 把整個
// 模組綁到別名 hx 下，之後以 `hx.<fn>` 呼叫其中的頂層函數。
//
// 背景：這種寫法（有別名、無函數名）先前在合併階段被一律當成錯誤
// （別名只對 `.noml` 導入或 `# path.fn as alias` 有意義），於是
// src/std/global.no 的 `# std/err error` 無法通過 `no vet src/std`。
// 別名的語義就是換掉模組前綴：`error.new(...)` / 型別 `error.err`。
//
// 註（既有缺口，與別名無關）：從模組導入的結構體字面量
// （`helper.h-str { v: 1 }`）在**無別名**的普通導入下同樣報 MIR NoVal
// （bug #85 同族），模組常量的別名形式（`hx.h-const`）也還沒接上——
// 常量合併時保留原名。這裡只釘住別名確實生效的部分。
func TestModuleAliasImportLLVM(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.noml"),
		[]byte(`{ test: '.' }`), 0644); err != nil {
		t.Fatal(err)
	}

	utilDir := filepath.Join(tmpDir, "util")
	if err := os.MkdirAll(utilDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(utilDir, "helper.no"), []byte(
		"h-fn = (x i64) (r i64) {\n"+
			"    r = x + 1\n"+
			"}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mainSrc := "# /util/helper hx\n" +
		"\n" +
		"r = hx.h-fn(41)\n" +
		"print(r)\n"
	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	llvmIR, err := trans.Compile(mainSrc)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if !irHasFunc(llvmIR, "h-fn") {
		t.Errorf("LLVM IR does not contain @h-fn — aliased module member was dropped")
	}
}

// TestModuleAliasImportJS 同一件事在 JS 後端：JS 輸出是扁平函數集、沒有模組
// 命名空間，所以別名導入的關鍵是「別名被登記成可解析的模組名 + 合併時記下來源
// 模組」，讓結尾的 ResolveModuleCalls 把 `hx.h-fn(41)` 攤平成 `h-fn(41)`。
// 這裡直接跑完整的 JS 產出，斷言生成的程式碼裡沒有殘留的 `hx.hFn`。
func TestModuleAliasImportJS(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.noml"),
		[]byte(`{ test: '.' }`), 0644); err != nil {
		t.Fatal(err)
	}
	// emit: 'js' —— 讓 BuildJS 走 JS 後端。
	if err := os.WriteFile(filepath.Join(tmpDir, "package.noml"),
		[]byte("{\n  name: 'aliasjs'\n  compiler: {\n    emit: 'js'\n  }\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	utilDir := filepath.Join(tmpDir, "util")
	if err := os.MkdirAll(utilDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(utilDir, "helper.no"),
		[]byte("h-fn = (x i64) (r i64) {\n    r = x + 1\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mainSrc := "# /util/helper hx\n" +
		"\n" +
		"r = hx.h-fn(41)\n" +
		"print(r)\n"
	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	code, err := BuildJS(mainPath, BuildOptions{UseJS: true})
	if err != nil {
		t.Fatalf("BuildJS: %v", err)
	}
	if strings.Contains(code, "hx.hFn") || strings.Contains(code, "hx.") {
		t.Errorf("aliased module prefix leaked into JS output (`hx.` must be flattened away):\n%s", code)
	}
	if !strings.Contains(code, "hFn(41)") {
		t.Errorf("flattened call hFn(41) missing from JS output:\n%s", code)
	}
}
