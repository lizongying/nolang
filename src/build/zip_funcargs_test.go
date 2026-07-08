package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestValidateFuncArgsStructFieldSliceArg reproduces the issue where calling
// a function with `.data` (a []byte struct field accessed via self) is wrongly
// reported as "expected '[]byte', got 'byte'".
//
// Root cause: StructField.Type only stores the element type (e.g. "byte"),
// while IsSlice/ArraySize flags store the container info separately.
// Several places collected field types via f.Type.String() without considering
// IsSlice/ArraySize, causing []byte fields to be registered as "byte".
func TestValidateFuncArgsStructFieldSliceArg(t *testing.T) {
	src := `zip {
    data []byte
}

u32le = (data []byte, off i64) (v i64) {
    v = 0
}

zip.find-eocd = () (off i64) {
    off = -1
    n = len(.data)
    i = n - 22
    i >= 0: {
        sig = u32le(.data, i)
        sig == 101010256 -> {
            off = i
            return
        }
        i = i - 1
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "expected '[]byte', got 'byte'") {
			t.Errorf("false positive: %s", r.Message)
		}
	}
}

// TestValidateFuncArgsStructFieldArrayArg tests that [N]type struct fields
// are also correctly resolved (not just []type slices).
func TestValidateFuncArgsStructFieldArrayArg(t *testing.T) {
	src := `buf-struct {
    buf [64]byte
}

read-byte = (data [64]byte) (v i64) {
    v = 0
}

buf-struct.first = () (v i64) {
    v = read-byte(.buf)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "expected '[64]byte'") {
			t.Errorf("false positive: %s", r.Message)
		}
	}
}
