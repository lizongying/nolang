package lsp

// fmt_signatures_test.go — LSP 跨模組簽名注入的正向斷言。
//
// 這三條測試分別釘住：std 表 / EnumVariants / 檔名不退化、本地模組簽名
// 真的循 use 進到 cfg、以及 format-on-save 的渲染結果因此改變。第三條是
// 端到端證據：光看 cfg 有表還不夠，必須證明 formatter 讀得到 program.Sem。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
	nfmt "github.com/lizongying/nolang/fmt"
)

func skipLSPWithoutStdSigKey(t *testing.T) {
	t.Helper()
	if checker.StdSigKey == "" {
		t.Skip("StdSigKey not injected (bare go test); run via make test")
	}
}

// newLSPProbeWorkspace 造一個帶 workspace.noml / package.noml 的最小專案，
// 其中 /lib/helper 的 is-ok 回傳純 bool（適合觀察 bool 比較化簡）。
func newLSPProbeWorkspace(t *testing.T) string {
	t.Helper()
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
	write("workspace.noml", "{\n  lib: '/lib'\n}\n")
	write("package.noml", "{\n  name: 'probe'\n}\n")
	write("lib/helper.no", "is-ok = () (b bool) {\n    b = true\n}\n")
	return dir
}

func TestLSPSigContextStdTablesAndFilename(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	c := newLSPSigContext()
	cfg := c.configFor("file:///tmp/probe/main.no", "f = () {\n    print('x')\n}\n")
	if len(cfg.FuncSigs) == 0 {
		t.Fatal("std function signatures missing from LSP config")
	}
	if cfg.MethodSigs == nil || cfg.StructFields == nil {
		t.Fatalf("std method/struct tables missing: %#v", cfg)
	}
	if len(cfg.EnumVariants) == 0 {
		t.Fatal("EnumVariants missing from LSP config (old main parse never injected it)")
	}
	// cfg.Apply 會無條件覆蓋 p.Filename；少了他診斷文字會漂移。
	if cfg.Filename != "main.no" {
		t.Fatalf("cfg.Filename = %q, want %q", cfg.Filename, "main.no")
	}
}

// TestLSPSigContextLoadsLocalModule 證明 LSP 不再是「只有 std 內嵌表」：
// use /lib/helper 會經 build.FmtSigLoader 的 resolveUse 讀到磁碟模組。
func TestLSPSigContextLoadsLocalModule(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	dir := newLSPProbeWorkspace(t)
	uri := "file://" + filepath.Join(dir, "main.no")
	c := newLSPSigContext()
	cfg := c.configFor(uri, "use /lib/helper\nf = () {\n    ok = helper.is-ok()\n}\n")
	got := cfg.FuncSigs["helper.is-ok"]
	if len(got) == 0 || got[0] != "bool" {
		t.Fatalf("local module signature missing from LSP config: %#v", got)
	}
}

// TestLSPFormattingUsesLocalModuleSignature 端到端：主解析的 doc.AST 帶了
// 跨模組簽名，format-on-save 才能把 `ok == true` 化簡成 `ok`。
// 對照組用裸單檔 FormatFile（舊 fallback），比較必須保留。
func TestLSPFormattingUsesLocalModuleSignature(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	dir := newLSPProbeWorkspace(t)
	uri := "file://" + filepath.Join(dir, "main.no")
	src := "use /lib/helper\nf = () {\n    ok = helper.is-ok()\n    ok == true -> return\n}\n"

	// 對照：沒有跨模組簽名時，bool 化簡規則必須保守地不動作。
	legacy := nfmt.FormatFile(src)
	if !strings.Contains(legacy, "ok == true") {
		t.Fatalf("legacy single-file format should keep the comparison, got:\n%s", legacy)
	}

	s := NewServer()
	if _, err := s.documents.OpenDocument(uri, src); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}
	if _, perrs, err := s.documents.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument: %v", err)
	} else if len(perrs) > 0 {
		t.Fatalf("unexpected parse diagnostics: %v", perrs)
	}
	doc, err := s.documents.GetDocument(uri)
	if err != nil {
		t.Fatal(err)
	}
	if doc.AST == nil {
		t.Fatal("ParseDocument did not publish doc.AST; the AST branch is not under test")
	}

	res, err := s.handleTextDocumentFormatting(DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options:      &FormattingOptions{TabSize: 4, InsertSpaces: true},
	})
	if err != nil {
		t.Fatalf("handleTextDocumentFormatting: %v", err)
	}
	edits, ok := res.([]TextEdit)
	if !ok || len(edits) == 0 {
		t.Fatalf("expected at least one TextEdit, got %#v", res)
	}
	out := edits[0].NewText
	if strings.Contains(out, "ok == true") {
		t.Fatalf("format-on-save should simplify the comparison using the local module signature, got:\n%s", out)
	}
	if !strings.Contains(out, "ok -> return") {
		t.Fatalf("unexpected formatted output:\n%s", out)
	}
}

