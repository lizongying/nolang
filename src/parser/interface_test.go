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
