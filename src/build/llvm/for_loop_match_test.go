package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestForLoopNestedOptionalMatch verifies that a for loop with a nested
// optional match (content: { nil -> {} err -> {} -> ... }) correctly
// allocates and uses the `it` variable in LLVM IR.
// Regression: the `it` variable was undefined when the match was
// nested inside a for loop body.
func TestForLoopNestedOptionalMatch(t *testing.T) {
	src := `
read-file = (path str) (out ?str) {
    out = path
}

main = () {
    i <- [0..3): {
        content = read-file('test')
        content: {
            nil -> {
                print('nil')
            }
            err -> {
                print('err')
            }
            ->
                file-content = it
                print(file-content)
        }
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The `it` variable must be allocated (alloca)
	if !strings.Contains(ir, "%it = alloca") {
		t.Errorf("IR should allocate %%it variable, got:\n%s", ir)
	}

	// The `it` variable must be stored to in the ok arm
	// (store %str-long ... %str-long* %it)
	storeToItFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store") && strings.Contains(trimmed, "%str-long* %it") {
			storeToItFound = true
			break
		}
	}
	if !storeToItFound {
		t.Errorf("IR should store to %%it in the ok arm, got:\n%s", ir)
	}

	// The `it` variable must be loaded from in the ok arm body
	// (file-content = it → load %str-long, %str-long* %it)
	loadFromItFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "load") && strings.Contains(trimmed, "%str-long* %it") {
			loadFromItFound = true
			break
		}
	}
	if !loadFromItFound {
		t.Errorf("IR should load from %%it in the ok arm body, got:\n%s", ir)
	}
}

// TestForLoopNestedOptionalMatchEmptyArms tests the same scenario but with
// empty arm bodies for nil and err arms (matching the user's exact code pattern).
func TestForLoopNestedOptionalMatchEmptyArms(t *testing.T) {
	src := `
read-file = (path str) (out ?str) {
    out = path
}

main = () {
    i <- [0..3): {
        content = read-file('test')
        content: {
            nil -> {}
            err -> {}
            ->
                file-content = it
                print(file-content)
        }
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The `it` variable must be allocated
	if !strings.Contains(ir, "%it = alloca") {
		t.Errorf("IR should allocate %%it variable, got:\n%s", ir)
	}

	// The `it` variable must be stored to in the ok arm
	storeToItFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store") && strings.Contains(trimmed, "%str-long* %it") {
			storeToItFound = true
			break
		}
	}
	if !storeToItFound {
		t.Errorf("IR should store to %%it in the ok arm, got:\n%s", ir)
	}
}

// TestCStyleForLoopNestedOptionalMatch tests the same scenario with a
// C-style for loop (for i = 0; i < n; i = i + 1).
func TestCStyleForLoopNestedOptionalMatch(t *testing.T) {
	src := `
read-file = (path str) (out ?str) {
    out = path
}

main = () {
    n = 3
    for i = 0; i < n; i = i + 1: {
        content = read-file('test')
        content: {
            nil -> {
                print('nil')
            }
            err -> {
                print('err')
            }
            ->
                file-content = it
                print(file-content)
        }
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The `it` variable must be allocated
	if !strings.Contains(ir, "%it = alloca") {
		t.Errorf("IR should allocate %%it variable, got:\n%s", ir)
	}
}

// TestModuleLevelOptionalMatch tests that `it` is correctly allocated
// for a module-level optional match (not inside a function).
func TestModuleLevelOptionalMatch(t *testing.T) {
	src := `
read-file = (path str) (out ?str) {
    out = path
}

content = read-file('test')
content: {
    nil -> {
        print('nil')
    }
    err -> {
        print('err')
    }
    ->
        print(it)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	g := NewGenerator()
	ir := g.Generate(prog)

	// The `it` variable must be allocated
	if !strings.Contains(ir, "%it = alloca") {
		t.Errorf("IR should allocate %%it variable for module-level match, got:\n%s", ir)
	}
}
