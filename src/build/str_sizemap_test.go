package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStrLenReadOnly verifies that str.len cannot be modified
func TestStrLenReadOnly(t *testing.T) {
	src := `
test = () {
	s = 'hello'
	s.len = 10
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// read-only check is in validateArrayBounds（使用合併後的單次遍歷）
	sizeMap := buildSizeMaps(prog)
	varTypes := map[string]string{}
	err := validateArrayBounds(prog, sizeMap.arraySizes, sizeMap.sliceSizes, sizeMap.stringSizes, varTypes)
	if err == nil {
		t.Fatal("expected read-only error for str.len assignment, but got none")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected read-only error, got: %v", err)
	}
}
