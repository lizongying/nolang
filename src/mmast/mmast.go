// Package mmast 把 Nolang 的 AST 打包成 MetaMessage（mm）並封進 zip。
//
// 產物：<tool>.ast.zip —— 這是 `no add <tool>[:<version>]@<host>/<owner>/<repo>`
// 會去 GitHub Releases 抓的檔案：
//
//	https://<host>/<owner>/<repo>/releases/download/<version>/<tool>.ast.zip
//
// zip 佈局：
//
//	manifest.mm    版本 / 模組名 / 分片數 / 節點數 / 根索引 / 原始碼 sha256
//	chunk-0000.mm  節點表分片（每片編碼後 ≤ maxWire 位元組）
//	chunk-0001.mm
//
// 為什麼要分片：mm v0.2.9 對「單一容器/字串編碼後」有 65535 位元組的硬限制
// （實測：`array too long, max length: 65535`）。實測 std 有 8/159 個模組光是
// gob+gzip 就超過 64KB（net/tls.no 181KB），所以分片不是優化而是必需。
package mmast

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mm "github.com/metamessage/metamessage"
	pkgspec "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// maxWire 是單一 mm 文檔的位元組上限（留出協議頭的餘量，低於 mm 的 65535）。
const maxWire = 60000

// ZipVer 是 zip 容器本身的版本（分塊/清單規則變化時 bump）。
const ZipVer = "2026-10-15.1"

// ── mm 側的具體結構（帶 allow_empty：mm 不接受任何零值/空容器）─────────────

type mmCell struct {
	N string `mm:"desc=欄位名"`
	T string `mm:"desc=值類型"`
	V string `mm:"desc=值"`
}

type mmNode struct {
	K string   `mm:"desc=節點型別名"`
	C []mmCell `mm:"allow_empty;desc=欄位"`
}

type mmChunk struct {
	I     int32    `mm:"allow_empty;desc=分片序號"`
	Nodes []mmNode `mm:"allow_empty;desc=節點"`
}

type mmManifest struct {
	ZipVer string `mm:"desc=zip 容器版本"`
	Wire   string `mm:"desc=節點表格式版本"`
	AstVer string `mm:"desc=parser.ASTVersion"`
	Tool   string `mm:"allow_empty;desc=模組/工具名"`
	Src    string `mm:"allow_empty;desc=來源檔名"`
	SrcSHA string `mm:"allow_empty;desc=來源 sha256"`
	Chunks int32  `mm:"desc=分片數"`
	Nodes  int32  `mm:"desc=節點總數"`
	Root   int32  `mm:"desc=根節點索引"`
}

// ── 寫出 ────────────────────────────────────────────────────────────────────

