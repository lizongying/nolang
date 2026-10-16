// Package mmast 把 Nolang 的 AST 打包成 MetaMessage（mm）並封進 zip。
//
// 產物：<tool>.ast.zip —— 這是 `no add <tool>[:<version>]@<host>/<owner>/<repo>`
// 會去 GitHub Releases 抓的檔案：
//
//	https://<host>/<owner>/<repo>/releases/download/<version>/<tool>.ast.zip
//
// zip 佈局：
//
//	manifest.mm  版本 / 模組名 / 節點數 / 原始碼 sha256
//	tree.mm      整棵完整遞歸 AST 樹（parser.ASTTree，單一 mm 文檔）
//
// 自 mm v0.2.13 起容器上限放開到 4GB（字串仍 64KB，超長字串的分段在 parser 側
// 用 splitLongCells 處理），一棵模組 AST 樹可原樣塞進單一 mm 文檔——因此這裡不再
// 有「按位元組把根森林切分片」那一套：只寫一個 tree.mm，讀也讀一個 tree.mm。
package mmast

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pkgspec "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
	mm "github.com/metamessage/metamessage"
)

// ZipVer 是 zip 容器本身的版本（佈局/清單規則變化時 bump）。
// 2026-10-16.2：節點樹改成「扁平節點表（Root 索引 + Table）」——移除 C/Kids 拆分與
// MMSlice/MMMap/MMVal/MMBytes/MMEntry/ASTSemEntry 合成節點，並把遞歸巢狀壓平成
// 常數嵌套深度（消除 mm 的 O(size×深度) 複製與空容器範例爆炸）。tree.mm 載體形狀隨之變。
const ZipVer = "2026-10-16.4"

// 單文檔在 zip 內的條目名。
const (
	manifestName = "manifest.mm"
	treeName     = "tree.mm"
)

// ── mm 側的具體結構（帶 allow_empty：mm 不接受任何零值/空容器）─────────────

type mmManifest struct {
	ZipVer string // zip 容器版本
	Wire   string // 節點樹格式版本（parser.MMWireVersion）
	AstVer string // parser.ASTVersion
	Tool   string `mm:"allow_empty"` // 模組/工具名
	Src    string `mm:"allow_empty"` // 來源檔名
	SrcSHA string `mm:"allow_empty"` // 來源 sha256
	Nodes  int32  // 節點總數
}

// ── 寫出 ────────────────────────────────────────────────────────────────────

// ZipStats 是打包結果的統計（CI 日誌 / `no pack` 輸出用）。
// Chunks 恆為 1（單文檔佈局），保留欄位以兼容既有日誌格式。
type ZipStats struct {
	Nodes  int
	Chunks int
	Bytes  int64
}

// WriteASTZip 把 *Program 打包成 <tool>.ast.zip 的內容並寫進 w。
func WriteASTZip(w io.Writer, prog *parser.Program, tool, src, srcSHA string) error {
	_, err := WriteASTZipWithStats(w, prog, tool, src, srcSHA)
	return err
}

// WriteASTZipWithStats 同 WriteASTZip，另回傳節點數/分片數/位元組數。
func WriteASTZipWithStats(w io.Writer, prog *parser.Program, tool, src, srcSHA string) (*ZipStats, error) {
	if prog == nil {
		return nil, fmt.Errorf("mmast: nil program")
	}
	tree, err := parser.EncodeASTTree(prog, srcSHA)
	if err != nil {
		return nil, fmt.Errorf("mmast: encode ast tree: %w", err)
	}
	if tree.Root < 0 || int(tree.Root) >= len(tree.Table) {
		return nil, fmt.Errorf("mmast: ast tree has no valid root (root=%d table=%d)", tree.Root, len(tree.Table))
	}

	cw := &countingWriter{w: w}
	zw := zip.NewWriter(cw)

	man := mmManifest{
		ZipVer: ZipVer,
		Wire:   tree.Ver,
		AstVer: tree.AstVer,
		Tool:   tool,
		Src:    src,
		SrcSHA: srcSHA,
		Nodes:  tree.Nodes,
	}
	mw, err := zw.Create(manifestName)
	if err != nil {
		return nil, err
	}
	mbs, err := mm.EncodeFromValue(man, "")
	if err != nil {
		return nil, fmt.Errorf("mmast: encode manifest: %w", err)
	}
	if _, err := mw.Write(mbs); err != nil {
		return nil, err
	}

	tw, err := zw.Create(treeName)
	if err != nil {
		return nil, err
	}
	tbs, err := mm.EncodeFromValue(*tree, "")
	if err != nil {
		return nil, fmt.Errorf("mmast: encode ast tree: %w", err)
	}
	if _, err := tw.Write(tbs); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &ZipStats{Nodes: int(tree.Nodes), Chunks: 1, Bytes: cw.n}, nil
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

// ASTZip 描述讀入的 zip（供呼叫方做進一步校驗）。
type ASTZip struct {
	Manifest mmManifest
	Tool     string
	Src      string
	SrcSHA   string
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
// 版本不符（wire/ast/zip 任一）由 ReadASTZip 硬錯攔下。
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

// ASTZipCachePath 返回 .ast.zip 的快取路徑。
func ASTZipCachePath(spec pkgspec.ToolSpec, version string) string {
	return filepath.Join(astCacheRoot(),
		cachePathPart(spec.Host), cachePathPart(spec.Owner), cachePathPart(spec.Repo),
		cachePathPart(version), spec.Name+".ast.zip")
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
// 版本不符即硬錯（與 incremental 的 hard-error 口徑一致）。
func ReadASTZip(data []byte) (*parser.Program, *ASTZip, error) {
	byName, err := readZipEntries(data)
	if err != nil {
		return nil, nil, fmt.Errorf("mmast: open zip: %w", err)
	}
	mb, ok := byName[manifestName]
	if !ok {
		return nil, nil, fmt.Errorf("mmast: %s missing from zip", manifestName)
	}
	var man mmManifest
	if err := mm.DecodeToValue(mb, &man); err != nil {
		return nil, nil, fmt.Errorf("mmast: decode manifest: %w", err)
	}
	if man.ZipVer != ZipVer {
		return nil, nil, fmt.Errorf("mmast: zip version mismatch: blob %q, this compiler %q", man.ZipVer, ZipVer)
	}

	tb, ok := byName[treeName]
	if !ok {
		return nil, nil, fmt.Errorf("mmast: %s missing from zip", treeName)
	}
	var tree parser.ASTTree
	if err := mm.DecodeToValue(tb, &tree); err != nil {
		return nil, nil, fmt.Errorf("mmast: decode ast tree: %w", err)
	}
	// 樹本身自帶版本欄位，DecodeASTTree 會做 wire/ast 版本校驗並硬錯。
	prog, err := parser.DecodeASTTree(&tree)
	if err != nil {
		return nil, nil, err
	}
	return prog, &ASTZip{Manifest: man, Tool: man.Tool, Src: man.Src, SrcSHA: man.SrcSHA}, nil
}
