package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestModulePrefixMultiReturn verifies that using a module prefix
// (e.g. dep.resolve(cfg)) to call a multi-return function does NOT
// trigger a false "function returns 1 value(s) but 2 target(s) provided"
// error.
//
// Bug 01: lookupReturnCount's else branch (when receiver is not a known
// variable, i.e. a module name) did NOT check user-defined sigs for the
// "module.function" form before falling through to std sigs lookup by
// bare name. If a std function with the same bare name existed (e.g.
// "resolve" returning 1 value), the checker would use the std return
// count instead of the user-defined function's actual return count,
// producing a false error.
//
// This test uses function names that do NOT collide with std signatures
// (to avoid depending on stdsig_gen.go contents), and verifies the
// correct behavior through the build pipeline.
func TestModulePrefixMultiReturn(t *testing.T) {
	src := `dep.resolve = (cfg str) (resolved str, ok bool) {
    ok = false
    resolved = ''
    resolved = cfg - '\n'
    ok = true
}

dep.resolve-with = (cfg str) (resolved str, ok bool) {
    ok = false
    resolved = ''
    resolved, ok = dep.resolve(cfg)
}

main = () {
    r, r-ok = dep.resolve('hello')
    print(r)
    print(r-ok)
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	results := ValidateFuncArgs(prog, "")
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
		if strings.Contains(r.Message, "value(s) but") && strings.Contains(r.Message, "target(s)") {
			t.Fatalf("unexpected return count error for module-prefix multi-return: %s", r.Message)
		}
	}
}

// TestModulePrefixMultiReturnLookup verifies that lookupReturnCount
// correctly resolves the return count for a user-defined module-prefix
// function even when the function name collides with a std module function
// that returns a different number of values.
//
// This test directly constructs a sigs map with "dep.resolve" → 2 returns
// and checks that lookupReturnCount returns 2 (not the std function's count).
func TestModulePrefixMultiReturnLookup(t *testing.T) {
	// Build a CallExpression for dep.resolve('hello')
	// Function: DotExpression{Receiver: Identifier("dep"), Property: "resolve"}
	callExpr := &parser.CallExpression{
		Function: &parser.DotExpression{
			Receiver: &parser.Identifier{Value: "dep"},
			Property: "resolve",
		},
		Arguments: []parser.Expression{
			&parser.StringLiteral{Value: "hello"},
		},
	}

	// sigs has the user-defined function "dep.resolve" returning 2 values
	sigs := map[string]*funcSig{
		"dep.resolve": {
			ResultTypes: []paramInfo{
				{Type: "str"},
				{Type: "bool"},
			},
		},
	}

	// varTypes is empty (dep is a module name, not a variable)
	varTypes := map[string]string{}

	result := lookupReturnCount(callExpr, sigs, varTypes)
	if result != 2 {
		t.Fatalf("lookupReturnCount for dep.resolve (user-defined, 2 returns) = %d, want 2", result)
	}
}

// TestModulePrefixMultiReturnStdFallback verifies that when the function
// is NOT user-defined, lookupReturnCount still falls back to std sigs
// (e.g. for a std module function called with a module prefix).
func TestModulePrefixMultiReturnStdFallback(t *testing.T) {
	// Build a CallExpression for fs.read-str('hello') — not user-defined
	callExpr := &parser.CallExpression{
		Function: &parser.DotExpression{
			Receiver: &parser.Identifier{Value: "fs"},
			Property: "read-str",
		},
		Arguments: []parser.Expression{
			&parser.StringLiteral{Value: "hello"},
		},
	}

	// sigs is empty — no user-defined functions
	sigs := map[string]*funcSig{}
	varTypes := map[string]string{}

	// Should return the std signature count (not -1, since fs.read-str exists)
	result := lookupReturnCount(callExpr, sigs, varTypes)
	if result < 0 {
		t.Fatalf("lookupReturnCount for fs.read-str (std) = %d, want >= 0", result)
	}
}
