package checker

import (
	"reflect"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// printfDeprTraceID is stamped on every "printf/eprintf was removed" diagnostic.
// Stable and greppable: `no vet … | grep printf-depr`.
const printfDeprTraceID = "printf-depr"

// removedPrintfBuiltins maps the removed print-family builtins to the message
// reported at every call site.
//
// WHY THE NAME STAYS IN THE SYMBOL TABLE
// --------------------------------------
// `printf` / `eprintf` remain registered (src/builtin/fmt.go) and keep their
// stub declarations in src/std/fmt.no, so a call still RESOLVES — it never
// degrades into "undefined function", and the LSP keeps completing the name.
// What is gone is the ability to CALL them: the MIR backend never implemented
// either one, and the old failure modes were
//
//	printf('x={x}')   ->  unknown callee fmt-int in func main
//	printf('plain')   ->  unsupported builtin printf
//
// — two backend-internal messages that name neither the offending builtin nor
// the fix. This pass replaces both with one actionable, source-located error
// (and `no vet` reports it as well, so it surfaces in the editor too).
//
// `sprintf` is deliberately ABSENT. It still lowers correctly (same path as
// `format`) and `format` is a drop-in rename, so it needs no hard stop — only
// printf/eprintf are unreachable.
var removedPrintfBuiltins = map[string]string{
	"printf":  "printf() is removed; use print(...) for stdout with a newline, or io.out(...) for stdout without one",
	"eprintf": "eprintf() is removed; use eprint(...) for stderr with a newline, or io.err(...) for stderr without one",
}

// removedPrintfCallee resolves a call's callee to a removed builtin name.
//
// Exactly two shapes count:
//
//	printf(...)      -> Identifier
//	fmt.printf(...)  -> DotExpression whose receiver is the `fmt` module
//
// A METHOD call (`w.printf(...)`) is deliberately NOT matched. `callName`
// flattens every DotExpression to its property, so matching on that would
// flag a user's own method that merely happens to share the name.
func removedPrintfCallee(fn parser.Expression) (string, bool) {
	switch f := fn.(type) {
	case *parser.Identifier:
		if _, ok := removedPrintfBuiltins[f.Value]; ok {
			return f.Value, true
		}
	case *parser.DotExpression:
		recv, ok := f.Receiver.(*parser.Identifier)
		if !ok || recv.Value != "fmt" {
			return "", false
		}
		if _, ok := removedPrintfBuiltins[f.Property]; ok {
			return f.Property, true
		}
	}
	return "", false
}

// ValidateDeprecatedPrintf reports every call to the removed print-family
// builtins printf / eprintf with a migration hint.
//
// Like ValidateDeprecatedLen this is appended to ValidateTypes, which is the
// pass the build path runs (build/transpiler.go) — so a call is a hard compile
// error for `no build` / `no run`, and `no vet` surfaces the same result as a
// diagnostic.
//
// The traversal is deliberately independent of checkPrintFormatInStmt: that
// walker threads a mutable varTypes scope through itself and only fires for
// print-family calls WITH at least one argument, whereas this rule must catch
// every call regardless of arity (printf() with no arguments is still a call).
func ValidateDeprecatedPrintf(program *parser.Program) []ValidateResult {
	var results []ValidateResult
	for _, stmt := range program.Statements {
		// Skip monomorphized instances: they are generated, so an error
		// reported against them would point at a line the user never wrote.
		//
		// NOTE this deliberately does NOT also skip generic TEMPLATES the way
		// the type-checking passes do (checker.go:1006 skips `GenericParams`
		// and `[`-prefixed names). Those passes skip templates because a `t`
		// argument cannot be type-checked before specialisation — but "is this
		// call spelled printf?" is a purely syntactic fact that needs no type
		// information. Keeping the template means a printf inside a generic
		// function is reported exactly once, at the author's own line; skipping
		// it (with the `__` instances skipped too) would silently let it
		// through. Pinned by TestPrintfDeprFlagsCalls/inside_a_generic_template.
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && strings.Contains(fd.Name, "__") {
			continue
		}
		walkStmtForRemovedPrintf(stmt, &results)
	}
	return results
}

func walkStmtForRemovedPrintf(stmt parser.Statement, out *[]ValidateResult) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		walkExprForRemovedPrintf(s.Expression, out)
	case *parser.LetStatement:
		walkExprForRemovedPrintf(s.Value, out)
	case *parser.MultiAssignStatement:
		walkExprForRemovedPrintf(s.Value, out)
		for _, t := range s.Targets {
			walkExprForRemovedPrintf(t, out)
		}
	case *parser.UnwrapAssignStatement:
		walkExprForRemovedPrintf(s.Value, out)
		walkExprForRemovedPrintf(s.Target, out)
	case *parser.ReturnStatement:
		walkExprForRemovedPrintf(s.ReturnValue, out)
	case *parser.BlockStatement:
		for _, sub := range s.Statements {
			walkStmtForRemovedPrintf(sub, out)
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, sub := range s.Body.Statements {
				walkStmtForRemovedPrintf(sub, out)
			}
		}
	case *parser.ForStatement:
		walkExprForRemovedPrintf(s.CountExpr, out)
		walkExprForRemovedPrintf(s.Condition, out)
		walkStmtForRemovedPrintf(s.Init, out)
		walkStmtForRemovedPrintf(s.Update, out)
		if s.Body != nil {
			for _, sub := range s.Body.Statements {
				walkStmtForRemovedPrintf(sub, out)
			}
		}
	}
}

