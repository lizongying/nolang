// Package mmast 把 Nolang 的 AST 打包成 zip 分發件（<tool>.ast.zip）。
//
// 產物：<tool>.ast.zip —— 這是 `no add <tool>[:<version>]@<host>/<owner>/<repo>`
// 會去 GitHub Releases 抓的檔案：
//
//	https://<host>/<owner>/<repo>/releases/download/<version>/<tool>.ast.zip
//
// zip 佈局：
//
//	ast.gob        唯一本體：原始 AST（直接就是 parser.CachedProgram，
//	               與磁盤 AST 快取同型別同編碼面）
//	package.jsonc  包的原始清單（可選：源目錄有 package.jsonc 時原樣附上一份，
//	               讓消費端 no add 不解包仓库就能拿到 name/version/dependencies）
//
// 版本資訊不入流（先是獨立 manifest.gob 條目、後是流内 ZipVer/AstVer 前置欄，
// 兩皆摈除）：std 的 .ast.zip 随编译器下发（同 build 產物，寫讀必同代）；
// 第三方包的識別資訊在 package.jsonc。唯一陳舊風險是本機殘留快取——由
// ASTZipCachePath 把 ASTVersion+ZipVer 編進檔名做路徑鍵控（與磁盤 AST 快取同法）：
// 版本 bump 即路徑變，舊檔自然不被讀，std 走 miss→自動重建，硬錯都不需要。
//
// 自 ZipVer 2026-10-16.5 起載體從 MetaMessage 節點表換成 encoding/gob 直存原始
// AST：介面欄位靠 gob 型別註冊表（parser/gob_register.go）原生落地，不需要中間
// 結構；同體積比 mm 路線小 ~7 倍、快 ~80 倍（基準：tests/tls.no）。gob 丢未導出
// 欄位、拍平共享指標的兩個坑，走增量構建 AST 快取現成的 CachedProgram /
// SerializeNodeSem / RestoreNodeSem occurrence 索引方案規避——與磁盤快取同一條
// 已被測試錐牢的路徑。
package mmast

