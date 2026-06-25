package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestValidateUnionTypes(t *testing.T) {
	src := `int i8 | i16 | i32 | i64
float f32 | f64
num int | float
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	aliases, results := ValidateUnionTypes(prog)
	if len(results) > 0 {
		t.Errorf("unexpected validate results: %v", results)
	}
	if len(aliases) != 3 {
		t.Errorf("expected 3 aliases, got %d", len(aliases))
	}
	if _, ok := aliases["num"]; !ok {
		t.Errorf("expected 'num' alias")
	}
	if _, ok := aliases["int"]; !ok {
		t.Errorf("expected 'int' alias")
	}
}

func TestFlattenUnionChain(t *testing.T) {
	src := `int i8 | i16 | i32 | i64
float f32 | f64
num int | float
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	aliases, _ := ValidateUnionTypes(prog)
	members := FlattenUnion("num", aliases)
	// Should expand num → int|float → i8|i16|i32|i64|f32|f64
	if len(members) != 6 {
		t.Errorf("expected 6 flattened members, got %d: %v", len(members), members)
	}
}

func TestMonomorphizeUnionFunction(t *testing.T) {
	src := `int i8 | i16 | i32 | i64
num int
max = (a ..num) (r num) {
    r = a
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	// ValidateUnionTypes is needed to populate VariadicUnion.
	ValidateUnionTypes(prog)
	// Confirm the function got tagged.
	var fd *parser.FunctionDefinition
	for _, s := range prog.Statements {
		if f, ok := s.(*parser.FunctionDefinition); ok {
			if f.Name == "max" {
				fd = f
			}
		}
	}
	if fd == nil {
		t.Fatalf("max not found")
	}
	// `num` is a single-type alias to `int`, so VariadicUnion stays empty;
	// but FlattenUnion will resolve `num` → `int` → {i8, i16, i32, i64}.
	// To force the variadic to monomorphize, we can mark VariadicUnion
	// explicitly here to simulate "num" being a union.
	fd.VariadicUnion = "num"
	monomorphizeUnions(prog)
	// After monomorphization: max__num_TEMPLATE, max__i8, max__i16, max__i32, max__i64
	names := make(map[string]bool)
	for _, s := range prog.Statements {
		if fd, ok := s.(*parser.FunctionDefinition); ok {
			names[fd.Name] = true
		}
	}
	if !names["max__num_TEMPLATE"] {
		t.Errorf("expected max__num_TEMPLATE")
	}
	for _, m := range []string{"max__i8", "max__i16", "max__i32", "max__i64"} {
		if !names[m] {
			t.Errorf("expected %s", m)
		}
	}
}

func TestUnionTypeAliasDuplicate(t *testing.T) {
	src := `int i8
int i64
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	_, results := ValidateUnionTypes(prog)
	if len(results) != 1 {
		t.Errorf("expected 1 duplicate error, got %d: %v", len(results), results)
	}
}

