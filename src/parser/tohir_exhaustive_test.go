package parser

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// This file guards the AST -> HIR lowering against silent incompleteness.
//
// ASTToHIR dispatches on a type switch. If a new AST node type is added to
// ast.go and nobody extends the switch, the node quietly lowers to a
// hir.KUnknown placeholder: the compiler still builds, the tree is still well
// formed, and the missing construct only shows up as a mysterious codegen or
// validation failure much later.
//
// Rather than hand-maintaining a list of node types (which drifts), these
// tests read the source of ast.go and tohir.go with go/ast and compare the
// two sets. Adding a node type to ast.go without lowering it fails the build's
// test step immediately, naming the exact type.

func thisDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve the test's own source path")
	}
	return filepath.Dir(self)
}

func parseGoFile(t *testing.T, path string) *ast.File {
	t.Helper()
	f, err := goparser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", filepath.Base(path), err)
	}
	return f
}

// astMarkerTypes returns marker method name -> set of concrete AST types that
// implement it. The AST uses unexported marker methods (statementNode,
// expressionNode, annotationValueNode, typeNode) to close its interfaces, so
// the receivers of those methods are exactly the concrete node types.
func astMarkerTypes(t *testing.T) map[string]map[string]bool {
	t.Helper()
	file := parseGoFile(t, filepath.Join(thisDir(t), "ast.go"))

	out := map[string]map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		if !strings.HasSuffix(fn.Name.Name, "Node") || ast.IsExported(fn.Name.Name) {
			continue
		}
		recv := fn.Recv.List[0].Type
		if star, ok := recv.(*ast.StarExpr); ok {
			recv = star.X
		}
		id, ok := recv.(*ast.Ident)
		if !ok {
			continue
		}
		if out[fn.Name.Name] == nil {
			out[fn.Name.Name] = map[string]bool{}
		}
		out[fn.Name.Name][id.Name] = true
	}
	if len(out) == 0 {
		t.Fatal("found no marker methods in ast.go; the detection heuristic broke")
	}
	return out
}

// switchCaseTypes returns function name -> set of pointer types named in that
// function's type-switch cases, for the requested functions of tohir.go.
func switchCaseTypes(t *testing.T, funcNames ...string) map[string]map[string]bool {
	t.Helper()
	file := parseGoFile(t, filepath.Join(thisDir(t), "tohir.go"))

	want := map[string]bool{}
	for _, n := range funcNames {
		want[n] = true
	}

	out := map[string]map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !want[fn.Name.Name] {
			continue
		}
		cases := map[string]bool{}
		ast.Inspect(fn, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				star, ok := e.(*ast.StarExpr)
				if !ok {
					continue
				}
				if id, ok := star.X.(*ast.Ident); ok {
					cases[id.Name] = true
				}
			}
			return true
		})
		out[fn.Name.Name] = cases
	}
	for _, n := range funcNames {
		if len(out[n]) == 0 {
			t.Fatalf("no type-switch cases found in tohir.go func %q", n)
		}
	}
	return out
}

func missing(required, covered map[string]bool) []string {
	var out []string
	for name := range required {
		if !covered[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func union(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, s := range sets {
		for k := range s {
			out[k] = true
		}
	}
	return out
}

// TestASTToHIRCoversAllStatements asserts every type implementing
// statementNode is lowered. Statements are accepted in either switch because a
// few node types are both statements and expressions. The statement switch
// lives in hirConv.stmtNode (hirConv.stmt is just an annotation wrapper that
// delegates to it).
func TestASTToHIRCoversAllStatements(t *testing.T) {
	markers := astMarkerTypes(t)
	cases := switchCaseTypes(t, "stmtNode", "exprNode")

	if got := missing(markers["statementNode"], union(cases["stmtNode"], cases["exprNode"])); len(got) > 0 {
		t.Errorf("AST statement types not lowered by ASTToHIR: %v\n"+
			"add a case to hirConv.stmtNode in tohir.go, or the node will silently "+
			"lower to hir.KUnknown", got)
	}
}

// TestASTToHIRCoversAllExpressions asserts every type implementing
// expressionNode is lowered.
func TestASTToHIRCoversAllExpressions(t *testing.T) {
	markers := astMarkerTypes(t)
	cases := switchCaseTypes(t, "stmtNode", "exprNode")

	if got := missing(markers["expressionNode"], union(cases["exprNode"], cases["stmtNode"])); len(got) > 0 {
		t.Errorf("AST expression types not lowered by ASTToHIR: %v\n"+
			"add a case to hirConv.exprNode in tohir.go, or the node will silently "+
			"lower to hir.KUnknown", got)
	}
}

// TestASTToHIRCoversAllAnnotationValues asserts every annotation value type is
// lowered by hirConv.annValue.
func TestASTToHIRCoversAllAnnotationValues(t *testing.T) {
	markers := astMarkerTypes(t)
	cases := switchCaseTypes(t, "annValue")

	if got := missing(markers["annotationValueNode"], cases["annValue"]); len(got) > 0 {
		t.Errorf("annotation value types not lowered by ASTToHIR: %v", got)
	}
}

// TestMarkerCountsAreSane is a canary on the detection heuristic itself: if a
// refactor renames the marker methods or moves node types out of ast.go, the
// coverage tests above would pass vacuously. Lower bounds, not exact counts,
// so that adding node types does not require touching this test.
func TestMarkerCountsAreSane(t *testing.T) {
	markers := astMarkerTypes(t)
	for _, tc := range []struct {
		marker string
		min    int
	}{
		{"statementNode", 15},
		{"expressionNode", 25},
		{"annotationValueNode", 5},
		{"typeNode", 6},
	} {
		if n := len(markers[tc.marker]); n < tc.min {
			t.Errorf("only %d types implement %s, expected at least %d; "+
				"has the marker been renamed or have node types moved out of ast.go?",
				n, tc.marker, tc.min)
		}
	}
}
