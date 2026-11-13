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
	p.SkipLowering = true
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
// nodes are mapped to the statement they govern, and that the governed
// statement's membership in the relevant set correctly flags the annotation as
// effective or ineffective.
//
// A standalone node only arises when the following statement's first token is
// NOT an attachable one (IDENT / USE / IN / RARROW / DOT / array-type method).
// The shape used here is the bare block in statement position — `{` stays
// unattachable on purpose: it is an L2 block host, and `#{overflow}` is L3-only
// (see docs/docs/lang/annotation-scope.md §8 step 4b). DOT-initial statements
// became attachable in ASTVersion 2026-11-12.1, so they no longer produce
// standalone nodes — that side is pinned by TestOverflowDotInitialAttaches.
func TestOverflowGovernedMap(t *testing.T) {
	// The first block contains integer arithmetic → overflow effective.
	// The second contains only char concatenation (`-` is concat, not
	// subtraction) → overflow ineffective.
	src := "Point = {\n" +
		"  x i64\n" +
		"  s str\n" +
		"}\n" +
		"Point.inc = (self Point) (self Point) {\n" +
		"  #{overflow=wrap}\n" +
		"  {\n" +
		"    .x = .x + 1\n" +
		"  }\n" +
		"}\n" +
		"Point.cat = (self Point) (self Point) {\n" +
		"  #{overflow=wrap}\n" +
		"  {\n" +
		"    .s = \"a\" - \"b\"\n" +
		"  }\n" +
		"}\n"

	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipLowering = true
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	relevant, governed := OverflowAnnotationRelevance(program)
	if relevant == nil {
		t.Fatal("expected non-nil relevant set")
	}

	// Collect standalone annotation nodes and the block statements in the
	// method bodies, then classify each block by what it contains.
	var annoNodes []*parser.AnnotationStatement
	var arithBlock, concatBlock *parser.BlockStatement
	for _, fn := range program.Statements {
		fd, ok := fn.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		for _, s := range fd.Body.Statements {
			if as, ok := s.(*parser.AnnotationStatement); ok {
				annoNodes = append(annoNodes, as)
				continue
			}
			bs, ok := s.(*parser.BlockStatement)
			if !ok {
				continue
			}
			isConcat, isArith := false, false
			for _, inner := range bs.Statements {
				v := statementValue(inner)
				if v == nil {
					continue
				}
				// Order matters: `-` is both concat and arithmetic, so check the
				// string-only shape first (same precedence as OverflowRelevantSet).
				if isStringConcatExpr(v) {
					isConcat = true
				} else if isIntArithExpr(v) {
					isArith = true
				}
			}
			if isConcat {
				concatBlock = bs
			} else if isArith {
				arithBlock = bs
			}
		}
	}
	if len(annoNodes) != 2 {
		t.Fatalf("expected 2 standalone annotation nodes, got %d", len(annoNodes))
	}
	if concatBlock == nil || arithBlock == nil {
		t.Fatal("could not locate the two governed block statements")
	}

	if relevant[concatBlock] {
		t.Errorf("block with only concat should not be in relevant set")
	}
	if !relevant[arithBlock] {
		t.Errorf("block with integer arithmetic must be in relevant set")
	}

	// Each standalone overflow node must govern exactly one of the blocks, and
	// its effectiveness must follow `relevant`.
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
		if gov == concatBlock {
			ineffective++
			if relevant[gov] {
				t.Errorf("overflow over concat block must be ineffective")
			}
		} else if gov == arithBlock {
			effective++
			if !relevant[gov] {
				t.Errorf("overflow over arithmetic block must be effective")
			}
		} else {
			t.Errorf("overflow node governs an unexpected statement %T", gov)
		}
	}
	if effective != 1 || ineffective != 1 {
		t.Errorf("expected 1 effective and 1 ineffective, got effective=%d ineffective=%d", effective, ineffective)
	}
}

