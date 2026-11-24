// Package mmast 把 Nolang 的 AST 打包成 zip 分發件（<tool>-ast.zip）。
//
// 產物：<tool>-ast.zip —— 這是 `no add <tool>[:<version>]@<host>/<owner>/<repo>`
// 會去 GitHub Releases 抓的檔案：
//
//	https://<host>/<owner>/<repo>/releases/download/<version>/<tool>-ast.zip
//
// zip 佈局（一個 zip = 一個**包**，包內可含多**模組**）：
//
//	<module>/<path>.gob   每個模組一個條目，條目名就是模組路徑 + ".gob"
//	                      （如 main.gob、remote.gob、deep/util.gob）。
//	                      條目內容是 gob(parser.CachedProgram)；模組路徑同時
//	                      以隱式註解 Program.ModulePath 寫在載荷裡，讀端交叉校驗。
//	package.noml          包的原始清單（可選：源目錄有 package.noml 時原樣附上，
//	                      讓消費端 no add 不解包仓库就能拿到 name/version/dependencies）
//
// 沒有「入口模組」這個概念：不存在 `use <pkg>/main` 這類導入。# 的語義是
//
//	# <pkg>/<module>      直接引入包內指定模組
//	# <pkg>.<fn>          <pkg> 只是包名，在包內按 <fn> 定位定義它的模組
//	# <pkg>               包別名：模組以 <pkg>/<module> 或 <pkg>.<module> 取用，
//	                      不導入 main，也不是一次性全部裸名導入
//
// 版本資訊不入流：唯一陳舊風險是本機殘留快取——由 ASTZipCachePath 把
// ASTVersion+ZipVer 編進檔名做路徑鍵控（與磁盤 AST 快取同法）：版本 bump 即
// 路徑變，舊檔自然不被讀。
//
// 自 ZipVer 2026-10-16.5 起載體從 MetaMessage 節點表換成 encoding/gob 直存原始
// AST：介面欄位靠 gob 型別註冊表（parser/gob_register.go）原生落地，不需要中間
// 結構；同體積比 mm 路線小 ~7 倍、快 ~80 倍（基準：tests/tls.no）。gob 丢未導出
// 欄位、拍平共享指標的兩個坑，走增量構建 AST 快取現成的 CachedProgram /
// occurrence 索引方案規避（三張 node-keyed 副表各自拍平成 map[uint32]…）——
// 與磁盤快取同一條已被測試錐牢的路徑。
package mmast

