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
// (build/transpiler.go sets `ce.Function = &parser.Identifier{…}`, unshifts the
// receiver into `ce.Arguments`, and — since the CalleeKind work — records
// `program.Sem.SetCallee(ce, parser.CalleeMethod)`). After that rewrite the
// syntactic DotExpression is gone and the receiver is just the first argument,
// so the receiver-mutation branch of collectAssignedNamesInExpr — the one that
// keys off `CallExpression{Function: *DotExpression}` — no longer fires.
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
//
// NOTE ON THE JUDGEMENT SOURCE. This used to be inferred from the callee NAME
// (does `Identifier{"enc-conn.init"}` match some method definition?). That
// inference is gone: it both missed cases (a `module.fn` free function that
// collides with a method definition name) and over-fired (a free function's
// first argument is passed by value and must not count as assigned). The
// decision now reads the explicit record on the semantic side-table, so the
// tests below pin the SIDE-TABLE contract, not a name-matching heuristic.
func TestFlattenedMethodCallMarksReceiverAssigned(t *testing.T) {
	sem := parser.NewSemanticContext()
	call := flattenedCall("enc-conn.init", "ec", "c", "key")
	sem.SetCallee(call, parser.CalleeMethod)

	assigned := map[string]bool{}
	collectAssignedNamesInExpr(call, assigned, sem)
	if !assigned["ec"] {
		t.Fatalf("flattened method call `enc-conn.init(ec, …)` must mark receiver `ec` as assigned, got %v", assigned)
	}

	// Control: a FREE function's first argument is passed by value and is not
	// mutated, so it must NOT be reported as assigned. Without this guard the
	// lint would lose its meaning for every single-argument call.
	assignedFree := map[string]bool{}
	collectAssignedNamesInExpr(flattenedCall("free-fn", "x"), assignedFree, sem)
	if assignedFree["x"] {
		t.Fatalf("free function first argument must not be treated as a mutated receiver, got %v", assignedFree)
	}
}

// The judgement must come from the side table, NOT from the callee name.
//
// A flattened-looking call whose callee happens to match a method definition
// name — but which carries no CalleeMethod record — must NOT be treated as
// receiver mutation. This is exactly the case the old name-based inference got
// wrong: `build/module_prefix.go` renames colliding free functions to
// `module.fn`, which shares its string namespace with method definitions, so a
// free function named like a method used to be silently misread as a method
// call (a false negative that would suppress a legitimate i3k422u3 warning).
func TestFlattenedShapeWithoutCalleeRecordIsNotReceiverMutation(t *testing.T) {
	sem := parser.NewSemanticContext() // no SetCallee: "never rewritten", or rewritten as a plain call

	assigned := map[string]bool{}
	collectAssignedNamesInExpr(flattenedCall("enc-conn.init", "ec", "c", "key"), assigned, sem)
	if assigned["ec"] {
		t.Fatalf("without a CalleeMethod record the first argument is an ordinary by-value argument, got %v", assigned)
	}
}

// A module-qualified free-function call keeps the same shape as a flattened
// method call (`module.fn(x)` → Arguments[0] = x), but its first argument is an
// ordinary by-value argument, not a receiver. Recording it as CalleeModuleFn
// (rather than CalleeMethod) must not mark it as mutated.
func TestModuleFnCalleeRecordIsNotReceiverMutation(t *testing.T) {
	sem := parser.NewSemanticContext()
	call := flattenedCall("json.parse", "s")
	sem.SetCallee(call, parser.CalleeModuleFn)

	assigned := map[string]bool{}
	collectAssignedNamesInExpr(call, assigned, sem)
	if assigned["s"] {
		t.Fatalf("a module-qualified free function does not mutate its first argument, got %v", assigned)
	}
}

// A nil side table must degrade safely (no panics, no bogus assignments).
func TestNilSemanticContextDegradesSafely(t *testing.T) {
	assigned := map[string]bool{}
	collectAssignedNamesInExpr(flattenedCall("enc-conn.init", "ec"), assigned, nil)
	if assigned["ec"] {
		t.Fatalf("nil Sem must not infer receiver mutation, got %v", assigned)
	}
}