// TestGenericUnionMonomorphize verifies that a non-variadic function whose
// parameters and results all use the same union alias is monomorphized into
// one concrete function per union member. The result types must be
// specialized to the concrete member (e.g. i8), not the union alias.
func TestGenericUnionMonomorphize(t *testing.T) {
	src := `int i8 | i16 | i32 | i64
float f32 | f64
num int | float
abs = (a num) (r num) {
    r = a
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	// ValidateUnionTypes sets GenericUnion for non-variadic functions
	// whose params/results are all the same union alias.
	ValidateUnionTypes(prog)

	// Confirm abs is tagged.
	var abs *parser.FunctionDefinition
	for _, s := range prog.Statements {
		if f, ok := s.(*parser.FunctionDefinition); ok {
			if f.Name == "abs" {
				abs = f
			}
		}
	}
	if abs == nil {
		t.Fatalf("abs not found")
	}
	if abs.GenericUnion != "num" {
		t.Errorf("expected GenericUnion=num, got %q", abs.GenericUnion)
	}

	monomorphizeUnions(prog)
	names := make(map[string]string) // name → result type
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var rt string
		if len(fd.Results) > 0 {
			rt = fd.Results[0].Type.String()
		}
		names[fd.Name] = rt
	}
	// Check concrete monomorphized functions with correct result type
	for _, mem := range []string{"i8", "i16", "i32", "i64", "f32", "f64"} {
		name := "abs__" + mem
		rt, ok := names[name]
		if !ok {
			t.Errorf("missing monomorphized function: %s", name)
			continue
		}
		if rt != mem {
			t.Errorf("abs__%s result type = %q, want %q", mem, rt, mem)
		}
	}
	// Check the template marker exists
	if _, ok := names["abs__num_TEMPLATE"]; !ok {
		t.Errorf("expected abs__num_TEMPLATE")
	}
}

// TestMonomorphizeVariadicMax verifies that max = (a ..num) (r num) { ... }
// expands into one concrete max function per num member (i8, i16, ..., f64).
// It also verifies the template is renamed, parameter slice element type
// becomes the concrete member, and result type is the concrete member.
func TestMonomorphizeVariadicMax(t *testing.T) {
	src := `int i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float f32 | f64
num int | float
max = (a ..num) (r num) {
    r = a[0]
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	type sig struct {
		paramSliceElem string
		resultType     string
	}
	got := map[string]sig{}
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var sliceElem, rt string
		if fd.IsVariadic && len(fd.Parameters) > 0 {
			if st, ok := fd.Parameters[len(fd.Parameters)-1].Type.(*parser.SliceType); ok {
				if nt, ok := st.Elem.(*parser.NamedType); ok {
					sliceElem = nt.Value
				}
			}
		}
		if len(fd.Results) > 0 {
			if nt, ok := fd.Results[0].Type.(*parser.NamedType); ok {
				rt = nt.Value
			}
		}
		got[fd.Name] = sig{paramSliceElem: sliceElem, resultType: rt}
	}

	want := []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64"}
	for _, m := range want {
		name := "max__" + m
		s, ok := got[name]
		if !ok {
			t.Errorf("missing monomorphized function: %s", name)
			continue
		}
		if s.paramSliceElem != m {
			t.Errorf("%s slice elem = %q, want %q", name, s.paramSliceElem, m)
		}
		if s.resultType != m {
			t.Errorf("%s result = %q, want %q", name, s.resultType, m)
		}
	}
	// Template should be renamed with __TEMPLATE suffix.
	if _, ok := got["max__num_TEMPLATE"]; !ok {
		t.Errorf("expected max__num_TEMPLATE")
	}
}

// TestMonomorphizeGenericSign verifies that sign = (a num) (r num) { ... }
// (non-variadic generic) generates per-member concrete versions with both
// the parameter and result type specialized.
func TestMonomorphizeGenericSign(t *testing.T) {
	src := `int i8 | i16 | i32 | i64
float f32 | f64
num int | float
sign = (a num) (r num) {
    r = 0
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	type sig struct {
		param, result string
	}
	got := map[string]sig{}
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var p, r string
		if len(fd.Parameters) > 0 {
			if nt, ok := fd.Parameters[0].Type.(*parser.NamedType); ok {
				p = nt.Value
			}
		}
		if len(fd.Results) > 0 {
			if nt, ok := fd.Results[0].Type.(*parser.NamedType); ok {
				r = nt.Value
			}
		}
		got[fd.Name] = sig{param: p, result: r}
	}

	for _, m := range []string{"i8", "i16", "i32", "i64", "f32", "f64"} {
		name := "sign__" + m
		s, ok := got[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		if s.param != m {
			t.Errorf("%s param = %q, want %q", name, s.param, m)
		}
		if s.result != m {
			t.Errorf("%s result = %q, want %q", name, s.result, m)
		}
	}
}

// TestMonomorphizeIntegerOnlyGeneric verifies that `even` / `odd` / `pow`
// functions declared as (a int) are only monomorphized for integer members
// (not floats), since `int` union does not include float types.
func TestMonomorphizeIntegerOnlyGeneric(t *testing.T) {
	src := `int i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float f32 | f64
num int | float
even = (a int) (yes int) {
    yes = 0
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	got := map[string]bool{}
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		got[fd.Name] = true
	}
	// even should be expanded for all 8 integer members, NOT for floats
	for _, m := range []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64"} {
		name := "even__" + m
		if !got[name] {
			t.Errorf("expected %s", name)
		}
	}
	for _, m := range []string{"f32", "f64"} {
		name := "even__" + m
		if got[name] {
			t.Errorf("unexpected %s (even should not be monomorphized for floats)", name)
		}
	}
	if !got["even__int_TEMPLATE"] {
		t.Errorf("expected even__int_TEMPLATE")
	}
}