// TestOverflowDotInitialAttaches pins the ASTVersion 2026-11-12.1 behaviour:
// `#{overflow=...}` above a DOT-initial statement (`.x = .x + 1`) attaches to
// that statement — OverflowMode is set on it and no standalone
// AnnotationStatement is left behind. Before the DOT attach target was added the
// annotation degraded into a dead line (a standalone node nobody reads), which is
// why the checker kept reporting the site.
func TestOverflowDotInitialAttaches(t *testing.T) {
	src := "Point = {\n" +
		"  x i64\n" +
		"}\n" +
		"Point.inc = (self Point) (self Point) {\n" +
		"  #{overflow=wrap}\n" +
		"  .x = .x + 1\n" +
		"}\n"

	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipLowering = true
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	var attached int
	var standalone int
	for _, fn := range program.Statements {
		fd, ok := fn.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		for _, s := range fd.Body.Statements {
			if _, ok := s.(*parser.AnnotationStatement); ok {
				standalone++
				continue
			}
			if es, ok := s.(*parser.ExpressionStatement); ok && es.OverflowMode != "" {
				attached++
				if es.OverflowMode != "wrap" {
					t.Errorf("expected OverflowMode=wrap, got %q", es.OverflowMode)
				}
			}
		}
	}
	if standalone != 0 {
		t.Errorf("expected 0 standalone annotation nodes, got %d", standalone)
	}
	if attached != 1 {
		t.Errorf("expected 1 statement carrying OverflowMode, got %d", attached)
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
	p.SkipLowering = true
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	results := LintIneffectiveOverflow(program, "src/app.no")
	var warnings int
	for _, r := range results {
		if r.Source != "nolang-overflow-ineffective" {
			t.Errorf("unexpected lint source %q: %s", r.Source, r.Message)
			continue
		}
		if r.Severity != LintWarning {
			t.Errorf("expected LintWarning, got %v", r.Severity)
		}
		// Every emitted diagnostic must carry its TraceID: the CLI / LSP print it
		// as a `[id]` suffix, and an empty one used to render as a bare `[]`.
		if r.TraceID == "" {
			t.Errorf("ineffective-overflow warning must carry a TraceID, got empty")
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

// TestOverflowBigintArithmeticIsIrrelevant pins the bigint half of the
// classifier: `bigint` is an arbitrary-precision integer, so `acc * radix` on a
// bigint accumulator can never overflow a fixed-width integer and an
// `#{overflow=...}` annotation governing it is dead weight.
//
// Regression for the two-pipeline mismatch that made `no vet` report
// nolang-overflow-ineffective on src/std/txt.no while `no fmt -d` kept the
// annotations: the single-file fmt path has an empty validationFuncTypes, so
// `acc = bigint.zero()` used to infer nothing and `acc` fell back to the
// conservative "unknown ⇒ signed integer" branch.
func TestOverflowBigintArithmeticIsIrrelevant(t *testing.T) {
	src := "f = () (out ?bigint) {\n" +
		"    acc = bigint.zero()\n" +
		"    radix = 10\n" +
		"    #{overflow=wrap}\n" +
		"    acc = acc * radix\n" +
		"    n i64 = 1\n" +
		"    #{overflow=wrap}\n" +
		"    n = n * radix\n" +
		"    out = acc\n" +
		"}\n"

	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipLowering = true
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}

	fd, ok := program.Statements[0].(*parser.FunctionDefinition)
	if !ok || fd.Body == nil {
		t.Fatal("expected a function definition with a body")
	}
	declared := collectFuncDeclared(fd, nil, nil)
	if got := declared["acc"]; got != "bigint" {
		t.Fatalf("acc should infer as bigint, got %q (declared=%v)", got, declared)
	}

	relevant, _ := OverflowAnnotationRelevance(program)
	var bigintStmt, intStmt parser.Statement
	for _, s := range fd.Body.Statements {
		ls, ok := s.(*parser.LetStatement)
		if !ok || ls.Name == nil {
			continue
		}
		switch ls.Name.Value {
		case "acc":
			if _, isMul := ls.Value.(*parser.InfixExpression); isMul {
				bigintStmt = s
			}
		case "n":
			if _, isMul := ls.Value.(*parser.InfixExpression); isMul {
				intStmt = s
			}
		}
	}
	if bigintStmt == nil || intStmt == nil {
		t.Fatalf("could not locate both assignment statements (bigint=%v int=%v)", bigintStmt, intStmt)
	}
	if relevant[bigintStmt] {
		t.Errorf("bigint arithmetic must NOT be in the relevant set")
	}
	if !relevant[intStmt] {
		t.Errorf("i64 arithmetic must stay in the relevant set")
	}
}

// TestTraceSuffix pins the empty-TraceID contract of the human-readable
// diagnostic suffix. An unassigned TraceID must render as nothing at all, not as
// a bare `[]` (the shape that made `no vet` look broken on
// nolang-overflow-ineffective before it was given an ID).
func TestTraceSuffix(t *testing.T) {
	if got := TraceSuffix(""); got != "" {
		t.Errorf("empty TraceID must render no suffix, got %q", got)
	}
	if got := TraceSuffix("0cccc2ei"); got != " [0cccc2ei]" {
		t.Errorf("TraceSuffix mismatch: %q", got)
	}
}
