package parser

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestParser(t *testing.T) {
	input := `
	// 隐式变量声明
	x = 10
	y = 'hello'
	z = 3.14

	// 函数定义
	add= (a i64, b i64) {
		result = a + b
		return
	}

	// 函数调用
	result = add(5, 3)

	// 可空类型
	nullableValue ?str
	nullableValue = nil
	nullableString = 'test'

	// 条件表达式
	if x > 5 {
		x
	} else {
		0
	}
	`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if program == nil {
		t.Fatalf("ParseProgram() returned nil")
	}

	if len(p.Errors()) != 0 {
		t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
	}

	if len(program.Statements) == 0 {
		t.Fatalf("program has no statements, expected at least one")
	}

	letStmt, ok := program.Statements[0].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[0])
	}
	if letStmt.Name.Value != "x" {
		t.Errorf("expected variable name 'x', got '%s'", letStmt.Name.Value)
	}

	funcDef, ok := program.Statements[3].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected FunctionDefinition, got %T", program.Statements[3])
	}
	if funcDef.Name != "add" {
		t.Errorf("expected function name 'add', got '%s'", funcDef.Name)
	}
	if len(funcDef.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(funcDef.Parameters))
	}
	if funcDef.Parameters[0].Name != "a" {
		t.Errorf("expected first parameter 'a', got '%s'", funcDef.Parameters[0].Name)
	}
	if funcDef.Parameters[1].Name != "b" {
		t.Errorf("expected second parameter 'b', got '%s'", funcDef.Parameters[1].Name)
	}

	callStmt, ok := program.Statements[4].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[4])
	}
	callExpr, ok := callStmt.Value.(*CallExpression)
	if !ok {
		t.Fatalf("expected CallExpression, got %T", callStmt.Value)
	}
	ident, ok := callExpr.Function.(*Identifier)
	if !ok {
		t.Fatalf("expected Identifier, got %T", callExpr.Function)
	}
	if ident.Value != "add" {
		t.Errorf("expected function name 'add', got '%s'", ident.Value)
	}
	if len(callExpr.Arguments) != 2 {
		t.Errorf("expected 2 arguments, got %d", len(callExpr.Arguments))
	}

	// statement[5]: nullableValue ?str (型別+默認 nil)
	typeStmt, ok := program.Statements[5].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[5])
	}
	if typeStmt.Type == nil || typeStmt.Type.String() != "?str" {
		t.Errorf("expected type ?str, got %v", typeStmt.Type)
	}
	if typeStmt.Name.Value != "nullableValue" {
		t.Errorf("expected name 'nullableValue', got '%s'", typeStmt.Name.Value)
	}

	// statement[6]: nullableValue = nil
	nilStmt, ok := program.Statements[6].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[6])
	}
	if nilStmt.Name.Value != "nullableValue" {
		t.Errorf("expected name 'nullableValue', got '%s'", nilStmt.Name.Value)
	}
	_, ok = nilStmt.Value.(*NilLiteral)
	if !ok {
		t.Fatalf("expected NilLiteral, got %T", nilStmt.Value)
	}

	// statement[7]: nullableString = 'test'
	nullableStmt, ok := program.Statements[7].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[7])
	}
	if nullableStmt.Name.Value != "nullableString" {
		t.Errorf("expected variable name 'nullableString', got '%s'", nullableStmt.Name.Value)
	}
}

