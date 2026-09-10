package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD (synthetic receiver must not be checked).
//
// For `t.method = (…) (…) { … }` the parser injects a synthetic first
// parameter `self` whose Type is the receiver name (parser/decl.go). That
// parameter is not user-written syntax, so ValidateCrossModuleTypeRefs must skip
// it. The JS-backend stubs under src/js use namespace names that collide with
// std structs (`json.parse`, `timer.x`), which used to produce a bogus
// "type 'json' not found; did you mean 'json.json'?" error.
func TestCrossModuleTypeRefsSkipsSyntheticReceiver(t *testing.T) {
	src := "json.parse = (data str) (obj str) {\n    obj = data\n}\n"
	prog := parser.New(lexer.New(src)).ParseProgram()
	for _, r := range ValidateCrossModuleTypeRefs(prog) {
		if strings.Contains(r.Message, "not found") {
			t.Fatalf("synthetic receiver flagged as a cross-module type ref: %s", r.Message)
		}
	}
}

// Control: a genuine cross-module struct used as a *user-written* parameter
// type is still reported (the receiver skip must not weaken the lint).
func TestCrossModuleTypeRefsStillFlagsRealParam(t *testing.T) {
	src := "f = (x json) (out i64) {\n    out = 0\n}\n"
	prog := parser.New(lexer.New(src)).ParseProgram()
	found := false
	for _, r := range ValidateCrossModuleTypeRefs(prog) {
		if strings.Contains(r.Message, "'json'") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a cross-module type error for user-written parameter type `json`, got none")
	}
}
