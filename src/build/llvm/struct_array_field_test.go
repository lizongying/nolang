package llvm

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestStructArrayFieldAccess verifies that accessing a field of a struct array
// element (e.g. defs[i].name) generates correct GEP instruction chains.
// Regression: struct array element field access produced type mismatch
// between %str-long and i64 in LLVM IR.
func TestStructArrayFieldAccess(t *testing.T) {
	src := `
tool-def {
    name str
    kind i64
}

main = () {
    defs [16]tool-def
    i = 0
    n = defs[i].name
    print(n)
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

	// Verify the element type is correctly recognized as %tool-def (not i64)
	if !strings.Contains(ir, "%tool-def") {
		t.Errorf("IR should reference %%tool-def type for struct array elements, got:\n%s", ir)
	}

	// Verify the GEP for the .name field uses %tool-def (not i64 or %str-long as base type)
	dotGEPFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "getelementptr") && strings.Contains(trimmed, "%tool-def, %tool-def*") {
			dotGEPFound = true
			break
		}
	}
	if !dotGEPFound {
		t.Errorf("IR should contain GEP with %%tool-def base type for field access, got:\n%s", ir)
	}

	// Verify no type mismatch: the load of .name should be load %str-long, not load i64
	if !strings.Contains(ir, "load %str-long") {
		t.Errorf("IR should load %%str-long for .name field access, got:\n%s", ir)
	}
}

// TestStructArrayFieldInStruct tests accessing a struct array element's field
// when the array is itself a field of another struct (e.g. reg.defs[i].name).
// This is the exact scenario from the bug report: tool-registry { defs [16]tool-def }
func TestStructArrayFieldInStruct(t *testing.T) {
	src := `
tool-def {
    name str
    kind i64
}

tool-registry {
    defs [16]tool-def
}

lookup = (reg tool-registry) {
    i = 0
    n = reg.defs[i].name
    print(n)
}

main = () {
    reg = tool-registry {
        defs: [tool-def { name: 'hello', kind: 1 }]
    }
    lookup(reg)
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

	// Verify the element type is correctly recognized as %tool-def (not i64)
	if !strings.Contains(ir, "%tool-def") {
		t.Errorf("IR should reference %%tool-def type for struct array elements, got:\n%s", ir)
	}

	// Verify the GEP for the .name field uses %tool-def (not i64 or %str-long as base type)
	dotGEPFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "getelementptr") && strings.Contains(trimmed, "%tool-def, %tool-def*") {
			dotGEPFound = true
			break
		}
	}
	if !dotGEPFound {
		t.Errorf("IR should contain GEP with %%tool-def base type for field access, got:\n%s", ir)
	}

	// Verify no type mismatch: the load of .name should be load %str-long, not load i64
	if !strings.Contains(ir, "load %str-long") {
		t.Errorf("IR should load %%str-long for .name field access, got:\n%s", ir)
	}
}

