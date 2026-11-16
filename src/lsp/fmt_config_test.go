package lsp

// fmt_config_test.go — LSP 格式化設定與「預設與 CLI 一致」的對照測試。
//
// 期望值取自 `./bin/no fmt <file>` 的實際輸出（檔案模式會跑完整主動改寫）：
//   x str = ''                    → x = ''
//   #{overflow=wrap} / 'a'+'b'   → 註解移除、'+' 翻成 '-'
//   m [str]i64 = {...}           → 標註保留（map 字面量防禦）
//
// 對照基準用檔案模式：stdin 現在也走 computeFmtResult（會做主動改寫），
// 但它的簽名上下文是 configFor(".")，子目錄檔案的 package root 可能不同。

import (
	"encoding/json"
	"strings"
	"testing"

	nfmt "github.com/lizongying/nolang/fmt"
	"github.com/lizongying/nolang/fmtflow"
)

func lspFormatText(t *testing.T, s *Server, uri, src string) string {
	t.Helper()
	if _, err := s.documents.OpenDocument(uri, src); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}
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
	if len(edits) == 0 {
		return src
	}
	return edits[0].NewText
}

func TestLSPFmtConfigDefaultsMatchCLI(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	if got := (NewServer().fmtCfg.get()); got != DefaultFmtConfig() {
		t.Fatalf("default config = %+v, want %+v", got, DefaultFmtConfig())
	}
	c := DefaultFmtConfig()
	if !c.Rewrite || !c.RedundantTypes || !c.IneffectiveOverflow || !c.StringConcat || !c.CrossModuleSignatures {
		t.Fatalf("CLI-compatible defaults must all be enabled: %+v", c)
	}
}

// TestLSPFormatDefaultRewritesLikeCLI 是「預設與 CLI 一致」的行為證據。
func TestLSPFormatDefaultRewritesLikeCLI(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	s := NewServer()
	if got := lspFormatText(t, s, "file:///tmp/probe-a.no", "x str = ''\n"); got != "x = ''\n" {
		t.Fatalf("redundant type should be removed by default, got %q", got)
	}
	if got := lspFormatText(t, s, "file:///tmp/probe-b.no", "#{overflow=wrap}\ns = 'a' + 'b'\n"); got != "s = 'a' - 'b'\n" {
		t.Fatalf("overflow annotation + string concat should be rewritten, got %q", got)
	}
	// map 字面量的型別標註不可省：防禦必須跟 CLI 一樣擋下。
	if got := lspFormatText(t, s, "file:///tmp/probe-d.no", "m [str]i64 = {\n    'a': 1\n}\n"); !strings.Contains(got, "[str]i64") {
		t.Fatalf("map literal annotation must be preserved, got %q", got)
	}
}

// TestLSPFormatConfigGranularity 逐項驗證開關確實獨立生效。
func TestLSPFormatConfigGranularity(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	s := NewServer()
	if _, err := s.Handle("initialize", json.RawMessage(`{"initializationOptions":{"fmt":{"redundantTypes":false}}}`)); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	c := s.fmtCfg.get()
	if !c.Rewrite || c.RedundantTypes || !c.IneffectiveOverflow || !c.StringConcat || !c.CrossModuleSignatures {
		t.Fatalf("patch must disable only redundantTypes: %+v", c)
	}
	if got := lspFormatText(t, s, "file:///tmp/probe-a2.no", "x str = ''\n"); !strings.Contains(got, "x str = ''") {
		t.Fatalf("redundantTypes=false should keep the annotation, got %q", got)
	}
	// 其他改寫仍應生效。
	if got := lspFormatText(t, s, "file:///tmp/probe-b2.no", "#{overflow=wrap}\ns = 'a' + 'b'\n"); got != "s = 'a' - 'b'\n" {
		t.Fatalf("other rewrites should stay enabled, got %q", got)
	}
}