import (
	"archive/zip"
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pkgspec "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// ZipVer 是 zip 容器本身的版本（佈局/規則變化時 bump）。
// 2026-10-16.5：AST 載體换成 gob 直存原始 AST，取代 MetaMessage 扁平節點表。
// 2026-10-16.6：新增可選 package.jsonc 條目（原樣隨包分發）。
// 2026-10-16.7：取消 manifest.gob（版本閘壓進 ast.gob 流内前置欄）。
// 2026-10-16.8：版本資訊不入流；本機陳舊快取由 ASTZipCachePath 檔名鍵控失效。
// 2026-10-16.9：配置格式硬切 .jsonc → .noml，zip 條目名改為 package.noml。
// 2026-11-24.1：新增 mods.gob（一包多模組）；ast.gob 保留為入口模組以兼容舊 reader。
// 2026-11-24.2：mods.gob 從 map[path]*CachedProgram 改為 [][]byte，路徑隨 AST 走
//
//	（Program.SourcePath / ModulePath）；舊 map 格式仍可讀。
//
// 2026-11-24.3：移除 ast.gob 與 mods.gob；每個模組一個 <modulePath>.gob 條目。
// 不再有入口模組 / "" key。
const ZipVer = "2026-11-24.3"

// zip 內的固定條目名；其餘條目都是 <modulePath> + moduleSuffix。
const (
	packageName  = "package.noml"
	moduleSuffix = ".gob"
)

// ── 寫出 ────────────────────────────────────────────────────────────────────

// ZipStats 是打包結果的統計（CI 日誌 / `no pack` 輸出用）。
type ZipStats struct {
	Nodes   int
	Modules int
	Bytes   int64
}

// WriteASTZip 把單一 *Program 打包成 <tool>-ast.zip 的內容並寫進 w。
// 模組路徑取 Program.ModulePath；空值時補 "main"（單檔打包的常見情形）。
func WriteASTZip(w io.Writer, prog *parser.Program, pkgNoml []byte) (*ZipStats, error) {
	if prog == nil {
		return nil, fmt.Errorf("mmast: nil program")
	}
	path := prog.ModulePath
	if path == "" {
		path = "main"
	}
	return WriteModuleASTZip(w, map[string]*parser.Program{path: prog}, pkgNoml)
}

// WriteModuleASTZip 把一個包的多個模組打包：key 為模組路徑（如 "main"、
// "remote"、"deep/util"）。每個模組寫成一個 <key>.gob 條目。
// pkgNoml 非空時以 package.noml 條目原樣附上（不改寫、不正規化）。
func WriteModuleASTZip(w io.Writer, mods map[string]*parser.Program, pkgNoml []byte) (*ZipStats, error) {
	if len(mods) == 0 {
		return nil, fmt.Errorf("mmast: empty module set")
	}
	nodes := 0
	paths := make([]string, 0, len(mods))
	for path, prog := range mods {
		if !validModulePath(path) {
			return nil, fmt.Errorf("mmast: invalid module path %q", path)
		}
		if prog == nil {
			return nil, fmt.Errorf("mmast: nil program for module %q", path)
		}
		nodes += parser.CountProgramNodes(prog)
		paths = append(paths, path)
	}
	if nodes == 0 {
		return nil, fmt.Errorf("mmast: no nodes in any module")
	}
	// map 走訪順序隨機，先排序才能讓 zip 位元組可重現。
	sort.Strings(paths)

	// 先把所有 gob 算好，避免寫到一半才發現某個模組編碼失敗。
	encoded := make(map[string][]byte, len(paths))
	for _, path := range paths {
		cp := parser.CachedProgram{Prog: stampModulePaths(mods[path], path)}
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(&cp); err != nil {
			return nil, fmt.Errorf("mmast: encode module %q: %w", path, err)
		}
		encoded[path] = buf.Bytes()
	}

	cw := &countingWriter{w: w}
	zw := zip.NewWriter(cw)
	// zip 條目也照路徑排序寫出（讀端不依賴順序，但位元組需可重現）。
	for _, path := range paths {
		tw, err := zw.Create(path + moduleSuffix)
		if err != nil {
			return nil, err
		}
		if _, err := tw.Write(encoded[path]); err != nil {
			return nil, fmt.Errorf("mmast: write module %q: %w", path, err)
		}
	}
	if len(pkgNoml) > 0 {
		pw, err := zw.Create(packageName)
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write(pkgNoml); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &ZipStats{Nodes: nodes, Modules: len(mods), Bytes: cw.n}, nil
}

// validModulePath 擋掉不能安全當 zip 條目名的模組路徑：空、絕對、點段、
// 反斜線、以及帶 ".gob" 後綴（會與條目命名規則打架）。
func validModulePath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") ||
		strings.HasSuffix(path, moduleSuffix) {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// stampModulePaths 把模組路徑與實際檔名當隱式註解蓋到 Program 的浅拷貝上。
// 呼叫端（no pack）已寫好 SourcePath 時尊重它；單檔打包 / 測試直接構造時
// 從模組路徑補一個，讓容器裡每個條目都自帶身份。
func stampModulePaths(prog *parser.Program, modulePath string) *parser.Program {
	stamped := *prog
	stamped.ModulePath = modulePath
	if stamped.SourcePath == "" {
		stamped.SourcePath = modulePath + ".no"
	}
	return &stamped
}

// countingWriter 統計實際寫出的位元組數（打包統計用）。
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// ── 讀入 ────────────────────────────────────────────────────────────────────

// ASTZip 描述讀入的 zip 的內容。
type ASTZip struct {
	PackageNoml []byte                     // zip 內附的 package.noml（無則 nil）
	Modules     map[string]*parser.Program // 全部模組，key = 模組路徑
}

// ModulePaths 回傳排序後的模組路徑（錯誤訊息 / 診斷用）。
func (z *ASTZip) ModulePaths() []string {
	if z == nil {
		return nil
	}
	out := make([]string, 0, len(z.Modules))
	for k := range z.Modules {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// FindModuleByFunction 在包內找定義了頂層函式/常量 name 的模組：
// 對應 `# <pkg>.<name>` 的語義。<pkg> 只是包名，不指向特定模組。
// 多个模組都定義同名符號時回傳錯誤，避免靜默選一個。
func (z *ASTZip) FindModuleByFunction(name string) (*parser.Program, error) {
	if z == nil || name == "" {
		return nil, fmt.Errorf("mmast: empty function name")
	}
	var hits []string
	for _, path := range z.ModulePaths() {
		if moduleDefines(z.Modules[path], name) {
			hits = append(hits, path)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("no module defines %q (available modules: %s)", name, strings.Join(z.ModulePaths(), ", "))
	case 1:
		return z.Modules[hits[0]], nil
	default:
		return nil, fmt.Errorf("function %q is defined in multiple modules: %s (use # <pkg>/<module>.%s)", name, strings.Join(hits, ", "), name)
	}
}

// moduleDefines 檢查模組是否定義了頂層函式或常量 name。
// Nolang 的函式有兩種 AST 形態：`name = (…) {…}` 是 LetStatement + FunctionLiteral，
// `name: (…) {…}` 是 FunctionDefinition；常量（LetStatement）也可被 `# pkg.name` 引入。
func moduleDefines(prog *parser.Program, name string) bool {
	if prog == nil {
		return false
	}
	for _, s := range prog.Statements {
		switch v := s.(type) {
		case *parser.FunctionDefinition:
			if v.Name == name {
				return true
			}
		case *parser.LetStatement:
			if v.Name != nil && v.Name.Value == name {
				return true
			}
		}
	}
	return false
}

// readZipEntries 讀出 zip 的所有條目（測試用：逐條校驗佈局）。
func readZipEntries(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		out[f.Name] = b
	}
	return out, nil
}

// FetchModuleSet 抓 <tool>-ast.zip 並解出**全部模組**（含快取）。
//
//	https://<host>/<owner>/<repo>/releases/download/<version>/<tool>-ast.zip
//
// 下載後落在 ~/no/cache/ast/... 作為快取；快取命中就不再打網路。
// 快取檔名已含 ASTVersion+ZipVer：舊版产物天然 miss，自動重取/重建。
func FetchModuleSet(spec pkgspec.ToolSpec, version string) (*ASTZip, error) {
	if version == "" {
		return nil, fmt.Errorf("mmast: version required to fetch %s", spec.DepKey())
	}
	cache := ASTZipCachePath(spec, version)
	if b, err := os.ReadFile(cache); err == nil && len(b) > 0 {
		return ReadModuleASTZip(b)
	}
	url := pkgspec.ReleaseASTZipURL(spec, version)
	data, err := pkgspec.DownloadReleaseAsset(url)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0755); err != nil {
		return nil, err
	}
	// 快取寫失敗不致命：下次重新下載即可
	_ = os.WriteFile(cache, data, 0644)
	return ReadModuleASTZip(data)
}

// ASTZipCachePath 返回 -ast.zip 的快取路徑。檔名攜帶 ASTVersion+ZipVer：版本 bump
// 即路徑變、舊檔自動失效（std 走 miss→BuildModuleAST 重建）——取代流内版本欄位
// 做閘，與磁盤 AST 快取（astdiskcache 以 ASTVersion 鍵控目錄）同一套做法。
func ASTZipCachePath(spec pkgspec.ToolSpec, version string) string {
	return filepath.Join(astCacheRoot(),
		cachePathPart(spec.Host), cachePathPart(spec.Owner), cachePathPart(spec.Repo),
		fmt.Sprintf("%s@%s-ast-%s-%s.zip", spec.Name, cachePathPart(version), parser.ASTVersion, ZipVer))
}

// astCacheRoot 返回快取根目錄：預設 ~/no/cache/ast，$NOLANG_AST_CACHE_DIR 可覆寫
// （CI 想把快取放進工作區、或單測想隔離時用）。
func astCacheRoot() string {
	if d := os.Getenv("NOLANG_AST_CACHE_DIR"); d != "" {
		return d
	}
	return filepath.Join(pkgspec.NoHomeDir(), "cache", "ast")
}

// cachePathPart 把 host / owner / version 清成安全的單層目錄名。
// host 可能是 "127.0.0.1:8080"（本機 registry），":" 在 Windows 上是非法檔名字元。
func cachePathPart(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, ":", "_"), "/", "_")
}

// ReadASTZip 解析 <tool>-ast.zip（空配置重建語義視圖）。保留舊簽名給測試用。
func ReadASTZip(data []byte) (*ASTZip, error) {
	return ReadASTZipWith(data, parser.SemRebuildConfig{})
}

// ReadASTZipWith 與 ReadASTZip 相同，但由呼叫方提供解析配置。
func ReadASTZipWith(data []byte, cfg parser.SemRebuildConfig) (*ASTZip, error) {
	return ReadModuleASTZipWith(data, cfg)
}

// ReadModuleASTZip 解析 <tool>-ast.zip 的**全部模組**（空配置重建語義視圖）。
// 語義視圖用空配置的理由：打包端與消費者是不同建置，本函式不知道當初的解析
// 輸入；知道配置的呼叫方請走 ReadModuleASTZipWith。
func ReadModuleASTZip(data []byte) (*ASTZip, error) {
	return ReadModuleASTZipWith(data, parser.SemRebuildConfig{})
}

// ReadModuleASTZipWith 遍歷 zip 內所有 <modulePath>.gob 條目，還原模組索引。
// 條目名是模組路徑的權威來源；載荷的 ModulePath 註解須與之相符（空則補上）。
// 沒有任何 .gob 條目 → 報錯；不再支援舊的 ast.gob / mods.gob 容器。
func ReadModuleASTZipWith(data []byte, cfg parser.SemRebuildConfig) (*ASTZip, error) {
	byName, err := readZipEntries(data)
	if err != nil {
		return nil, fmt.Errorf("mmast: open zip: %w", err)
	}
	out := &ASTZip{PackageNoml: byName[packageName], Modules: map[string]*parser.Program{}}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == packageName || !strings.HasSuffix(name, moduleSuffix) {
			continue
		}
		path := strings.TrimSuffix(name, moduleSuffix)
		if !validModulePath(path) {
			return nil, fmt.Errorf("mmast: bad module entry %q", name)
		}
		var cp parser.CachedProgram
		if err := gob.NewDecoder(bytes.NewReader(byName[name])).Decode(&cp); err != nil {
			return nil, fmt.Errorf("mmast: decode module %q: %w", path, err)
		}
		if cp.Prog == nil {
			return nil, fmt.Errorf("mmast: module %q carries no program", path)
		}
		// 舊容器的 ast.gob 是無 ModulePath 註解的入口條目。名字恰好與
		// 合法模組 "ast" 相同，故靠載荷缺註解來辨識並明確拒絕。
		if (name == "ast.gob" || name == "mods.gob") && cp.Prog.ModulePath == "" {
			return nil, fmt.Errorf("mmast: legacy container entry %q is no longer supported", name)
		}
		if cp.Prog.ModulePath != "" && cp.Prog.ModulePath != path {
			return nil, fmt.Errorf("mmast: module %q annotation disagrees with entry (%q)", path, cp.Prog.ModulePath)
		}
		cp.Prog.ModulePath = path
		// 分發物只有語法樹（與磁 disk AST 快取同一個 CachedProgram），衍生事實一律重建。
		parser.BuildSemViews(cp.Prog, cfg)
		out.Modules[path] = cp.Prog
	}
	if len(out.Modules) == 0 {
		return nil, fmt.Errorf("mmast: zip carries no %s module entries", moduleSuffix)
	}
	return out, nil
}