func walkExprForRemovedPrintf(expr parser.Expression, out *[]ValidateResult) {
	if expr == nil {
		return
	}
	// A nil *parser.X stored inside an Expression interface is a *typed-nil*
	// value: it evades the `expr == nil` check above and still matches a case
	// below, where dereferencing then panics (same hazard walkExprForLen guards).
	if rv := reflect.ValueOf(expr); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		if name, ok := removedPrintfCallee(e.Function); ok {
			*out = append(*out, ValidateResult{
				TraceID: printfDeprTraceID,
				Line:    e.Token.Line,
				Column:  e.Token.Column,
				Message: "deprecated: " + removedPrintfBuiltins[name],
			})
		}
		// Recurse into the callee too: `fmt.printf(x)` hides its receiver there,
		// and a nested call can appear as an argument (`print(printf('x'))`).
		walkExprForRemovedPrintf(e.Function, out)
		for _, a := range e.Arguments {
			walkExprForRemovedPrintf(a, out)
		}
	case *parser.InfixExpression:
		walkExprForRemovedPrintf(e.Left, out)
		walkExprForRemovedPrintf(e.Right, out)
	case *parser.PrefixExpression:
		walkExprForRemovedPrintf(e.Right, out)
	case *parser.GroupedExpression:
		walkExprForRemovedPrintf(e.Expression, out)
	case *parser.DotExpression:
		walkExprForRemovedPrintf(e.Receiver, out)
	case *parser.IndexExpression:
		walkExprForRemovedPrintf(e.Left, out)
		walkExprForRemovedPrintf(e.Index, out)
	case *parser.SliceExpression:
		walkExprForRemovedPrintf(e.Left, out)
		if e.Range != nil {
			walkExprForRemovedPrintf(e.Range.Start, out)
			walkExprForRemovedPrintf(e.Range.End, out)
		}
	case *parser.RangeExpression:
		walkExprForRemovedPrintf(e.Start, out)
		walkExprForRemovedPrintf(e.End, out)
	case *parser.AssignExpression:
		walkExprForRemovedPrintf(e.Left, out)
		walkExprForRemovedPrintf(e.Value, out)
	case *parser.ConditionalExpression:
		walkExprForRemovedPrintf(e.Condition, out)
		walkExprForRemovedPrintf(e.Consequence, out)
		walkExprForRemovedPrintf(e.Alternative, out)
	case *parser.CastExpression:
		walkExprForRemovedPrintf(e.Expr, out)
	case *parser.RunExpression:
		walkExprForRemovedPrintf(e.Call, out)
	case *parser.AwaitExpression:
		walkExprForRemovedPrintf(e.Right, out)
	case *parser.IfExpression:
		// `x: { pattern -> body }` — the matched expression lives in MatchedExpr,
		// the arms in Consequence / Alternative (there is no Match node).
		walkExprForRemovedPrintf(e.Condition, out)
		walkExprForRemovedPrintf(e.MatchedExpr, out)
		if e.Consequence != nil {
			for _, sub := range e.Consequence.Statements {
				walkStmtForRemovedPrintf(sub, out)
			}
		}
		if e.Alternative != nil {
			for _, sub := range e.Alternative.Statements {
				walkStmtForRemovedPrintf(sub, out)
			}
		}
	case *parser.ArrayLiteral:
		for _, el := range e.Elements {
			walkExprForRemovedPrintf(el, out)
		}
	case *parser.SliceLiteral:
		for _, el := range e.Elements {
			walkExprForRemovedPrintf(el, out)
		}
	case *parser.MapLiteral:
		for _, p := range e.Pairs {
			walkExprForRemovedPrintf(p.Key, out)
			walkExprForRemovedPrintf(p.Value, out)
		}
	case *parser.StructLiteral:
		for _, f := range e.Fields {
			walkExprForRemovedPrintf(f.Value, out)
		}
	case *parser.FunctionLiteral:
		if e.Body != nil {
			for _, sub := range e.Body.Statements {
				walkStmtForRemovedPrintf(sub, out)
			}
		}
	}
}
