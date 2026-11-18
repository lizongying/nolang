package checker

import (
	"strings"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
)

// ValidateCoSpawn enforces what a `co` spawn is allowed to target.
//
// `co` is a COLORLESS SPAWN OF A PROCEDURE, not a parallel value constructor:
//
//	co f(args)    -- f must not declare a return value
//	co { ... }    -- the block is a procedure body, so it has none either
//
// A method's implicit `self` result does not count (parser.DeclaredResults
// strips it), because `self` is the receiver out-param every method carries so
// calls can chain — `co obj.flush()` is a perfectly ordinary spawn.
//
// WHY THE RESULT BAN IS A RULE AND NOT A STYLE CHOICE: a spawned call's value
// can only be read after the group joins, and the group's join point is not
// where the reader is. That is exactly the case `chan` exists for. Allowing
// `r = co f()` would keep a whole class of programs that read `r` before the
// spawn landed, with the read silently seeing whatever the slot held.
//
// Statement position is enforced by the parser (`co` produces no value, so
// `r = co f()` and `print(co f())` are rejected there); this pass only has to
// check the CALLEE.
func ValidateCoSpawn(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	byName := map[string][]*parser.FunctionDefinition{}
	byBase := map[string][]*parser.FunctionDefinition{}
	add := func(fd *parser.FunctionDefinition) {
		byName[fd.Name] = append(byName[fd.Name], fd)
		if i := strings.LastIndex(fd.Name, "."); i >= 0 {
			base := fd.Name[i+1:]
			byBase[base] = append(byBase[base], fd)
		}
	}
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			add(s)
		case *parser.LetStatement:
			// `f = (a i64) i64 { ... }` is a function definition spelled as a
			// binding (parser/tohir.go treats it as one), so it has to be in the
			// index or `co f(1)` would slip through unchecked.
			if fl, ok := s.Value.(*parser.FunctionLiteral); ok && s.Name != nil {
				add(&parser.FunctionDefinition{Name: s.Name.Value, FuncSignature: fl.FuncSignature})
			}
		}
	}
	if len(byName) == 0 {
		return nil
	}

	var results []ValidateResult
	parser.WalkProgramNodes(program, func(n parser.Node) {
		co, ok := n.(*parser.CoExpression)
		if !ok {
			return
		}
		// `co { ... }` is lowered from an anonymous block; there is no callee
		// to look up and nothing to return.
		call, ok := co.Call.(*parser.CallExpression)
		if !ok {
			return
		}
		defs := resolveCoCallee(call.Function, byName, byBase)
		if len(defs) == 0 {
			return // unknown callee (builtin, extern, cross-module): not ours to judge
		}
		// Only complain when EVERY overload of the name returns something. A
		// name shared by a value-returning and a void definition resolves to
		// one of them at lowering, and guessing wrong here would reject valid
		// code.
		for _, fd := range defs {
			if len(parser.DeclaredResults(fd)) == 0 {
				return
			}
		}
		results = append(results, ValidateResult{
			TraceID: "c0retval",
			Line:    co.Token.Line,
			Column:  co.Token.Column,
			Message: i18n.T("c0retval",
				"'co' spawns a procedure, but '%s' declares a return value; send the result through a channel instead",
				calleeLabel(call.Function)),
		})
	})
	return results
}

// resolveCoCallee maps the callee expression of a `co` spawn onto the function
// definitions it can name. Two spellings matter:
//
//	f(...)        bare name
//	recv.m(...)   `m` is either a top-level `mod.m` or a method `Type.m`
//
// A method call is written as `obj.m`, but the definition is registered under
// `Type.m`, so the exact lookup usually misses and the BASE-name index is what
// finds it.
func resolveCoCallee(fn parser.Expression, byName, byBase map[string][]*parser.FunctionDefinition) []*parser.FunctionDefinition {
	switch f := fn.(type) {
	case *parser.Identifier:
		return byName[f.Value]
	case *parser.DotExpression:
		if recv, ok := f.Receiver.(*parser.Identifier); ok {
			if defs := byName[recv.Value+"."+f.Property]; len(defs) > 0 {
				return defs
			}
		}
		return byBase[f.Property]
	}
	return nil
}

// calleeLabel renders the callee the way the user wrote it, for diagnostics.
func calleeLabel(fn parser.Expression) string {
	switch f := fn.(type) {
	case *parser.Identifier:
		return f.Value
	case *parser.DotExpression:
		if recv, ok := f.Receiver.(*parser.Identifier); ok {
			return recv.Value + "." + f.Property
		}
		return f.Property
	}
	return "?"
}
