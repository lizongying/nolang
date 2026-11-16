package lsp

// fmt_clone_test.go — 驗證 format-on-save 真的複用 parseSnapshot.ast，且只碰
// 工作副本：索引樹 / 快照樹不能被 FixStringConcatOperators 之類的就地改寫污染。

import (
	"encoding/json"
	"strings"
	"testing"

	nfmt "github.com/lizongying/nolang/fmt"
)

// TestRewriteAndFormatUsesSnapshotAST 用「只有 base 才看得見的簽名」當探針：
// 若 rewriteAndFormat 偷偷 fresh parse，`mod.f` 的 bool 結果證據就會消失，
// `ok == true` 不會被化簡。同時用 '+' 節點證明改寫只落在副本上。
func TestRewriteAndFormatUsesSnapshotAST(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	src := "f = () {\n    s = 'a' + 'b'\n    ok = mod.f()\n    ok == true -> return\n}\n"
	cfg := nfmt.ParseConfig{}
	base, errs := nfmt.ParseSourceWithConfig(src, cfg)
	if len(errs) > 0 || base == nil {
		t.Fatalf("base parse: errs=%v", errs)
	}
	if base.Sem.ExternFuncSigs == nil {
		base.Sem.ExternFuncSigs = make(map[string][]string)
	}
	base.Sem.ExternFuncSigs["mod.f"] = []string{"bool"}

	got := rewriteAndFormat(src, cfg, DefaultFmtConfig(), base)
	if !strings.Contains(got, "ok -> return") || strings.Contains(got, "ok == true") {
		t.Fatalf("rewrite must consume the snapshot AST's signature view, got:\n%s", got)
	}
	rerender := nfmt.FormatProgram(base, src)
	if !strings.Contains(rerender, "+") {
		t.Fatalf("snapshot AST was mutated by the rewrite pass:\n%s", rerender)
	}
}

// TestRewriteAndFormatFallsBackWhenSignatureViewIsNarrowed 對應 B5：關掉
// 跨模組簽名時 base.sem 仍帶完整表，必須改走 fresh parse 才不會兩套視野。
func TestRewriteAndFormatFallsBackWhenSignatureViewIsNarrowed(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	src := "f = () {\n    ok = mod.f()\n    ok == true -> return\n}\n"
	cfg := nfmt.ParseConfig{}
	base, errs := nfmt.ParseSourceWithConfig(src, cfg)
	if len(errs) > 0 || base == nil {
		t.Fatalf("base parse: errs=%v", errs)
	}
	base.Sem.ExternFuncSigs = map[string][]string{"mod.f": {"bool"}}
	off := DefaultFmtConfig()
	off.CrossModuleSignatures = false
	got := rewriteAndFormat(src, cfg, off, base)
	if !strings.Contains(got, "ok == true") {
		t.Fatalf("narrowed view must not inherit base's signature table, got:\n%s", got)
	}
}

// TestLSPFormattingAnnotationGuardsMatchCLI 回歸：LSP 過去比 cmd/no 多擋了
// slice / map 型別，同一份檔案「存檔自動格式化」與「no fmt -w」會給出不同
// 結果（直接違反單一來源契約）。現在兩邊共用 fmt.RedundantTypeAnnotationSafe：
// `[]T` 這種純型別提示要移除，定長陣列 / map 字面值仍必須保留。
func TestLSPFormattingAnnotationGuardsMatchCLI(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	uri := "file:///tmp/probe/annot-guard.no"
	src := "# std/str\nf = () {\n    k []byte = 'Jefe'.to-bytes()\n    a [3]i64 = [1, 2, 3]\n    m [str]i64 = { 'a': 1 }\n}\n"
	s := NewServer()
	if _, err := s.Handle("initialize", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, perrs := openParsedLSPDoc(t, s, uri, src); len(perrs) > 0 {
		t.Fatalf("unexpected live diagnostics: %v", perrs)
	}
	edits := lspFormattingEdits(t, s, uri)
	if len(edits) == 0 {
		t.Fatal("expected formatting edits")
	}
	out := edits[0].NewText
	if strings.Contains(out, "k []byte") || !strings.Contains(out, "k = 'Jefe'.to-bytes()") {
		t.Fatalf("slice annotation must be removed exactly like no fmt -w:\n%s", out)
	}
	if !strings.Contains(out, "a [3]i64") || !strings.Contains(out, "m [str]i64") {
		t.Fatalf("load-bearing array / map-literal annotations must stay:\n%s", out)
	}
}

// TestLSPFormattingReusesSnapshotAST 是端到端版本：走 initialize →
// OpenDocument/ParseDocument → textDocument/formatting，預設設定下主動改寫
// 必須全部生效，且 snap.ast 事後仍是原來那棵樹。
func TestLSPFormattingReusesSnapshotAST(t *testing.T) {
	skipLSPWithoutStdSigKey(t)
	uri := "file:///tmp/probe/clone-reuse.no"
	src := "# std/args\nf = () {\n    x str = ''\n    hit = args.has-flag('x')\n    hit == true -> return\n    s = 'a' + 'b'\n}\n"
	s := NewServer()
	if _, err := s.Handle("initialize", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	snap, perrs := openParsedLSPDoc(t, s, uri, src)
	if len(perrs) > 0 {
		t.Fatalf("unexpected live diagnostics: %v", perrs)
	}
	edits := lspFormattingEdits(t, s, uri)
	if len(edits) == 0 {
		t.Fatal("expected formatting edits")
	}
	out := edits[0].NewText
	if !strings.Contains(out, "x = ''") || strings.Contains(out, "x str") {
		t.Fatalf("redundant annotation removal lost:\n%s", out)
	}
	if !strings.Contains(out, "hit -> return") || strings.Contains(out, "hit == true") {
		t.Fatalf("cross-module bool simplification lost through the clone:\n%s", out)
	}
	rerender := nfmt.FormatProgram(snap.ast, src)
	if !strings.Contains(rerender, "+") {
		t.Fatalf("snapshot AST was mutated while formatting:\n%s", rerender)
	}
}
