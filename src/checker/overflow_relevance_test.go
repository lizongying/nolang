package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestOverflowRelevantSet verifies the conservative (over-approximating)
// classifier that decides which statements contain integer-overflow-prone
// arithmetic. A statement NOT in the set is provably free of overflow, so any
// #{overflow=...} annotation governing it is safe to strip.
func TestOverflowRelevantSet(t *testing.T) {
	// `s = "a" - "b"` is char/string concatenation (`-` is concat, not
	// subtraction) → not overflow-prone. `n = 1 + 2` is integer arithmetic →
	// overflow-prone.
	src := "main = () {\n" +
		"    s = \"a\" - \"b\"\n" +
		"    n = 1 + 2\n" +
		"}\n"

	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	relevant, _ := OverflowAnnotationRelevance(program)
	if relevant == nil {
		t.Fatal("expected non-nil relevant set")
	}

	var concatStmt, arithStmt parser.Statement
	for _, fn := range program.Statements {
		fd, ok := fn.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		for _, s := range fd.Body.Statements {
			v := statementValue(s)
			if v == nil {
				continue
			}
			if isStringConcatExpr(v) {
				concatStmt = s
			} else if isIntArithExpr(v) {
				arithStmt = s
			}
		}
	}
	if concatStmt == nil {
		t.Fatal("string-concat statement not found")
	}
	if arithStmt == nil {
		t.Fatal("integer-arithmetic statement not found")
	}

	if relevant[concatStmt] {
		t.Errorf("string-concat statement should NOT be in the relevant set")
	}
	if !relevant[arithStmt] {
		t.Errorf("integer-arithmetic statement MUST be in the relevant set")
	}
}

// TestOverflowGovernedMap verifies that standalone #{overflow=...} annotation
// nodes (those that cannot be attached to an IDENT-initial statement — here,
// ones above DOT-initial field assignments inside a struct method) are mapped
// to the statement they govern, and that the governed statement's membership in
// the relevant set correctly flags the annotation as effective or ineffective.
func TestOverflowGovernedMap(t *testing.T) {
	// `.x = .x + 1` is integer arithmetic → overflow effective.
	// `.s = "a" - "b"` is char concatenation (`-` is concat, not subtraction) →
	// overflow ineffective. Both are DOT-initial so the overflow annotations stay
	// as standalone AnnotationStatement nodes (cannot be attached).
	src := "Point = {\n" +
		"  x i64\n" +
		"  s str\n" +
		"}\n" +
		"Point.inc = (self Point) (self Point) {\n" +
		"  #{overflow=wrap}\n" +
		"  .x = .x + 1\n" +
		"}\n" +
		"Point.cat = (self Point) (self Point) {\n" +
		"  #{overflow=wrap}\n" +
		"  .s = \"a\" - \"b\"\n" +
		"}\n"

	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	relevant, governed := OverflowAnnotationRelevance(program)
	if relevant == nil {
		t.Fatal("expected non-nil relevant set")
	}

	// Collect standalone annotation nodes present in the method bodies.
	var annoNodes []*parser.AnnotationStatement
	var concatStmt, arithStmt parser.Statement
	for _, fn := range program.Statements {
		fd, ok := fn.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		for _, s := range fd.Body.Statements {
			if as, ok := s.(*parser.AnnotationStatement); ok {
				annoNodes = append(annoNodes, as)
			}
			v := statementValue(s)
			if v == nil {
				continue
			}
			if isStringConcatExpr(v) {
				concatStmt = s
			} else if isIntArithExpr(v) {
				arithStmt = s
			}
		}
	}
	if len(annoNodes) != 2 {
		t.Fatalf("expected 2 standalone annotation nodes, got %d", len(annoNodes))
	}
	if concatStmt == nil || arithStmt == nil {
		t.Fatal("could not locate the two governed statements")
	}

	if relevant[concatStmt] {
		t.Errorf("concat statement should not be in relevant set")
	}
	if !relevant[arithStmt] {
		t.Errorf("arithmetic statement must be in relevant set")
	}

	// Each standalone overflow node must govern exactly one of the statements,
	// and its effectiveness must follow `relevant`.
	effective, ineffective := 0, 0
	for _, as := range annoNodes {
		gov, ok := governed[as]
		if !ok {
			t.Errorf("standalone overflow node not present in governed map")
			continue
		}
		if gov == nil {
			t.Errorf("governed statement is nil for a standalone overflow node")
			continue
		}
		if gov == concatStmt {
			ineffective++
			if relevant[gov] {
				t.Errorf("overflow over concat statement must be ineffective")
			}
		} else if gov == arithStmt {
			effective++
			if !relevant[gov] {
				t.Errorf("overflow over arithmetic statement must be effective")
			}
		} else {
			t.Errorf("overflow node governs an unexpected statement %T", gov)
		}
	}
	if effective != 1 || ineffective != 1 {
		t.Errorf("expected 1 effective and 1 ineffective, got effective=%d ineffective=%d", effective, ineffective)
	}
}

