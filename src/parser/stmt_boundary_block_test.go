package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// This file pins two silent-miscompile fixes in the statement layer. Both bugs
// were pre-existing, produced rc=0 with no diagnostic, and are invisible to the
// type checker because the damaged AST is still "well typed":
//
//  1. A bare `{ ... }` block at STATEMENT position was classified by
//     classifyBlockAtCurrent as a declaration (blockEnum / blockIface /
//     blockTaggedEnum) and routed to parseExpressionStatement, whose LBRACE
//     case (expr.go) accepts only blockStruct / blockMatch and otherwise does
//     `p.nextToken(); return nil`. The `{` was eaten, the block's inner
//     statements became SIBLINGS of the enclosing body, and the block's own
//     `}` closed the ENCLOSING block — so every statement after the bare block
//     leaked one level outward.
//
//  2. `awy` and `co` must be in isStatementBoundary(). When either begins a
//     statement that is NOT the first statement of a block, the previous
//     statement's skipToStatementEnd() skips over it (NEWLINE is deliberately
//     not a boundary). An `awy { ... }` group would lose its keyword, and an
//     in-group `co f(x)` would degrade to a SYNCHRONOUS `f(x)` (no spawn).

// parseSrc parses src and fails the test on any parser error.
func parseSrc(t *testing.T, src string) *Program {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v\n--- src ---\n%s", errs, src)
	}
	return prog
}

// topFunc returns the top-level *FunctionDefinition named name.
func topFunc(t *testing.T, prog *Program, name string) *FunctionDefinition {
	t.Helper()
	for _, s := range prog.Statements {
		if fd, ok := s.(*FunctionDefinition); ok && fd.Name == name {
			return fd
		}
	}
	t.Fatalf("no top-level *FunctionDefinition named %q (top level has %d stmts)",
		name, len(prog.Statements))
	return nil
}

// exprStmt asserts that s is an *ExpressionStatement and returns its expression.
func exprStmt(t *testing.T, s Statement, where string) Expression {
	t.Helper()
	es, ok := s.(*ExpressionStatement)
	if !ok {
		t.Fatalf("%s: statement is %T, want *ExpressionStatement", where, s)
	}
	return es.Expression
}

// TestBareBlockDoesNotLeakFollowingStatements is the core pin for bug 1.
//
// Before the fix, `f`'s body was truncated at the bare block's `}` and
// `print(x)` / `print(99)` became TOP-LEVEL statements (so `f()` printed
// nothing). The program still compiled and ran with rc=0.
func TestBareBlockDoesNotLeakFollowingStatements(t *testing.T) {
	src := `f = () {
  x = 1
  {
    x = 2
  }
  print(x)
  print(99)
}
f()
`
	prog := parseSrc(t, src)

	// Exactly two top-level statements: the function definition and the call.
	// A leak would add the trailing `print` statements here.
	if len(prog.Statements) != 2 {
		t.Fatalf("top level has %d statements, want 2 — statements after the bare "+
			"block leaked out of the function body", len(prog.Statements))
	}
	if _, ok := prog.Statements[0].(*FunctionDefinition); !ok {
		t.Fatalf("statement 0 is %T, want *FunctionDefinition", prog.Statements[0])
	}

	fd := topFunc(t, prog, "f")
	if fd.Body == nil {
		t.Fatal("function f has a nil body")
	}
	if got := len(fd.Body.Statements); got != 4 {
		t.Fatalf("f body has %d statements, want 4 (let, bare block, print, print)", got)
	}
	if _, ok := fd.Body.Statements[0].(*LetStatement); !ok {
		t.Errorf("f body[0] is %T, want *LetStatement", fd.Body.Statements[0])
	}
	if _, ok := fd.Body.Statements[1].(*BlockStatement); !ok {
		t.Errorf("f body[1] is %T, want *BlockStatement (the bare block must stay "+
			"a statement, not be swallowed as an expression)", fd.Body.Statements[1])
	}
	exprStmt(t, fd.Body.Statements[2], "f body[2]")
	exprStmt(t, fd.Body.Statements[3], "f body[3]")
}

// TestBareBlockWithCallFirstDoesNotLeak covers the blockIface classification:
// a bare block whose first member is `print(1)` looked like an interface
// declaration (`ord { t.gt(...) }`) and leaked the same way.
func TestBareBlockWithCallFirstDoesNotLeak(t *testing.T) {
	src := `f = () {
  print(1)
  {
    print(2)
  }
  print(3)
}
f()
`
	prog := parseSrc(t, src)
	if len(prog.Statements) != 2 {
		t.Fatalf("top level has %d statements, want 2", len(prog.Statements))
	}
	fd := topFunc(t, prog, "f")
	if got := len(fd.Body.Statements); got != 3 {
		t.Fatalf("f body has %d statements, want 3", got)
	}
	if _, ok := fd.Body.Statements[1].(*BlockStatement); !ok {
		t.Errorf("f body[1] is %T, want *BlockStatement", fd.Body.Statements[1])
	}
}

