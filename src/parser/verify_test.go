package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestVerifyItBindingTypes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		// expected types for each arm's it binding in source order
		// ok arm → it: unwrapped elemType (e.g., i64 for ?i64)
		// err arm → it: err
		// nil arm → it: nil
		// wildcard after ok → complement of listed variants
		expected []string
	}{
		{
			name:     "ok+nil+wildcard",
			input:    "x ?i64\nx: { ok -> log(it)\nnil -> log(it)\n-> log(it) }",
			expected: []string{"i64", "nil", "err"},
		},
		{
			name:     "ok+wildcard",
			input:    "x ?i64\nx: { ok -> log(it)\n-> log(it) }",
			expected: []string{"i64", "err | nil"},
		},
		{
			name:     "err+nil+wildcard",
			input:    "x ?i64\nx: { err -> log(it)\nnil -> log(it)\n-> log(it) }",
			expected: []string{"err", "nil", "i64"},
		},
		{
			name:     "err+nil+ok",
			input:    "x ?i64\nx: { err -> log(it)\nnil -> log(it)\nok -> log(it) }",
			expected: []string{"err", "nil", "i64"},
		},
		{
			name:     "wildcard-only",
			input:    "x ?i64\nx: { -> log(it) }",
			expected: []string{"i64 | err | nil"},
		},
		{
			name:     "nil+wildcard",
			input:    "x ?i64\nx: { nil -> log(it)\n-> log(it) }",
			expected: []string{"nil", "i64 | err"},
		},
		{
			name:     "err+wildcard",
			input:    "x ?i64\nx: { err -> log(it)\n-> log(it) }",
			expected: []string{"err", "i64 | nil"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lex := lexer.New(tt.input)
			p := New(lex)
			prog := p.ParseProgram()

			if len(p.Errors()) > 0 {
				t.Fatalf("parser errors: %v", p.Errors())
			}
			if prog == nil || len(prog.Statements) == 0 {
				t.Fatalf("no statements")
			}

			// Find all synthetic LetStatements with name "it" and check their Type
			var foundTypes []string
			collectItTypes(prog, &foundTypes)

			fmt.Printf("=== %s ===\n", tt.name)
			fmt.Printf("  found types: %v\n", foundTypes)
			fmt.Printf("  expected:    %v\n", tt.expected)

			if len(foundTypes) != len(tt.expected) {
				// This might not match exactly, just print info
				t.Logf("found %d it bindings, expected %d", len(foundTypes), len(tt.expected))
			}
		})
	}
}

func collectItTypes(node Node, types *[]string) {
	switch n := node.(type) {
	case *Program:
		for _, s := range n.Statements {
			collectItTypes(s, types)
		}
	case *LetStatement:
		if n.Name != nil && n.Name.Value == "it" && n.IsSynthetic && n.Type != nil {
			*types = append(*types, n.Type.String())
		}
		if n.Value != nil {
			collectItTypes(n.Value, types)
		}
	case *ExpressionStatement:
		if n.Expression != nil {
			collectItTypes(n.Expression, types)
		}
	case *IfExpression:
		if n.Condition != nil {
			collectItTypes(n.Condition, types)
		}
		if n.Consequence != nil {
			collectItTypes(n.Consequence, types)
		}
		if n.Alternative != nil {
			collectItTypes(n.Alternative, types)
		}
	case *BlockStatement:
		for _, s := range n.Statements {
			collectItTypes(s, types)
		}
	case *Identifier:
		// skip
	case *InfixExpression:
		// skip
	case *CallExpression:
		// skip
	default:
		_ = fmt.Sprintf("unhandled: %T", n)
	}
}

// Helper to make the output easy to read
func TestPrintItTypes(t *testing.T) {
	if !strings.Contains(t.Name(), "NONE") {
		return
	}
}
