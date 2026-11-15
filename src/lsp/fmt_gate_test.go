package lsp

// fmt_gate_test.go — 「一次解析、兩處消費」的長期架構回歸測試。
//
// 實證（不是推測）：簽名會讓 parse **多** 報一條語義診斷。`fs.write-str` 回傳
// `?bool`，注入 std 表後 `r = fs.write-str(...)` 會被推成 option，於是
// `r: { ok -> … / nil -> … }` 觸發「option match 必須涵蓋 err」；裸 parse
// 看不到回傳型別，靜默通過（方向與先前口頭描述的「帶簽名才不報錯」相反）。
//
// 舊結構：即時診斷走 ParseDocument(cfg.Apply)，format 閘門另起一次裸 parse，
// 兩套錯誤集合。長期結構：ParseDocument 發布 parseSnapshot（診斷 + 糖 AST +
// cfg），format 直接消費同一份，快取落後時同步補跑同一個 ParseDocument。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// gateOptionMatchSrc 是「只有帶跨模組簽名才看得出錯」的最小樣例。
const gateOptionMatchSrc = "# std/fs\nf = () {\n    r = fs.write-str('/tmp/x', 'y')\n    r: {\n        ok -> print('yes')\n        nil -> print('no')\n    }\n}\n"

func openParsedLSPDoc(t *testing.T, s *Server, uri, src string) (*parseSnapshot, []string) {
	t.Helper()
	if _, err := s.documents.OpenDocument(uri, src); err != nil {
		t.Fatal(err)
	}
	_, perrs, err := s.documents.ParseDocument(uri)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	snap := s.documents.snapshotFor(uri)
	if snap == nil {
		t.Fatal("ParseDocument must publish a parseSnapshot")
	}
	return snap, perrs
}

func lspFormattingEdits(t *testing.T, s *Server, uri string) []TextEdit {
	t.Helper()
	res, err := s.handleTextDocumentFormatting(DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options:      &FormattingOptions{TabSize: 4, InsertSpaces: true},
	})
	if err != nil {
		t.Fatalf("handleTextDocumentFormatting: %v", err)
	}
	edits, ok := res.([]TextEdit)
	if !ok {
		t.Fatalf("result is not []TextEdit: %#v", res)
	}
	return edits
}

// TestLSPFormatGateUsesLiveParseSnapshot 驗收快照內容與「有紅線就不格式化」。
func TestLSPFormatGateUsesLiveParseSnapshot(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	uri := "file:///tmp/probe/gate-main.no"

	// 對照組：裸 parse 眼中這段原始碼完全合法。
	bare := parser.New(lexer.New(gateOptionMatchSrc))
	bare.ParseProgram()
	if len(bare.Errors()) != 0 {
		t.Fatalf("bare parse should NOT see the option-match error: %v", bare.Errors())
	}

	s := NewServer()
	snap, _ := openParsedLSPDoc(t, s, uri, gateOptionMatchSrc)
	joined := strings.Join(snap.errs, "\n")
	if !strings.Contains(joined, "option match must handle all branches") {
		t.Fatalf("live parse must see the signature-dependent diagnostic, got: %v", snap.errs)
	}
	if len(snap.cfg.FuncSigs) == 0 || snap.ast == nil || snap.ast.Sem == nil || len(snap.ast.Sem.ExternFuncSigs) == 0 {
		t.Fatal("snapshot must carry the injected signature config and the sugar AST")
	}
	// cfg.Apply 會覆寫 p.Filename，少了他診斷文字會從 "x.no:" 退化成無前綴。
	if !strings.Contains(joined, "gate-main.no") || snap.cfg.Filename != "gate-main.no" {
		t.Fatalf("diagnostics lost the filename: errs=%v cfg.Filename=%q", snap.errs, snap.cfg.Filename)
	}
	if edits := lspFormattingEdits(t, s, uri); len(edits) != 0 {
		t.Fatalf("gate must skip formatting on diagnostics, got %#v", edits)
	}
}

// TestLSPFormatGateIsSameResultAsLiveDiagnostics 是「不再有兩套結果」的硬證據：
// 快照的 errs 與 ParseDocument 回傳的 perrs 必須是同一個底層陣列，而非複本；
// 且文字未變時 ensureFormatSnapshot 直接回傳同一個快照（零額外 parse）。
func TestLSPFormatGateIsSameResultAsLiveDiagnostics(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	uri := "file:///tmp/probe/gate-live.no"
	s := NewServer()
	snap, perrs := openParsedLSPDoc(t, s, uri, gateOptionMatchSrc)
	if len(perrs) == 0 || len(snap.errs) != len(perrs) {
		t.Fatalf("unexpected diagnostics: snap=%v perrs=%v", snap.errs, perrs)
	}
	if &snap.errs[0] != &perrs[0] {
		t.Fatal("gate diagnostics were copied from live diagnostics; one parse must feed both")
	}
	cached, err := s.ensureFormatSnapshot(uri)
	if err != nil {
		t.Fatal(err)
	}
	if cached != snap {
		t.Fatal("unchanged buffer must reuse the same snapshot without re-parsing")
	}
}

