package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestParseArrayTypeMethodDefinitionNullableSlice verifies that
// `[?]t.method(…)` is recognized as an array-type method definition
// and the `[?]` prefix is preserved in the function name. Before the
// fix, the parser stripped the `[?]` and the function name was just
// `t.method`, which caused the formatter to output `[nil]t.method`
// for the receiver type.
func TestParseArrayTypeMethodDefinitionNullableSlice(t *testing.T) {
	src := `[?]ord.ast = () (res [?]ord) {
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	fd, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected *FunctionDefinition, got %T", prog.Statements[0])
	}
	if fd.Name != "[?]ord.ast" {
		t.Errorf("expected name '[?]ord.ast', got %q", fd.Name)
	}
	if len(fd.Parameters) != 1 || fd.Parameters[0].Name != "self" {
		t.Fatalf("expected 1 self param, got %d", len(fd.Parameters))
	}
	if fd.Parameters[0].Type == nil || fd.Parameters[0].Type.String() != "[?]ord" {
		t.Errorf("expected self type '[?]ord', got %v", fd.Parameters[0].Type)
	}
	if len(fd.Results) != 1 || fd.Results[0].Type.String() != "[?]ord" {
		t.Errorf("expected result type '[?]ord', got %v", fd.Results[0].Type)
	}
}

// TestParseInterfaceMethodGenericReceiver verifies that an interface can
// declare a method with a generic-receiver form `t.method(params) (results)`,
func TestParseInterfaceMethodGenericReceiver(t *testing.T) {
	src := `ord {
    t.gt(b t) (res bool)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	id, ok := prog.Statements[0].(*InterfaceDefinition)
	if !ok {
		t.Fatalf("expected *InterfaceDefinition, got %T", prog.Statements[0])
	}
	if id.Name != "ord" {
		t.Errorf("expected interface name 'ord', got %q", id.Name)
	}
	if len(id.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(id.Methods))
	}
	m := id.Methods[0]
	if m.Name != "gt" {
		t.Errorf("expected method name 'gt', got %q", m.Name)
	}
	if !m.IsGenericReceiver {
		t.Errorf("expected IsGenericReceiver=true, got false")
	}
	if m.Receiver != "t" {
		t.Errorf("expected Receiver 't', got %q", m.Receiver)
	}
	if len(m.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(m.Parameters))
	}
	if m.Parameters[0].Name != "b" || m.Parameters[0].Type.String() != "t" {
		t.Errorf("expected param (b t), got (%s %s)", m.Parameters[0].Name, m.Parameters[0].Type.String())
	}
	if len(m.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(m.Results))
	}
	if m.Results[0].Name != "res" || m.Results[0].Type.String() != "bool" {
		t.Errorf("expected result (res bool), got (%s %s)", m.Results[0].Name, m.Results[0].Type.String())
	}
}

// TestParseInterfaceWithImplements verifies that an interface can inherit
// from other interfaces using the `name iface1, iface2 { ... }` syntax,
// e.g.:
//
//	db enter, leave {
//	    close() (ok bool)
//	    query(sql str) (rs rows)
//	}
//
// This is interface inheritance/merging — the interface `db` inherits the
// method signatures from `enter` and `leave` and adds its own methods.
func TestParseInterfaceWithImplements(t *testing.T) {
	src := `db enter, leave {
    close() (ok bool)
    query(sql str) (rs rows)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	id, ok := prog.Statements[0].(*InterfaceDefinition)
	if !ok {
		t.Fatalf("expected *InterfaceDefinition, got %T", prog.Statements[0])
	}
	if id.Name != "db" {
		t.Errorf("expected interface name 'db', got %q", id.Name)
	}
	if len(id.Implements) != 2 {
		t.Fatalf("expected 2 implemented interfaces, got %d", len(id.Implements))
	}
	if id.Implements[0] != "enter" || id.Implements[1] != "leave" {
		t.Errorf("expected implements [enter, leave], got %v", id.Implements)
	}
	if len(id.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(id.Methods))
	}
	if id.Methods[0].Name != "close" {
		t.Errorf("expected first method 'close', got %q", id.Methods[0].Name)
	}
	if id.Methods[1].Name != "query" {
		t.Errorf("expected second method 'query', got %q", id.Methods[1].Name)
	}
}

// TestParseInterfaceSingleImplements verifies interface inheritance with
// a single inherited interface.
func TestParseInterfaceSingleImplements(t *testing.T) {
	src := `writer flush {
    write(data str) (n i64)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	id, ok := prog.Statements[0].(*InterfaceDefinition)
	if !ok {
		t.Fatalf("expected *InterfaceDefinition, got %T", prog.Statements[0])
	}
	if id.Name != "writer" {
		t.Errorf("expected interface name 'writer', got %q", id.Name)
	}
	if len(id.Implements) != 1 || id.Implements[0] != "flush" {
		t.Errorf("expected implements [flush], got %v", id.Implements)
	}
	if len(id.Methods) != 1 || id.Methods[0].Name != "write" {
		t.Errorf("expected 1 method 'write', got %v", id.Methods)
	}
}

// TestParseStructWithImplementsStillWorks verifies that struct-with-implements
// syntax (`name iface { fields }`) is still parsed as a struct, not an interface.
func TestParseStructWithImplementsStillWorks(t *testing.T) {
	src := `file enter, leave {
    path str
    fd i64
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	sd, ok := prog.Statements[0].(*StructDefinition)
	if !ok {
		t.Fatalf("expected *StructDefinition, got %T", prog.Statements[0])
	}
	if sd.Name != "file" {
		t.Errorf("expected struct name 'file', got %q", sd.Name)
	}
	if len(sd.Implements) != 2 {
		t.Fatalf("expected 2 implemented interfaces, got %d", len(sd.Implements))
	}
	if sd.Implements[0] != "enter" || sd.Implements[1] != "leave" {
		t.Errorf("expected implements [enter, leave], got %v", sd.Implements)
	}
	if len(sd.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sd.Fields))
	}
}

// TestParseInterfaceMethodWithResult verifies that interface methods
// can declare a return type using `(res type)` after the parameter
// list, e.g.:
//
//	ord {
//	    gt(x t) (res bool)
//	}
//
// Before the fix, the parser rejected the trailing `(res type)` with
// "expected method name in interface, got LPAREN", which caused
// `bool` to leak out as a free-standing identifier and get flagged
// as undefined by the validator.
func TestParseInterfaceMethodWithResult(t *testing.T) {
	src := `ord {
    gt(x t) (res bool)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	id, ok := prog.Statements[0].(*InterfaceDefinition)
	if !ok {
		t.Fatalf("expected *InterfaceDefinition, got %T", prog.Statements[0])
	}
	if id.Name != "ord" {
		t.Errorf("expected interface name 'ord', got %q", id.Name)
	}
	if len(id.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(id.Methods))
	}
	m := id.Methods[0]
	if m.Name != "gt" {
		t.Errorf("expected method name 'gt', got %q", m.Name)
	}
	if len(m.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(m.Parameters))
	}
	if m.Parameters[0].Name != "x" || m.Parameters[0].Type.String() != "t" {
		t.Errorf("expected param (x t), got (%s %s)", m.Parameters[0].Name, m.Parameters[0].Type.String())
	}
	if len(m.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(m.Results))
	}
	if m.Results[0].Name != "res" || m.Results[0].Type.String() != "bool" {
		t.Errorf("expected result (res bool), got (%s %s)", m.Results[0].Name, m.Results[0].Type.String())
	}
}
