package parser

import (
	"reflect"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
)

// parseSurface parses src keeping the surface AST (safe-index / unwrap lowering
// skipped), matching the `no fmt` path so AnalyzeInBoundsIndex sees the original
// IndexExpression nodes and their resolved types.
func parseSurface(t *testing.T, src string) *Program {
	t.Helper()
	p := New(lexer.New(src))
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func TestArrayStaticLen(t *testing.T) {
	cases := []struct {
		lt   string
		want int64
		ok   bool
	}{
		{"[32]byte", 32, true},
		{"[4]i64", 4, true},
		{"?i64", 0, false},   // option scalar
		{"[]byte", 0, false}, // slice: runtime length unknown
		{"[?]byte", 0, false},
		{"[n]i64", 0, false}, // symbolic size
		{"vec[i64]", 0, false},
		{"i64", 0, false},
	}
	for _, c := range cases {
		got, ok := arrayStaticLen(c.lt)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("arrayStaticLen(%q) = (%d,%v), want (%d,%v)", c.lt, got, ok, c.want, c.ok)
		}
	}
}

// TestProvablyInBoundsReads counts how many index reads a program proves
// in-bounds, to pin the conservative rule without depending on node pointers.
func TestProvablyInBoundsReads(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "fixed_array_literal",
			src: `f = () (res i64) {
    a [4]i64 = [1, 2, 3, 4]
    res = a[0]
}`,
			want: 1,
		},
		{
			name: "fixed_array_loop_index",
			src: `f = () (res i64) {
    a [8]i64 = [0, 1, 2, 3, 4, 5, 6, 7]
    i <- [0..8): {
        res = a[i]
    }
}`,
			want: 1,
		},
		{
			name: "slice_not_provable",
			src: `f = (v []i64) (res i64) {
    i <- [0..4): {
        res = v[i]
    }
}`,
			want: 0,
		},
		{
			// p-tmp[i] read is provable; the p[i] write target is not a read.
			name: "write_target_excluded_from_reads",
			src: `f = () {
    p-tmp [32]byte
    p []byte = with-len(32)
    i <- [0..32): {
        p[i] = p-tmp[i]
    }
}`,
			want: 1,
		},
		{
			name: "literal_out_of_range_not_provable",
			src: `f = () (res i64) {
    a [4]i64 = [1, 2, 3, 4]
    res = a[9]
}`,
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog := parseSurface(t, c.src)
			got := len(ProvablyInBoundsIndexReads(prog))
			if got != c.want {
				t.Errorf("ProvablyInBoundsIndexReads = %d, want %d\nsrc:\n%s", got, c.want, c.src)
			}
		})
	}
}

// TestAnalyzeInBoundsIndexReadNotRemovable verifies the formatter-facing result:
// under D7 final decision, a statement WITH a read-position index is NOT marked
// StmtRemovable even when that index is provably in-bounds — the #{index-out}
// annotation carries DEF-match consumption and must be preserved. (Statements
// with no read-position index at all, e.g. pure declarations, remain removable —
// that is exercised by TestAnalyzeInBoundsIndexPureWriteRemovableOnly.)
func TestAnalyzeInBoundsIndexReadNotRemovable(t *testing.T) {
	prog := parseSurface(t, `f = () {
    p-tmp [32]byte
    p []byte = with-len(32)
    i <- [0..32): {
        #{index-out=zero}
        p[i] = p-tmp[i]
    }
}`)
	res := AnalyzeInBoundsIndex(prog)
	// The for-loop body statement `p[i] = p-tmp[i]` has a read-position index
	// (p-tmp[i]) → must NOT be removable.
	for stmt := range res.StmtRemovable {
		if s, ok := stmt.(*ExpressionStatement); ok {
			if assign, ok := s.Expression.(*AssignExpression); ok {
				if _, isIdx := assign.Value.(*IndexExpression); isIdx {
					t.Errorf("statement with a read-position index must NOT be removable: %v", assign)
				}
			}
		}
	}
}

// TestAnalyzeInBoundsIndexPureWriteRemovableOnly: a pure write with no read-position
// index IS removable (dead annotation).
func TestAnalyzeInBoundsIndexPureWriteRemovableOnly(t *testing.T) {
	prog := parseSurface(t, `f = () {
    p []byte = with-len(32)
    #{index-out=zero}
    p[0] = 10
}`)
	res := AnalyzeInBoundsIndex(prog)
	if len(res.StmtRemovable) == 0 {
		t.Fatalf("pure-write statement should be removable, got none")
	}
}

// TestAnalyzeInBoundsIndexNotRemovableForSlice: a slice read keeps its annotation.
func TestAnalyzeInBoundsIndexNotRemovableForSlice(t *testing.T) {
	prog := parseSurface(t, `f = (v []i64) (res i64) {
    i <- [0..4): {
        #{index-out=zero}
        res = v[i]
    }
}`)
	res := AnalyzeInBoundsIndex(prog)
	if len(res.AnnRemovable) != 0 {
		t.Fatalf("slice read #{index-out} must NOT be removable, got %d", len(res.AnnRemovable))
	}
}

func TestInBoundsProofLivesInSemanticSideTable(t *testing.T) {
	if _, ok := reflect.TypeOf(IndexExpression{}).FieldByName("InBounds"); ok {
		t.Fatal("IndexExpression still carries semantic InBounds state")
	}

	prog := parseSurface(t, `f = () {
    a [3]i64 = [0, 0, 0]
    x = a[1]
}`)
	var idx *IndexExpression
	walkNodes(prog, func(n interface{}) {
		if e, ok := n.(*IndexExpression); ok {
			idx = e
		}
	})
	if idx == nil {
		t.Fatal("fixture did not produce an IndexExpression")
	}
	if !prog.Sem.IndexIsInBounds(idx) {
		t.Fatal("ParseProgram did not publish the in-bounds proof to Program.Sem")
	}

	pkg := ASTToHIR(prog)
	if pkg == nil {
		t.Fatal("ASTToHIR returned nil")
	}
	foundHIRProof := false
	for i := range pkg.Nodes {
		n := &pkg.Nodes[i]
		if n.Kind == hir.KIndex && n.Has(hir.FlagInBounds) {
			foundHIRProof = true
			break
		}
	}
	if !foundHIRProof {
		t.Fatal("ASTToHIR failed to transfer the semantic proof to hir.FlagInBounds")
	}

	// AnalyzeInBoundsIndex returns its proof set but must not write either the
	// syntax AST or the Program.Sem consumer table as a hidden side effect.
	prog.Sem.SetIndexInBounds(idx, false)
	res := AnalyzeInBoundsIndex(prog)
	if !res.Reads[idx] {
		t.Fatal("AnalyzeInBoundsIndex omitted the provably in-bounds read")
	}
	if prog.Sem.IndexIsInBounds(idx) {
		t.Fatal("AnalyzeInBoundsIndex mutated Program.Sem; callers must publish results explicitly")
	}

	unknown := parseSurface(t, `f = (i i64) {
    a [3]i64 = [0, 0, 0]
    x = a[i]
}`)
	unknownHIR := ASTToHIR(unknown)
	for i := range unknownHIR.Nodes {
		n := &unknownHIR.Nodes[i]
		if n.Kind == hir.KIndex && n.Has(hir.FlagInBounds) {
			t.Fatal("ASTToHIR marked an unproven index read as in-bounds")
		}
	}
}
