package checker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestFilterByExportsKeepsHelpersCalledFromNestedBlock guards the regression
// where a helper called only from inside a nested `{ ... }` if-group (parsed as
// a *parser.BlockStatement statement) was dropped from the export keep-set.
// filterByExports walks the bodies of exported functions to discover the
// transitive set of same-module callees that must be kept. Skipping
// BlockStatement statements made the walker blind to every call inside a
// nested arm: the helper was filtered out while its call site survived, and
// MIR codegen later failed with "unknown callee <helper> [4qhyyund]".
// Real-world trigger: noimg/src/jpeg.no jpeg-read-block-420 / -444, called
// from jpeg-encode-scan's subsampling arms.
func TestFilterByExportsKeepsHelpersCalledFromNestedBlock(t *testing.T) {
	dir := t.TempDir()
	libPath := filepath.Join(dir, "lib.no")
	modPath := filepath.Join(dir, "mod.no")
	if err := os.WriteFile(libPath, []byte("@ /mod.open\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := `open = (x i64) (r i64) {
	{
		x > 0 -> {
			{
				x > 1 -> {
					r = nested-helper(x)
				}

				-> {
					r = 0
				}
			}
		}

		-> {
			r = 0
		}
	}
	return
}
nested-helper = (x i64) (r i64) {
	r = x + 1
	return
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	// Sanity check: both definitions must be present before filtering.
	defs := make(map[string]bool)
	for _, st := range prog.Statements {
		if fd, ok := st.(*parser.FunctionDefinition); ok {
			defs[fd.Name] = true
		}
	}
	if !defs["open"] || !defs["nested-helper"] {
		t.Fatalf("test input is missing definitions: %v", defs)
	}

	filtered := filterByExports(prog, libPath, modPath)
	keep := make(map[string]bool)
	for _, st := range filtered.Statements {
		if fd, ok := st.(*parser.FunctionDefinition); ok {
			keep[fd.Name] = true
		}
	}
	if !keep["open"] {
		t.Fatal("exported function open was dropped")
	}
	if !keep["nested-helper"] {
		t.Fatal("helper called only from a nested block was dropped from the export keep-set")
	}
}
