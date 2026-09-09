package checker

import (
	"fmt"

	"github.com/lizongying/nolang/parser"
)

// matchNonexTraceID is the diagnostic ID for non-exhaustive option matches.
const matchNonexTraceID = "match-nonex"

// optionMatchVariants are the option variants a match arm can test against.
var optionMatchVariants = map[string]bool{"ok": true, "nil": true, "err": true}

// ValidateNonExhaustiveMatch reports matches over option-typed values that handle
// neither the failure variants (nil/err) nor provide a catch-all `->` arm.
//
// nolang desugars `x: { pattern -> body }` into an if/else chain in
// parser/lowering.go, so a match carrying only `ok ->` becomes a plain
// `if x == ok { ... }` with no else: when the option is nil/err the body is
// silently skipped and execution continues with whatever default the surrounding
// code had. That is a frequent source of "the operation failed yet we carried
// on with a zero value" bugs. We therefore require option matches to be
// exhaustive.
//
// RELATIONSHIP TO THE PARSER'S [RAL] CHECK — this rule is COMPLEMENTARY, not
// redundant. The parser enforces "err, nil, ok" completeness at parse time
// (see "option match must handle all branches" in parser/expr.go and
// parser/match.go), but ONLY when matchedIsOption() can prove the subject is an
// option: it handles a bare *Identifier whose type is already recorded in
// p.sem.VarTypes with a "?" prefix. Matches whose subject is anything else —
// most importantly a CALL, e.g. `foo(): { ok -> ... }` — parse cleanly with no
// [RAL] diagnostic and are caught only here. Do NOT delete this rule on the
// assumption that [RAL] subsumes it; see TestNonExhaustiveMatchCallSubject.
//
// Only option matches are reported: bare guard blocks (`{ cond -> body }`) have
// no MatchedExpr, and matches over plain values (`x: { 1 -> ... }`) test no
// option variant, so neither is flagged — keeping the rule low-noise.
func ValidateNonExhaustiveMatch(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	for _, root := range collectMatchRoots(program) {
		vs := matchVariants(root)
		// Not an option match: no arm tests ok/nil/err (bare guard blocks and
		// matches over plain values land here and stay unreported).
		if len(vs) == 0 {
			continue
		}
		// Exhaustive: terminates in a catch-all `-> ` arm, or already handles a
		// failure variant (nil/err) — in which case the unlisted variant simply
		// falls through by design.
		if matchHasCatchAll(root) || vs["nil"] || vs["err"] {
			continue
		}
		pos := root.Pos()
		results = append(results, ValidateResult{
			TraceID: matchNonexTraceID,
			Line:    pos.Line,
			Column:  pos.Column,
			Message: fmt.Sprintf(
				"non-exhaustive match on option '%s': the nil/err case is unhandled and will silently fall through — add a `-> ` (else) arm, or handle nil/err",
				matchSubjectName(root)),
		})
	}
	return results
}

// matchSubjectName returns a printable name for the matched expression.
func matchSubjectName(ife *parser.IfExpression) string {
	if ife == nil || ife.MatchedExpr == nil {
		return "?"
	}
	switch m := ife.MatchedExpr.(type) {
	case *parser.Identifier:
		return m.Value
	case *parser.DotExpression:
		return m.Property
	case *parser.CallExpression:
		// e.g. `foo(): { ok -> ... }` — show the callee so the diagnostic is
		// actionable (this shape is where the parser's [RAL] check cannot
		// fire, since it only recognises plain identifiers).
		switch fn := m.Function.(type) {
		case *parser.Identifier:
			return fn.Value + "()"
		case *parser.DotExpression:
			return fn.Property + "()"
		}
	}
	return "?"
}

