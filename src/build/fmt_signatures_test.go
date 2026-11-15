package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// fmt_signatures_test.go 需要內嵌 std 簽名表；bare `go test`（StdSigKey 未
// 由 make 注入）時跳過，與 parser/dump、checker 既有測試同一口徑。

func skipWithoutStdSigKey(t *testing.T) {
	t.Helper()
	if checker.StdSigKey == "" {
		t.Skip("StdSigKey not injected (bare go test); run via make test")
	}
}

func TestFmtSigLoaderStdTables(t *testing.T) {
	skipWithoutStdSigKey(t)
	stdSigs, stdFields := checker.CollectStdModuleSignatures()
	stdMethods := checker.CollectStdMethodSigs()

	l := NewFmtSigLoader(nil)
	cfg := l.ConfigFor("f = () {\n    print('x')\n}\n", "")
	if len(cfg.FuncSigs) != len(stdSigs) {
		t.Fatalf("std func sigs: got %d, want %d", len(cfg.FuncSigs), len(stdSigs))
	}
	if len(cfg.MethodSigs) != len(stdMethods) {
		t.Fatalf("std method sigs: got %d, want %d", len(cfg.MethodSigs), len(stdMethods))
	}
	if len(cfg.StructFields) != len(stdFields) {
		t.Fatalf("std struct fields: got %d, want %d", len(cfg.StructFields), len(stdFields))
	}
	if cfg.Filename != "" {
		t.Fatalf("empty filePath must not populate cfg.Filename, got %q", cfg.Filename)
	}
	if l.disabled {
		t.Fatal("loader unexpectedly disabled with a baked StdSigKey")
	}
}

func TestFmtSigLoaderCachesTablesByUseSet(t *testing.T) {
	skipWithoutStdSigKey(t)
	l := NewFmtSigLoader(nil)

	// std/ use 一律由內嵌表全量提供、不進 paths，所以兩份 std-only source
	// 的 use 集合都是空的，共用同一組表。
	a := l.ConfigFor("# std/fs\nf = () {\n    print('x')\n}\n", "")
	b := l.ConfigFor("# std/net\ng = () {\n    print('y')\n}\n", "")
	if len(l.tablesCache) != 1 {
		t.Fatalf("std-only use sets should share one cached table set, got %d", len(l.tablesCache))
	}
	if len(a.FuncSigs) != len(b.FuncSigs) {
		t.Fatalf("std tables differ across std-only sources: %d vs %d", len(a.FuncSigs), len(b.FuncSigs))
	}
	// 不同的**本地**模組 use 集合 → 第二組表（解析失敗也快取，不重複 I/O）。
	l.ConfigFor("use /some/local/mod\nh = () {\n    print('z')\n}\n", "")
	if len(l.tablesCache) != 2 {
		t.Fatalf("local use set should add a table set, got %d", len(l.tablesCache))
	}
}

func TestFmtSigLoaderBadUsePathIsCached(t *testing.T) {
	skipWithoutStdSigKey(t)
	l := NewFmtSigLoader(nil)
	src := "use /definitely/not/here\nf = () {\n    print('x')\n}\n"

	cfg := l.ConfigFor(src, "")
	if cfg.FuncSigs == nil {
		t.Fatal("bad use path must not wipe the std signature tables")
	}
	l.ConfigFor(src, "")
	if len(l.moduleCache) == 0 {
		t.Fatal("failed use path should be cached to avoid repeated disk I/O")
	}
}

func TestFmtSigLoaderDisabledWithoutKey(t *testing.T) {
	// 直接構造 disabled loader，模擬非 make 建構的二進位：必須優雅退化成
	// 零配置，而不是觸發 CollectStdModuleSignatures 的 os.Exit(1)。
	l := &FmtSigLoader{disabled: true}
	cfg := l.ConfigFor("f = () { print('x') }\n", "")
	if cfg.FuncSigs != nil || cfg.MethodSigs != nil || cfg.StructFields != nil {
		t.Fatalf("disabled loader must return a zero config, got %#v", cfg)
	}
}

// TestFmtSigLoaderLoadsLocalModule 驗證 S2 的「真正載入本地模組」：不是只看
// 內嵌 std 表，而是能循 use / # 路徑 resolveUse 到磁碟模組、收集其宣告的
// 回傳型別並放進 cfg。
func TestFmtSigLoaderLoadsLocalModule(t *testing.T) {
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
	write("workspace.noml", "{\n  lib: '/lib'\n}\n")
	write("package.noml", "{\n  name: 'probe'\n}\n")
	write("lib/helper.no", "is-ok = () (b bool) {\n    b = true\n}\n")

	p, err := LoadPackage(dir)
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if p == nil {
		t.Fatal("LoadPackage returned nil")
	}
	l := NewFmtSigLoader(p)
	cfg := l.ConfigFor("use /lib/helper\nf = () {\n    ok = helper.is-ok()\n}\n", "")
	got := cfg.FuncSigs["helper.is-ok"]
	if len(got) == 0 || got[0] != "bool" {
		t.Fatalf("local module signature missing from cfg: %#v (moduleCache keys=%d)",
			got, len(l.moduleCache))
	}
}
