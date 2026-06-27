package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestCharType verifies that char type is correctly mapped to i32
func TestCharType(t *testing.T) {
	src := `
test = () {
    c char = 'A'
    d = c.is-digit()
    e = c.is-alpha()
    f = c.to-upper()
    g = c.to-lower()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}

// TestCharMethods verifies char method calls
func TestCharMethods(t *testing.T) {
	src := `
test = () {
    c char = 'x'
    a = c.is-digit()
    b = c.is-alpha()
    d = c.is-alnum()
    e = c.is-space()
    f = c.is-upper()
    g = c.is-lower()
    h = c.to-upper()
    i = c.to-lower()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}

// TestCharComparison verifies char comparison methods
func TestCharComparison(t *testing.T) {
	src := `
test = () {
    a char = 'A'
    b char = 'B'
    eq = a.eq(b)
    cmp = a.cmp(b)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}

// TestCharArithmetic verifies char arithmetic methods
func TestCharArithmetic(t *testing.T) {
	src := `
test = () {
    c char = 'A'
    d = c.add(1)
    e = c.sub(1)
    f = c.diff('B')
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}

// TestCharToStr verifies char to string conversion
func TestCharToStr(t *testing.T) {
	src := `
test = () {
    c char = '中'
    s = c.to-str()
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}

// TestStrFill verifies str.fill method with UTF-8 encoding
func TestStrFill(t *testing.T) {
	src := `
test = () {
    s = '(10)'
    c char = 'x'
    s.fill(c, 10)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}

// TestStrFillUnicode verifies str.fill with Unicode characters
func TestStrFillUnicode(t *testing.T) {
	src := `
test = () {
    s = '(20)'
    c char = '中'
    s.fill(c, 20)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	results := ValidateTypes(prog)
	for _, r := range results {
		t.Errorf("validate error: %s", r.Message)
	}
}
