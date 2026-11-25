package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ignore 是可選欄位：缺席時不警告、不補；出現時在工作區構建裡跳過命中的
// 檔案與整棵目錄。壞檔若被誤編進去，BuildWorkspace 會直接失敗。
func TestBuildWorkspaceHonorsIgnorePatterns(t *testing.T) {
	skipWithoutStdSigKey(t)

	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("workspace.noml", "{\n  demo: '/demo'\n}\n")
	write("demo/package.noml", `{
  name: 'ignore-probe'
  description: ''
  category: ''
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: ''
  publish: true
  dependencies: []
  dev-dependencies: []
  ignore: ['bad_skip.no', 'gen_skip', 'bad-dir']
  mirrors: []
  compiler: {
    version: 'v0.1.0'
  }
  output: '/dist'
}
`)
	write("demo/main.no", "print('ok')\n")
	write("demo/tests/good.no", "print('ok')\n")
	write("demo/tests/bad_skip.no", "print('bad'\n")
	write("demo/tests/gen_skip/bad.no", "print('bad'\n")
	write("demo/tests/bad-dir/bad.no", "print('bad'\n")

	if err := BuildWorkspace(dir, BuildOptions{CC: "clang"}); err != nil {
		t.Fatalf("BuildWorkspace compiled an ignored source: %v", err)
	}
}

// 明確以「目錄」當構建輸入時，被 ignore 命中的 main.no 必須擋下；
// 明確指定單一檔案則不受 ignore 限制（由 checkIgnoredDirEntry 的輸入判斷保證）。
func TestCheckIgnoredDirEntry(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.noml", `{
  name: 'ignored-entry'
  description: ''
  category: ''
  keywords: []
  author: ''
  email: ''
  organization: ''
  repository: ''
  homepage: ''
  license: ''
  publish: true
  dependencies: []
  dev-dependencies: []
  ignore: ['main.no']
  mirrors: []
  compiler: {
    version: 'v0.1.0'
  }
  output: '/dist'
}
`)
	write("main.no", "main = () { print('ok') }\n")

	if err := checkIgnoredDirEntry(dir); err == nil {
		t.Fatal("checkIgnoredDirEntry accepted an ignored main.no")
	} else if !strings.Contains(err.Error(), "ignored by package.noml") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := checkIgnoredDirEntry(filepath.Join(dir, "main.no")); err != nil {
		t.Fatalf("explicit file must bypass ignore check: %v", err)
	}
}
