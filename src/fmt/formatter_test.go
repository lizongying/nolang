package fmt

import (
	"fmt"
	"strings"
	"testing"
)

// cd ./src/fmt && go test -v . -run TestFormatBasic/method_definition
func TestFormatBasic(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "simple identifier",
			input:    "x",
			expected: "x",
		},
		{
			name:     "function definition",
			input:    "add(a int,b int){a+b}",
			expected: "add(a int, b int) {\n    a + b\n}",
		},
		{
			name:     "operator spacing",
			input:    "a+b",
			expected: "a + b",
		},
		{
			name:     "multiple operators",
			input:    "a+b*c",
			expected: "a + b * c",
		},
		{
			name:     "comparison operators",
			input:    "a==b",
			expected: "a == b",
		},
		{
			name:     "not equals",
			input:    "a!=b",
			expected: "a != b",
		},
		{
			name:     "comma spacing in function call",
			input:    "add(1,2)",
			expected: "add(1, 2)",
		},
		{
			name:     "nested function calls",
			input:    "add(1,add(2,3))",
			expected: "add(1, add(2, 3))",
		},
		{
			name:     "variable declaration",
			input:    "x=5",
			expected: "x = 5",
		},
		{
			name:     "typed variable declaration",
			input:    "a i8=2",
			expected: "a i8 = 2",
		},
		{
			name:     "if statement",
			input:    "if x>0{x=1}else{x=0}",
			expected: "if x > 0 {\n    x = 1\n} else {\n    x = 0\n}",
		},
		{
			name:     "for loop",
			input:    "for x<10{x=x+1}",
			expected: "for x < 10 {\n    x = x + 1\n}",
		},
		{
			name:     "infinite for loop with break",
			input:    "for{break}",
			expected: "for {\n    break\n}",
		},
		{
			name:     "return statement",
			input:    "foo(a int){return}",
			expected: "foo(a int) {\n    return\n}",
		},
		{
			name:     "boolean literals",
			input:    "true",
			expected: "true",
		},
		{
			name:     "nil literal",
			input:    "nil",
			expected: "nil",
		},
		{
			name:     "string literal",
			input:    `'hello'`,
			expected: "'hello'",
		},
		{
			name:     "float literal",
			input:    "3.14",
			expected: "3.14",
		},
		{
			name:     "complex expression",
			input:    "a+b*c-d/e",
			expected: "a + b * c-d / e",
		},
		{
			name:     "logical and",
			input:    "a&&b",
			expected: "a && b",
		},
		{
			name:     "logical or",
			input:    "a||b",
			expected: "a || b",
		},
		{
			name:     "not operator",
			input:    "!a",
			expected: "! a",
		},
		{
			name: "method_definition",
			input: strings.TrimSpace(`
str.len() (n    i64)      {
    n = .len
}
			`),
			expected: strings.TrimSpace(`
str.len() (n i64) {
    n = .len
}
			`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			fmt.Println("tt.input", tt.input)
			fmt.Println("result", result)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatIndentation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "nested blocks",
			input:    "outer(){inner(){x=1}}",
			expected: "outer() {\n    inner() {\n        x = 1\n    }\n}",
		},
		{
			name:     "deep nesting",
			input:    "if x>0{if x>0{if x>0{x=1}}}",
			expected: "if x > 0 {\n    if x > 0 {\n        if x > 0 {\n            x = 1\n        }\n    }\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatFunction(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "function with no args",
			input:    "hello(){return}",
			expected: "hello() {\n    return\n}",
		},
		{
			name:     "function with multiple args",
			input:    "add(a int,b int,c int){a+b+c}",
			expected: "add(a int, b int, c int) {\n    a + b + c\n}",
		},
		{
			name:     "method with result parameter",
			input:    "str.len() (n i64) {n = .len}",
			expected: "str.len() (n i64) {\n    n = .len\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Format(tt.input)
			if result != tt.expected {
				t.Errorf("Format(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
