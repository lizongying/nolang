package checker

import (
	"fmt"
	"strings"

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
// NonExhaustiveMatch describes one option match that ValidateNonExhaustiveMatch
// reports: the desugared root arm plus the option variants it fails to handle.
//
// It exists so automated fixers (see `no fmt --fix=match`) can drive off the
// *same* detection the diagnostic uses instead of re-deriving arm structure from
// source text. Root is the outermost IfExpression of the match; its MatchEndPos
// carries the source position of the match's closing `}` (set by the parser's
// match desugar), which is where a fixer inserts the missing arms.
type NonExhaustiveMatch struct {
	Root    *parser.IfExpression
	Missing []string // subset of ok / nil / err, in that canonical order
}

// CollectNonExhaustiveMatches returns every option match in program that neither
// terminates in a catch-all arm nor handles all three option variants.
// ValidateNonExhaustiveMatch is a thin reporting wrapper over it.
func CollectNonExhaustiveMatches(program *parser.Program) []NonExhaustiveMatch {
	if program == nil {
		return nil
	}
	var out []NonExhaustiveMatch
	roots, rootFunc := collectMatchRoots(program)
	for _, root := range roots {
		vs := matchVariants(root)
		// Not an option match: no arm tests ok/nil/err (bare guard blocks and
		// matches over plain values land here and stay unreported).
		if len(vs) == 0 {
			continue
		}
		// Matches synthesised by the `?=` / safe-index lowerings have no source
		// block of their own (MatchEndPos stays zero): the user cannot add arms
		// to them, so demanding completeness is both unfixable and wrong.
		if root.MatchEndPos.Line == 0 {
			continue
		}
		// Tagged-enum matches are not option matches: `q: { ok(v) -> ... fail -> ... }`
		// matches the enum's OWN variants, and `ok` is simply a variant name that
		// collides with the option vocabulary. Demanding nil/err arms there is a
		// false positive — the match is already exhaustive over its enum.
		if subjectIsDeclaredEnum(program, root, rootFunc[root]) {
			continue
		}
		// Exhaustive: terminates in a catch-all `-> ` arm, or handles every
		// option variant (ok/nil/err). err is a real variant of any ?T (std's
		// read-stdin-str returns ?str and matches err ->), so all three are
		// required — mirroring the parser's [RAL] completeness rule.
		if matchHasCatchAll(root) || (vs["ok"] && vs["nil"] && vs["err"]) {
			continue
		}
		var missing []string
		for _, v := range []string{"ok", "nil", "err"} {
			if !vs[v] {
				missing = append(missing, v)
			}
		}
		out = append(out, NonExhaustiveMatch{Root: root, Missing: missing})
	}
	return out
}

func ValidateNonExhaustiveMatch(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	for _, m := range CollectNonExhaustiveMatches(program) {
		pos := m.Root.Pos()
		results = append(results, ValidateResult{
			TraceID: matchNonexTraceID,
			Line:    pos.Line,
			Column:  pos.Column,
			Message: fmt.Sprintf(
				"non-exhaustive match on option '%s': missing arm(s): %s — add a `-> ` (else) arm, or handle every variant",
				matchSubjectName(m.Root), strings.Join(m.Missing, ", ")),
		})
	}
	return results
}

// subjectIsDeclaredEnum reports whether the matched subject's static type is a
// user-declared enum (e.g. `e-res`). Such a match ranges over that enum's own
// variants, so the option completeness rule (ok/nil/err) does not apply.
// Only a statically known type can be excluded — unknown types stay reported,
// which is what lets call subjects (`foo(): { ok -> ... }`) still be caught.
//
// The subject is looked up with FuncVarType(funcName, ...) so function-local
// types (parameters included) are found in the per-function map first, falling
// back to the global VarTypes for module-level matches — rather than reading
// the global snapshot directly, which misses parameters that now live only in
// the per-function map.
func subjectIsDeclaredEnum(program *parser.Program, root *parser.IfExpression, funcName string) bool {
	if program == nil || program.Sem == nil || len(program.Sem.EnumVariants) == 0 {
		return false
	}
	id, ok := root.MatchedExpr.(*parser.Identifier)
	if !ok {
		return false
	}
	t, ok := program.Sem.FuncVarType(funcName, id.Value)
	if !ok || t == "" || strings.HasPrefix(t, "?") {
		return false
	}
	_, isEnum := program.Sem.EnumVariants[t]
	return isEnum
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
// program, together with the name of the enclosing function ("" for module-level
// matches). Chained arms (an arm sitting in another arm's else slot) are
// excluded so each match is reported exactly once.
func collectMatchRoots(program *parser.Program) ([]*parser.IfExpression, map[*parser.IfExpression]string) {
	var all []*parser.IfExpression
	children := make(map[*parser.IfExpression]bool)
	rootFunc := make(map[*parser.IfExpression]string)

	var walkExpr func(funcName string, e parser.Expression)
	var walkStmts func(funcName string, stmts []parser.Statement)

	walkStmts = func(funcName string, stmts []parser.Statement) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			switch st := s.(type) {
			case *parser.FunctionDefinition:
				fn := st.Name
				if st.Body != nil {
					walkStmts(fn, st.Body.Statements)
				}
			case *parser.BlockStatement:
				walkStmts(funcName, st.Statements)
			case *parser.LetStatement:
				walkExpr(funcName, st.Value)
			case *parser.MultiAssignStatement:
				for _, t := range st.Targets {
					walkExpr(funcName, t)
				}
				walkExpr(funcName, st.Value)
			case *parser.UnwrapAssignStatement:
				walkExpr(funcName, st.Value)
			case *parser.ReturnStatement:
				walkExpr(funcName, st.ReturnValue)
			case *parser.ExpressionStatement:
				walkExpr(funcName, st.Expression)
			case *parser.ForStatement:
				walkExpr(funcName, st.Condition)
				if st.Init != nil {
					walkStmts(funcName, []parser.Statement{st.Init})
				}
				if st.Update != nil {
					walkStmts(funcName, []parser.Statement{st.Update})
				}
				if st.Body != nil {
					walkStmts(funcName, st.Body.Statements)
				}
			}
		}
	}

	walkExpr = func(funcName string, e parser.Expression) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *parser.IfExpression:
			if x.MatchedExpr != nil {
				all = append(all, x)
				rootFunc[x] = funcName
				if c := matchChainedChild(x); c != nil {
					children[c] = true
				}
			}
			walkExpr(funcName, x.Condition)
			if x.Consequence != nil {
				walkStmts(funcName, x.Consequence.Statements)
			}
			if x.Alternative != nil {
				walkStmts(funcName, x.Alternative.Statements)
			}
		case *parser.InfixExpression:
			walkExpr(funcName, x.Left)
			walkExpr(funcName, x.Right)
		case *parser.PrefixExpression:
			walkExpr(funcName, x.Right)
		case *parser.CallExpression:
			walkExpr(funcName, x.Function)
			for _, a := range x.Arguments {
				walkExpr(funcName, a)
			}
		case *parser.AssignExpression:
			walkExpr(funcName, x.Left)
			walkExpr(funcName, x.Value)
		case *parser.IndexExpression:
			walkExpr(funcName, x.Left)
			walkExpr(funcName, x.Index)
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				walkExpr(funcName, el)
			}
		case *parser.GroupedExpression:
			walkExpr(funcName, x.Expression)
		case *parser.AwaitExpression:
			walkExpr(funcName, x.Right)
		case *parser.DotExpression:
			walkExpr(funcName, x.Receiver)
		}
	}

	walkStmts("", program.Statements)

	roots := make([]*parser.IfExpression, 0, len(all))
	for _, ife := range all {
		if !children[ife] {
			roots = append(roots, ife)
		}
	}
	return roots, rootFunc
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
		for _, v := range armVariantNames(node) {
			vs[v] = true
		}
		node = matchChainedChild(node)
	}
	return vs
}