// collectMatchRoots returns the outermost IfExpression of every match in the
// program. Chained arms (an arm sitting in another arm's else slot) are excluded
// so each match is reported exactly once.
func collectMatchRoots(program *parser.Program) []*parser.IfExpression {
	var all []*parser.IfExpression
	children := make(map[*parser.IfExpression]bool)

	var walkExpr func(parser.Expression)
	var walkStmts func([]parser.Statement)

	walkStmts = func(stmts []parser.Statement) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				if st.Body != nil {
					walkStmts(st.Body.Statements)
				}
			case *parser.BlockStatement:
				walkStmts(st.Statements)
			case *parser.LetStatement:
				walkExpr(st.Value)
			case *parser.MultiAssignStatement:
				for _, t := range st.Targets {
					walkExpr(t)
				}
				walkExpr(st.Value)
			case *parser.UnwrapAssignStatement:
				walkExpr(st.Value)
			case *parser.ReturnStatement:
				walkExpr(st.ReturnValue)
			case *parser.ExpressionStatement:
				walkExpr(st.Expression)
			case *parser.ForStatement:
				walkExpr(st.Condition)
				if st.Init != nil {
					walkStmts([]parser.Statement{st.Init})
				}
				if st.Update != nil {
					walkStmts([]parser.Statement{st.Update})
				}
				if st.Body != nil {
					walkStmts(st.Body.Statements)
				}
			}
		}
	}

	walkExpr = func(e parser.Expression) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *parser.IfExpression:
			if x.MatchedExpr != nil {
				all = append(all, x)
				if c := matchChainedChild(x); c != nil {
					children[c] = true
				}
			}
			walkExpr(x.Condition)
			if x.Consequence != nil {
				walkStmts(x.Consequence.Statements)
			}
			if x.Alternative != nil {
				walkStmts(x.Alternative.Statements)
			}
		case *parser.InfixExpression:
			walkExpr(x.Left)
			walkExpr(x.Right)
		case *parser.PrefixExpression:
			walkExpr(x.Right)
		case *parser.CallExpression:
			walkExpr(x.Function)
			for _, a := range x.Arguments {
				walkExpr(a)
			}
		case *parser.AssignExpression:
			walkExpr(x.Left)
			walkExpr(x.Value)
		case *parser.IndexExpression:
			walkExpr(x.Left)
			walkExpr(x.Index)
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				walkExpr(el)
			}
		case *parser.GroupedExpression:
			walkExpr(x.Expression)
		case *parser.AwaitExpression:
			walkExpr(x.Right)
		case *parser.DotExpression:
			walkExpr(x.Receiver)
		}
	}

	walkStmts(program.Statements)

	roots := make([]*parser.IfExpression, 0, len(all))
	for _, ife := range all {
		if !children[ife] {
			roots = append(roots, ife)
		}
	}
	return roots
}

// matchChainedChild returns the next arm of the if/else chain, i.e. the match
// arm that sits alone in this arm's else block. It returns nil when the else
// block is a real catch-all body (or absent).
func matchChainedChild(ife *parser.IfExpression) *parser.IfExpression {
	if ife == nil || ife.Alternative == nil || len(ife.Alternative.Statements) != 1 {
		return nil
	}
	es, ok := ife.Alternative.Statements[0].(*parser.ExpressionStatement)
	if !ok {
		return nil
	}
	inner, ok := es.Expression.(*parser.IfExpression)
	if !ok || inner.MatchedExpr == nil {
		return nil
	}
	return inner
}

// matchHasCatchAll reports whether the match chain terminates in an else body
// (a `-> ` wildcard arm, or any arm placed in the default slot).
func matchHasCatchAll(root *parser.IfExpression) bool {
	seen := make(map[*parser.IfExpression]bool)
	for node := root; node != nil; {
		if seen[node] {
			return true // defensive: malformed cycle
		}
		seen[node] = true
		if node.Alternative == nil {
			return false // chain ends without an else
		}
		if child := matchChainedChild(node); child != nil {
			node = child
			continue
		}
		return true // else block holds a real body
	}
	return true
}

// matchVariants collects the option variants (ok / nil / err) tested by the
// arms of a match chain. An empty result means the match is not over an option.
func matchVariants(root *parser.IfExpression) map[string]bool {
	vs := make(map[string]bool)
	seen := make(map[*parser.IfExpression]bool)
	for node := root; node != nil; {
		if seen[node] {
			break
		}
		seen[node] = true
		if v := armVariantName(node); v != "" {
			vs[v] = true
		}
		node = matchChainedChild(node)
	}
	return vs
}

// armVariantName returns the option variant a single desugared arm tests
// ("ok" / "nil" / "err"), or "" when the arm tests something else.
func armVariantName(ife *parser.IfExpression) string {
	if id, ok := ife.EqualityPattern.(*parser.Identifier); ok && optionMatchVariants[id.Value] {
		return id.Value
	}
	// `nil ->` carries a NilLiteral pattern, not an identifier.
	if _, ok := ife.EqualityPattern.(*parser.NilLiteral); ok {
		return "nil"
	}
	for _, p := range ife.OptionPatterns {
		if optionMatchVariants[p] {
			return p
		}
	}
	// ok-> / .-> val branch: only meaningful for options.
	if ife.DotValBody != nil {
		return "ok"
	}
	// Fallback: inspect the desugared condition `matched == ok` / `matched == nil`.
	if inf, ok := ife.Condition.(*parser.InfixExpression); ok && inf.Operator == "==" {
		if id, ok := inf.Right.(*parser.Identifier); ok && optionMatchVariants[id.Value] {
			return id.Value
		}
		if _, ok := inf.Right.(*parser.NilLiteral); ok {
			return "nil"
		}
	}
	return ""
}
