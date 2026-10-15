package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/parser/dump"
)

func parseFile(t *testing.T, path string) *parser.Program {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p := parser.New(lexer.New(string(src)))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse %s: %v", path, errs)
	}
	return prog
}

// TestASTTreeRoundTripStd：對 std 語料逐模組做 編碼→解碼，用 L2 oracle
// （dump 文本）做逐位元組等價判據——含註解副表，所以 nodeSem 的往返也被覆蓋。
func TestASTTreeRoundTripStd(t *testing.T) {
	root := filepath.Join("..", "std")
	var files []string
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && filepath.Ext(p) == ".no" {
			files = append(files, p)
		}
		return nil
	})
	if len(files) < 50 {
		t.Fatalf("expected the std corpus, got %d files", len(files))
	}

	opts := dump.Options{Annotations: true}
	failed := 0
	for _, f := range files {
		prog := parseFile(t, f)
		nt, err := parser.EncodeASTTree(prog, "")
		if err != nil {
			t.Errorf("%s: encode: %v", f, err)
			failed++
			continue
		}
		back, err := parser.DecodeASTTree(nt)
		if err != nil {
			t.Errorf("%s: decode: %v", f, err)
			failed++
			continue
		}
		want := dump.DumpWithOptions(prog, opts)
		got := dump.DumpWithOptions(back, opts)
		if want != got {
			t.Errorf("%s: dump mismatch (%d vs %d bytes)", f, len(want), len(got))
			if failed < 3 {
				a, b := firstDiff(want, got)
				t.Errorf("  first divergence:\n  want: %s\n  got : %s", a, b)
			}
			failed++
		}
		_ = strings.TrimSpace
	}
	if failed != 0 {
		t.Fatalf("%d/%d modules failed the mm ast-tree round trip", failed, len(files))
	}
	t.Logf("round-tripped %d std modules", len(files))
}

// TestASTTreeVersionGuard：版本不符必須硬錯，不靜默解出結構不同的樹。
func TestASTTreeVersionGuard(t *testing.T) {
	prog := parseFile(t, filepath.Join("..", "std", "vec.no"))
	nt, err := parser.EncodeASTTree(prog, "")
	if err != nil {
		t.Fatal(err)
	}
	nt.Ver = "1900-01-01.0"
	if _, err := parser.DecodeASTTree(nt); err == nil {
		t.Fatal("expected wire version mismatch error")
	}
	nt.Ver = parser.MMWireVersion
	nt.AstVer = "1900-01-01.0"
	if _, err := parser.DecodeASTTree(nt); err == nil {
		t.Fatal("expected AST version mismatch error")
	}
}

// firstDiff 返回 want/got 第一個不同行之附近的上下文（便於定位）。
func firstDiff(want, got string) (string, string) {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return a, b
		}
	}
	return "", ""
}
