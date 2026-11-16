package main

// main_fmt_signatures_test.go — 目錄批次 fmt 的 LoadPackage 快取回歸。
//
// nbuild.LoadPackage 是 compat.go 的 var，可在測試內暫時包一層計數；本套件
// 無 t.Parallel()，defer 還原即可（與 lsp/fmt_signatures_test.go 同口徑）。

import (
	"os"
	"path/filepath"
	"testing"

	nbuild "github.com/lizongying/nolang/build"
)

func TestFmtSigContextCachesPackageLookup(t *testing.T) {
	orig := nbuild.LoadPackage
	defer func() { nbuild.LoadPackage = orig }()
	calls := map[string]int{}
	nbuild.LoadPackage = func(dir string) (*nbuild.Package, error) {
		calls[dir]++
		return nil, nil // 連「找不到 manifest」的 nil 結果也必須快取
	}

	c := newFmtSigContext()
	for i := 0; i < 4; i++ {
		c.configFor("/tmp/fmt-sig-cache-a", "x str = ''\n")
	}
	if got := calls["/tmp/fmt-sig-cache-a"]; got != 1 {
		t.Fatalf("same dir must hit LoadPackage exactly once, got %d", got)
	}
	c.configFor("/tmp/fmt-sig-cache-b", "y = 1\n")
	if got := calls["/tmp/fmt-sig-cache-b"]; got != 1 {
		t.Fatalf("second dir must hit LoadPackage exactly once, got %d", got)
	}
	if got := calls["/tmp/fmt-sig-cache-a"]; got != 1 {
		t.Fatalf("first dir must stay cached, got %d", got)
	}
	if len(c.pkgs) != 2 {
		t.Fatalf("both dirs (including a nil manifest) must be cached, got %d", len(c.pkgs))
	}
}

// TestFixRedundantTypeInFileUsesSignatureContext 驗收 B6：`--fix=redundant`
// 的 Path 1 不再是單檔零配置視野，而是經 configFor 注入與預設 `no fmt`
// 同一份 cfg。LoadPackage 是可觀測的注入證據；nil sigCtx 必須維持舊行為。
func TestFixRedundantTypeInFileUsesSignatureContext(t *testing.T) {
	orig := nbuild.LoadPackage
	defer func() { nbuild.LoadPackage = orig }()
	calls := map[string]int{}
	nbuild.LoadPackage = func(dir string) (*nbuild.Package, error) {
		calls[dir]++
		return nil, nil
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "t-redundant.no")
	src := "main = () () {\n  x str = ''\n}\n"
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	// 對照組：零配置仍走 standalone Path 1，完全不碰 package loader。
	if _, ok := fixRedundantTypeInFile(p, nil); !ok {
		t.Fatal("expected a redundant-type fix")
	}
	if len(calls) != 0 {
		t.Fatalf("nil sigCtx must not load packages: %v", calls)
	}
	// 帶 sigCtx：Path 1 必須注入 cfg（一次 LoadPackage）。Path 2 的
	// VetFileWithLints 不該被觸發，否則計數會 > 1。
	if _, ok := fixRedundantTypeInFile(p, newFmtSigContext()); !ok {
		t.Fatal("expected a redundant-type fix with injected config")
	}
	if got := calls[dir]; got != 1 {
		t.Fatalf("Path 1 must inject cfg via configFor, got %d LoadPackage calls: %v", got, calls)
	}
}
