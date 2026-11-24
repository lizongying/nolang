package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSumFileNewFormatRoundTrip 驗證新格式 sum.noml 的寫出/讀回：
// packages 是陣列、package 欄位含版本、首行 auto-generated 註解，
// 且 key/version 能從 package 字串還原（與 CheckSumFile 的 key@version 查法一致）。
func TestSumFileNewFormatRoundTrip(t *testing.T) {
	dir := t.TempDir()
	packages := map[string]SumPkg{
		"nogit@github.com/lizongying/notools@v0.1.38": {
			Key: "nogit@github.com/lizongying/notools", Version: "v0.1.38",
			SHA256: strings.Repeat("a", 64),
		},
		"noimg@github.com/lizongying/notools@v0.1.35": {
			Key: "noimg@github.com/lizongying/notools", Version: "v0.1.35",
			SHA256: strings.Repeat("b", 64),
		},
		"github.com/lizongying/nolang/test2@v1.0.0": {
			Key: "github.com/lizongying/nolang/test2", Version: "v1.0.0",
			SHA256: strings.Repeat("c", 64),
		},
	}
	if err := SaveSumPackages(dir, packages); err != nil {
		t.Fatalf("SaveSumPackages: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, SumFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(raw)
	if !strings.HasPrefix(text, sumFileHeader) {
		t.Errorf("missing header %q; got: %s", sumFileHeader, text)
	}
	if !strings.Contains(text, "package: 'nogit:v0.1.38@github.com/lizongying/notools'") {
		t.Errorf("package spec (含版本) 不在輸出中: %s", text)
	}
	if !strings.Contains(text, "packages: [") {
		t.Errorf("packages 不是陣列: %s", text)
	}

	got, err := LoadSumFile(dir)
	if err != nil {
		t.Fatalf("LoadSumFile: %v", err)
	}
	if len(got.Packages) != len(packages) {
		t.Fatalf("packages len = %d, want %d: %+v", len(got.Packages), len(packages), got.Packages)
	}
	for name, want := range packages {
		entry, ok := got.Packages[name]
		if !ok {
			t.Errorf("missing entry %s; got keys: %v", name, keysOf(got.Packages))
			continue
		}
		if entry.Key != want.Key || entry.Version != want.Version || entry.SHA256 != want.SHA256 {
			t.Errorf("entry %s = %+v, want %+v", name, entry, want)
		}
	}
}

func keysOf(m map[string]SumPkg) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestSumFileLegacyMapFormat 驗證舊 map 格式仍可讀（向後相容）。
func TestSumFileLegacyMapFormat(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  version: '1'
  packages: {
    'nogit@github.com/lizongying/notools@v0.1.0': {
      key: 'nogit@github.com/lizongying/notools'
      version: 'v0.1.0'
      sha256: 'abcdef'
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, SumFileName), []byte(legacy), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadSumFile(dir)
	if err != nil {
		t.Fatalf("LoadSumFile legacy: %v", err)
	}
	entry, ok := got.Packages["nogit@github.com/lizongying/notools@v0.1.0"]
	if !ok || entry.SHA256 != "abcdef" {
		t.Errorf("legacy entry = %+v (ok=%v), want sha256=abcdef", entry, ok)
	}
}