// TestBareBlockAtTopLevelDoesNotSwallowFollowingStatements pins the same
// behaviour at top level (no enclosing function).
func TestBareBlockAtTopLevelDoesNotSwallowFollowingStatements(t *testing.T) {
	src := `x = 1
{
  x = 2
}
print(x)
`
	prog := parseSrc(t, src)
	if len(prog.Statements) != 3 {
		t.Fatalf("top level has %d statements, want 3 (let, block, print) — the "+
			"block swallowed the following statement", len(prog.Statements))
	}
	if _, ok := prog.Statements[1].(*BlockStatement); !ok {
		t.Errorf("statement 1 is %T, want *BlockStatement", prog.Statements[1])
	}
	exprStmt(t, prog.Statements[2], "statement 2")
}

// TestStatementBoundaryIncludesAwyAndCo pins bug 2 under the new coroutine
// syntax. isStatementBoundary() is the whitelist of tokens that can BEGIN a
// statement; skipToStatementEnd() advances until it sees one. `awy` (group
// start) and `co` (in-group spawn) must be boundaries, otherwise a preceding
// statement's skipToStatementEnd() swallows them when they are non-first.
func TestStatementBoundaryIncludesAwyAndCo(t *testing.T) {
	for _, tok := range []lexer.TokenType{lexer.AWY, lexer.CO} {
		if !isStatementBoundary(tok) {
			t.Errorf("isStatementBoundary(%v) = false, want true — skipToStatementEnd "+
				"will swallow this keyword when it begins a non-first statement", tok)
		}
	}
}

// TestAwaitGroupSurvivesAsNonFirstStatement pins the AST consequence of bug 2
// under the new syntax: an `awy { ... }` group that is not the first statement
// must still be parsed as a BlockStatement with IsAwaitGroup set, and the
// in-group `co` calls must not be swallowed by the preceding statement.
func TestAwaitGroupSurvivesAsNonFirstStatement(t *testing.T) {
	src := `f = () {
  y = 1
  awy {
    r1 = co dbl(21)
    r2 = co dbl(22)
  }
}
`
	prog := parseSrc(t, src)
	fd := topFunc(t, prog, "f")
	if fd.Body == nil {
		t.Fatal("function f has a nil body")
	}
	if got := len(fd.Body.Statements); got != 2 {
		t.Fatalf("f body has %d statements, want 2 (let, awy group)", got)
	}
	if _, ok := fd.Body.Statements[0].(*LetStatement); !ok {
		t.Errorf("f body[0] is %T, want *LetStatement", fd.Body.Statements[0])
	}
	group, ok := fd.Body.Statements[1].(*BlockStatement)
	if !ok {
		t.Fatalf("f body[1] is %T, want *BlockStatement (awy group)", fd.Body.Statements[1])
	}
	if !group.IsAwaitGroup {
		t.Error("f body[1] BlockStatement.IsAwaitGroup = false, want true")
	}
	if got := len(group.Statements); got != 2 {
		t.Fatalf("await group has %d statements, want 2 co spawns", got)
	}
}

// TestCoOutsideGroupIsRejected pins the new rule: co <call> is only legal
// inside an `awy { ... }` group.
func TestCoOutsideGroupIsRejected(t *testing.T) {
	src := `f = () {
  r = co dbl(21)
}
`
	p := New(lexer.New(src))
	p.ParseProgram()
	found := false
	for _, d := range p.Diagnostics() {
		if d.Code == "E_CO_OUTSIDE_GROUP" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected E_CO_OUTSIDE_GROUP diagnostic for group-external co, got: %+v",
			p.Diagnostics())
	}
}

// TestBareBlockDeclarationControlsAreUnaffected guards the widened statement
// dispatch: routing bare `{` blocks to the statement path must not break the
// name-prefixed DECLARATION forms, which are dispatched earlier via
// classifyBlock() from the IDENT + LBRACE branch.
func TestBareBlockDeclarationControlsAreUnaffected(t *testing.T) {
	t.Run("enum", func(t *testing.T) {
		prog := parseSrc(t, "color {\n  red, green, blue\n}\n")
		if _, ok := prog.Statements[0].(*EnumDefinition); !ok {
			t.Fatalf("statement 0 is %T, want *EnumDefinition", prog.Statements[0])
		}
	})
	t.Run("interface", func(t *testing.T) {
		prog := parseSrc(t, "ord {\n  t.gt(b t) (res bool)\n}\n")
		if _, ok := prog.Statements[0].(*InterfaceDefinition); !ok {
			t.Fatalf("statement 0 is %T, want *InterfaceDefinition", prog.Statements[0])
		}
	})
	t.Run("struct", func(t *testing.T) {
		prog := parseSrc(t, "Point {\n  f i64\n  g str\n}\n")
		if _, ok := prog.Statements[0].(*StructDefinition); !ok {
			t.Fatalf("statement 0 is %T, want *StructDefinition", prog.Statements[0])
		}
	})
	// An anonymous struct-literal `{ field: value }` must STILL be an
	// expression statement (blockStruct deliberately stays on the expression
	// path); it must not be demoted to a statement block.
	t.Run("anonymous_struct_literal_still_expression", func(t *testing.T) {
		prog := parseSrc(t, "p = { f: 1 }\n")
		let, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("statement 0 is %T, want *LetStatement", prog.Statements[0])
		}
		if _, ok := let.Value.(*StructLiteral); !ok {
			t.Fatalf("RHS is %T, want *StructLiteral", let.Value)
		}
	})
}
