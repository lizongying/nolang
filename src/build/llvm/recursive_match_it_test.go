package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestRecursiveMatchItTypeContamination verifies D13: recursive function + Optional
// match where `it` is used directly as a function parameter.
//
// The bug: when optionInnerTypes is not populated for the source variable of a
// synthetic `it` binding, varLLVMType fell back to g.varTypes["it"], which could
// be a wrong type from a previous match arm (e.g. %ws.server-conn from a
// ?ws.server-conn match). This caused "use of undefined value '%it'" LLVM errors.
//
// The fix: when optionInnerTypes is not populated, return "i64" (matching
// generateExprWithSB's default) instead of the potentially-wrong g.varTypes["it"].
func TestRecursiveMatchItTypeContamination(t *testing.T) {
	src := `
; Struct type simulating ws.server-conn
ws-conn {
    fd i64
    buf [1024]i8
}

ws-handle = (conn ws-conn) {
}

tool-read-file = (path str) (result ?str) {
    result = path
}

search-lines = (path str, content str, pattern str) (result str) {
    result = content
}

; Recursive function: matches on ?str and uses it as parameter
search-dir = (dir str, pattern str) (result ?str) {
    result = nil

    content = tool-read-file(dir)
    content: {
        nil -> return
        err -> return
        ->
            ; Use it directly as function parameter (the bug trigger)
            match-str = search-lines(dir, it, pattern)
            result = match-str
    }

    ; Recursive call to self
    sub = search-dir(dir, pattern)
    sub: {
        nil -> return
        err -> return
        -> result = it
    }
}

main = () {
    r = search-dir('test', 'pattern')
    r: {
        nil -> return
        err -> return
        -> print-line(it)
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

	// The `it` variable must NOT be typed as %ws-conn
	if strings.Contains(ir, "%it = alloca %ws-conn") {
		t.Errorf("IR should NOT allocate %%it as %%ws-conn (type contamination), got:\n%s", ir)
	}

	// Verify stores to %it don't use %ws-conn type
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store") && strings.Contains(trimmed, "%it") {
			if strings.Contains(trimmed, "%ws-conn") {
				t.Errorf("IR should not store %%ws-conn to %%it, got: %s", trimmed)
			}
		}
	}

	// Verify loads from %it don't use %ws-conn type
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "load") && strings.Contains(trimmed, "%it") {
			if strings.Contains(trimmed, "%ws-conn") {
				t.Errorf("IR should not load %%ws-conn from %%it, got: %s", trimmed)
			}
		}
	}
}

// TestRecursiveMatchItAsParam verifies basic recursive match with it as parameter
func TestRecursiveMatchItAsParam(t *testing.T) {
	src := `
read-file = (path str) (result ?str) {
    result = path
}

print-line = (content str) {
}

search-dir = (dir str, pattern str) (result ?str) {
    result = nil

    content = read-file(dir)
    content: {
        nil -> return
        err -> return
        ->
            print-line(it)
    }

    sub = search-dir(dir, pattern)
    sub: {
        nil -> return
        err -> return
        -> result = it
    }
}

main = () {
    r = search-dir('test', 'pattern')
    r: {
        nil -> return
        err -> return
        -> print-line(it)
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

	// Must have stores to %it with %str-long type
	storeCount := 0
	loadCount := 0
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store") && strings.Contains(trimmed, "%str-long") && strings.Contains(trimmed, "%it") {
			storeCount++
		}
		if strings.Contains(trimmed, "load") && strings.Contains(trimmed, "%str-long") && strings.Contains(trimmed, "%it") {
			loadCount++
		}
	}

	if storeCount == 0 {
		t.Errorf("IR should have at least one store of %%str-long to %%it, got:\n%s", ir)
	}
	if loadCount == 0 {
		t.Errorf("IR should have at least one load of %%str-long from %%it, got:\n%s", ir)
	}
}

// TestMultiMatchDifferentInnerTypes verifies that `it` shared across matches
// with different option inner types is correctly typed for each match arm
func TestMultiMatchDifferentInnerTypes(t *testing.T) {
	src := `
conn {
    fd i64
    data [256]i8
}

accept-conn = () (c ?conn) {
    c = nil
}

read-data = () (result ?str) {
    result = 'hello'
}

process = (data str) {
}

multi-match-fn = () (result ?str) {
    result = nil

    c = accept-conn()
    c: {
        nil -> return
        err -> return
        ->
            fd-val = it.fd
    }

    s = read-data()
    s: {
        nil -> return
        err -> return
        ->
            process(it)
    }
}

main = () {
    multi-match-fn()
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

	// For the ?str match, there must be a store of %str-long to %it
	hasStrLongStore := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "store") && strings.Contains(trimmed, "%str-long") && strings.Contains(trimmed, "%it") {
			hasStrLongStore = true
			break
		}
	}
	if !hasStrLongStore {
		t.Errorf("IR should have a store of %%str-long to %%it for the ?str match, got:\n%s", ir)
	}

	// For the ?str match, there must be a load of %str-long from %it
	hasStrLongLoad := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "load") && strings.Contains(trimmed, "%str-long") && strings.Contains(trimmed, "%it") {
			hasStrLongLoad = true
			break
		}
	}
	if !hasStrLongLoad {
		t.Errorf("IR should have a load of %%str-long from %%it for the ?str match, got:\n%s", ir)
	}
}
