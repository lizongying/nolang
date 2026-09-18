package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD (i3k422u3 false positive on flattened method calls).
//
// RunAllLints executes on the MERGED program. Merging std rewrites the call
// graph: `recv.m(args)` becomes the free-function call `Type.m(recv, args)`
// (build/transpiler.go sets `ce.Function = &parser.Identifier{…}` and unshifts
// the receiver into `ce.Arguments`). After that rewrite the syntactic
// DotExpression is gone and the receiver is just the first argument, so the
// receiver-mutation branch of collectAssignedNamesInExpr — the one that keys
// off `CallExpression{Function: *DotExpression}` — no longer fires.
//
// The visible symptom was `src/std/net/net.no:693`:
//
//	enc-accept = (c conn, key [32]byte) (ec enc-conn) {
//	    ec.init(c, key)          ; this *does* initialise `ec`
//	}
//	; → [WARNING] result parameter 'ec' (enc-conn) is never assigned … [i3k422u3]
//
// A source-level test cannot reach this path: the flattened shape is produced
// by the merger, not by the parser, so the AST is rewritten by hand here.
func TestFlattenedMethodCallMarksReceiverAssigned(t *testing.T) {
	methods := map[string]bool{"enc-conn.init": true}

	assigned := map[string]bool{}
	collectAssignedNamesInExpr(flattenedCall("enc-conn.init", "ec", "c", "key"), assigned, methods)
	if !assigned["ec"] {
		t.Fatalf("flattened method call `enc-conn.init(ec, …)` must mark receiver `ec` as assigned, got %v", assigned)
	}

	// Control: a FREE function's first argument is passed by value and is not
	// mutated, so it must NOT be reported as assigned. Without this guard the
	// lint would lose its meaning for every single-argument call.
	assignedFree := map[string]bool{}
	collectAssignedNamesInExpr(flattenedCall("free-fn", "x"), assignedFree, methods)
	if assignedFree["x"] {
		t.Fatalf("free function first argument must not be treated as a mutated receiver, got %v", assignedFree)
	}
}

// End-to-end at the lint level: a named result parameter whose only write is a
// flattened method call on itself must not trigger i3k422u3.
func TestFlattenedMethodCallSuppressesUnassignedReturn(t *testing.T) {
	src := `enc-conn.init = (c str, key str) {
    .c = c
}

enc-accept = (c str, key str) (ec enc-conn) {
    ec.init(c, key)
}

main = () {
    x = enc-accept('c', 'k')
}
`
	prog := parser.New(lexer.New(src)).ParseProgram()
	if n := flattenCalls(prog); n != 1 {
		t.Fatalf("expected to flatten exactly 1 method call, flattened %d (test fixture drifted)", n)
	}

	results := ValidateUnassignedReturns(prog)
	for _, r := range results {
		t.Logf("L%d:C%d %s", r.Line, r.Column, r.Message)
	}
	for _, r := range results {
		if strings.Contains(r.Message, "'ec'") {
			t.Fatalf("flattened method call initialises `ec`; i3k422u3 must not fire: %s", r.Message)
		}
	}

	// Control: the guard must not be a blanket suppression. A result parameter
	// whose only mention is a *free* function call is still reported.
	src2 := `helper = (s str) (r i64) {
    r = 0
}

enc-accept2 = (c str, key str) (ec enc-conn) {
    helper(c)
}

main = () {
    y = enc-accept2('c', 'k')
}
`
	prog2 := parser.New(lexer.New(src2)).ParseProgram()
	results2 := ValidateUnassignedReturns(prog2)
	found := false
	for _, r := range results2 {
		if strings.Contains(r.Message, "'ec'") && strings.Contains(r.Message, "never assigned") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a result param only touched by a free-function call must still be reported, got %v", results2)
	}
}

// flattenedCall builds the post-merge shape of a method call:
// CallExpression{Function: Identifier{"Type.method"}, Arguments: [recv, …]}.
func flattenedCall(callee string, args ...string) *parser.CallExpression {
	ce := &parser.CallExpression{Function: &parser.Identifier{Value: callee}}
	for _, a := range args {
		ce.Arguments = append(ce.Arguments, &parser.Identifier{Value: a})
	}
	return ce
}

// flattenCalls rewrites every `recv.m(args)` in the parsed program into the
// post-merge `Type.m(recv, args)` form, mirroring what build/transpiler.go does
// to the merged program. Only the subset needed by these tests is handled: a
// call is flattened when the receiver's declared type carries a matching method
// definition. Returns the number of rewrites performed (callers assert on it so
// a silent fixture drift cannot turn the test into a no-op).
func flattenCalls(prog *parser.Program) int {
	methods := map[string]bool{} // "Type.method" -> is a method definition
	for _, stmt := range prog.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || !fd.IsMethodDef {
			continue
		}
		// parser injects `self` at Results[0] (parser/decl.go).
		if len(fd.Results) > 0 && fd.Results[0].Name == "self" {
			methods[fd.Name] = true
		}
	}

	n := 0
	for _, stmt := range prog.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		locals := map[string]string{}
		for _, p := range fd.Parameters {
			locals[p.Name] = p.Type.String()
		}
		for _, p := range fd.Results {
			locals[p.Name] = p.Type.String()
		}
		for _, s := range fd.Body.Statements {
			es, ok := s.(*parser.ExpressionStatement)
			if !ok {
				continue
			}
			call, ok := es.Expression.(*parser.CallExpression)
			if !ok {
				continue
			}
			dot, ok := call.Function.(*parser.DotExpression)
			if !ok {
				continue
			}
			recvName, ok := dot.Receiver.(*parser.Identifier)
			if !ok {
				continue
			}
			full := locals[recvName.Value] + "." + dot.Property
			if !methods[full] {
				continue
			}
			call.Function = &parser.Identifier{Value: full}
			call.Arguments = append([]parser.Expression{recvName}, call.Arguments...)
			n++
		}
	}
	return n
}
