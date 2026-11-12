package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// parseExportSrc 只在包內用：解析 src 並回傳全部解析期診斷。
func parseExportSrc(src string) []string {
	p := New(lexer.New(src))
	p.ParseProgram()
	return p.Errors()
}

// TestExportWithoutSymbolRejected 釘住「`@ path` 單獨寫」不再是靜默 no-op。
//
// 背景（2026-11-12）：`src/std/global.no` 曾被寫上 `@ std/txt` / `@ std/str`，
// 想表達「txt / str 的定義在那些模組」。但 `@` 的每個消費方
// （hir.registerAliasSigs、mir.collectAliases、checker.ValidateExportSymbols、
// checker.filterByExports）都以「有符號名」為前提，Function 為空的宣告會被
// 全部跳過：既不改變行為，也不報錯。作者因此會誤以為綁定成功了。
//
// 這種「無意義但也沒人管」的寫法與 docs/docs/lang/annotation-scope.md 的原則
// （非法與無意義都必須報錯）衝突，所以改成解析期錯誤 lgxbfk9a。
func TestExportWithoutSymbolRejected(t *testing.T) {
	cases := map[string]string{
		"bare path":        "@ std/txt\n",
		"bare path slash":  "@ /std/txt\n",
		"path with dots":   "@ github.com/x/y\n",
		"trailing newline": "@ std/txt\n\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			errs := parseExportSrc(src)
			if len(errs) == 0 {
				t.Fatalf("no diagnostic for %q; a symbol-less export must be rejected", strings.TrimSpace(src))
			}
			if !strings.Contains(strings.Join(errs, "\n"), "lgxbfk9a") {
				t.Errorf("diagnostics %v do not mention lgxbfk9a", errs)
			}
		})
	}
}

// TestExportWithSymbolAccepted 是反面的防誤傷：合法的導出（有符號、可選別名）
// 不得被上一條規則波及。
func TestExportWithSymbolAccepted(t *testing.T) {
	cases := map[string]string{
		"symbol":                       "@ std/err.new\n",
		"symbol + alias":               "@ std/err.new err\n",
		"symbol + as alias":            "@ std/err.new as err\n",
		"symbol on slash path":         "@ /src/utils.greet a\n",
		"symbol that is a struct":      "@ /example/mysql-driver/src/mysql.db-mysql\n",
		"alias only, no symbol (as)":   "@ /cfg/server as cfg\n",
		"alias only, no symbol (bare)": "@ /cfg/server cfg\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if errs := parseExportSrc(src); len(errs) != 0 {
				t.Errorf("unexpected diagnostics for %q: %v", strings.TrimSpace(src), errs)
			}
		})
	}
}

// TestExportAliasEqualToSymbolStillReported 確認既有的 k0fwb241（別名與符號同名）
// 沒有被新規則取代或吞掉。
func TestExportAliasEqualToSymbolStillReported(t *testing.T) {
	errs := parseExportSrc("@ std/err.new new\n")
	if len(errs) == 0 {
		t.Fatal("no diagnostic for alias == symbol")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "k0fwb241") {
		t.Errorf("diagnostics %v do not mention k0fwb241", errs)
	}
}
