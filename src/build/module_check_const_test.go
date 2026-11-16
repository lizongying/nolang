package build

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// These guards pin the MODULE-CONSTANT half of checkUnresolvedModuleCalls.
//
// The function-call half (`mod.missing-fn()`) has been checked for a long time;
// the CONSTANT half (`mod.MISSING-CONST`) was not, and the failure mode was
// invisible: `resolveModuleCalls` only flattens constants that really exist, so
// a misspelt name survived as a DotExpression, lowered to NoVal, and surfaced
// far away in codegen as
//
//	EmitLLVM: MIR->LLVM: func f: consumer reads value 0 (NoVal) ... (bug #85)
//
// which names neither the module nor the identifier. Measured on
// compiler/tests/verify-struct-elem.no, whose `asp.K_LET_STATEMENT` never
// existed in compiler/src/asp.no at all: `no vet` said "No issues found".
//
// WHY THE ASSERTION IS ON checkUnresolvedModuleCalls, NOT ON `no vet` OUTPUT
// ------------------------------------------------------------------------
// The checker is what `no vet` and `no build` both call; testing it directly
// keeps the case independent of CLI plumbing and of the module graph on disk.

// parseForModuleCheck parses src and returns the program plus its import list,
// which is what the transpiler would pass to checkUnresolvedModuleCalls.
func parseForModuleCheck(t *testing.T, src string, imports []string) *parser.Program {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if prog.Sem == nil {
		t.Fatal("ParseProgram did not attach a SemanticContext")
	}
	return prog
}

// A misspelt module CONSTANT must be reported, naming the module and the name.
func TestUnresolvedModuleConstantReported(t *testing.T) {
	src := `x = asp.K_LET_STATEMENT
print(x.to-str())
`
	prog := parseForModuleCheck(t, src, []string{"asp"})
	err := checkUnresolvedModuleCalls(prog, []string{"asp"})
	if err == nil {
		t.Fatal("checkUnresolvedModuleCalls accepted `asp.K_LET_STATEMENT`, which no module declares; " +
			"the reference lowers to NoVal and only fails later in codegen as bug #85")
	}
	msg := err.Error()
	if !strings.Contains(msg, "K_LET_STATEMENT") || !strings.Contains(msg, "top-level constant") {
		t.Fatalf("diagnostic does not name the missing constant: %s", msg)
	}
}

// A constant that IS declared at module level must pass — including one
// referenced through a module qualifier that resolveModuleCalls may or may not
// have flattened by the time this check runs.
func TestDeclaredModuleConstantAccepted(t *testing.T) {
	src := `K_LET_STATEMENT = 44
x = asp.K_LET_STATEMENT
print(x.to_str())
`
	prog := parseForModuleCheck(t, src, []string{"asp"})
	if err := checkUnresolvedModuleCalls(prog, []string{"asp"}); err != nil {
		t.Fatalf("declared module constant rejected: %v", err)
	}
}

// Lowercase member reads are FIELD/METHOD access, never module constants, and
// must stay accepted: a variable may legitimately shadow a module short name
// (`path` is both a std module and the conventional parameter name).
func TestLowercaseMemberReadAccepted(t *testing.T) {
	src := `f = (path str) (n i64) {
    n = path.len()
}
print(f('abc').to_str())
`
	prog := parseForModuleCheck(t, src, []string{"path"})
	if err := checkUnresolvedModuleCalls(prog, []string{"path"}); err != nil {
		t.Fatalf("lowercase member read rejected: %v", err)
	}
}

// std modules carry no merged `let` (their bodies are builtin stubs), so an
// uppercase member read on a std short name cannot be validated here and must
// NOT be reported.
func TestStdModuleConstantNotReported(t *testing.T) {
	src := `x = math.PI
print(x.to_str())
`
	prog := parseForModuleCheck(t, src, []string{"math"})
	if err := checkUnresolvedModuleCalls(prog, []string{"math"}); err != nil {
		t.Fatalf("std module constant rejected: %v", err)
	}
}
