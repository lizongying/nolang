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
	add(a i64, b i64) {
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
	if typeStmt.Type == nil || typeStmt.Type.Value != "?str" {
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
		// Inferred types (README 36-41): Type is set to variable name when no explicit annotation
		{name: "infer_i64", input: "x = 1", wantName: "x", wantType: "x"},
		{name: "infer_f64", input: "y = 1.0", wantName: "y", wantType: "y"},
		{name: "infer_str", input: "name = 'hello'", wantName: "name", wantType: "name"},
		{name: "infer_bool", input: "flag = true", wantName: "flag", wantType: "flag"},
		{name: "reassign_str", input: "name = 'World'", wantName: "name", wantType: "name"},
		// String concatenation with - (README 42)
		{name: "str_concat", input: "greeting = 'Hello, ' - name", wantName: "greeting", wantType: "greeting"},
		// Explicit type annotations (README 44-47)
		{name: "explicit_i8", input: "a i8 = 2", wantName: "a", wantType: "i8"},
		{name: "explicit_char", input: "c char = 中", wantName: "c", wantType: "char"},
		{name: "infer_byte", input: "b = x00", wantName: "b", wantType: "b"},
		// Variable name is type name, type auto-inferred (README 50)
		{name: "type_as_name", input: "i8 = 3", wantName: "i8", wantType: "i8"},
		// Hyphenated variable names (README 52-54)
		{name: "hyphen_int", input: "foo-bar = 42", wantName: "foo-bar", wantType: "foo-bar"},
		{name: "hyphen_str", input: "hello-world = 'Hello World'", wantName: "hello-world", wantType: "hello-world"},
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
				if letStmt.Type == nil {
					t.Fatalf("expected type %q, got nil", tt.wantType)
				}
				if letStmt.Type.Value != tt.wantType {
					t.Errorf("expected type %q, got %q", tt.wantType, letStmt.Type.Value)
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
	program.Print()
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
	program.Print()
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
	program.Print()
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
	program.Print()
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
	program.Print()
}

func TestFunctionDefinition2(t *testing.T) {
	input := `
	foo(a int, b string) {
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
	if funcDef.Parameters[0].Type != "int" {
		t.Errorf("expected first parameter type 'int', got '%s'", funcDef.Parameters[0].Type)
	}

	if funcDef.Parameters[1].Name != "b" {
		t.Errorf("expected second parameter name 'b', got '%s'", funcDef.Parameters[1].Name)
	}
	if funcDef.Parameters[1].Type != "string" {
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
	foo(a int, b string) {
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
				t.Fatalf("expected type annotation, got nil")
			}
			if letStmt.Type.Value != tt.wantType {
				t.Errorf("expected type %q, got %q", tt.wantType, letStmt.Type.Value)
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
			input: `result = x {
    1| 10
    2| 20
    _| 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_stmt_with_blocks",
			input: `x {
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
			input: `result = x {
    1| 10
    | 0
}`,
			wantErr:  false,
			wantArms: 2,
		},
		{
			name: "match_no_expr",
			input: `{
    a == 1|
        x = 1
    a == 2|
        y = 2
    :
        z = 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_block_body",
			input: `x {
    1|
        a = 1
        b = 2
    2|
        doSomething()
    |
        c = 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_expr_body",
			input: `result = x {
    1| 1
    2| 2 + 1
    | a + b
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_no_expr",
			input: `{
    a == 1|
        a = 1
        b = 2
    a == 2|
        doSomething()
    |
        c = 0
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_err_nil",
			input: `x {
    err| log(it)
    nil| log('nil')
    |
        doRightThing(it)
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_readme_option_empty_nil",
			input: `x {
    err| log(it)
    nil|
    |
        doRightThing(it)
}`,
			wantErr:  false,
			wantArms: 3,
		},
		{
			name: "match_expr_block_error",
			input: `result = x {
    1|
        a = 1
}`,
			wantErr:  true,
			wantArms: 0,
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
}`,
			wantErr:   false,
			hasCond:   true,
			hasInit:   true,
			hasUpdate: true,
			hasBody:   true,
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

func TestNamedLoop(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "labeled_for_range",
			input: `outer for i in [0..10) {
    inner for j in [0..10) {
        break outer
    }
}`,
			wantErr: false,
		},
		{
			name: "labeled_continue",
			input: `outer for i in [0..10) {
    inner for j in [0..10) {
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
				if let.ArraySize != 3 {
					t.Errorf("expected ArraySize=3, got %d", let.ArraySize)
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
				if let.ArraySize != 3 {
					t.Errorf("expected ArraySize=3, got %d", let.ArraySize)
				}
				if let.ElemType != "u16" {
					t.Errorf("expected ElemType='u16', got %q", let.ElemType)
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
				if !let.IsSlice {
					t.Errorf("expected IsSlice=true")
				}
				if let.ElemType != "u8" {
					t.Errorf("expected ElemType='u8', got %q", let.ElemType)
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
				if sd.Fields[0].Name != "name" || sd.Fields[0].Type != "str" {
					t.Errorf("field 0: expected name:str, got %s:%s", sd.Fields[0].Name, sd.Fields[0].Type)
				}
				if sd.Fields[1].Name != "age" || sd.Fields[1].Type != "i64" {
					t.Errorf("field 1: expected age:i64, got %s:%s", sd.Fields[1].Name, sd.Fields[1].Type)
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
println(u.name)`,
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
println(u.name)`,
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
    super.to-json()
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
			input: `add(a i64, b i64) {
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
			input: "add3(a ..i64) {\n}",
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
				if fd.Parameters[0].Type != "[]i64" {
					t.Errorf("expected param type '[]i64', got %q", fd.Parameters[0].Type)
				}
			},
		},
		// README 161-165: function with println
		{
			name: "func_with_println",
			input: `add(a i64, b i64) {
    result = a + b
    println('result:', result)
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
				letStmt, ok := stmts[0].(*LetStatement)
				if !ok {
					t.Fatalf("expected LetStatement, got %T", stmts[0])
				}
				if letStmt.Name.Value != "add" {
					t.Errorf("expected name 'add', got %q", letStmt.Name.Value)
				}
				fn, ok := letStmt.Value.(*FunctionLiteral)
				if !ok {
					t.Fatalf("expected FunctionLiteral, got %T", letStmt.Value)
				}
				if len(fn.Parameters) != 2 {
					t.Errorf("expected 2 params, got %d", len(fn.Parameters))
				}
			},
		},
		// README 172: IIFE (immediately invoked function expression)
		{
			name:  "iife",
			input: "(a i64) { println(a) }(10)",
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
println('Sum:', sum)`,
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
println(numbers)`,
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
			input: `add(a i64, b i64) {
    result = a + b
    return
    result2 = a + b
}
add3(a ..i64) {
}
add(a i64, b i64) {
    result = a + b
    println('result:', result)
}
add = (a i64, b i64) {
}
(a i64) { println(a) }(10)
add(a, b)
result = add(5, 3)
res = 0
add1(5, 3, res)
sum = 0
for i < 10 {
    sum = sum + i
    i = i + 1
}
println('Sum:', sum)
numbers = 5[1, 2, 3, 4, 5]
println(numbers)`,
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