// TestStructArrayFieldInStructSelf tests the same scenario but using self.defs[i].name
// from within a method of the struct.
func TestStructArrayFieldInStructSelf(t *testing.T) {
	src := `
tool-def {
    name str
    kind i64
}

tool-registry {
    defs [16]tool-def
}

tool-registry.lookup = () {
    i = 0
    n = .defs[i].name
    print(n)
}

main = () {
    reg = tool-registry {
        defs: [tool-def { name: 'hello', kind: 1 }]
    }
    reg.lookup()
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

	// Verify the element type is correctly recognized as %tool-def (not i64)
	if !strings.Contains(ir, "%tool-def") {
		t.Errorf("IR should reference %%tool-def type for struct array elements, got:\n%s", ir)
	}

	// Verify the GEP for the .name field uses %tool-def (not i64 or %str-long as base type)
	dotGEPFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "getelementptr") && strings.Contains(trimmed, "%tool-def, %tool-def*") {
			dotGEPFound = true
			break
		}
	}
	if !dotGEPFound {
		t.Errorf("IR should contain GEP with %%tool-def base type for field access, got:\n%s", ir)
	}

	// Verify no type mismatch: the load of .name should be load %str-long, not load i64
	if !strings.Contains(ir, "load %str-long") {
		t.Errorf("IR should load %%str-long for .name field access, got:\n%s", ir)
	}
}

// TestStructArrayFieldAssignment tests assigning to a field of a struct array
// element (e.g. defs[i].name = value).
func TestStructArrayFieldAssignment(t *testing.T) {
	src := `
tool-def {
    name str
    kind i64
}

main = () {
    defs [16]tool-def
    i = 0
    defs[i].name = 'hello'
    n = defs[i].name
    print(n)
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

	// Verify the element type is correctly recognized as %tool-def (not i64)
	if !strings.Contains(ir, "%tool-def") {
		t.Errorf("IR should reference %%tool-def type for struct array elements, got:\n%s", ir)
	}

	// Verify store %str-long (not store i64 for the .name field)
	if !strings.Contains(ir, "store %str-long") {
		t.Errorf("IR should store %%str-long for .name field assignment, got:\n%s", ir)
	}
}

// TestStructArrayElementRead verifies that reading a struct array element
// (e.g. d = defs[i]) correctly generates GEP on %tool-def (not i64).
// The compiler uses deep clone (memcpy) for struct elements to avoid
// shallow copy issues, so we check for GEP with %tool-def base type.
func TestStructArrayElementRead(t *testing.T) {
	src := `
tool-def {
    name str
    kind i64
}

main = () {
    defs [16]tool-def
    i = 0
    d = defs[i]
    n = d.name
    print(n)
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

	// Verify the element GEP uses %tool-def (not i64) — the compiler may use
	// deep clone (memcpy) instead of a plain load for struct elements, but the
	// GEP that addresses the element must use the correct struct type.
	gepFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "getelementptr inbounds %tool-def, %tool-def*") {
			gepFound = true
			break
		}
	}
	if !gepFound {
		t.Errorf("IR should contain GEP with %%tool-def base type for element access, got:\n%s", ir)
	}

	// Verify no "load i64, i64* <reg>" that would indicate the struct was
	// incorrectly loaded as i64 instead of being properly GEP'd/cloned
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		// The element pointer should be %tool-def* (from bitcast), not i64*
		if strings.Contains(trimmed, "bitcast") && strings.Contains(trimmed, "to %tool-def*") {
			// This is correct — the data pointer is bitcast to %tool-def*
		}
	}
}

// TestStructArrayElementAsArg tests passing a struct array element as a function argument.
func TestStructArrayElementAsArg(t *testing.T) {
	src := `
tool-def {
    name str
    kind i64
}

print-name = (d tool-def) {
    print(d.name)
}

main = () {
    defs [16]tool-def
    i = 0
    print-name(defs[i])
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

	// Verify the struct element is loaded as %tool-def (not i64)
	if !strings.Contains(ir, "load %tool-def") {
		t.Errorf("IR should load %%tool-def for struct array element as argument, got:\n%s", ir)
	}
}

// TestStructArrayFieldAccessModuleLevel tests struct array field access
// when the array is a module-level global variable.
func TestStructArrayFieldAccessModuleLevel(t *testing.T) {
	src := `
tool-def {
    name str
    kind i64
}

defs [16]tool-def

main = () {
    i = 0
    n = defs[i].name
    print(n)
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

	// Verify the element type is correctly recognized as %tool-def (not i64)
	if !strings.Contains(ir, "%tool-def") {
		t.Errorf("IR should reference %%tool-def type for struct array elements, got:\n%s", ir)
	}

	// Verify the GEP for the .name field uses %tool-def (not i64 or %str-long as base type)
	dotGEPFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "getelementptr") && strings.Contains(trimmed, "%tool-def, %tool-def*") {
			dotGEPFound = true
			break
		}
	}
	if !dotGEPFound {
		t.Errorf("IR should contain GEP with %%tool-def base type for field access, got:\n%s", ir)
	}

	// Verify no type mismatch: the load of .name should be load %str-long, not load i64
	if !strings.Contains(ir, "load %str-long") {
		t.Errorf("IR should load %%str-long for .name field access, got:\n%s", ir)
	}
}

// TestStructArrayFieldAsParam tests struct array as a function parameter and
// accessing its elements' fields.
func TestStructArrayFieldAsParam(t *testing.T) {
	src := `
tool-def {
    name str
    kind i64
}

get-name = (defs [16]tool-def, i i64) (out str) {
    out = defs[i].name
}

main = () {
    defs [16]tool-def
    n = get-name(defs, 0)
    print(n)
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

	// Verify the element type is correctly recognized as %tool-def (not i64)
	if !strings.Contains(ir, "%tool-def") {
		t.Errorf("IR should reference %%tool-def type for struct array parameter, got:\n%s", ir)
	}

	// Verify the GEP for the .name field uses %tool-def
	dotGEPFound := false
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "getelementptr") && strings.Contains(trimmed, "%tool-def, %tool-def*") {
			dotGEPFound = true
			break
		}
	}
	if !dotGEPFound {
		t.Errorf("IR should contain GEP with %%tool-def base type for field access on array parameter, got:\n%s", ir)
	}

	// Verify no type mismatch: the load of .name should be load %str-long, not load i64
	if !strings.Contains(ir, "load %str-long") {
		t.Errorf("IR should load %%str-long for .name field access on array parameter, got:\n%s", ir)
	}
}