// TestLintIneffectiveOverflow emits a WARNING (source nolang-overflow-ineffective)
// for an #{overflow=...} annotation that governs a statement with no integer
// arithmetic, and does NOT emit one for a genuinely effective annotation.
func TestLintIneffectiveOverflow(t *testing.T) {
	src := "Point = {\n" +
		"  x i64\n" +
		"  s str\n" +
		"}\n" +
		"Point.inc = (self Point) (self Point) {\n" +
		"  #{overflow=wrap}\n" +
		"  .x = .x + 1\n" +
		"}\n" +
		"Point.cat = (self Point) (self Point) {\n" +
		"  #{overflow=wrap}\n" +
		"  .s = \"a\" - \"b\"\n" +
		"}\n"

	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	results := LintIneffectiveOverflow(program)
	var warnings int
	for _, r := range results {
		if r.Source != "nolang-overflow-ineffective" {
			t.Errorf("unexpected lint source %q: %s", r.Source, r.Message)
			continue
		}
		if r.Severity != LintWarning {
			t.Errorf("expected LintWarning, got %v", r.Severity)
		}
		warnings++
	}
	// Exactly one ineffective annotation (over `.s = "a" - "b"`); the effective
	// one over `.x = .x + 1` must NOT be reported.
	if warnings != 1 {
		t.Errorf("expected exactly 1 ineffective-overflow warning, got %d", warnings)
	}
}

// statementValue extracts the value/expression of a declaration or bare
// expression statement (both `x = expr` and `x T = expr` parse as
// LetStatement; a bare expression is an ExpressionStatement).
func statementValue(s parser.Statement) parser.Expression {
	switch v := s.(type) {
	case *parser.LetStatement:
		return v.Value
	case *parser.ExpressionStatement:
		if e, ok := v.Expression.(*parser.AssignExpression); ok {
			return e.Value
		}
		return v.Expression
	case *parser.ReturnStatement:
		return v.ReturnValue
	}
	return nil
}

// isStringConcatExpr reports whether e is a char/string concatenation
// (`a - b` where both operands are char/string literals or identifiers). The
// relevant-set classifier treats unknown operand types conservatively as
// integers, so a statement with only string/char operands is the case we confirm
// is excluded from the relevant set.
func isStringConcatExpr(e parser.Expression) bool {
	infix, ok := e.(*parser.InfixExpression)
	if !ok || infix.Operator != "-" {
		return false
	}
	return isStringExpr(infix.Left) && isStringExpr(infix.Right)
}

func isStringExpr(e parser.Expression) bool {
	if e == nil {
		return false
	}
	_, isStrLit := e.(*parser.StringLiteral)
	_, isCharLit := e.(*parser.CharLiteral)
	_, isIdent := e.(*parser.Identifier)
	return isStrLit || isCharLit || isIdent
}

// isIntArithExpr reports whether e is an integer arithmetic infix (`+`/`-`).
func isIntArithExpr(e parser.Expression) bool {
	infix, ok := e.(*parser.InfixExpression)
	if !ok {
		return false
	}
	return infix.Operator == "+" || infix.Operator == "-"
}