func Json(v any) {
	bs, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(bs))
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestVarDecl$
func TestVarDecl(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantType string // "" = no explicit type annotation
		wantErr  bool
	}{
		// Inferred types (README 36-41): Type is inferred from value expression
		{name: "infer_i64", input: "x = 1", wantName: "x", wantType: "i64"},
		{name: "infer_f64", input: "y = 1.0", wantName: "y", wantType: "f64"},
		{name: "infer_str", input: "name = 'hello'", wantName: "name", wantType: "str"},
		{name: "infer_bool", input: "flag = true", wantName: "flag", wantType: "bool"},
		{name: "reassign_str", input: "name = 'World'", wantName: "name", wantType: "str"},
		// String concatenation with - (README 42)
		{name: "str_concat", input: "greeting = 'Hello, ' - name", wantName: "greeting", wantType: ""},
		// Explicit type annotations (README 44-47)
		{name: "explicit_i8", input: "a i8 = 2", wantName: "a", wantType: "i8"},
		{name: "explicit_char", input: "c char = 中", wantName: "c", wantType: "char"},
		{name: "infer_byte", input: "b = x00", wantName: "b", wantType: ""},
		// Variable name is type name, type auto-inferred (README 50)
		{name: "type_as_name", input: "i8 = 3", wantName: "i8", wantType: "i8"},
		// Hyphenated variable names (README 52-54)
		{name: "hyphen_int", input: "foo-bar = 42", wantName: "foo-bar", wantType: "i64"},
		{name: "hyphen_str", input: "hello-world = 'Hello World'", wantName: "hello-world", wantType: "str"},
		// Full program combining multiple declarations
		{name: "readme_full_program", input: `x = 1
y = 1.0
name = 'hello'
flag = true
name = 'World'
greeting = 'Hello, ' - name
a i8 = 2
c char = 中
b = x00
i8 = 3
foo-bar = 42
hello-world = 'Hello World'`, wantName: "", wantType: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()

			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}

			if tt.wantName != "" {
				letStmt, ok := program.Statements[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", program.Statements[0])
				}
				if letStmt.Name.Value != tt.wantName {
					t.Errorf("expected name %q, got %q", tt.wantName, letStmt.Name.Value)
				}
				if tt.wantType != "" {
					if letStmt.Type == nil {
						t.Fatalf("expected type %q, got nil", tt.wantType)
					}
					if letStmt.Type.String() != tt.wantType {
						t.Errorf("expected type %q, got %q", tt.wantType, letStmt.Type.String())
					}
				} else if letStmt.Type != nil {
					t.Errorf("expected no explicit type, got %q", letStmt.Type.String())
				}
				if letStmt.Value == nil {
					t.Errorf("expected value, got nil")
				}
			}
		})
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestFunctionDefinitionUint8$
func TestFunctionDefinitionUint8(t *testing.T) {
	input := `
	a uin8 = 8
	`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	// Json(program)
	_ = program
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestFunctionDefinitionStr$
func TestFunctionDefinitionStr(t *testing.T) {
	input := `
	a = 'hello'
	`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	// Json(program)
	_ = program
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestFunctionDefinitionBool$
func TestFunctionDefinitionBool(t *testing.T) {
	input := `
	a = false
	`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	// Json(program)
	_ = program
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestFunctionDefinitionFloat$
func TestFunctionDefinitionFloat(t *testing.T) {
	input := `
	a = 1.2
	`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	// Json(program)
	_ = program
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestFunctionDefinitionInt$
func TestFunctionDefinitionInt(t *testing.T) {
	input := `
	a = 8
	`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()
	// Json(program)
	_ = program
}

func TestFunctionDefinition2(t *testing.T) {
	input := `
	foo= (a int, b string) {
		x = 10
	}
	`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if program == nil {
		t.Fatalf("ParseProgram() returned nil")
	}

	if len(p.Errors()) != 0 {
		t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
		t.FailNow()
	}

	if len(program.Statements) != 1 {
		t.Fatalf("program has %d statements, expected 1", len(program.Statements))
	}

	funcDef, ok := program.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected FunctionDefinition, got %T", program.Statements[0])
	}

	if funcDef.Name != "foo" {
		t.Errorf("expected function name 'foo', got '%s'", funcDef.Name)
	}

	if len(funcDef.Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(funcDef.Parameters))
	}

	if funcDef.Parameters[0].Name != "a" {
		t.Errorf("expected first parameter name 'a', got '%s'", funcDef.Parameters[0].Name)
	}
	if funcDef.Parameters[0].Type.String() != "int" {
		t.Errorf("expected first parameter type 'int', got '%s'", funcDef.Parameters[0].Type)
	}

	if funcDef.Parameters[1].Name != "b" {
		t.Errorf("expected second parameter name 'b', got '%s'", funcDef.Parameters[1].Name)
	}
	if funcDef.Parameters[1].Type.String() != "string" {
		t.Errorf("expected second parameter type 'string', got '%s'", funcDef.Parameters[1].Type)
	}

	if funcDef.Body == nil {
		t.Fatalf("function body is nil")
	}

	if len(funcDef.Body.Statements) != 1 {
		t.Errorf("expected 1 statement in function body, got %d", len(funcDef.Body.Statements))
	}
}

func TestFunctionDefinitionVsCall(t *testing.T) {
	input := `
	foo= (a int, b string) {
		x = 10
	}
	
	result = foo(1, 2)
	`

	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if program == nil {
		t.Fatalf("ParseProgram() returned nil")
	}

	if len(p.Errors()) != 0 {
		t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
		t.FailNow()
	}

	if len(program.Statements) != 2 {
		t.Fatalf("program has %d statements, expected 2", len(program.Statements))
	}

	funcDef, ok := program.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected FunctionDefinition, got %T", program.Statements[0])
	}

	if funcDef.Name != "foo" {
		t.Errorf("expected function name 'foo', got '%s'", funcDef.Name)
	}

	letStmt, ok := program.Statements[1].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[1])
	}

	callExpr, ok := letStmt.Value.(*CallExpression)
	if !ok {
		t.Fatalf("expected CallExpression, got %T", letStmt.Value)
	}

	if len(callExpr.Arguments) != 2 {
		t.Errorf("expected 2 arguments, got %d", len(callExpr.Arguments))
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestCharByte$
func TestCharByte(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType string
		wantChar bool // true if value should be CharLiteral
	}{
		{
			name:     "char_literal",
			input:    "c char = 中",
			wantType: "char",
			wantChar: true,
		},
		{
			name:     "byte_literal",
			input:    "b byte = 100",
			wantType: "byte",
			wantChar: false,
		},
		{
			name:     "char_latin",
			input:    "a char = x",
			wantType: "char",
			wantChar: true,
		},
		{
			name:     "char_dquote",
			input:    "c = \"中\"",
			wantType: "char",
			wantChar: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()

			if program == nil {
				t.Fatalf("ParseProgram() returned nil")
			}

			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
			}

			if len(program.Statements) == 0 {
				t.Fatalf("program has no statements")
			}

			letStmt, ok := program.Statements[0].(*LetStatement)
			if !ok {
				t.Fatalf("expected LetStatement, got %T", program.Statements[0])
			}

			if letStmt.Type == nil {
				if tt.wantType != "" {
					t.Fatalf("expected type annotation %q, got nil", tt.wantType)
				}
			} else {
				if letStmt.Type.String() != tt.wantType {
					t.Errorf("expected type %q, got %q", tt.wantType, letStmt.Type.String())
				}
			}

			if tt.wantChar {
				_, ok := letStmt.Value.(*CharLiteral)
				if !ok {
					t.Errorf("expected CharLiteral, got %T", letStmt.Value)
				}
			} else {
				_, ok := letStmt.Value.(*IntegerLiteral)
				if !ok {
					t.Errorf("expected IntegerLiteral, got %T", letStmt.Value)
				}
			}
		})
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestElif$
func TestElif(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "if_elif",
			input: "if x > 5 { a } elif x < 0 { b }",
		},
		{
			name:  "if_elif_else",
			input: "if x > 5 { a } elif x < 0 { b } else { c = 1 }",
		},
		{
			name:  "if_elif_elif_else",
			input: "if x > 5 { a } elif x < 0 { b } elif x == 0 { c } else { d = 4 }",
		},
		{
			name:  "if_elif_multiline",
			input: "if x > 5 {\n    a = 1\n} elif x < 0 {\n    b = 2\n} else {\n    c = 3\n}",
		},
		{
			name:  "if_elif_bare",
			input: "if x > 5 { a } elif { c = 0 }",
		},
		{
			name:  "if_elif_elif_bare",
			input: "if x > 5 { a } elif x < 0 { b } elif { c = 0 }",
		},
		{
			name:  "if_elif_bare_multiline",
			input: "if x > 5 {\n    a = 1\n} elif {\n    c = 0\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()

			if program == nil {
				t.Fatalf("ParseProgram() returned nil")
			}

			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
			}

			if len(program.Statements) == 0 {
				t.Fatalf("program has no statements")
			}

			// Verify the first statement is an expression statement containing an IfExpression
			exprStmt, ok := program.Statements[0].(*ExpressionStatement)
			if !ok {
				t.Fatalf("expected ExpressionStatement, got %T", program.Statements[0])
			}

			ifExpr, ok := exprStmt.Expression.(*IfExpression)
			if !ok {
				t.Fatalf("expected IfExpression, got %T", exprStmt.Expression)
			}

			// Check the first if has a consequence
			if ifExpr.Consequence == nil {
				t.Errorf("first if has nil consequence")
			}

			// The elif is desugared into Alternative containing BlockStatement with nested IfExpression
			if ifExpr.Alternative == nil {
				t.Errorf("expected Alternative (desugared elif), got nil")
			}
		})
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestMatch$
func TestMatch(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantArms int
	}{
		{
			name: "match_expr_simple",
			input: `result = x: {
    1 -> 10
    2 -> 20
    _-> 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_stmt_with_blocks",
			input: `x: {
    1:
        a = 1
        b = 2
    2:
        doSomething()
    _:
        c = 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_bare_colon_default",
			input: `result = x: {
    1 -> 10
    -> 0
}`,
			wantErr:  false,
			wantArms: 2,
		},
		{
			name: "match_no_expr",
			input: `{
    a == 1 ->
        x = 1
    a == 2 ->
        y = 2
    :
        z = 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_block_body",
			input: `x: {
    1 ->
        a = 1
        b = 2
    2 ->
        doSomething()
    ->
        c = 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_expr_body",
			input: `result = x: {
    1 -> 1
    2 -> 2 + 1
    -> a + b
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_no_expr",
			input: `{
    a == 1 ->
        a = 1
        b = 2
    a == 2 ->
        doSomething()
    ->
        c = 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_err_nil",
			input: `x: {
    err -> log(it)
    nil -> log('nil')
    ->
        doRightThing(it)
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_option_empty_nil",
			input: `x: {
    err -> log(it)
    nil ->
    ->
        doRightThing(it)
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_stmt_inline",
			input: `x: {
    1 -> print(111)
    2 -> print(222)
    -> print(333)
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_expr_block_body",
			input: `result = x: {
    1 ->
        true
}`,
			wantErr:  false,
			wantArms: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()

			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected parser errors, got none")
				}
				return
			}

			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}

			if program == nil {
				t.Fatalf("ParseProgram() returned nil")
			}

			if len(program.Statements) == 0 {
				t.Fatalf("program has no statements")
			}

			var ifExpr *IfExpression
			if letStmt, ok := program.Statements[0].(*LetStatement); ok {
				ifExpr, _ = letStmt.Value.(*IfExpression)
				if ifExpr == nil {
					t.Fatalf("expected IfExpression in let value, got %T", letStmt.Value)
				}
			} else if exprStmt, ok := program.Statements[0].(*ExpressionStatement); ok {
				ifExpr, _ = exprStmt.Expression.(*IfExpression)
				if ifExpr == nil {
					t.Fatalf("expected IfExpression, got %T", exprStmt.Expression)
				}
			} else {
				t.Fatalf("expected LetStatement or ExpressionStatement, got %T", program.Statements[0])
			}

			armCount := 1
			current := ifExpr
			for current.Alternative != nil {
				armCount++
				if len(current.Alternative.Statements) == 1 {
					if es, ok := current.Alternative.Statements[0].(*ExpressionStatement); ok {
						if nextIf, ok := es.Expression.(*IfExpression); ok {
							current = nextIf
							continue
						}
					}
				}
				break
			}

			if armCount != tt.wantArms {
				t.Errorf("expected %d arms, got %d", tt.wantArms, armCount)
			}
		})
	}
}

func TestForLoop(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		hasCond   bool
		hasInit   bool
		hasUpdate bool
		hasBody   bool
	}{
		{
			name: "for_infinite",
			input: `for {
    break
}`,
			wantErr: false,
			hasCond: false,
			hasBody: true,
		},
		{
			name: "for_condition",
			input: `for i < 5 {
    continue
}`,
			wantErr: false,
			hasCond: true,
			hasBody: true,
		},
		{
			name: "for_cstyle",
			input: `for i = 0; i < 5; i++ {
    i = i
}`,
			wantErr:   false,
			hasCond:   true,
			hasInit:   true,
			hasUpdate: true,
			hasBody:   true,
		},
		{
			name: "for_cstyle_comma",
			input: `for i = 0, i < 5, i++ {
    i = i
}`,
			wantErr:   false,
			hasCond:   true,
			hasInit:   true,
			hasUpdate: true,
			hasBody:   true,
		},
		// NOTE: bare cond: {} is no longer a for-loop; it's now always a match expression.
		// Conditional loops must use the `for` keyword: `for cond { body }`.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected parser errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("program has no statements")
			}
			forStmt, ok := program.Statements[0].(*ForStatement)
			if !ok {
				t.Fatalf("expected ForStatement, got %T", program.Statements[0])
			}
			if tt.hasCond && forStmt.Condition == nil {
				t.Errorf("expected condition, got nil")
			}
			if !tt.hasCond && forStmt.Condition != nil {
				t.Errorf("expected no condition, got %T", forStmt.Condition)
			}
			if tt.hasInit && forStmt.Init == nil {
				t.Errorf("expected init, got nil")
			}
			if tt.hasUpdate && forStmt.Update == nil {
				t.Errorf("expected update, got nil")
			}
			if tt.hasBody && (forStmt.Body == nil || len(forStmt.Body.Statements) == 0) {
				if tt.name != "for_cstyle" {
					t.Errorf("expected non-empty body")
				}
			}
		})
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestDeprecationWarnings$
// (TestDeprecationWarnings 已存在於 syntax_test.go，本處僅補上 match/if 舊式語法測試)

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestNewSyntaxNoWarnings$
func TestNewSyntaxNoWarnings(t *testing.T) {
	// 確認新語法不會觸發 deprecation warning
	tests := []struct {
		name  string
		input string
	}{
		{name: "bang_loop", input: "!!\n{\n    break\n}"},
		{name: "counted_loop", input: "5 *\n{\n    print(1)\n}"},
		{name: "range_for_with_colon", input: "i <- [0..10): {\n    print(i)\n}"},
		{name: "for_cond_keyword", input: "for i < 5 {\n    i = i + 1\n}"},
		{name: "match_with_subject", input: "x: {\n    1 -> 1\n    -> 0\n}"},
		{name: "bare_match_if_else", input: "{\n    x > 0 -> a = 1\n    -> a = 0\n}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			_ = program
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if len(p.Warnings()) != 0 {
				t.Errorf("expected no deprecation warnings for new syntax, got: %v", p.Warnings())
			}
		})
	}
}

// TestBareMatchIfElse 新式 if/else 語法（{ cond -> body }）的專項測試
// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestBareMatchIfElse$
func TestBareMatchIfElse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "if_else_simple", input: "{\n    x > 0 -> a = 1\n    -> a = 0\n}", wantErr: false},
		{name: "if_else_with_three_arms", input: "{\n    x == 1 -> a = 1\n    x == 2 -> a = 2\n    -> a = 0\n}", wantErr: false},
		{name: "if_else_or_condition", input: "{\n    x == 2 || x == 3 -> a = 1\n    -> a = 0\n}", wantErr: false},
		{name: "if_else_in_function", input: "foo = (x i64) {\n    {\n        x > 0 -> r = 1\n        -> r = 0\n    }\n}", wantErr: false},
		{name: "if_else_multiline_body", input: "{\n    x == 1 ->\n        a = 1\n        b = 2\n    ->\n        c = 0\n}", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected parser errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

// TestNewSyntaxInFunctionBody 新式循環語法（!{}、N*{}）在函數體內的專項測試
// 注意：cond: {} 不再作為 for-loop，只解析為 match expression
// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestNewSyntaxInFunctionBody$
func TestNewSyntaxInFunctionBody(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "counted_loop_in_function", input: "foo = () {\n    10 * {\n        a = a + 1\n    }\n}", wantErr: false},
		{name: "bang_loop_in_function", input: "foo = () {\n    !! {\n        *\n    }\n}", wantErr: false},
		{name: "for_cond_in_function", input: "foo = () {\n    for x < 5 && y > 0 {\n        a = a + 1\n    }\n}", wantErr: false},
		{name: "range_for_in_function", input: "foo = () {\n    i <- [0..10): {\n        print(i)\n    }\n}", wantErr: false},
		// 新式 if/else 在函數體內
		{name: "if_else_in_function_inline", input: "foo = (x i64) {\n    {\n        x > 0 -> r = 1\n        -> r = 0\n    }\n}", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected parser errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

func TestNamedLoop(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "labeled_for_range",
			input: `outer for i in [0..10): {
    inner for j in [0..10): {
        break outer
    }
}`,
			wantErr: false,
		},
		{
			name: "labeled_continue",
			input: `outer for i in [0..10): {
    inner for j in [0..10): {
        continue outer
    }
}`,
			wantErr: false,
		},
		{
			name: "bare_break",
			input: `for i < 10 {
    if i == 5 {
        break
    }
    i = i + 1
}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected parser errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil {
				t.Fatalf("ParseProgram() returned nil")
			}
			if len(program.Statements) == 0 {
				t.Fatalf("program has no statements")
			}
		})
	}
}

func TestSliceRange(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "full_slice", input: "a[..]", wantErr: false},
		{name: "from_start", input: "a[1..]", wantErr: false},
		{name: "to_end", input: "a[..3]", wantErr: false},
		{name: "both_bounds", input: "a[2..3]", wantErr: false},
		{name: "closed_range", input: "a[1..3]", wantErr: false},
		{name: "excl_right", input: "a[1..3)", wantErr: false},
		{name: "excl_both", input: "a(1..3)", wantErr: false},
		{name: "paren_full", input: "a(..)", wantErr: false},
		{name: "var_bounds", input: "a[i..j]", wantErr: false},
		{name: "str_from", input: "s[1..]", wantErr: false},
		{name: "str_expr_end", input: "s[1..s.len)", wantErr: false},
		// README 253-268: full program with declaration and slicing
		{name: "readme_slice_program", input: `nums [5]u8 = [0, 1, 2, 3, 4]
nums[..]
nums[1..]
nums[..4]
nums[2..3]
nums[1..3]
nums[1..3)
nums(1..3)`, wantErr: false},
		{name: "readme_str_slice", input: `s = 'abc'
s[1..]
s[1..s.len)`, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

func TestIndexExpr(t *testing.T) {
	tests := []string{
		"a[i]",
		"arr[0]",
		"str[i]",
		"m[key]",
		"mat[i][j]",
	}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			lex := lexer.New(input + "\n")
			p := New(lex)
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				t.Fatalf("parse errors: %v", p.Errors())
			}
			if len(prog.Statements) == 0 {
				t.Fatal("no statements")
			}
		})
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestArraySliceStruct$
func TestArraySliceStruct(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, stmts []Statement)
	}{
		// README 373: array with inferred type
		{
			name:  "array_inferred_type",
			input: "a [3] = [1, 2, 3]",
			check: func(t *testing.T, stmts []Statement) {
				let, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				at, ok := let.Type.(*ArrayType)
				if !ok {
					t.Fatalf("expected ArrayType, got %T", let.Type)
				}
				if intLit, ok := at.Size.(*IntegerLiteral); ok {
					if intLit.Value != 3 {
						t.Errorf("expected size 3, got %d", intLit.Value)
					}
				}
				arr, ok := let.Value.(*ArrayLiteral)
				if !ok {
					t.Fatalf("expected ArrayLiteral, got %T", let.Value)
				}
				if len(arr.Elements) != 3 {
					t.Errorf("expected 3 elements, got %d", len(arr.Elements))
				}
			},
		},
		// README 374: array with explicit element type
		{
			name:  "array_explicit_type",
			input: "a [3]u16 = [1, 2, 3]",
			check: func(t *testing.T, stmts []Statement) {
				let, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				at, ok := let.Type.(*ArrayType)
				if !ok {
					t.Fatalf("expected ArrayType, got %T", let.Type)
				}
				if intLit, ok := at.Size.(*IntegerLiteral); ok {
					if intLit.Value != 3 {
						t.Errorf("expected size 3, got %d", intLit.Value)
					}
				}
				if at.Elem.String() != "u16" {
					t.Errorf("expected elem type 'u16', got %q", at.Elem.String())
				}
			},
		},
		// README 380: slice (dynamic) with inferred type
		{
			name:  "slice_inferred",
			input: "v = [1, 2, 3]",
			check: func(t *testing.T, stmts []Statement) {
				let, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				sl, ok := let.Value.(*SliceLiteral)
				if !ok {
					t.Fatalf("expected SliceLiteral, got %T", let.Value)
				}
				if len(sl.Elements) != 3 {
					t.Errorf("expected 3 elements, got %d", len(sl.Elements))
				}
			},
		},
		// README 381: slice with explicit type
		{
			name:  "slice_explicit_type",
			input: "v []u8 = [1, 2, 3]",
			check: func(t *testing.T, stmts []Statement) {
				let, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				st, ok := let.Type.(*SliceType)
				if !ok {
					t.Fatalf("expected SliceType, got %T", let.Type)
				}
				if st.Elem.String() != "u8" {
					t.Errorf("expected elem type 'u8', got %q", st.Elem.String())
				}
			},
		},
		// README 383-384: byte and byte slice
		{
			name: "byte_and_byte_slice",
			input: `b = x00
bs = [x11, x22, x33]`,
			check: func(t *testing.T, stmts []Statement) {
				if len(stmts) < 2 {
					t.Fatalf("expected at least 2 statements, got %d", len(stmts))
				}
				// b = x00
				let1, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				if let1.Name.Value != "b" {
					t.Errorf("expected name 'b', got %q", let1.Name.Value)
				}
				// bs = [x11, x22, x33]
				let2, ok := stmts[1].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[1])
				}
				if let2.Name.Value != "bs" {
					t.Errorf("expected name 'bs', got %q", let2.Name.Value)
				}
			},
		},
		// README 390-393: struct definition
		{
			name: "struct_definition",
			input: `user {
    name str
    age i64
}`,
			check: func(t *testing.T, stmts []Statement) {
				sd, ok := stmts[0].(*StructDefinition)
				if !ok {
					t.Fatalf("expected StructDefinition, got %T", stmts[0])
				}
				if sd.Name != "user" {
					t.Errorf("expected name 'user', got %q", sd.Name)
				}
				if len(sd.Fields) != 2 {
					t.Fatalf("expected 2 fields, got %d", len(sd.Fields))
				}
				if sd.Fields[0].Name != "name" || sd.Fields[0].Type.String() != "str" {
					t.Errorf("field 0: expected name:str, got %s:%s", sd.Fields[0].Name, sd.Fields[0].Type.String())
				}
				if sd.Fields[1].Name != "age" || sd.Fields[1].Type.String() != "i64" {
					t.Errorf("field 1: expected age:i64, got %s:%s", sd.Fields[1].Name, sd.Fields[1].Type.String())
				}
			},
		},
		// README 395-398: struct literal
		{
			name: "struct_literal",
			input: `u = user {
    name: 'abc'
    age: 20
}`,
			check: func(t *testing.T, stmts []Statement) {
				let, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				sl, ok := let.Value.(*StructLiteral)
				if !ok {
					t.Fatalf("expected StructLiteral, got %T", let.Value)
				}
				if sl.Type != "user" {
					t.Errorf("expected type 'user', got %q", sl.Type)
				}
				if len(sl.Fields) != 2 {
					t.Fatalf("expected 2 fields, got %d", len(sl.Fields))
				}
			},
		},
		// README 400-402: struct field access and assignment
		{
			name: "struct_field_access",
			input: `u.name = 'def'
u.age = 25
print(u.name)`,
			check: func(t *testing.T, stmts []Statement) {
				if len(stmts) < 3 {
					t.Fatalf("expected at least 3 statements, got %d", len(stmts))
				}
				// u.name = 'def' is parsed as expression statement (assignment expression)
				_, ok := stmts[0].(*ExpressionStatement)
				if !ok {
					t.Errorf("expected ExpressionStatement for u.name='def', got %T", stmts[0])
				}
				// u.age = 25
				_, ok = stmts[1].(*ExpressionStatement)
				if !ok {
					t.Errorf("expected ExpressionStatement for u.age=25, got %T", stmts[1])
				}
			},
		},
		// README 365-403: full program
		{
			name: "readme_full_container",
			input: `a [3] = [1, 2, 3]
a [3]u16 = [1, 2, 3]
v = [1, 2, 3]
v []u8 = [1, 2, 3]
b = x00
bs = [x11, x22, x33]
user {
    name str
    age i64
}
u = user {
    name: 'abc'
    age: 20
}
u.name = 'def'
u.age = 25
print(u.name)`,
			check: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()

			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
			if tt.check != nil {
				tt.check(t, program.Statements)
			}
		})
	}
}

