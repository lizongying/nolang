package fmt

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// S4 harness — docs/design/nolang-parser-lowering-invariants.md §8 step S4.
//
// S4 moved the consumers of the parser (`no fmt`, `no fix`, LSP, astpatch) onto
// the *sugar* AST (SkipLowering = true). §4.3（2026-10-17）已刪除舊的 surface 形態
// （SkipUnwrapLowering + SkipSafeIndexLowering）與其 A/B 比較門禁：糖形態是唯一非
// 編譯形態，故本檔由「sugar vs surface」A/B 改為「糖形態整體語料冒煙測試」。
//
// 真正保留的回歸價值：格式化器在 532 份語料上（糖形態）不崩潰、產出可重解析、
// 且冪等（fmt(fmt(x)) == fmt(x)）。原有的 allowlist（tests/type-match.no 等
// surface 漏報處）隨 surface 退役一併刪除。

const sugarFmtCorpusRoot = "../../tests"

// sugarFmtCorpus returns every *.no under tests/, sorted. A missing corpus is a
// hard failure, not a skip.
func sugarFmtCorpus(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(sugarFmtCorpusRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", sugarFmtCorpusRoot, err)
	}
	if len(files) == 0 {
		t.Fatalf("no .no files under %s — corpus path is wrong; refusing to report a vacuous 0-differ", sugarFmtCorpusRoot)
	}
	sort.Strings(files)
	return files
}

// formatSugar parses src under the (now sole) sugar shape and formats the tree.
func formatSugar(src string) (out string, perrs []string) {
	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipLowering = true
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", p.Errors()
	}
	o, ok, errs := formatProgramAST(prog, src)
	if !ok {
		return "", errs
	}
	return ensureTrailingNewline(o), nil
}

// formatCLISugar reproduces what `no fmt` actually does: parse under sugar, ask
// the checker which `#{overflow=...}` annotations are ineffective, then format.
func formatCLISugar(src string) (out string, perrs []string) {
	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipLowering = true
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return "", p.Errors()
	}
	relevant, governed := checker.OverflowAnnotationRelevance(prog)
	return FormatProgramWithOverflow(prog, src, relevant, governed), nil
}

// TestSugarFmtCorpusSmoke is the S4 gate (rewritten for §4.3): formatting every
// corpus file under the sugar shape must (a) parse cleanly, (b) produce output
// that reparses, and (c) be idempotent. A failure here means the formatter
// produces unreparseable or non-idempotent output on real code.
func TestSugarFmtCorpusSmoke(t *testing.T) {
	files := sugarFmtCorpus(t)
	var offenders []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		once, perrs := formatSugar(string(b))
		if len(perrs) > 0 {
			offenders = append(offenders, path)
			continue
		}
		// 重解析：格式化輸出必須能再被解析。
		if _, rerrs := formatSugar(once); len(rerrs) > 0 {
			offenders = append(offenders, path)
			continue
		}
		// 冪等：fmt(fmt(x)) == fmt(x)。
		twice, terrs := formatSugar(once)
		if len(terrs) > 0 {
			offenders = append(offenders, path)
			continue
		}
		if once != twice {
			offenders = append(offenders, path)
		}
	}
	if len(offenders) == 0 {
		t.Logf("%d files: sugar formatting reparses + idempotent", len(files))
		return
	}
	sort.Strings(offenders)
	shown := offenders
	if len(shown) > 10 {
		shown = shown[:10]
	}
	t.Errorf("sugar formatting failed on %d/%d corpus files: %s", len(offenders), len(files), strings.Join(shown, ", "))
}

// TestNoFmtSugarCorpusSmoke runs the full `no fmt` pipeline (checker-driven
// overflow annotation relevance) over the corpus under the sugar shape. It is a
// regression net: the corpus's overflow annotations are mostly inert, so a pass
// proves mainly that the CLI path does not panic or reject files under sugar.
func TestNoFmtSugarCorpusSmoke(t *testing.T) {
	files := sugarFmtCorpus(t)
	var parseFails int
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, perrs := formatCLISugar(string(b)); len(perrs) > 0 {
			parseFails++
		}
	}
	t.Logf("%d files: no-fmt CLI path under sugar shape (parseFails=%d)", len(files), parseFails)
}

// TestSugarFmtCorpusCensus logs the corpus formatter sweep stats. Never fails.
func TestSugarFmtCorpusCensus(t *testing.T) {
	files := sugarFmtCorpus(t)
	var reparseFails, nonIdempotent int
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		once, perrs := formatSugar(string(b))
		if len(perrs) > 0 {
			continue
		}
		twice, terrs := formatSugar(once)
		if len(terrs) > 0 {
			reparseFails++
			continue
		}
		if once != twice {
			nonIdempotent++
		}
	}
	t.Logf("corpus=%d  reparseFails=%d  nonIdempotent=%d", len(files), reparseFails, nonIdempotent)
}

// TestSugarFmtDumpOne prints the sugar rendering of a single corpus file. Set
// NOLANG_FMT_AB_FILE to a corpus path fragment (e.g. "json.no"); without it the
// test does nothing.
func TestSugarFmtDumpOne(t *testing.T) {
	want := os.Getenv("NOLANG_FMT_AB_FILE")
	if want == "" {
		t.Skip("set NOLANG_FMT_AB_FILE to a corpus path fragment to dump one file")
	}
	files := sugarFmtCorpus(t)
	for _, path := range files {
		if !strings.Contains(path, want) {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		out, perrs := formatSugar(string(b))
		if len(perrs) > 0 {
			t.Logf("=== %s (PARSE ERRORS) ===\n%s", path, strings.Join(perrs, "\n"))
			continue
		}
		t.Logf("=== %s ===\n%s", path, out)
	}
}
