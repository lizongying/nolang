package parser

import (
	"bytes"
	"encoding/gob"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestBuildSemViewsRestoresGrowWrite pins the NodeFacts.GrowWrite rebuild.
//
// A place-write `a[i] ?= v` desugars into a synthetic
// `let __match_subj_L_C = a[__idx_L_C]`. emitSubjectMatchPlain marks that
// fresh IndexExpression as the write subject with SetGrowWrite(true), and the
// H0 path (tohir.go) does the same for its own fresh node. The fact lives in
// a node-keyed side table that gob deliberately does NOT carry, so
// BuildSemViews must rebuild it from the tree. Without the rebuild, ASTToHIR
// drops hir.FlagGrowWrite and the decoded program's HIR diverges from a fresh
// parse (real regression on std/heap.no: 3 facts = 45 bytes).
//
// Self-contained on purpose: the corpus sweep only catches this while some
// std file happens to contain a `?=` write target.
func TestBuildSemViewsRestoresGrowWrite(t *testing.T) {
	// Must be a plain `?=` in a function with an option result parameter:
	// inside a block match the expansion is delegated to H0's
	// lowerBlockMatchToHIR, so the parser AST carries no __match_subj_*
	// binding and there is no node fact to rebuild.
	const src = `h {
    data [4]i64
}

h.set = () (ok ?i64) {
    .data[0] ?= 7
    ok = nil
}
`
	cfg := SemRebuildConfig{Filename: "growwrite.no"}
	p := New(lexer.NewCached("growwrite.no", src))
	cfg.Apply(p)
	fresh := p.ParseProgram()
	if fresh == nil || len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	countGrow := func(prog *Program) int {
		n := 0
		walkProgramNodes(prog, func(nd Node) {
			if prog.Sem.GrowWriteOf(nd) {
				n++
			}
		})
		return n
	}
	if n := countGrow(fresh); n == 0 {
		t.Fatal("anti-vacuity: fixture produced no GrowWrite fact")
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&CachedProgram{Prog: fresh}); err != nil {
		t.Fatal(err)
	}
	var decoded CachedProgram
	if err := gob.NewDecoder(bytes.NewReader(buf.Bytes())).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	BuildSemViews(decoded.Prog, cfg)

	if want, got := countGrow(fresh), countGrow(decoded.Prog); want != got {
		t.Errorf("GrowWrite facts: fresh=%d rebuilt=%d", want, got)
	}
	if want, got := ASTToHIR(fresh).Dump(), ASTToHIR(decoded.Prog).Dump(); want != got {
		t.Errorf("HIR diverged after decode+rebuild (%d vs %d bytes)", len(want), len(got))
	}
}