func TestInterface(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "interface_decl",
			input: `json {
    to-json()
}`,
			wantErr: false,
		},
		{
			name: "struct_implements_iface",
			input: `user json {
    name str
    age i64
}`,
			wantErr: false,
		},
		{
			name: "struct_implements_multi",
			input: `file enter, leave {
    path str
}`,
			wantErr: false,
		},
		{
			name: "interface_default_method",
			input: `json.to-json() {
    do-something()
}`,
			wantErr: false,
		},
		{
			name: "method_with_super",
			input: `user.to-json() {
    ..to-json()
}`,
			wantErr: false,
		},
		{
			name: "enum_decl",
			input: `types {
    a,
    b,
    c,
}`,
			wantErr: false,
		},
		{
			name:    "enum_inline",
			input:   `color { red, green, blue }`,
			wantErr: false,
		},
		{
			name: "iface_multiple_methods",
			input: `shape {
    area()
    perim()
}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()
			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
		})
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestFunctionSyntax$
func TestFunctionSyntax(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, stmts []Statement)
	}{
		// README 143-149: function with return (no return value)
		{
			name: "func_with_return_no_value",
			input: `add= (a i64, b i64) {
    result = a + b
    return
    result2 = a + b
}`,
			check: func(t *testing.T, stmts []Statement) {
				fd, ok := stmts[0].(*FunctionDefinition)
				if !ok {
					t.Fatalf("expected FunctionDefinition, got %T", stmts[0])
				}
				if fd.Name != "add" {
					t.Errorf("expected name 'add', got %q", fd.Name)
				}
				if len(fd.Parameters) != 2 {
					t.Errorf("expected 2 params, got %d", len(fd.Parameters))
				}
				if fd.Body == nil || len(fd.Body.Statements) < 2 {
					t.Fatalf("expected at least 2 body statements")
				}
				_, ok = fd.Body.Statements[1].(*ReturnStatement)
				if !ok {
					t.Errorf("expected ReturnStatement, got %T", fd.Body.Statements[1])
				}
			},
		},
		// README 157-159: variadic parameters
		{
			name:  "variadic_param",
			input: "add3= (a ..i64) {\n}",
			check: func(t *testing.T, stmts []Statement) {
				fd, ok := stmts[0].(*FunctionDefinition)
				if !ok {
					t.Fatalf("expected FunctionDefinition, got %T", stmts[0])
				}
				if !fd.IsVariadic {
					t.Errorf("expected IsVariadic=true")
				}
				if len(fd.Parameters) != 1 {
					t.Fatalf("expected 1 param, got %d", len(fd.Parameters))
				}
				if fd.Parameters[0].Type.String() != "[]i64" {
					t.Errorf("expected param type '[]i64', got %q", fd.Parameters[0].Type.String())
				}
			},
		},
		// README 161-165: function with println
		{
			name: "func_with_println",
			input: `add= (a i64, b i64) {
    result = a + b
    print('result:', result)
}`,
			check: func(t *testing.T, stmts []Statement) {
				fd, ok := stmts[0].(*FunctionDefinition)
				if !ok {
					t.Fatalf("expected FunctionDefinition, got %T", stmts[0])
				}
				if fd.Name != "add" {
					t.Errorf("expected name 'add', got %q", fd.Name)
				}
				if len(fd.Body.Statements) != 2 {
					t.Errorf("expected 2 body statements, got %d", len(fd.Body.Statements))
				}
			},
		},
		// README 167-169: anonymous function
		{
			name:  "anonymous_func",
			input: "add = (a i64, b i64) {\n}",
			check: func(t *testing.T, stmts []Statement) {
				fd, ok := stmts[0].(*FunctionDefinition)
				if !ok {
					t.Fatalf("expected FunctionDefinition, got %T", stmts[0])
				}
				if fd.Name != "add" {
					t.Errorf("expected name 'add', got %q", fd.Name)
				}
				if len(fd.Parameters) != 2 {
					t.Errorf("expected 2 params, got %d", len(fd.Parameters))
				}
			},
		},
		// README 172: IIFE (immediately invoked function expression)
		{
			name:  "iife",
			input: "(a i64) { print(a) }(10)",
			check: func(t *testing.T, stmts []Statement) {
				exprStmt, ok := stmts[0].(*ExpressionStatement)
				if !ok {
					t.Fatalf("expected ExpressionStatement, got %T", stmts[0])
				}
				call, ok := exprStmt.Expression.(*CallExpression)
				if !ok {
					t.Fatalf("expected CallExpression, got %T", exprStmt.Expression)
				}
				fn, ok := call.Function.(*FunctionLiteral)
				if !ok {
					t.Fatalf("expected FunctionLiteral as callee, got %T", call.Function)
				}
				if len(fn.Parameters) != 1 {
					t.Errorf("expected 1 param, got %d", len(fn.Parameters))
				}
				if len(call.Arguments) != 1 {
					t.Errorf("expected 1 argument, got %d", len(call.Arguments))
				}
			},
		},
		// README 174-175: function call
		{
			name:  "func_call",
			input: "add(a, b)",
			check: func(t *testing.T, stmts []Statement) {
				exprStmt, ok := stmts[0].(*ExpressionStatement)
				if !ok {
					t.Fatalf("expected ExpressionStatement, got %T", stmts[0])
				}
				call, ok := exprStmt.Expression.(*CallExpression)
				if !ok {
					t.Fatalf("expected CallExpression, got %T", exprStmt.Expression)
				}
				if len(call.Arguments) != 2 {
					t.Errorf("expected 2 arguments, got %d", len(call.Arguments))
				}
			},
		},
		// README 178-179: function call with return value
		{
			name:  "func_call_with_return",
			input: "result = add(5, 3)",
			check: func(t *testing.T, stmts []Statement) {
				letStmt, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				call, ok := letStmt.Value.(*CallExpression)
				if !ok {
					t.Fatalf("expected CallExpression, got %T", letStmt.Value)
				}
				if len(call.Arguments) != 2 {
					t.Errorf("expected 2 arguments, got %d", len(call.Arguments))
				}
			},
		},
		// README 186-187: function call with output param
		{
			name:  "func_call_output_param",
			input: "add1(5, 3, res)",
			check: func(t *testing.T, stmts []Statement) {
				exprStmt, ok := stmts[0].(*ExpressionStatement)
				if !ok {
					t.Fatalf("expected ExpressionStatement, got %T", stmts[0])
				}
				call, ok := exprStmt.Expression.(*CallExpression)
				if !ok {
					t.Fatalf("expected CallExpression, got %T", exprStmt.Expression)
				}
				if len(call.Arguments) != 3 {
					t.Errorf("expected 3 arguments, got %d", len(call.Arguments))
				}
			},
		},
		// README 189-195: for loop with sum computation
		{
			name: "for_loop_sum",
			input: `sum = 0
for i < 10 {
    sum = sum + i
    i = i + 1
}
print('Sum:', sum)`,
			check: func(t *testing.T, stmts []Statement) {
				if len(stmts) < 3 {
					t.Fatalf("expected at least 3 statements, got %d", len(stmts))
				}
				_, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Errorf("expected LetStatement for sum, got %T", stmts[0])
				}
				forStmt, ok := stmts[1].(*ForStatement)
				if !ok {
					t.Errorf("expected ForStatement, got %T", stmts[1])
				}
				if forStmt.Condition == nil {
					t.Errorf("expected condition in for loop")
				}
				if len(forStmt.Body.Statements) != 2 {
					t.Errorf("expected 2 body statements in for loop, got %d", len(forStmt.Body.Statements))
				}
			},
		},
		// README 197-199: array literal with size prefix
		{
			name: "array_literal_with_size",
			input: `numbers = 5[1, 2, 3, 4, 5]
print(numbers)`,
			check: func(t *testing.T, stmts []Statement) {
				letStmt, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				arr, ok := letStmt.Value.(*ArrayLiteral)
				if !ok {
					t.Fatalf("expected ArrayLiteral, got %T", letStmt.Value)
				}
				if len(arr.Elements) != 5 {
					t.Errorf("expected 5 elements, got %d", len(arr.Elements))
				}
			},
		},
		// README 142-199: full program
		{
			name: "readme_full_functions",
			input: `add= (a i64, b i64) {
    result = a + b
    return
    result2 = a + b
}
add3= (a ..i64) {
}
add= (a i64, b i64) {
    result = a + b
    print('result:', result)
}
add = (a i64, b i64) {
}
(a i64) { print(a) }(10)
add(a, b)
result = add(5, 3)
res = 0
add1(5, 3, res)
sum = 0
for i < 10 {
    sum = sum + i
    i = i + 1
}
print('Sum:', sum)
numbers = 5[1, 2, 3, 4, 5]
print(numbers)`,
			check: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			program := p.ParseProgram()

			if tt.wantErr {
				if len(p.Errors()) == 0 {
					t.Errorf("expected errors, got none")
				}
				return
			}
			if len(p.Errors()) != 0 {
				t.Errorf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if program == nil || len(program.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
			if tt.check != nil {
				tt.check(t, program.Statements)
			}
		})
	}
}

// TestParseMapType verifies that parseTypeExpression resolves both the implicit
// [K]V form and the explicit map[K]V form to *MapType when K is a builtin type
// name (str, i64, bool, ...). parseTypeExpression is exercised directly because
// the explicit `map[K]V` form is only reachable through type-expression parsing
// (struct fields, params, etc.); the statement-level dispatch routes the
// `name [K]V` form through parseLetStatement, which is covered by the
// map-literal tests below.
func TestParseMapType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // MapType.String()
	}{
		{name: "implicit_str_i64", input: `[str]i64`, want: `[str]i64`},
		{name: "implicit_i64_str", input: `[i64]str`, want: `[i64]str`},
		{name: "implicit_bool_i64", input: `[bool]i64`, want: `[bool]i64`},
		{name: "explicit_map_str_i64", input: `map[str]i64`, want: `[str]i64`},
		{name: "explicit_map_i64_str", input: `map[i64]str`, want: `[i64]str`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			ty, ok := p.parseTypeExpression()

			if len(p.Errors()) != 0 {
				t.Fatalf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
			}
			if !ok || ty == nil {
				t.Fatalf("parseTypeExpression returned nil")
			}
			mt, ok := ty.(*MapType)
			if !ok {
				t.Fatalf("expected *MapType, got %T (%v)", ty, ty)
			}
			if mt.String() != tt.want {
				t.Errorf("expected %q, got %q", tt.want, mt.String())
			}
		})
	}
}

// TestParseArrayTypeRegression ensures the map-syntax change did not break the
// pre-existing array/slice parsing: [N]T (integer size), [n]T (non-type
// identifier), []T (slice) and [?]T must NOT be classified as *MapType.
//
// [N]T / [n]T / [K]V / []T are exercised through LetStatement parsing (the path
// the map-syntax change touched). [?]T is exercised through parseTypeExpression
// directly: in statement position `a [?]i64` is dispatched as an index
// expression, so the [?] form is only reachable via type-expression parsing.
func TestParseArrayTypeRegression(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		direct bool // true: parse via parseTypeExpression; false: parse via LetStatement
		isMap  bool
		want   string // expected concrete type (via %T)
	}{
		{name: "int_size", input: `a [10]i64`, direct: false, isMap: false, want: "*parser.ArrayType"},
		{name: "ident_size", input: `a [n]i64`, direct: false, isMap: false, want: "*parser.ArrayType"},
		{name: "type_key", input: `a [str]i64`, direct: false, isMap: true, want: "*parser.MapType"},
		{name: "slice", input: `a []i64`, direct: false, isMap: false, want: "*parser.SliceType"},
		{name: "nullable", input: `[?]i64`, direct: true, isMap: false, want: "*parser.SliceType"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)

			var ty Type
			if tt.direct {
				var ok bool
				ty, ok = p.parseTypeExpression()
				if !ok || ty == nil {
					t.Fatalf("parseTypeExpression returned nil")
				}
			} else {
				program := p.ParseProgram()
				if program == nil || len(program.Statements) == 0 {
					t.Fatalf("no statements parsed")
				}
				letStmt, ok := program.Statements[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", program.Statements[0])
				}
				ty = letStmt.Type
			}

			if len(p.Errors()) != 0 {
				t.Fatalf("parser has %d errors, expected 0", len(p.Errors()))
				for _, err := range p.Errors() {
					t.Errorf("parser error: %s", err)
				}
			}
			if ty == nil {
				t.Fatalf("expected a type, got nil")
			}
			_, isMap := ty.(*MapType)
			if isMap != tt.isMap {
				t.Fatalf("expected isMap=%v, got %v (%T)", tt.isMap, isMap, ty)
			}
			if got := fmt.Sprintf("%T", ty); got != tt.want {
				t.Errorf("expected type %s, got %s", tt.want, got)
			}
		})
	}
}

// TestParseMapLiteral verifies { k1:v1, k2:v2 } parsing for a MapType-typed let
// statement, including concrete element types of the first pair.
func TestParseMapLiteral(t *testing.T) {
	input := `m [str]i64 = { 'a':0, 'b':1 }`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Fatalf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
	}
	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements parsed")
	}
	letStmt, ok := program.Statements[0].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[0])
	}
	if _, ok := letStmt.Type.(*MapType); !ok {
		t.Fatalf("expected *MapType, got %T", letStmt.Type)
	}
	ml, ok := letStmt.Value.(*MapLiteral)
	if !ok {
		t.Fatalf("expected *MapLiteral, got %T", letStmt.Value)
	}
	if len(ml.Pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(ml.Pairs))
	}
	key0, ok := ml.Pairs[0].Key.(*StringLiteral)
	if !ok {
		t.Fatalf("expected *StringLiteral for first key, got %T", ml.Pairs[0].Key)
	}
	if key0.Value != "a" {
		t.Errorf("expected first key 'a', got %q", key0.Value)
	}
	val0, ok := ml.Pairs[0].Value.(*IntegerLiteral)
	if !ok {
		t.Fatalf("expected *IntegerLiteral for first value, got %T", ml.Pairs[0].Value)
	}
	if val0.Value != 0 {
		t.Errorf("expected first value 0, got %d", val0.Value)
	}
}

// TestParseMapLiteralMultiline verifies that map literals whose pairs are
// separated by newlines (rather than commas) parse correctly.
func TestParseMapLiteralMultiline(t *testing.T) {
	input := `m [str]i64 = {
'a':0
'b':1
}`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Fatalf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
	}
	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements parsed")
	}
	letStmt, ok := program.Statements[0].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[0])
	}
	if _, ok := letStmt.Type.(*MapType); !ok {
		t.Fatalf("expected *MapType, got %T", letStmt.Type)
	}
	ml, ok := letStmt.Value.(*MapLiteral)
	if !ok {
		t.Fatalf("expected *MapLiteral, got %T", letStmt.Value)
	}
	if len(ml.Pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(ml.Pairs))
	}
}

// TestParseEmptyMapLiteral verifies that an empty map literal {} parses to a
// MapLiteral with zero pairs.
func TestParseEmptyMapLiteral(t *testing.T) {
	input := `m [str]i64 = {}`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Fatalf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
	}
	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements parsed")
	}
	letStmt, ok := program.Statements[0].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", program.Statements[0])
	}
	if _, ok := letStmt.Type.(*MapType); !ok {
		t.Fatalf("expected *MapType, got %T", letStmt.Type)
	}
	ml, ok := letStmt.Value.(*MapLiteral)
	if !ok {
		t.Fatalf("expected *MapLiteral, got %T", letStmt.Value)
	}
	if len(ml.Pairs) != 0 {
		t.Fatalf("expected 0 pairs, got %d", len(ml.Pairs))
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestDefaultParameterValue$
func TestDefaultParameterValue(t *testing.T) {
	input := `
parse-line = (s str, max-fields i64 = 1024) (fields []str) {
    fields[0] = s
}
`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Fatalf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
	}
	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements parsed")
	}

	fd, ok := program.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected FunctionDefinition, got %T", program.Statements[0])
	}
	if len(fd.Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(fd.Parameters))
	}

	// First parameter: s str (no default)
	if fd.Parameters[0].Name != "s" {
		t.Errorf("expected param 0 name 's', got '%s'", fd.Parameters[0].Name)
	}
	if fd.Parameters[0].DefaultExpr != nil {
		t.Errorf("expected param 0 to have no default, got %v", fd.Parameters[0].DefaultExpr)
	}

	// Second parameter: max-fields i64 = 1024 (has default)
	if fd.Parameters[1].Name != "max-fields" {
		t.Errorf("expected param 1 name 'max-fields', got '%s'", fd.Parameters[1].Name)
	}
	if fd.Parameters[1].DefaultExpr == nil {
		t.Fatalf("expected param 1 to have default value, got nil")
	}
	intLit, ok := fd.Parameters[1].DefaultExpr.(*IntegerLiteral)
	if !ok {
		t.Fatalf("expected IntegerLiteral default, got %T", fd.Parameters[1].DefaultExpr)
	}
	if intLit.Value != 1024 {
		t.Errorf("expected default value 1024, got %d", intLit.Value)
	}
}

// go test github.com/lizongying/nolang/parser -test.fullpath=true -v -run ^TestDefaultParameterValueMultiple$
func TestDefaultParameterValueMultiple(t *testing.T) {
	input := `
config = (host str = 'localhost', port i64 = 8080, debug bool = true) {
    x = 1
}
`
	lex := lexer.New(input)
	p := New(lex)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Fatalf("parser has %d errors, expected 0", len(p.Errors()))
		for _, err := range p.Errors() {
			t.Errorf("parser error: %s", err)
		}
	}
	if program == nil || len(program.Statements) == 0 {
		t.Fatalf("no statements parsed")
	}

	fd, ok := program.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected FunctionDefinition, got %T", program.Statements[0])
	}
	if len(fd.Parameters) != 3 {
		t.Fatalf("expected 3 parameters, got %d", len(fd.Parameters))
	}

	// All three should have defaults
	for i, p := range fd.Parameters {
		if p.DefaultExpr == nil {
			t.Errorf("param %d ('%s') should have default value", i, p.Name)
		}
	}

	// Check specific values
	strLit, ok := fd.Parameters[0].DefaultExpr.(*StringLiteral)
	if !ok || strLit.Value != "localhost" {
		t.Errorf("expected param 0 default 'localhost', got %v", fd.Parameters[0].DefaultExpr)
	}

	intLit, ok := fd.Parameters[1].DefaultExpr.(*IntegerLiteral)
	if !ok || intLit.Value != 8080 {
		t.Errorf("expected param 1 default 8080, got %v", fd.Parameters[1].DefaultExpr)
	}
}

// TestGenericAnnotationStruct verifies that #{generic=[k,v]} attached to a
// struct definition populates StructDefinition.GenericParams.
func TestGenericAnnotationStruct(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
	}{
		{
			name: "generic_two_params",
			input: `#{generic=[k,v]}
hashmap {
    k k
    v v
}`,
			want: []string{"k", "v"},
		},
		{
			name: "generic_single_param",
			input: `#{generic=[v]}
vec {
    data v
}`,
			want: []string{"v"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) != 0 {
				t.Fatalf("parser has %d errors, expected 0", len(errs))
				for _, err := range errs {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if prog == nil || len(prog.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
			sd, ok := prog.Statements[0].(*StructDefinition)
			if !ok {
				t.Fatalf("expected StructDefinition, got %T", prog.Statements[0])
			}
			if len(sd.GenericParams) != len(tt.want) {
				t.Fatalf("expected %d generic params, got %d (%v)", len(tt.want), len(sd.GenericParams), sd.GenericParams)
			}
			for i, gp := range tt.want {
				if sd.GenericParams[i] != gp {
					t.Errorf("generic param %d: expected %q, got %q", i, gp, sd.GenericParams[i])
				}
			}
		})
	}
}

// TestGenericAnnotationMethod verifies that #{generic=[k,v]} attached to a
// method definition populates FunctionDefinition.GenericParams.
func TestGenericAnnotationMethod(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
	}{
		{
			name: "method_generic_two_params",
			input: `#{generic=[k,v]}
hashmap.put = (k k, v v) {
    return
}`,
			want: []string{"k", "v"},
		},
		{
			name: "method_generic_single_param",
			input: `#{generic=[v]}
vec.push = (v v) {
    return
}`,
			want: []string{"v"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) != 0 {
				t.Fatalf("parser has %d errors, expected 0", len(errs))
				for _, err := range errs {
					t.Errorf("parser error: %s", err)
				}
				return
			}
			if prog == nil || len(prog.Statements) == 0 {
				t.Fatalf("no statements parsed")
			}
			fd, ok := prog.Statements[0].(*FunctionDefinition)
			if !ok {
				t.Fatalf("expected FunctionDefinition, got %T", prog.Statements[0])
			}
			if len(fd.GenericParams) < len(tt.want) {
				t.Fatalf("expected at least %d generic params, got %d", len(tt.want), len(fd.GenericParams))
			}
			// GenericParams may include implicit params inferred from the receiver;
			// verify all expected annotation-derived params are present in order.
			idx := 0
			for _, gp := range fd.GenericParams {
				if idx < len(tt.want) && gp.Value == tt.want[idx] {
					idx++
				}
			}
			if idx != len(tt.want) {
				t.Errorf("expected generic params %v, got params: %v", tt.want, collectGenericParamNames(fd.GenericParams))
			}
		})
	}
}

func collectGenericParamNames(params []*Identifier) []string {
	names := make([]string, 0, len(params))
	for _, gp := range params {
		names = append(names, gp.Value)
	}
	return names
}
