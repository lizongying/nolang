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

// ─────────────────────────────────────────────────────────────────────────────
// S2 precondition — closing the ast.go blind spot.
//
// Every coverage test above derives its marker set from ast.go ALONE. A node
// type declared in any other file is therefore invisible to them, and an
// unlowered node of that kind silently becomes hir.KUnknown.
//
// That is not hypothetical: SurfaceMatch is declared in lowering.go, and it was
// the one sugar node ASTToHIR could not lower (design doc §13.4.1). sugarParserSources
// already scans every non-test .go file and its own doc comment calls this out
// as load-bearing — but until now nothing joined the two scans together.
//
// That gap is now CLOSED: `case *SurfaceMatch` landed in tohir.go's exprNode
// (S2 core), so ASTToHIR lowers the node itself instead of relying on a
// pre-pass. The exemption map below is therefore empty, and the gate now
// asserts the stronger property: EVERY marker-implementing type declared
// outside ast.go must have a lowering case.
//
// The map is kept rather than deleted so the forward guard survives: a future
// node type declared outside ast.go without a case fails the `uncovered` loop
// and is named, and the map is the documented escape hatch for a deliberate,
// reasoned exception.
// ─────────────────────────────────────────────────────────────────────────────

// unloweredSugarExemptions is the set of sugar node types ASTToHIR does NOT yet
// lower. Each entry is checked three ways, and the set must match the measured
// gap EXACTLY in both directions:
//
//   - it must be a registered sugar type (sugarASTTypes) — a BASE type here
//     would be a genuinely unlowered node, not an exemption;
//   - it must still be absent from tohir.go's type switches — the entry is live
//     evidence of the gap, so landing the missing case flips the assertion and
//     names this entry as stale;
//   - together the entries must account for the WHOLE blind spot — a node type
//     declared outside ast.go that is neither lowered nor listed here fails.
//
// EMPTY as of S2 core: the only entry ever recorded was SurfaceMatch, which
// tohir.go now lowers directly.
var unloweredSugarExemptions = map[string]string{}

// TestASTToHIRCoverageHasNoAstGoBlindSpot extends the coverage gates above to
// node types declared outside ast.go.
//
// This is the STATIC half of C4 ("does a case exist for every type?"). Its
// runtime counterpart — "did anything actually fall through on the corpus?" —
// is TestH0DropsNothingUnknown in parser/dump/h0_sugar_gap_test.go. Neither
// subsumes the other: this one cannot see a type whose case exists but whose
// sub-branch still drops, and that one cannot see a type the corpus never
// exercises.
func TestASTToHIRCoverageHasNoAstGoBlindSpot(t *testing.T) {
	// The all-files marker set (type name -> marker names). This is the set the
	// coverage tests above do NOT have.
	_, allFiles := sugarParserSources(t)
	if len(allFiles) == 0 {
		t.Fatal("sugarParserSources found no marker-implementing types; the scan broke")
	}

	// The set the coverage tests above DO have, flattened to type names.
	onlyAstGo := astMarkerTypes(t)
	inAstGo := map[string]bool{}
	for _, types := range onlyAstGo {
		for typ := range types {
			inAstGo[typ] = true
		}
	}

	// The blind spot: marker-implementing types declared outside ast.go.
	blind := map[string]bool{}
	for typ := range allFiles {
		if !inAstGo[typ] {
			blind[typ] = true
		}
	}

	// Anti-vacuity: the blind spot must be real, or this gate proves nothing.
	if len(blind) == 0 {
		t.Fatal("every marker-implementing type is declared in ast.go; the blind-spot " +
			"premise broke and this gate is vacuous — delete it and say why")
	}

	// Which of them does ASTToHIR actually cover today?
	cases := switchCaseTypes(t, "stmtNode", "exprNode", "annValue")
	covered := union(cases["stmtNode"], cases["exprNode"], cases["annValue"])

	uncovered := map[string]bool{}
	for typ := range blind {
		if !covered[typ] {
			uncovered[typ] = true
		}
	}
	t.Logf("ast.go covers %d marker types; %d more are declared outside it (%v); "+
		"of those, %d are still unlowered by ASTToHIR: %v",
		len(inAstGo), len(blind), sortedKeys(blind), len(uncovered), sortedKeys(uncovered))

	// The gap must be EXACTLY the recorded exemption set — no more, no less.
	for typ := range uncovered {
		if _, ok := unloweredSugarExemptions[typ]; !ok {
			t.Errorf("%s is declared outside ast.go and ASTToHIR has no case for it, so it "+
				"silently lowers to hir.KUnknown; it is also missing from the ast.go-only "+
				"coverage gates above. Either add a case to tohir.go, or record it in "+
				"unloweredSugarExemptions with a rationale", typ)
		}
	}
	for typ := range unloweredSugarExemptions {
		if _, ok := sugarASTTypes[typ]; !ok {
			t.Errorf("%s is exempted from ASTToHIR coverage but is not registered in "+
				"sugarASTTypes; a BASE node type must be lowered, not exempted", typ)
		}
		if !uncovered[typ] {
			t.Errorf("exemption %s is stale: ASTToHIR now covers it. Delete it from "+
				"unloweredSugarExemptions (and consider enabling the sugar-HIR gate in "+
				"hir/sugar_kinds_test.go)", typ)
		}
	}
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
