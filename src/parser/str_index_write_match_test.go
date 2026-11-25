package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestPlaceMatchIndexWriteStrRejected pins the rule that a `str` (and `txt`)
// container cannot be written through a place-match. Direct `s[i] = v` is
// rejected by the checker's `idxwrite`; the D9 place-match form was the only
// remaining write path and silently bit-copied a `str` RHS into a char slot
// (measured: `v str = 'b'` -> `s[i]: { ok -> it = v }` wrote 0x01 bytes).
//
// The rejection must happen at parse/lowering time, before the synthetic
// index store is emitted, and must name the allowed alternatives.
func TestPlaceMatchIndexWriteStrRejected(t *testing.T) {
	src := `probe = () {
    s str = ' '.repeat(4)
    i <- [0..4): {
        v str = 'b'
        s[i]: {
            ok -> it = v
            -> _
        }
    }
}
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	p.DesugarSurfaceMatchesPlain(prog)
	found := false
	for _, e := range p.Errors() {
		if strings.Contains(e, "stridxwr") {
			found = true
			if !strings.Contains(e, "push") || !strings.Contains(e, "fill") || !strings.Contains(e, "to-str") {
				t.Fatalf("diagnostic must list allowed alternatives, got: %s", e)
			}
		}
	}
	if !found {
		t.Fatalf("expected stridxwr diagnostic for str place-match write, got: %v", p.Errors())
	}
}

// TestPlaceMatchIndexWriteOtherContainersAllowed guards against over-rejection:
// non-string positional containers keep their D9 place-write path.
func TestPlaceMatchIndexWriteOtherContainersAllowed(t *testing.T) {
	src := `probe = () {
    b []byte = [32, 32, 32, 32]
    a []i64 = [1, 2, 3, 4]
    i <- [0..4): {
        b[i]: {
            ok -> it = 98
            -> _
        }
        a[i]: {
            ok -> it = 7
            -> _
        }
    }
}
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	p.DesugarSurfaceMatchesPlain(prog)
	for _, e := range p.Errors() {
		if strings.Contains(e, "stridxwr") {
			t.Fatalf("stridxwr must not fire for []byte / []i64: %s", e)
		}
	}
}

// TestPlaceMatchIndexReadStrAllowed: a read-only option match on a str element
// must stay valid — only the `it = v` write form is forbidden.
func TestPlaceMatchIndexReadStrAllowed(t *testing.T) {
	src := `probe = () {
    s = 'abc'
    s[0]: {
        ok -> print(it)
        -> _
    }
}
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	p.DesugarSurfaceMatchesPlain(prog)
	for _, e := range p.Errors() {
		if strings.Contains(e, "stridxwr") {
			t.Fatalf("stridxwr must not fire for a read-only match: %s", e)
		}
	}
}

// TestUnwrapAssignIndexWriteStrRejected pins the `s[i] ?= v` write path.
// lowerUnwrapAssignToIndex builds a SurfaceMatch and routes it through
// rewriteItPlaceWrites, so the same str/txt ban must fire on the build path
// (not just in the checker's surface-AST pass used by no vet).
func TestUnwrapAssignIndexWriteStrRejected(t *testing.T) {
	src := `probe = () (out ?char) {
    out = nil
    s str = 'abcd'
    s[1] ?= "Z"
}
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	p.DesugarSurfaceMatchesPlain(prog)
	found := false
	for _, e := range p.Errors() {
		if strings.Contains(e, "stridxwr") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected stridxwr diagnostic for str `?=` write, got: %v", p.Errors())
	}
}

// TestUnwrapAssignIndexWriteNonStrAllowed guards the flip side: `a[i] ?= v`
// on non-str/txt containers remains the D9-recommended write path.
func TestUnwrapAssignIndexWriteNonStrAllowed(t *testing.T) {
	src := `probe = () (out ?i64) {
    out = nil
    a [3]i64 = [1, 2, 3]
    a[1] ?= 7
}
`
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	p.DesugarSurfaceMatchesPlain(prog)
	for _, e := range p.Errors() {
		if strings.Contains(e, "stridxwr") {
			t.Fatalf("stridxwr must not fire for []i64 `?=` write: %s", e)
		}
	}
}