// TestLSPFormatGateRefreshesStaleSnapshot 模擬 didChange 後非同步 parse 還沒
// 趕上就存檔：不能拿舊的合法診斷放行格式化。
func TestLSPFormatGateRefreshesStaleSnapshot(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	uri := "file:///tmp/probe/gate-stale.no"
	s := NewServer()
	stale, _ := openParsedLSPDoc(t, s, uri, "x str = ''\n")
	if len(stale.errs) != 0 {
		t.Fatalf("baseline should be clean, got %v", stale.errs)
	}
	if _, err := s.documents.UpdateDocument(uri, []TextDocumentContentChange{{Text: gateOptionMatchSrc}}, 2); err != nil {
		t.Fatal(err)
	}
	snap, err := s.ensureFormatSnapshot(uri)
	if err != nil {
		t.Fatal(err)
	}
	if snap == stale || snap.text != gateOptionMatchSrc {
		t.Fatalf("stale snapshot must be replaced by a fresh parse of the current text: %#v", snap)
	}
	if !strings.Contains(strings.Join(snap.errs, "\n"), "option match must handle all branches") {
		t.Fatalf("fresh snapshot must see the new diagnostics, got: %v", snap.errs)
	}
	if edits := lspFormattingEdits(t, s, uri); len(edits) != 0 {
		t.Fatalf("stale diagnostics must not let a broken buffer be formatted: %#v", edits)
	}
}

// TestLSPFormatConfigDoesNotAlterDiagnostics：fmt 設定只影響渲染視野，不該
// 改寫即時診斷（紅線存在就該擋下格式化），否則 gate 又會和編輯器分岔。
func TestLSPFormatConfigDoesNotAlterDiagnostics(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	s := NewServer()
	if _, err := s.Handle("initialize", json.RawMessage(`{"initializationOptions":{"fmt":{"crossModuleSignatures":false}}}`)); err != nil {
		t.Fatal(err)
	}
	uri := "file:///tmp/probe/gate-off.no"
	snap, _ := openParsedLSPDoc(t, s, uri, gateOptionMatchSrc)
	if !strings.Contains(strings.Join(snap.errs, "\n"), "option match must handle all branches") {
		t.Fatalf("live diagnostics must not be narrowed by fmt config, got: %v", snap.errs)
	}
	if len(snap.cfg.FuncSigs) == 0 {
		t.Fatal("live parse must keep full cross-module signatures regardless of fmt config")
	}
	if edits := lspFormattingEdits(t, s, uri); len(edits) != 0 {
		t.Fatalf("gate must still respect live diagnostics, got %#v", edits)
	}
	// 設定本身仍然生效（渲染視野收斂）：見 fmt_config_test.go 的
	// TestLSPFormatCrossModuleSignaturesOff。
}

// TestLSPFormatStillBlocksSyntaxErrors 確保合併沒有放寬舊的語法閘門。
func TestLSPFormatStillBlocksSyntaxErrors(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	s := NewServer()
	badURI := "file:///tmp/probe/broken.no"
	openParsedLSPDoc(t, s, badURI, "f = () {\n    x =\n}\n")
	if edits := lspFormattingEdits(t, s, badURI); len(edits) != 0 {
		t.Fatalf("syntax errors must still block formatting, got %#v", edits)
	}
	okURI := "file:///tmp/probe/ok.no"
	if got := lspFormatText(t, s, okURI, "x str = ''\n"); got != "x = ''\n" {
		t.Fatalf("valid source must still be formatted with default rewrites, got %q", got)
	}
}

// TestLSPWatchedFilesInvalidatesBothCaches 驗證 manifest 變更才失效快取，
// 一般 .no 存檔不會讓 loader 重建；診斷快照也必須一併清掉。
func TestLSPWatchedFilesInvalidatesBothCaches(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	s := NewServer()
	c := s.documents.sig
	uri := "file:///tmp/probe/cache-main.no"
	openParsedLSPDoc(t, s, uri, "x str = ''\n")
	if len(c.pkgs) == 0 || s.documents.snapshotFor(uri) == nil {
		t.Fatal("ParseDocument should populate both caches")
	}

	send := func(changes string) {
		t.Helper()
		if _, err := s.Handle("workspace/didChangeWatchedFiles", json.RawMessage(changes)); err != nil {
			t.Fatalf("didChangeWatchedFiles: %v", err)
		}
	}
	send(`{"changes":[{"uri":"file:///tmp/probe/other.no","type":2}]}`)
	if len(c.pkgs) == 0 || s.documents.snapshotFor(uri) == nil {
		t.Fatal("unrelated .no changes must not invalidate anything")
	}
	send(`{"changes":[{"uri":"file:///tmp/probe/package.noml","type":2}]}`)
	if len(c.pkgs) != 0 || len(c.loaders) != 0 || s.documents.snapshotFor(uri) != nil {
		t.Fatalf("package.noml change must clear all three caches: pkgs=%d loaders=%d snap=%v",
			len(c.pkgs), len(c.loaders), s.documents.snapshotFor(uri))
	}
	// workspace.noml 也認得。
	c.configFor(uri, "x str = ''\n")
	send(`{"changes":[{"uri":"file:///tmp/probe/workspace.noml","type":1}]}`)
	if len(c.pkgs) != 0 {
		t.Fatal("workspace.noml change must invalidate the cache")
	}
}
