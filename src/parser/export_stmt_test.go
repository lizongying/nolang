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

// TestExportWithoutSymbolAccepted 釘住「`@ path` 單獨寫」是合法的「整包導出」。
//
// 背景：過去此寫法被解析期錯誤 lgxbfk9a 拒絕（consumers 都以有符號名為前提，
// Function 為空的宣告會被全部跳過）。但包級 lib.no 需要它來聲明「這個包導出
// 某個模組」：`@ /test/src/assert` 等價於 `@ /test/src/assert assert`，
// 讓 `# test/assert` 可以導入。現在全局放寬，語義由 checker/build 的
// lib.no 解析鏈賦予；hir/mir 仍不把它註冊成 callable alias。
func TestExportWithoutSymbolAccepted(t *testing.T) {
	cases := map[string]string{
		"bare path":        "@ std/txt\n",
		"bare path slash":  "@ /std/txt\n",
		"path with dots":   "@ github.com/x/y\n",
		"trailing newline": "@ std/txt\n\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if errs := parseExportSrc(src); len(errs) != 0 {
				t.Errorf("unexpected diagnostics for %q: %v", strings.TrimSpace(src), errs)
			}
		})
	}
}

// TestExportMultiSegmentAlias 釘住多段模組名別名：`@ path test/assert` 讓
// `# test/assert` 可以按這個名字導入模組（見 checker.ResolvePackageExport）。
func TestExportMultiSegmentAlias(t *testing.T) {
	cases := map[string]string{
		"bare module + multi alias": "@ /test/src/assert test/assert\n",
		"explicit as + multi alias": "@ /test/src/assert as test/assert\n",
		"symbol + single alias":     "@ /test/src/assert.eq assert-eq\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if errs := parseExportSrc(src); len(errs) != 0 {
				t.Errorf("unexpected diagnostics for %q: %v", strings.TrimSpace(src), errs)
			}
		})
	}
	p := New(lexer.New("@ /test/src/assert test/assert\n"))
	prog := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	var es *ExportStatement
	for _, stmt := range prog.Statements {
		if e, ok := stmt.(*ExportStatement); ok {
			es = e
		}
	}
	if es == nil {
		t.Fatal("no ExportStatement parsed")
	}
	if es.Path != "/test/src/assert" {
		t.Errorf("Path = %q, want /test/src/assert", es.Path)
	}
	if es.Alias != "test/assert" {
		t.Errorf("Alias = %q, want test/assert", es.Alias)
	}
	if es.Function != "" {
		t.Errorf("Function = %q, want empty", es.Function)
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