// TestLSPFormatConfigRewriteOffIsPureLayout 驗證總開關：退回純排版。
func TestLSPFormatConfigRewriteOffIsPureLayout(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	s := NewServer()
	if _, err := s.Handle("initialize", json.RawMessage(`{"initializationOptions":{"fmt":{"rewrite":false}}}`)); err != nil {
		t.Fatal(err)
	}
	if got := lspFormatText(t, s, "file:///tmp/probe-a3.no", "x str = ''\n"); !strings.Contains(got, "x str = ''") {
		t.Fatalf("rewrite=false must be pure layout, got %q", got)
	}
	if got := lspFormatText(t, s, "file:///tmp/probe-b3.no", "#{overflow=wrap}\ns = 'a' + 'b'\n"); !strings.Contains(got, "#{overflow=wrap}") {
		t.Fatalf("rewrite=false must keep overflow annotations, got %q", got)
	}
}

// TestLSPFormatConfigDidChangeConfiguration 驗證執行期改設定。
func TestLSPFormatConfigDidChangeConfiguration(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	s := NewServer()
	if _, err := s.Handle("workspace/didChangeConfiguration", json.RawMessage(`{"settings":{"nolang":{"fmt":{"rewrite":false}}}}`)); err != nil {
		t.Fatal(err)
	}
	if s.fmtCfg.get().Rewrite {
		t.Fatal("didChangeConfiguration should have disabled rewrite")
	}
	if got := lspFormatText(t, s, "file:///tmp/probe-a4.no", "x str = ''\n"); !strings.Contains(got, "x str = ''") {
		t.Fatalf("runtime config must affect formatting, got %q", got)
	}
	// 壞設定不該把現值重置成預設。
	if _, err := s.Handle("workspace/didChangeConfiguration", json.RawMessage(`{"settings":{"unrelated":123}}`)); err != nil {
		t.Fatal(err)
	}
	if s.fmtCfg.get().Rewrite {
		t.Fatal("unrelated settings must not reset the config to defaults")
	}
}

// TestLSPFormatCrossModuleSignaturesOff 驗證關掉簽名後視野收斂：
// 跨模組 bool 呼叫不再被化簡，但主動改寫仍要跑（不能因 fresh parse 失敗
// 退回純排版）。
func TestLSPFormatCrossModuleSignaturesOff(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	src := "# std/args\nf = () {\n    x str = ''\n    hit = args.has-flag('x')\n    hit == true -> return\n}\n"
	s := NewServer()
	if _, err := s.Handle("initialize", json.RawMessage(`{"initializationOptions":{"fmt":{"crossModuleSignatures":false}}}`)); err != nil {
		t.Fatal(err)
	}
	got := lspFormatText(t, s, "file:///tmp/probe-c5.no", src)
	if !strings.Contains(got, "hit == true") {
		t.Fatalf("no cross-module signatures => keep the comparison, got:\n%s", got)
	}
	if !strings.Contains(got, "x = ''") || strings.Contains(got, "x str = ''") {
		t.Fatalf("turning off signatures must not fall back to plain formatting, got:\n%s", got)
	}
}

// TestNarrowConfigKeepsParseOnlyFields 釘住「關掉跨模組簽名」只關三張
// 簽名表。EnumVariants / Filename / AllowAnonymousFnType 不是函式簽名，但會
// 影響解析成敗，清掉會讓渲染用的 fresh parse 與 gate 的完整 cfg 分歧。
func TestNarrowConfigKeepsParseOnlyFields(t *testing.T) {
	cfg := nfmt.ParseConfig{
		Filename:             "probe.no",
		AllowAnonymousFnType: true,
		FuncSigs:             map[string][]string{"fs.write-str": {"?bool"}},
		MethodSigs:           map[string][]string{"str.starts-with": {"bool"}},
		StructFields:         map[string]map[string]string{"t": {"n": "i64"}},
		EnumVariants:         map[string][]string{"code": {"io", "arg"}},
	}
	off := fmtflow.NarrowConfig(cfg)
	if off.FuncSigs != nil || off.MethodSigs != nil || off.StructFields != nil {
		t.Fatal("crossModuleSignatures=false must drop the three signature tables")
	}
	if off.Filename != "probe.no" || len(off.EnumVariants) == 0 || !off.AllowAnonymousFnType {
		t.Fatalf("parse-affecting fields must survive: %+v", off)
	}
	if cfg.FuncSigs == nil || cfg.EnumVariants == nil {
		t.Fatal("NarrowConfig must not mutate the shared source config")
	}
}
