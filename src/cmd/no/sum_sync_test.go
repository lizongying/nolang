package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/mmast"
	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// sumSyncModuleSrc 是最小可打包模組，用來產生真實的 -ast.zip 快取檔。
const sumSyncModuleSrc = "greet = (name str) (out str) {\n  out = 'hi ' + name\n}\n"

// seedASTZip 把一個真實 ast.zip 寫進指定依賴版本的快取路徑，回傳其 SHA256。
func seedASTZip(t *testing.T, key, version string) string {
	t.Helper()
	pp := parser.New(lexer.NewCached("mod.no", sumSyncModuleSrc))
	pp.Filename = "mod.no"
	prog := pp.ParseProgram()
	if prog == nil || len(pp.Errors()) > 0 {
		t.Fatalf("parse: nil=%v errs=%v", prog == nil, pp.Errors())
	}
	var buf bytes.Buffer
	if _, err := mmast.WriteASTZip(&buf, prog, nil); err != nil {
		t.Fatalf("WriteASTZip: %v", err)
	}
	data := buf.Bytes()
	spec, err := pkg.ParseToolSpec(key)
	if err != nil {
		t.Fatalf("ParseToolSpec(%q): %v", key, err)
	}
	cache := mmast.ASTZipCachePath(spec, version)
	if err := os.MkdirAll(filepath.Dir(cache), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cache, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// inTempProject 把 cwd 切到一個含 package.noml 的臨時包目錄。
func inTempProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("NOLANG_AST_CACHE_DIR", filepath.Join(dir, "astcache"))
	createConfigFile(ProjectConfig{
		Name:         "sumsync",
		Dependencies: map[string]string{},
	})
}

func readSum(t *testing.T) *pkg.SumFile {
	t.Helper()
	if _, err := os.Stat(pkg.SumFileName); err != nil {
		t.Fatalf("stat %s: %v (sum.noml 應由依賴命令生成)", pkg.SumFileName, err)
	}
	sum, err := pkg.LoadSumFile(".")
	if err != nil || sum == nil {
		t.Fatalf("LoadSumFile: sum=%v err=%v", sum, err)
	}
	return sum
}

// TestSumSyncAddRemoveUpdate 驗證四個命令共用的 refreshSumFile：
// add 寫入條目 + SHA256，update 遷移版本鍵，remove 清空條目。
func TestSumSyncAddRemoveUpdate(t *testing.T) {
	inTempProject(t)
	key := "mymod@127.0.0.1:1/owner/repo" // host 帶埠、離線：只走快取，不觸網
	h1 := seedASTZip(t, key, "v1.0.0")
	h2 := seedASTZip(t, key, "v2.0.0")

	// add：生成 sum.noml，條目 key@version，SHA256 取自 ast.zip
	refreshSumAfterChange(map[string]string{key: "v1.0.0"})
	sum := readSum(t)
	if sum.Version != pkg.SumFileVersion {
		t.Errorf("version = %q, want %q", sum.Version, pkg.SumFileVersion)
	}
	got, ok := sum.Packages[key+"@v1.0.0"]
	if !ok {
		t.Fatalf("missing entry %s; got %+v", key+"@v1.0.0", sum.Packages)
	}
	if got.SHA256 != h1 {
		t.Errorf("sha256 = %q, want %q", got.SHA256, h1)
	}

	// update：舊版本鍵消失、新版本鍵帶新 hash
	refreshSumAfterChange(map[string]string{key: "v2.0.0"})
	sum = readSum(t)
	if _, ok := sum.Packages[key+"@v1.0.0"]; ok {
		t.Error("stale v1.0.0 entry remains after update")
	}
	got, ok = sum.Packages[key+"@v2.0.0"]
	if !ok || got.SHA256 != h2 {
		t.Errorf("v2 entry = %+v (ok=%v), want sha256 %q", got, ok, h2)
	}

	// remove：條目清空，但 sum.noml 仍在（版本頭保留）
	refreshSumAfterChange(map[string]string{})
	sum = readSum(t)
	if len(sum.Packages) != 0 {
		t.Errorf("packages = %+v, want empty after remove", sum.Packages)
	}
	if sum.Version != pkg.SumFileVersion {
		t.Errorf("version = %q after remove, want %q", sum.Version, pkg.SumFileVersion)
	}
}

// TestSumSyncPreservesNonASTHash 驗證非 AST 依賴的舊 hash 不被覆寫
// （源碼依賴的 hash 由 EnsureDependencies 寫入，命令列不重算）。
func TestSumSyncPreservesNonASTHash(t *testing.T) {
	inTempProject(t)
	key := "github.com/lizongying/nolang/test2"
	oldHash := strings.Repeat("ab", 32)
	data, err := pkg.MarshalNoml(map[string]any{
		"version": pkg.SumFileVersion,
		"packages": map[string]any{
			key + "@v0.1.0": map[string]any{
				"key": key, "version": "v0.1.0", "sha256": oldHash,
			},
		},
	})
	if err != nil {
		t.Fatalf("MarshalNoml: %v", err)
	}
	if err := os.WriteFile(pkg.SumFileName, []byte(data), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	refreshSumAfterChange(map[string]string{key: "v0.1.0"})
	sum := readSum(t)
	got := sum.Packages[key+"@v0.1.0"]
	if got.SHA256 != oldHash {
		t.Errorf("sha256 = %q, want preserved %q", got.SHA256, oldHash)
	}
}

// TestSumSyncOfflineKeepsSum 驗證快取缺失且下載失敗時仍寫出 sum.noml
// （SHA256 留空），不讓 add/remove/update 因離線而失敗。
func TestSumSyncOfflineKeepsSum(t *testing.T) {
	inTempProject(t)
	key := "mymod@127.0.0.1:1/owner/repo" // 沒有 server，下載必失敗
	refreshSumAfterChange(map[string]string{key: "v9.9.9"})
	sum := readSum(t)
	entry, ok := sum.Packages[key+"@v9.9.9"]
	if !ok {
		t.Fatalf("entry missing after offline fetch; got %+v", sum.Packages)
	}
	if entry.SHA256 != "" {
		t.Errorf("sha256 = %q, want empty on offline failure", entry.SHA256)
	}
}
