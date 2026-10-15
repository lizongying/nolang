package mmast

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	mm "github.com/metamessage/metamessage"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestWireCalibration（臨時校準）：對代表性模組逐提升根「單獨成片」實測 wire，
// 對比 parser.ASTNodeWireEstimate，校準結構開銷常量。
func TestWireCalibration(t *testing.T) {
	for _, rel := range []string{"vec.no", "json.no", "str.no", "net/tls.no", "magic.no"} {
		src, err := os.ReadFile(filepath.Join("..", "std", rel))
		if err != nil {
			t.Fatal(err)
		}
		p := parser.New(lexer.New(string(src)))
		prog := p.ParseProgram()
		if errs := p.Errors(); len(errs) > 0 {
			t.Fatalf("%s: %v", rel, errs)
		}
		tree, err := parser.EncodeASTTree(prog, "")
		if err != nil {
			t.Fatalf("%s: encode: %v", rel, err)
		}
		worstEst, worstAct, worstRatio := int64(0), 0, float64(0)
		total := 0
		for i, r := range tree.Roots {
			var buf bytes.Buffer
			wire, err := mm.EncodeFromValue(mmChunk{I: 0, Roots: []parser.ASTNode{*r}}, "")
			if err != nil {
				t.Fatalf("%s root %d: %v", rel, i, err)
			}
			_ = buf
			est := parser.ASTNodeWireEstimate(r)
			ratio := float64(len(wire)) / float64(est)
			total += len(wire)
			if len(wire) > worstAct {
				worstAct, worstEst = len(wire), est
			}
			if ratio > worstRatio {
				worstRatio = ratio
			}
		}
		t.Logf("%-14s roots=%4d total=%8d worst: est=%6d act=%6d ratio=%.2f maxratio=%.2f",
			rel, len(tree.Roots), total, worstEst, worstAct, float64(worstAct)/float64(worstEst), worstRatio)
	}
}