import (
	"archive/zip"
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pkgspec "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// ZipVer 是 zip 容器本身的版本（佈局/規則變化時 bump）。
// 2026-10-16.5：AST 載體换成 gob 直存原始 AST，取代 MetaMessage 扁平節點表。
// 2026-10-16.6：新增可選 package.jsonc 條目（原樣隨包分發）。
// 2026-10-16.7：取消 manifest.gob（版本閘壓進 ast.gob 流内前置欄）。
// 2026-10-16.8：版本資訊不入流——std 随编译器下发、第三方識別靠 package.jsonc；
//               本機陳舊快取由 ASTZipCachePath 檔名鍵控 ASTVersion+ZipVer 自動失效。
const ZipVer = "2026-10-16.8"

// zip 內的條目名。
const (
	treeName    = "ast.gob"
	packageName = "package.jsonc"
)

// ── 寫出 ────────────────────────────────────────────────────────────────────

// ZipStats 是打包結果的統計（CI 日誌 / `no pack` 輸出用）。
type ZipStats struct {
	Nodes int
	Bytes int64
}

// WriteASTZip 把 *Program 打包成 <tool>.ast.zip 的內容並寫進 w；
// pkgJSONC 非空時以 package.jsonc 條目原樣附上（不改寫、不正規化）。
func WriteASTZip(w io.Writer, prog *parser.Program, pkgJSONC []byte) (*ZipStats, error) {
	if prog == nil {
		return nil, fmt.Errorf("mmast: nil program")
	}
	nodes := parser.CountProgramNodes(prog)
	if nodes == 0 {
		return nil, fmt.Errorf("mmast: program has no nodes")
	}

	cw := &countingWriter{w: w}
	zw := zip.NewWriter(cw)

	tw, err := zw.Create(treeName)
	if err != nil {
		return nil, err
	}
	cp := parser.CachedProgram{
		Prog:    prog,
		NodeSem: parser.SerializeNodeSem(prog),
	}
	if err := gob.NewEncoder(tw).Encode(&cp); err != nil {
		return nil, fmt.Errorf("mmast: encode ast: %w", err)
	}

	if len(pkgJSONC) > 0 {
		pw, err := zw.Create(packageName)
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write(pkgJSONC); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &ZipStats{Nodes: nodes, Bytes: cw.n}, nil
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

// ASTZip 描述讀入的 zip 的附帶資訊。
type ASTZip struct {
	PackageJSONC []byte // zip 內附的 package.jsonc（無則 nil）
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

// FetchASTZip 抓 <tool>.ast.zip 並解出 AST：
//
//	https://<host>/<owner>/<repo>/releases/download/<version>/<tool>.ast.zip
//
// 下載後落在 ~/no/cache/ast/... 作為快取；快取命中就不再打網路。
// 快取檔名已含 ASTVersion+ZipVer：舊版产物天然 miss，自動重取/重建。
func FetchASTZip(spec pkgspec.ToolSpec, version string) (*parser.Program, *ASTZip, error) {
	if version == "" {
		return nil, nil, fmt.Errorf("mmast: version required to fetch %s", spec.DepKey())
	}
	cache := ASTZipCachePath(spec, version)
	if b, err := os.ReadFile(cache); err == nil && len(b) > 0 {
		return ReadASTZip(b)
	}
	url := pkgspec.ReleaseASTZipURL(spec, version)
	data, err := pkgspec.DownloadReleaseAsset(url)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0755); err != nil {
		return nil, nil, err
	}
	// 快取寫失敗不致命：下次重新下載即可
	_ = os.WriteFile(cache, data, 0644)
	return ReadASTZip(data)
}

// ASTZipCachePath 返回 .ast.zip 的快取路徑。檔名攜帶 ASTVersion+ZipVer：版本 bump
// 即路徑變、舊檔自動失效（std 走 miss→BuildModuleAST 重建）——取代流内版本欄位
// 做閘，與磁盤 AST 快取（astdiskcache 以 ASTVersion 鍵控目錄）同一套做法。
func ASTZipCachePath(spec pkgspec.ToolSpec, version string) string {
	return filepath.Join(astCacheRoot(),
		cachePathPart(spec.Host), cachePathPart(spec.Owner), cachePathPart(spec.Repo),
		fmt.Sprintf("%s@%s.ast-%s-%s.zip", spec.Name, cachePathPart(version), parser.ASTVersion, ZipVer))
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

// ReadASTZip 解析 <tool>.ast.zip 的內容，還原成 *Program。
// 版本閘不在流内——由 ASTZipCachePath 的檔名鍵控（std 随编译器下发、第三方識別
// 在 package.jsonc，跨代讀取場景不存在）。gob 型別名不匹配（舊 astDoc/mm 格式）
// 會直接報錯，不会静默錯解。
func ReadASTZip(data []byte) (*parser.Program, *ASTZip, error) {
	byName, err := readZipEntries(data)
	if err != nil {
		return nil, nil, fmt.Errorf("mmast: open zip: %w", err)
	}
	tb, ok := byName[treeName]
	if !ok {
		return nil, nil, fmt.Errorf("mmast: %s missing from zip", treeName)
	}
	var cp parser.CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(tb)).Decode(&cp); err != nil {
		return nil, nil, fmt.Errorf("mmast: decode ast: %w", err)
	}
	if cp.Prog == nil {
		return nil, nil, fmt.Errorf("mmast: ast.gob carries no program")
	}
	// 語義副表按 SerializeNodeSem 的 occurrence 順序重建（與磁盤 AST 快取同機制）。
	parser.RestoreNodeSem(cp.Prog, cp.NodeSem)
	return cp.Prog, &ASTZip{PackageJSONC: byName[packageName]}, nil
}
