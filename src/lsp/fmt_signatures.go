package lsp

// fmt_signatures.go — LSP 的跨模組簽名上下文。
//
// 過去 LSP 主解析（documents.go ParseDocument）只手動注入 std 內嵌表，
// 而 format-on-save 另起一次裸單檔 parse，兩邊既看不到本地模組簽名、
// 也沒有 EnumVariants，診斷與格式化還是兩套錯誤集合。這裡讓 LSP 與
// `no fmt` 共用 build.FmtSigLoader 產出同一份 parser.SemRebuildConfig，
// 並由 ParseDocument 一次發布給診斷與格式化兩處消費（見 parseSnapshot）。
//
// 兩個容易踩的點：
//   - cfg.Apply 會**無條件**覆蓋 p.Filename，所以 configFor 一定要帶檔名，
//     否則 LSP 診斷文字會從 "x.no:line ..." 變成無檔名前綴。
//   - FmtSigLoader 在 StdSigKey 為空（非 make 建構）時優雅退化成零配置；
//     此時仍要補回舊 LSP 的 std 三表，避免解析視野反而變窄。

import (
	"path/filepath"
	"strings"
	"sync"

	nbuild "github.com/lizongying/nolang/build"
	"github.com/lizongying/nolang/checker"
	nfmt "github.com/lizongying/nolang/fmt"
)

// lspSigContext 在一次 LSP session 中按 package root 快取 FmtSigLoader，
// 並按檔案目錄快取 LoadPackage 的結果。目錄批次 vet 與連續 format-on-save
// 因此只解析一次本地模組，連「向上逐級 stat 找 manifest」也只做一次。
type lspSigContext struct {
	mu      sync.Mutex
	loaders map[string]*nbuild.FmtSigLoader
	pkgs    map[string]packageLookup
}

// packageLookup 連「找不到 manifest（pkg == nil）」也要記成一筆，否則沒有
// package.noml 的目錄（tmp / 單檔 playground）每次擊鍵都會重跑整串 stat。
type packageLookup struct {
	pkg *nbuild.Package
	err error
}

func newLSPSigContext() *lspSigContext {
	return &lspSigContext{
		loaders: make(map[string]*nbuild.FmtSigLoader),
		pkgs:    make(map[string]packageLookup),
	}
}

// loadPackage 以雙檢鎖快取 LoadPackage。磁碟 IO 留在鎖外，符合 ParseDocument
// 「heavy work outside lock」的既有要求；兩個同時首次進來的請求可能各做一次
// IO，但只有一個結果會寫入，不會互相覆寫。
func (c *lspSigContext) loadPackage(dir string) (*nbuild.Package, error) {
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

// invalidate 在 workspace.noml / package.noml 變動時清掉兩層快取。
// loader 也必須清：它持有 package root 與已解析的本地模組簽名，只清 pkgs
// 會讓 manifest 改了但簽名表仍是舊的。session 內若客戶端沒註冊 file
// watcher，快取就不失效（改 manifest 本來就需要重開視窗）。
func (c *lspSigContext) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pkgs = make(map[string]packageLookup)
	c.loaders = make(map[string]*nbuild.FmtSigLoader)
}

// configFor 依文件所屬 package root 取得 loader，為 source 產出解析配置。
//
// uri 只需是 LSP 的 file:// 形式；非 file:// 或檔案系統不可用時仍會回傳
// 至少含 std 內嵌表的配置。c 為 nil（舊測試直接構造 DocumentManager）時
// 走同一條 std 兜底，行為與舊主解析一致。
func (c *lspSigContext) configFor(uri, source string) nfmt.ParseConfig {
	docPath := strings.TrimPrefix(uri, "file://")
	dir := filepath.Dir(docPath)
	name := filenameFromURI(uri)

	var cfg nfmt.ParseConfig
	var l *nbuild.FmtSigLoader
	if c != nil {
		p, _ := c.loadPackage(dir)
		key := dir
		if p != nil && p.RootDir != "" {
			key = p.RootDir
		}
		c.mu.Lock()
		var ok bool
		l, ok = c.loaders[key]
		if !ok {
			l = nbuild.NewFmtSigLoader(p)
			c.loaders[key] = l
		}
		c.mu.Unlock()
		cfg = l.ConfigFor(source, name)
	}

	// loader 停用（StdSigKey 為空）或沒有 context 時，退回舊 LSP 的三行
	// SetExternSignatures 口徑；EnumVariants 由 build.astParseConfig 的同名
	// 來源補上。零值 cfg 與「注入了空表」在 parser 的簽名指紋上是兩種
	// 狀態，故一律顯式填表而不是讓 Apply 自己判斷。
	if cfg.FuncSigs == nil && cfg.MethodSigs == nil && cfg.StructFields == nil {
		if checker.StdSigKey != "" {
			cfg.FuncSigs, cfg.StructFields = checker.CollectStdModuleSignatures()
			cfg.MethodSigs = checker.CollectStdMethodSigs()
			cfg.EnumVariants = checker.CollectStdEnumVariants()
		}
		cfg.Filename = name
	}
	return cfg
}
