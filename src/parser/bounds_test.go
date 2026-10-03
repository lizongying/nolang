package parser

import (
	"testing"

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

// TestAnalyzeInBoundsIndexRemovable verifies the formatter-facing result: a
// statement whose every container read is provably in-bounds is marked
// StmtRemovable. Note: a standalone `#{index-out=0}` on its own line is ATTACHED
// to the following statement by the parser (the governed statement is the sole
// list entry), so removal flows through StmtRemovable; AnnRemovable only covers
// annotation nodes that survive as standalone statements (e.g. block-governed),
// which is exercised end-to-end by the fmt package tests.
func TestAnalyzeInBoundsIndexRemovable(t *testing.T) {
	prog := parseSurface(t, `f = () {
    p-tmp [32]byte
    p []byte = with-len(32)
    i <- [0..32): {
        #{index-out=0}
        p[i] = p-tmp[i]
    }
}`)
	res := AnalyzeInBoundsIndex(prog)
	if len(res.StmtRemovable) == 0 {
		t.Fatalf("expected at least one removable statement, got none")
	}
}

// TestAnalyzeInBoundsIndexNotRemovableForSlice: a slice read keeps its annotation.
func TestAnalyzeInBoundsIndexNotRemovableForSlice(t *testing.T) {
	prog := parseSurface(t, `f = (v []i64) (res i64) {
    i <- [0..4): {
        #{index-out=0}
        res = v[i]
    }
}`)
	res := AnalyzeInBoundsIndex(prog)
	if len(res.AnnRemovable) != 0 {
		t.Fatalf("slice read #{index-out} must NOT be removable, got %d", len(res.AnnRemovable))
	}
}
