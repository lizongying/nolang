package fmtflow

import (
	"strings"
	"testing"

	nfmt "github.com/lizongying/nolang/fmt"
)

// TestFormatRemovesRedundantAnnotations 驗證共用管線的預設行為：可省的標註
// （named type / slice）被移除，不可省的（定長陣列 / map 字面值）保留。
func TestFormatRemovesRedundantAnnotations(t *testing.T) {
	src := "f = () () {\n    k []byte = b()\n    a [3]i64 = [1, 2, 3]\n    m [str]i64 = { 'a': 1 }\n}\n" +
		"b = () (r []byte) {\n    r = []\n}\n"
	out, errs := Format(src, nfmt.ParseConfig{}, DefaultOptions(nfmt.LoopStylePrefix))
	if len(errs) > 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if strings.Contains(out, "k []byte") || !strings.Contains(out, "k = b()") {
		t.Fatalf("slice annotation must be removed:\n%s", out)
	}
	if !strings.Contains(out, "a [3]i64") || !strings.Contains(out, "m [str]i64") {
		t.Fatalf("load-bearing annotations must stay:\n%s", out)
	}
}

// TestFormatHonoursLoopStyle 驗證 loopStyle 一路傳到渲染器（B1 的回歸點：
// 過去主動改寫路徑硬編碼 prefix，`-loop-style=suffix` 靜默失效）。
func TestFormatHonoursLoopStyle(t *testing.T) {
	src := "f = () () {\n    3 * {\n        print('x')\n    }\n}\n"
	out, _ := Format(src, nfmt.ParseConfig{}, DefaultOptions(nfmt.LoopStyleSuffix))
	if !strings.Contains(out, "} * 3") {
		t.Fatalf("suffix loop style lost in the shared pipeline:\n%s", out)
	}
}

// TestFormatBaseCloneMatchesFreshParse 驗證「複用快照 AST 的深拷貝」與
// 「自己 fresh parse」產出一致 —— 這是 LSP 省掉一次 parse 的前提。
func TestFormatBaseCloneMatchesFreshParse(t *testing.T) {
	src := "f = () () {\n    s = 'a' + 'b'\n    x str = ''\n}\n"
	base, perrs := nfmt.ParseSourceWithConfig(src, nfmt.ParseConfig{})
	if len(perrs) > 0 || base == nil {
		t.Fatalf("base parse: %v", perrs)
	}
	fresh, _ := Format(src, nfmt.ParseConfig{}, DefaultOptions(nfmt.LoopStylePrefix))
	opt := DefaultOptions(nfmt.LoopStylePrefix)
	opt.Base = base
	cloned, _ := Format(src, nfmt.ParseConfig{}, opt)
	if fresh != cloned {
		t.Fatalf("clone path diverged from fresh parse:\nfresh:\n%s\nclone:\n%s", fresh, cloned)
	}
	// base 不得被就地改寫（FixStringConcatOperators 會改樹）。
	if again := nfmt.FormatProgram(base, src); !strings.Contains(again, "'a' + 'b'") {
		t.Fatalf("base AST was mutated by the rewrite pass:\n%s", again)
	}
}

// TestFormatFallsBackOnParseError 驗證解析失敗時退回純排版並回報錯誤
// （CLI 靠這個 perrs 拒絕寫檔）。
func TestFormatFallsBackOnParseError(t *testing.T) {
	src := "f = () {\n    x = (\n}\n"
	out, errs := Format(src, nfmt.ParseConfig{}, DefaultOptions(nfmt.LoopStylePrefix))
	if len(errs) == 0 {
		t.Fatal("expected parse errors to be reported")
	}
	if out == "" {
		t.Fatal("fallback must still produce output")
	}
}

// TestFormatAllSwitchesOffIsPureFormat 驗證三個開關全關時完全不解析、
// 直接走純排版（LSP 的 rewrite=false）。
func TestFormatAllSwitchesOffIsPureFormat(t *testing.T) {
	src := "f = () () {\n    x str = ''\n}\n"
	got, errs := Format(src, nfmt.ParseConfig{}, Options{LoopStyle: nfmt.LoopStylePrefix})
	want, _ := nfmt.FormatFileWithConfig(src, nfmt.ParseConfig{}, nfmt.LoopStylePrefix)
	if got != want || len(errs) > 0 {
		t.Fatalf("pure format mismatch:\ngot:  %q\nwant: %q\nerrs: %v", got, want, errs)
	}
	if strings.Contains(got, "x = ''") {
		t.Fatal("rewrite switches are off; annotations must not be removed")
	}
}