// ZipStats 是打包結果的統計（CI 日誌 / `no pack` 輸出用）。
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
	nt, err := parser.EncodeNodeTable(prog, srcSHA)
	if err != nil {
		return nil, fmt.Errorf("mmast: encode node table: %w", err)
	}
	chunks, err := chunkNodes(nt.Nodes)
	if err != nil {
		return nil, err
	}

	zw := zip.NewWriter(w)
	cw := &countingWriter{w: w}
	zw = zip.NewWriter(cw)
	mw, err := zw.Create("manifest.mm")
	if err != nil {
		return nil, err
	}
	man := mmManifest{
		ZipVer: ZipVer,
		Wire:   nt.Ver,
		AstVer: nt.AstVer,
		Tool:   tool,
		Src:    src,
		SrcSHA: srcSHA,
		Chunks: int32(len(chunks)),
		Nodes:  int32(len(nt.Nodes)),
		Root:   nt.Root,
	}
	mbs, err := mm.EncodeFromValue(man, "")
	if err != nil {
		return nil, fmt.Errorf("mmast: encode manifest: %w", err)
	}
	if _, err := mw.Write(mbs); err != nil {
		return nil, err
	}

	for i, c := range chunks {
		cw2, err := zw.Create(fmt.Sprintf("chunk-%04d.mm", i))
		if err != nil {
			return nil, err
		}
		wire, err := mm.EncodeFromValue(mmChunk{I: int32(i), Nodes: c}, "")
		if err != nil {
			return nil, fmt.Errorf("mmast: encode chunk %d: %w", i, err)
		}
		if len(wire) > maxWire {
			return nil, fmt.Errorf("mmast: chunk %d is %d bytes, over the mm 64KB container limit", i, len(wire))
		}
		if _, err := cw2.Write(wire); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &ZipStats{Nodes: len(nt.Nodes), Chunks: len(chunks), Bytes: cw.n}, nil
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

// chunkNodes 把節點表切成若干片，使每片編碼後 ≤ maxWire。
//
// 難點：mm 超標時是**編碼失敗**（`array too long`）而不是回傳一個太大的 buffer，
// 所以不能「先編再量」。這裡先用節點的負載位元組數估一個批量，真的失敗就對半回縮
// 重試，成功後用實測密度更新估計值（收斂很快）。
func chunkNodes(nodes []parser.NTNode) ([][]mmNode, error) {
	if len(nodes) == 0 {
		return [][]mmNode{{}}, nil
	}
	var out [][]mmNode
	batch := estimateBatch(nodes, 0, len(nodes))
	start := 0
	for start < len(nodes) {
		if batch < 1 {
			batch = 1
		}
		end := start + batch
		if end > len(nodes) {
			end = len(nodes)
		}
		wire, err := mm.EncodeFromValue(mmChunk{I: int32(len(out)), Nodes: toMMNodes(nodes[start:end])}, "")
		for (err != nil || len(wire) > maxWire) && end-start > 1 {
			batch = (end - start) / 2
			if batch < 1 {
				batch = 1
			}
			end = start + batch
			wire, err = mm.EncodeFromValue(mmChunk{I: int32(len(out)), Nodes: toMMNodes(nodes[start:end])}, "")
		}
		if err != nil {
			return nil, fmt.Errorf("mmast: encode chunk at node %d: %w", start, err)
		}
		if len(wire) > maxWire {
			return nil, fmt.Errorf("mmast: node %d alone encodes over the mm %d-byte container limit; its fields need splitting",
				start, maxWire)
		}
		out = append(out, toMMNodes(nodes[start:end]))
		start = end
		batch = estimateBatch(nodes, start, len(nodes))
	}
	return out, nil
}

// estimateBatch 依節點的字串負載估算「幾個節點會落在 maxWire 內」。
// 係數 1.35 是實測的協議開銷（鍵名 + 類型前綴 + 長度前綴），再留 15% 餘量。
func estimateBatch(nodes []parser.NTNode, start, end int) int {
	n := end - start
	if n <= 0 {
		return 64
	}
	if n > 32 {
		n = 32 // 只採樣前 32 個節點，避免估計本身太慢
	}
	payload := 0
	for i := start; i < start+n && i < len(nodes); i++ {
		nd := nodes[i]
		payload += len(nd.K)
		for _, c := range nd.C {
			payload += len(c.N) + len(c.T) + len(c.V)
		}
	}
	if payload == 0 {
		return 256
	}
	per := float64(payload) / float64(n) * 1.35
	b := int(float64(maxWire) * 0.85 / per)
	if b < 1 {
		return 1
	}
	return b
}

func toMMNodes(ns []parser.NTNode) []mmNode {
	out := make([]mmNode, len(ns))
	for i, n := range ns {
		cs := make([]mmCell, len(n.C))
		for j, c := range n.C {
			cs[j] = mmCell{N: c.N, T: c.T, V: c.V}
		}
		out[i] = mmNode{K: n.K, C: cs}
	}
	return out
}

// ── 讀入 ────────────────────────────────────────────────────────────────────

// ASTZip 描述讀入的 zip（供呼叫方做進一步校驗）。
type ASTZip struct {
	Manifest mmManifest
	Tool     string
	Src      string
	SrcSHA   string
}

// readZipEntries 讀出 zip 的所有條目（測試用：逐條校驗分片大小）。
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
// 版本/分片/節點數任一不符即硬錯（與 incremental 的 hard-error 口徑一致）。
func ReadASTZip(data []byte) (*parser.Program, *ASTZip, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("mmast: open zip: %w", err)
	}
	byName := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, nil, err
		}
		byName[f.Name] = b
	}
	mb, ok := byName["manifest.mm"]
	if !ok {
		return nil, nil, fmt.Errorf("mmast: manifest.mm missing from zip")
	}
	var man mmManifest
	if err := mm.DecodeToValue(mb, &man); err != nil {
		return nil, nil, fmt.Errorf("mmast: decode manifest: %w", err)
	}
	if man.ZipVer != ZipVer {
		return nil, nil, fmt.Errorf("mmast: zip version mismatch: blob %q, this compiler %q", man.ZipVer, ZipVer)
	}

	// 分片檔按名字排序讀取，順序不依賴 zip 內的條目順序
	var names []string
	for n := range byName {
		if n != "manifest.mm" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) != int(man.Chunks) {
		return nil, nil, fmt.Errorf("mmast: manifest says %d chunks, zip has %d", man.Chunks, len(names))
	}

	nodes := make([]parser.NTNode, 0, man.Nodes)
	for _, n := range names {
		var c mmChunk
		if err := mm.DecodeToValue(byName[n], &c); err != nil {
			return nil, nil, fmt.Errorf("mmast: decode %s: %w", n, err)
		}
		for _, mn := range c.Nodes {
			cs := make([]parser.NTCell, 0, len(mn.C))
			for _, mc := range mn.C {
				// mm 的空 slice（allow_empty）解碼回來是「一個零值元素」而不是空
				// （實測：全零欄位的 Token 節點會產生這個退化項）。我們的 cell
				// 三欄永遠非空，所以可以直接丟掉。
				if mc.N == "" && mc.T == "" && mc.V == "" {
					continue
				}
				cs = append(cs, parser.NTCell{N: mc.N, T: mc.T, V: mc.V})
			}
			nodes = append(nodes, parser.NTNode{K: mn.K, C: cs})
		}
	}
	if int32(len(nodes)) != man.Nodes {
		return nil, nil, fmt.Errorf("mmast: manifest says %d nodes, decoded %d", man.Nodes, len(nodes))
	}

	prog, err := parser.DecodeNodeTable(&parser.NodeTable{
		Ver:    man.Wire,
		AstVer: man.AstVer,
		Root:   man.Root,
		Nodes:  nodes,
	})
	if err != nil {
		return nil, nil, err
	}
	return prog, &ASTZip{Manifest: man, Tool: man.Tool, Src: man.Src, SrcSHA: man.SrcSHA}, nil
}