// The side table's own contract: default is CalleeUnknown, a set value is
// readable, and a nil receiver / nil node is safe.
func TestSemanticContextCalleeRecord(t *testing.T) {
	sem := parser.NewSemanticContext()
	call := flattenedCall("enc-conn.init", "ec")
	if got := sem.CalleeOf(call); got != parser.CalleeUnknown {
		t.Fatalf("unrecorded call must read back CalleeUnknown, got %v", got)
	}
	sem.SetCallee(call, parser.CalleeMethod)
	if got := sem.CalleeOf(call); got != parser.CalleeMethod {
		t.Fatalf("recorded CalleeMethod must read back, got %v", got)
	}
	// Distinct nodes must not share a record.
	if got := sem.CalleeOf(flattenedCall("enc-conn.init", "ec")); got != parser.CalleeUnknown {
		t.Fatalf("a different node must not inherit the record, got %v", got)
	}

	var nilSem *parser.SemanticContext
	nilSem.SetCallee(call, parser.CalleeMethod) // must not panic
	if got := nilSem.CalleeOf(call); got != parser.CalleeUnknown {
		t.Fatalf("nil Sem must read back CalleeUnknown, got %v", got)
	}
	if got := sem.CalleeOf(nil); got != parser.CalleeUnknown {
		t.Fatalf("nil node must read back CalleeUnknown, got %v", got)
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
	if prog.Sem == nil {
		t.Fatalf("ParseProgram must install the semantic side table (fixture drifted)")
	}
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

// The OTHER producer of the flattened shape: the source-level static call
// `Type.method(recv, …)`. Here the receiver is written explicitly by the user
// as the first argument, and checker.resolveMethodCallsInExpr only completes
// the callee name (`Type.method` → `net.Type.method`) while leaving the
// arguments alone. So Arguments[0] is still the receiver and the rewrite must
// be recorded as CalleeMethod — otherwise this form regresses straight back to
// the i3k422u3 false positive, because the old name-based inference used to
// cover it (the completed name did match a method definition).
func TestStaticMethodCallRewriteRecordsCalleeMethod(t *testing.T) {
	sem := parser.NewSemanticContext()
	ce := &parser.CallExpression{
		Function: &parser.DotExpression{
			Receiver: &parser.Identifier{Value: "enc-conn"},
			Property: "init",
		},
		Arguments: []parser.Expression{
			&parser.Identifier{Value: "ec"},
			&parser.Identifier{Value: "c"},
			&parser.Identifier{Value: "key"},
		},
	}
	typeOwner := map[string]string{"net.enc-conn": "enc-conn"}
	definedMethods := map[string]bool{"net.enc-conn.init": true}

	out := resolveMethodCallsInExpr(ce, sem, typeOwner, definedMethods)
	call, ok := out.(*parser.CallExpression)
	if !ok {
		t.Fatalf("rewrite must keep the CallExpression, got %#v", out)
	}
	id, ok := call.Function.(*parser.Identifier)
	if !ok || id.Value != "net.enc-conn.init" {
		t.Fatalf("callee must be completed to net.enc-conn.init, got %#v", call.Function)
	}
	if got := sem.CalleeOf(ce); got != parser.CalleeMethod {
		t.Fatalf("static method call rewrite must record CalleeMethod, got %v", got)
	}

	assigned := map[string]bool{}
	collectAssignedNamesInExpr(ce, assigned, sem)
	if !assigned["ec"] {
		t.Fatalf("the explicitly written receiver `ec` must count as assigned, got %v", assigned)
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
// to the merged program — BOTH halves of it: the AST rewrite and the
// `Sem.SetCallee(…, CalleeMethod)` record. If the record half is dropped the
// fixture stops reproducing the real pipeline and the lint silently loses the
// ability to see the receiver.
//
// Only the subset needed by these tests is handled: a call is flattened when
// the receiver's declared type carries a matching method definition. Returns
// the number of rewrites performed (callers assert on it so a silent fixture
// drift cannot turn the test into a no-op).
func flattenCalls(prog *parser.Program) int {
	if prog.Sem == nil {
		return 0
	}
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
			// The half that makes this shape self-describing: without it the
			// checker cannot tell a rewritten receiver from a plain argument.
			prog.Sem.SetCallee(call, parser.CalleeMethod)
			n++
		}
	}
	return n
}
