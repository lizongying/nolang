package main

// main_fmt_signatures.go — `no fmt` 的跨模組簽名上下文。
//
// 目錄批次會走訪上百個 .no 檔；若每檔都新建 FmtSigLoader，std 內嵌表要
// 重複合併、同一個 use 模組要重複 resolveUse。這裡按 package root 快取
// 一份 loader，loader 內部再按「排序後的 use 集合」快取三張表，於是一次
// `no fmt <dir>` 的模組載入成本與檔案數無關。
//
// 檔案路徑刻意不進 cfg.Filename：它只影響診斷文字，而 `no fmt` 的錯誤
// 輸出必須與舊的單檔解析逐位元組一致。

import (
	"os"
	"path/filepath"
	"sync"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
	nfmt "github.com/lizongying/nolang/fmt"
)

// packageLookup 連「找不到 manifest」的結果一起快取（見 loadPackage）。
type packageLookup struct {
	pkg *nbuild.Package
	err error
}

type fmtSigContext struct {
	mu      sync.Mutex
	loaders map[string]*nbuild.FmtSigLoader
	pkgs    map[string]packageLookup
}

func newFmtSigContext() *fmtSigContext {
	return &fmtSigContext{
		loaders: make(map[string]*nbuild.FmtSigLoader),
		pkgs:    make(map[string]packageLookup),
	}
}

// loadPackage 以雙檢鎖快取 LoadPackage：目錄批次會對上百個 .no 檔呼叫
// configFor，而 LoadPackage 每次都向上逐級 os.Stat + os.ReadFile +
// ParseNomlMap。與 lsp.lspSigContext.loadPackage 同一份協定（磁碟 IO 留在
// 鎖外、連 nil manifest 也快取，避免每檔重走一次失敗路徑）。
func (c *fmtSigContext) loadPackage(dir string) (*nbuild.Package, error) {
	c.mu.Lock()
	v, ok := c.pkgs[dir]
	c.mu.Unlock()
	if ok {
		return v.pkg, v.err
	}
	p, err := nbuild.LoadPackage(dir)
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.pkgs[dir]; ok {
		return v.pkg, v.err
	}
	c.pkgs[dir] = packageLookup{pkg: p, err: err}
	return p, err
}

// configFor 依檔案所屬 package 取得 loader，並為該份 source 產出解析配置。
// c 為 nil（舊函式 / 測試直接呼叫的路徑）時回傳零配置，行為與未注入相同。
func (c *fmtSigContext) configFor(path, source string) nfmt.ParseConfig {
	if c == nil {
		return nfmt.ParseConfig{}
	}
	dir := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}
	p, _ := c.loadPackage(dir)
	key := ""
	if p != nil {
		key = p.RootDir
	}
	c.mu.Lock()
	l, ok := c.loaders[key]
	if !ok {
		l = nbuild.NewFmtSigLoader(p)
		c.loaders[key] = l
	}
	c.mu.Unlock()
	return l.ConfigFor(source, "")
}

// externTablesFromConfig 把 parser 用的「結果型別列表」轉成 checker 單檔
// validator 吃的「單一回傳型別」表。cfg 為零值時回傳空表，validator 行為
// 與舊的單檔解析完全一致。
func externTablesFromConfig(cfg nfmt.ParseConfig) checker.ExternSigTables {
	// 轉換邏輯已移到 checker，讓 CLI 與 LSP 的主動改寫共用同一份表。
	return checker.ExternSigTablesFromConfig(cfg)
}
