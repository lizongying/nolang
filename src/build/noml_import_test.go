package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNomlImportSource 驗證 .noml → nolang 源碼的重建規則：
// 結構體名 = 文件名、巢狀物件生成巢狀結構體、`#{type}` 註解優先於值推斷。
func TestNomlImportSource(t *testing.T) {
	noml := `{
  host: 'localhost'
  port: 8080
  ratio: 0.5
  debug: false
  tags: [] #{type = '[]str'}
  #{type = 'i64'}
  workers: 4
  compiler: {
    version: 'v0.1.0'
    emit: 'js'
  } #{doc = 'build settings'}
}`
	src, err := NomlImportSource("/ws/cfg/server.noml", []byte(noml))
	if err != nil {
		t.Fatalf("NomlImportSource: %v", err)
	}
	for _, want := range []string{
		"server {",
		"host str",
		"port i64",
		"ratio f64",
		"debug bool",
		"tags []str",
		"workers i64",
		"server-compiler {",
		"compiler server-compiler",
		"server = server {",
		"compiler: server-compiler {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q:\n%s", want, src)
		}
	}
}

// TestNomlImportCompile 端到端驗證 `# /cfg/server` 直接引入 .noml：
// 生成物走既有模組合併管線，`server.port` 可讀，且型別名保持裸名 server
// （結構體名 = 文件名，不參與模組前綴）。
func TestNomlImportCompile(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.noml"),
		[]byte(`{ test: '.' }`), 0644); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(tmpDir, "cfg")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// 檔名特意取 server.noml：std 也有 server 模組（其型別會被前綴為
	// server.server），noml 型別必須保持裸名，不被同名模組型別改寫。
	noml := `{
  host: 'localhost'
  port: 8080
  tags: [] #{type = '[]str'}
  compiler: {
    version: 'v0.1.0'
    emit: 'js'
  }
}`
	if err := os.WriteFile(filepath.Join(cfgDir, "server.noml"), []byte(noml), 0644); err != nil {
		t.Fatal(err)
	}
	mainSrc := "# /cfg/server\n" +
		"print(server.port)\n" +
		"print(server.host)\n" +
		"print(server.compiler.version)\n"
	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	ir, err := trans.Compile(mainSrc)
	if err != nil {
		t.Fatalf("compile error: %v\n--- IR ---\n%s", err, ir)
	}
	if !strings.Contains(ir, "%server = type {") {
		t.Errorf("IR is missing the bare struct type %%server:\n%s", ir)
	}
}

// TestNomlImportAlias 驗證 `# /cfg/server as cfg`：值綁定改為別名，
// 結構體名仍是文件名。
func TestNomlImportAlias(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "workspace.noml"),
		[]byte(`{ test: '.' }`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "server.noml"),
		[]byte("{\n  port: 9000\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mainSrc := "# /server as cfg\nprint(cfg.port)\n"
	mainPath := filepath.Join(tmpDir, "main.no")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	if _, err := trans.Compile(mainSrc); err != nil {
		t.Fatalf("compile error: %v", err)
	}
}

// TestNomlImportEmptyContainerNeedsType 驗證空容器必須有型別註解：
// noml 的值本身無型別，空陣列/空物件無法推斷。
func TestNomlImportEmptyContainerNeedsType(t *testing.T) {
	_, err := NomlImportSource("/ws/cfg/a.noml", []byte("{\n  tags: []\n}\n"))
	if err == nil || !strings.Contains(err.Error(), "#{type") {
		t.Fatalf("expected an annotation hint for the empty array, got %v", err)
	}
}
