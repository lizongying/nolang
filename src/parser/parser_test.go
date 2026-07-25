package parser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestRangeSyntaxParsing 驗證範圍語法 [a..b], [a..b), (a..b], (a..b) 的解析。
// [0..59]  → RangeExpression{Start:0, End:59, LeftInc:true, RightInc:true}
// [0..60)  → RangeExpression{Start:0, End:60, LeftInc:true, RightInc:false}
// (0..59]  → RangeExpression{Start:0, End:59, LeftInc:false, RightInc:true}
// (0..60)  → RangeExpression{Start:0, End:60, LeftInc:false, RightInc:false}
func TestRangeSyntaxParsing(t *testing.T) {
	t.Run("closed_interval", func(t *testing.T) {
		src := `x = n: {
  [0..59] -> 1
  -> 0
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		if prog == nil || len(prog.Statements) == 0 {
			t.Fatalf("expected program with statements")
		}
	})

	t.Run("half_open_interval", func(t *testing.T) {
		src := `x = n: {
  [0..60) -> 1
  -> 0
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		if prog == nil || len(prog.Statements) == 0 {
			t.Fatalf("expected program with statements")
		}
	})

	t.Run("half_open_right_inclusive", func(t *testing.T) {
		src := `x = n: {
  (0..59] -> 1
  -> 0
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		if prog == nil || len(prog.Statements) == 0 {
			t.Fatalf("expected program with statements")
		}
	})

	t.Run("open_interval", func(t *testing.T) {
		src := `x = n: {
  (0..60) -> 1
  -> 0
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		if prog == nil || len(prog.Statements) == 0 {
			t.Fatalf("expected program with statements")
		}
	})

	t.Run("mixed_intervals", func(t *testing.T) {
		src := `x = n: {
  [0..60) -> 1
  [60..100) -> 2
  [100..1000] -> 3
  -> -1
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		if prog == nil || len(prog.Statements) == 0 {
			t.Fatalf("expected program with statements")
		}
	})

	t.Run("all_four_forms", func(t *testing.T) {
		src := `x = n: {
  [0..10] -> 1
  (10..20] -> 2
  (20..30) -> 3
  [30..40) -> 4
  -> -1
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		if prog == nil || len(prog.Statements) == 0 {
			t.Fatalf("expected program with statements")
		}
	})
}

// TestRangeExpressionDesugar 驗證範圍條件正確 desugar 為布爾表達式。
// [a..b]  → matched >= a && matched <= b
// [a..b)  → matched >= a && matched < b
// (a..b]  → matched > a && matched <= b
// (a..b)  → matched > a && matched < b
func TestRangeExpressionDesugar(t *testing.T) {
	t.Run("closed_interval_desugar", func(t *testing.T) {
		src := `x = n: {
  [0..59] -> 1
  -> 0
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		// The match desugar produces an IfExpression with condition:
		// it >= 0 && it <= 59
		ls, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
		}
		ifExpr, ok := ls.Value.(*IfExpression)
		if !ok {
			t.Fatalf("expected IfExpression from match desugar, got %T", ls.Value)
		}
		cond, ok := ifExpr.Condition.(*InfixExpression)
		if !ok {
			t.Fatalf("expected InfixExpression condition, got %T", ifExpr.Condition)
		}
		if cond.Operator != "&&" {
			t.Errorf("expected && operator, got %s", cond.Operator)
		}
		left, ok := cond.Left.(*InfixExpression)
		if !ok {
			t.Fatalf("expected left InfixExpression, got %T", cond.Left)
		}
		if left.Operator != ">=" {
			t.Errorf("expected >= operator for left bound, got %s", left.Operator)
		}
		right, ok := cond.Right.(*InfixExpression)
		if !ok {
			t.Fatalf("expected right InfixExpression, got %T", cond.Right)
		}
		if right.Operator != "<=" {
			t.Errorf("expected <= operator for closed right bound, got %s", right.Operator)
		}
	})

	t.Run("half_open_interval_desugar", func(t *testing.T) {
		src := `x = n: {
  [0..60) -> 1
  -> 0
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		ls, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
		}
		ifExpr, ok := ls.Value.(*IfExpression)
		if !ok {
			t.Fatalf("expected IfExpression from match desugar, got %T", ls.Value)
		}
		cond, ok := ifExpr.Condition.(*InfixExpression)
		if !ok {
			t.Fatalf("expected InfixExpression condition, got %T", ifExpr.Condition)
		}
		if cond.Operator != "&&" {
			t.Errorf("expected && operator, got %s", cond.Operator)
		}
		right, ok := cond.Right.(*InfixExpression)
		if !ok {
			t.Fatalf("expected right InfixExpression, got %T", cond.Right)
		}
		if right.Operator != "<" {
			t.Errorf("expected < operator for half-open right bound, got %s", right.Operator)
		}
	})

	t.Run("left_exclusive_right_inclusive_desugar", func(t *testing.T) {
		src := `x = n: {
  (0..59] -> 1
  -> 0
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		ls, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
		}
		ifExpr, ok := ls.Value.(*IfExpression)
		if !ok {
			t.Fatalf("expected IfExpression from match desugar, got %T", ls.Value)
		}
		cond, ok := ifExpr.Condition.(*InfixExpression)
		if !ok {
			t.Fatalf("expected InfixExpression condition, got %T", ifExpr.Condition)
		}
		if cond.Operator != "&&" {
			t.Errorf("expected && operator, got %s", cond.Operator)
		}
		left, ok := cond.Left.(*InfixExpression)
		if !ok {
			t.Fatalf("expected left InfixExpression, got %T", cond.Left)
		}
		if left.Operator != ">" {
			t.Errorf("expected > operator for left-exclusive bound, got %s", left.Operator)
		}
		right, ok := cond.Right.(*InfixExpression)
		if !ok {
			t.Fatalf("expected right InfixExpression, got %T", cond.Right)
		}
		if right.Operator != "<=" {
			t.Errorf("expected <= operator for right-inclusive bound, got %s", right.Operator)
		}
	})

	t.Run("open_interval_desugar", func(t *testing.T) {
		src := `x = n: {
  (0..60) -> 1
  -> 0
}
`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		ls, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
		}
		ifExpr, ok := ls.Value.(*IfExpression)
		if !ok {
			t.Fatalf("expected IfExpression from match desugar, got %T", ls.Value)
		}
		cond, ok := ifExpr.Condition.(*InfixExpression)
		if !ok {
			t.Fatalf("expected InfixExpression condition, got %T", ifExpr.Condition)
		}
		if cond.Operator != "&&" {
			t.Errorf("expected && operator, got %s", cond.Operator)
		}
		left, ok := cond.Left.(*InfixExpression)
		if !ok {
			t.Fatalf("expected left InfixExpression, got %T", cond.Left)
		}
		if left.Operator != ">" {
			t.Errorf("expected > operator for left-exclusive bound, got %s", left.Operator)
		}
		right, ok := cond.Right.(*InfixExpression)
		if !ok {
			t.Fatalf("expected right InfixExpression, got %T", cond.Right)
		}
		if right.Operator != "<" {
			t.Errorf("expected < operator for right-exclusive bound, got %s", right.Operator)
		}
	})
}

// TestRangeNotSliceLiteral 驗證 [0..59] 不被誤解析為切片字面量。
// 錯誤訊息 "expected comma or right bracket in slice literal, got ELLIPSIS" 不應出現。
func TestRangeNotSliceLiteral(t *testing.T) {
	src := `x = n: {
  [0..59] -> 1
  -> 0
}
`
	l := lexer.New(src)
	p := New(l)
	_ = p.ParseProgram()
	for _, msg := range p.Errors() {
		if strings.Contains(msg, "expected comma or right bracket in slice literal") {
			t.Errorf("range [0..59] should not be parsed as slice literal: %s", msg)
		}
	}
}

// TestUnboundedRangeParsing 驗證無界區間的解析。
// 無上限: [a..), [a..], (a..), (a..]
// 無下限: [..b], [..b), (..b], (..b)
// 完全無界: [..], [..), (..], (..)
func TestUnboundedRangeParsing(t *testing.T) {
	cases := []string{
		// 無上限
		`x = n: { [5..) -> 1 -> 0 }`,
		`x = n: { [5..] -> 1 -> 0 }`,
		`x = n: { (5..) -> 1 -> 0 }`,
		`x = n: { (5..] -> 1 -> 0 }`,
		// 無下限
		`x = n: { [..5] -> 1 -> 0 }`,
		`x = n: { [..5) -> 1 -> 0 }`,
		`x = n: { (..5] -> 1 -> 0 }`,
		`x = n: { (..5) -> 1 -> 0 }`,
		// 完全無界
		`x = n: { [..] -> 1 -> 0 }`,
		`x = n: { [..) -> 1 -> 0 }`,
		`x = n: { (..] -> 1 -> 0 }`,
		`x = n: { (..) -> 1 -> 0 }`,
	}
	for i, src := range cases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			l := lexer.New(src)
			p := New(l)
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				t.Fatalf("parse errors: %v", p.Errors())
			}
			if prog == nil || len(prog.Statements) == 0 {
				t.Fatalf("expected program with statements")
			}
		})
	}
}

// TestUnboundedRangeDesugar 驗證無界區間正確 desugar 為單側布爾表達式。
func TestUnboundedRangeDesugar(t *testing.T) {
	t.Run("no_upper_bound_left_inclusive", func(t *testing.T) {
		// [5..) → it >= 5
		src := `x = n: { [5..) -> 1 -> 0 }`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		ls, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
		}
		ifExpr, ok := ls.Value.(*IfExpression)
		if !ok {
			t.Fatalf("expected IfExpression, got %T", ls.Value)
		}
		cond, ok := ifExpr.Condition.(*InfixExpression)
		if !ok {
			t.Fatalf("expected InfixExpression, got %T", ifExpr.Condition)
		}
		if cond.Operator != ">=" {
			t.Errorf("expected >= for [a..), got %s", cond.Operator)
		}
	})

	t.Run("no_upper_bound_left_exclusive", func(t *testing.T) {
		// (5..) → it > 5
		src := `x = n: { (5..) -> 1 -> 0 }`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		ls, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
		}
		ifExpr, ok := ls.Value.(*IfExpression)
		if !ok {
			t.Fatalf("expected IfExpression, got %T", ls.Value)
		}
		cond, ok := ifExpr.Condition.(*InfixExpression)
		if !ok {
			t.Fatalf("expected InfixExpression, got %T", ifExpr.Condition)
		}
		if cond.Operator != ">" {
			t.Errorf("expected > for (a..), got %s", cond.Operator)
		}
	})

	t.Run("no_lower_bound_right_inclusive", func(t *testing.T) {
		// [..5] → it <= 5
		src := `x = n: { [..5] -> 1 -> 0 }`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		ls, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
		}
		ifExpr, ok := ls.Value.(*IfExpression)
		if !ok {
			t.Fatalf("expected IfExpression, got %T", ls.Value)
		}
		cond, ok := ifExpr.Condition.(*InfixExpression)
		if !ok {
			t.Fatalf("expected InfixExpression, got %T", ifExpr.Condition)
		}
		if cond.Operator != "<=" {
			t.Errorf("expected <= for [..b], got %s", cond.Operator)
		}
	})

	t.Run("no_lower_bound_right_exclusive", func(t *testing.T) {
		// [..5) → it < 5
		src := `x = n: { [..5) -> 1 -> 0 }`
		l := lexer.New(src)
		p := New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		ls, ok := prog.Statements[0].(*LetStatement)
		if !ok {
			t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
		}
		ifExpr, ok := ls.Value.(*IfExpression)
		if !ok {
			t.Fatalf("expected IfExpression, got %T", ls.Value)
		}
		cond, ok := ifExpr.Condition.(*InfixExpression)
		if !ok {
			t.Fatalf("expected InfixExpression, got %T", ifExpr.Condition)
		}
		if cond.Operator != "<" {
			t.Errorf("expected < for [..b), got %s", cond.Operator)
		}
	})

	t.Run("fully_unbounded_always_true", func(t *testing.T) {
		// [..] → true (1)
		// (..) → true (1)
		// [..) → true (1)
		// (..] → true (1)
		forms := []string{`[..]`, `(..)`, `[..)`, `(..]`}
		for _, form := range forms {
			src := fmt.Sprintf(`x = n: { %s -> 1 -> 0 }`, form)
			l := lexer.New(src)
			p := New(l)
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				t.Fatalf("parse errors for %s: %v", form, p.Errors())
			}
			ls, ok := prog.Statements[0].(*LetStatement)
			if !ok {
				t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
			}
			ifExpr, ok := ls.Value.(*IfExpression)
			if !ok {
				t.Fatalf("expected IfExpression, got %T", ls.Value)
			}
			// 完全無界 desugar 為整數 1 (true)
			intLit, ok := ifExpr.Condition.(*IntegerLiteral)
			if !ok {
				t.Fatalf("expected IntegerLiteral for %s, got %T", form, ifExpr.Condition)
			}
			if intLit.Value != 1 {
				t.Errorf("expected 1 (true) for %s, got %d", form, intLit.Value)
			}
		}
	})
}

// TestSliceLiteralStillWorks 驗證普通切片字面量 [1, 2, 3] 仍正常解析。
func TestSliceLiteralStillWorks(t *testing.T) {
	src := `v = [1, 2, 3]
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	ls, ok := prog.Statements[0].(*LetStatement)
	if !ok {
		t.Fatalf("expected LetStatement, got %T", prog.Statements[0])
	}
	sl, ok := ls.Value.(*SliceLiteral)
	if !ok {
		t.Fatalf("expected SliceLiteral, got %T", ls.Value)
	}
	if len(sl.Elements) != 3 {
		t.Errorf("expected 3 elements, got %d", len(sl.Elements))
	}
}
