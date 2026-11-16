package main

// fmt_result_test.go — `no fmt` 預設路徑（computeFmtResult）的行為回歸。
//
// computeFmtResult 現在只是 fmtflow.Format 的薄封裝（與 LSP format-on-save
// 共用同一份編排）；這裡釘住它在 CLI 側仍做到三件事：移除可省的標註、
// 保留不可省的標註、不再靠「先改源碼字串」達成正確結果。

import (
	"strings"
	"testing"

	nfmt "github.com/lizongying/nolang/fmt"
)

func TestComputeFmtResultRemovesRedundantAnnotations(t *testing.T) {
	src := "main = () () {\n  k []byte = bytes-of()\n  a [3]i64 = [1, 2, 3]\n}\n" +
		"bytes-of = () (r []byte) {\n  r = []\n}\n"
	out, perrs := computeFmtResult(src, nfmt.LoopStylePrefix, nfmt.ParseConfig{})
	if len(perrs) > 0 {
		t.Fatalf("unexpected parse errors: %v", perrs)
	}
	if strings.Contains(out, "k []byte") || !strings.Contains(out, "k = bytes-of()") {
		t.Fatalf("slice annotation must be removed by the default path:\n%s", out)
	}
	if !strings.Contains(out, "a [3]i64") {
		t.Fatalf("fixed-size array annotation must stay:\n%s", out)
	}
}

// TestComputeFmtResultPropagatesParseErrors 驗證解析失敗時 CLI 仍拿得到
// perrs（fmtProcessFile 靠它拒絕寫檔）。
func TestComputeFmtResultPropagatesParseErrors(t *testing.T) {
	_, perrs := computeFmtResult("main = () {\n  x = (\n}\n", nfmt.LoopStylePrefix, nfmt.ParseConfig{})
	if len(perrs) == 0 {
		t.Fatal("expected parse errors to be reported to the CLI")
	}
}