// TestMonomorphizeRecursiveCallInBody verifies that when a generic function
// recursively calls itself, the monomorphized body's call is renamed from
// the original generic name to the concrete monomorphized name.
func TestMonomorphizeRecursiveCallInBody(t *testing.T) {
	src := `int i8 | i16
float f32 | f64
num int | float
gcd = (a num, b num) (r num) {
    if b == 0 {
        r = a
    } else {
        gcd(b, a % b, r)
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	// Find the monomorphized gcd__i8 and check its body references
	// "gcd__i8" instead of "gcd".
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok || fd.Name != "gcd__i8" {
			continue
		}
		// Walk the body and find call expressions
		hasOldCall, hasNewCall := false, false
		var walk func(parser.Expression)
		walk = func(e parser.Expression) {
			if e == nil {
				return
			}
			switch x := e.(type) {
			case *parser.CallExpression:
				if id, ok := x.Function.(*parser.Identifier); ok {
					switch id.Value {
					case "gcd":
						hasOldCall = true
					case "gcd__i8":
						hasNewCall = true
					}
				}
			}
		}
		_ = walk
		// Simple: look at the body's last statement
		if fd.Body == nil || len(fd.Body.Statements) == 0 {
			t.Fatalf("gcd__i8 has empty body")
		}
		// Find the recursive call by looking at all expressions in body
		var scan func(parser.Expression)
		var scanStmt func(parser.Statement)
		scan = func(e parser.Expression) {
			if e == nil {
				return
			}
			switch x := e.(type) {
			case *parser.CallExpression:
				if id, ok := x.Function.(*parser.Identifier); ok {
					switch id.Value {
					case "gcd":
						hasOldCall = true
					case "gcd__i8":
						hasNewCall = true
					}
				}
			case *parser.IfExpression:
				scan(x.Condition)
				if x.Consequence != nil {
					for _, ss := range x.Consequence.Statements {
						scanStmt(ss)
					}
				}
				if x.Alternative != nil {
					for _, ss := range x.Alternative.Statements {
						scanStmt(ss)
					}
				}
			}
		}
		scanStmt = func(st parser.Statement) {
			if st == nil {
				return
			}
			if es, ok := st.(*parser.ExpressionStatement); ok {
				scan(es.Expression)
			}
		}
		for _, ss := range fd.Body.Statements {
			scanStmt(ss)
		}
		if hasOldCall {
			t.Errorf("gcd__i8 still has a call to 'gcd' (the generic name)")
		}
		if !hasNewCall {
			t.Errorf("gcd__i8 does not have a recursive call to 'gcd__i8'")
		}
		return
	}
	t.Errorf("gcd__i8 not found")
}

// TestFlattenUnionAllMembers enumerates all members produced by flattening
// `num int | float` with the full int/float declarations used in number.no.
// This guards against accidentally dropping or duplicating members during
// recursive flattening.
func TestFlattenUnionAllMembers(t *testing.T) {
	src := `int i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float f32 | f64
num int | float
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	aliases, _ := ValidateUnionTypes(prog)
	members := FlattenUnion("num", aliases)
	if len(members) != 10 {
		t.Fatalf("expected 10 members for num, got %d: %v", len(members), members)
	}
	want := []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64"}
	got := make([]string, len(members))
	for i, m := range members {
		if nt, ok := m.(*parser.NamedType); ok {
			got[i] = nt.Value
		}
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("members[%d] = %q, want %q (full list: %v)", i, got[i], w, got)
		}
	}
}