// armVariantNames returns every option variant ("ok" / "nil" / "err") a single
// desugared arm claims. A combined arm (`nil || err ->`) records BOTH variants
// in OptionPatterns, so collect ALL of them: returning only the first would make
// the exhaust checker treat `nil || err` as covering just `nil` and falsely
// report the `err` arm as missing. The parser's own [RAL] completeness check
// already treats a combined pattern as covering every listed variant (see
// parser/expr.go hasErrArm), so this keeps the checker consistent with it. An
// empty result means the arm tests something other than an option variant.
func armVariantNames(ife *parser.IfExpression) []string {
	if ife == nil {
		return nil
	}
	// Combined option patterns (`nil || err`): claim every listed variant.
	if len(ife.OptionPatterns) > 0 {
		var out []string
		for _, p := range ife.OptionPatterns {
			if optionMatchVariants[p] {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if v := armVariantName(ife); v != "" {
		return []string{v}
	}
	return nil
}

// armVariantName returns the single option variant a non-combined desugared arm
// tests ("ok" / "nil" / "err"), or "" when the arm tests something else.
func armVariantName(ife *parser.IfExpression) string {
	if ife == nil {
		return ""
	}
	if id, ok := ife.EqualityPattern.(*parser.Identifier); ok && optionMatchVariants[id.Value] {
		return id.Value
	}
	// `nil ->` carries a NilLiteral pattern, not an identifier.
	if _, ok := ife.EqualityPattern.(*parser.NilLiteral); ok {
		return "nil"
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