// TestLSPSigContextCachesPackageLookup 直接數 LoadPackage 呼叫次數，證明
// 「每次擊鍵重跑一次向上逐級 stat + noml 解析」已經收掉。
// nbuild.LoadPackage 是 compat.go 的 var，故可在測試內暫時包一層計數；
// 本套件沒有 t.Parallel()，且 defer 一定還原，不會外溢到別的測試。
func TestLSPSigContextCachesPackageLookup(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	dir := newLSPProbeWorkspace(t)
	uri := "file://" + filepath.Join(dir, "main.no")

	orig := nbuild.LoadPackage
	calls := 0
	nbuild.LoadPackage = func(d string) (*nbuild.Package, error) {
		calls++
		return orig(d)
	}
	defer func() { nbuild.LoadPackage = orig }()

	c := newLSPSigContext()
	for i := 0; i < 3; i++ {
		c.configFor(uri, fmt.Sprintf("x str = '' // %d\n", i))
	}
	if calls != 1 {
		t.Fatalf("same dir must hit LoadPackage exactly once, got %d", calls)
	}
	if len(c.pkgs) != 1 {
		t.Fatalf("expected one cached package lookup, got %d", len(c.pkgs))
	}

	// 沒有 manifest 的目錄也要快取「查不到」這個結果，否則單檔 playground
	// 每次擊鍵仍會重跑整串 stat。
	bare := "file:///tmp/no-manifest-probe-dir/main.no"
	for i := 0; i < 2; i++ {
		c.configFor(bare, "x str = ''\n")
	}
	if calls != 2 {
		t.Fatalf("nil manifest result should be cached too, LoadPackage calls = %d", calls)
	}
	entry, ok := c.pkgs["/tmp/no-manifest-probe-dir"]
	if !ok || entry.pkg != nil {
		t.Fatalf("nil manifest lookup not cached: %#v", c.pkgs)
	}

	// manifest 失效後重新查一次，且兩層快取都清空。
	c.invalidate()
	if len(c.pkgs) != 0 || len(c.loaders) != 0 {
		t.Fatal("invalidate must clear both caches")
	}
	c.configFor(uri, "x str = ''\n")
	if calls != 3 {
		t.Fatalf("invalidate should force a fresh lookup, LoadPackage calls = %d", calls)
	}
}

// TestLSPSigContextDegradesToStdTables 釘住「舊測試直接構造 DocumentManager
// （sig == nil）」與 loader 停用兩條退化路徑：仍至少有 std 表，視野不變窄。
func TestLSPSigContextDegradesToStdTables(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	var nilCtx *lspSigContext
	cfg := nilCtx.configFor("file:///tmp/probe/x.no", "f = () {\n    print('x')\n}\n")
	if len(cfg.FuncSigs) == 0 || cfg.EnumVariants == nil {
		t.Fatalf("nil context must still provide std tables, got %#v", cfg)
	}
	if cfg.Filename != "x.no" {
		t.Fatalf("cfg.Filename = %q, want %q", cfg.Filename, "x.no")
	}
}
