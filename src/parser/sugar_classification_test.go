package parser

// ─────────────────────────────────────────────────────────────────────────────
// S0 instrumentation for docs/design/nolang-parser-lowering-invariants.md.
//
// This file adds NO production code and changes NO behaviour. It makes
// invariant P1 / constraint C2 *measurable*:
//
//	P1  the AST must faithfully mirror source syntax, with every piece of sugar
//	    represented as a dedicated sugar node;
//	C2  every AST type implementing a marker interface must be EXPLICITLY
//	    classified as BASE or SUGAR — there is no default arm.
//
// The absence of a default arm is the whole point. A "default to BASE" rule
// would let a newly introduced sugar node slip through silently, which is
// exactly the failure mode the invariant exists to prevent. So adding an AST
// node type fails this test until it is classified, and the failure names the
// design doc.
//
// The gates for C3 ("every sugar node is consumed by the lowering pass") cannot
// run yet: today the desugaring lives in lowering.go and executes *inside*
// ParseProgram, driven by reflection, so there is no dispatch table to compare a
// registry against. They are present below as t.Skip'd tests so S2 can enable
// them without re-deriving them.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sugarMarkerNames is the closed set of AST marker interfaces. A concrete node
// type is identified by the receiver of one of these methods. The names are
// spelled out rather than matched by a suffix heuristic (as
// tohir_exhaustive_test.go does) so that an unrelated unexported method ending
// in "Node" cannot be mistaken for a node type.
var sugarMarkerNames = map[string]bool{
	"statementNode":       true,
	"expressionNode":      true,
	"annotationValueNode": true,
	"typeNode":            true,
}

// sugarASTTypes names every AST node type that is SUGAR: a node whose semantics
// is not "itself" but the expansion of some larger base construct
// (design doc §1.3). H0 must translate each of these into base HIR nodes; none
// may survive into HIR.
//
// The set is the design doc's §4.1 minimum, restricted to the node types that
// exist *today* — the rest of §4.1 (CompoundAssign, OptionArith,
// IndexOutAssign, Pipeline) is currently expanded inside the parser without a
// dedicated node, which is precisely violations V3/V4/V5/V6. Those entries
// appear here only once the corresponding node type exists.
var sugarASTTypes = map[string]string{
	"SurfaceMatch":          "§4.1 #1 — match syntax; expands to an if/elif/else chain",
	"UnwrapAssignStatement": "§4.1 #2 — `x ?= e`; expands to a tag test + let/return",
	"MultiAssignStatement":  "§4.1 #6 — `a, b = f()`; destructuring, H0 emits KMultiAssign 1:1",
	"GroupedExpression": "§5 rules KGrouped SUGAR (pure parentheses, no semantics), " +
		"so H0 must erase it — §10.2 lists this as the first non-vacuous instance",
}

// baseASTTypes names every remaining AST node type: nodes H0 emits 1:1 with a
// single, type-free lowering rule.
//
// It is an explicit list rather than "everything not in sugarASTTypes" so that
// the classification is a genuine total partition: a new node type fails the
// gate in both directions (unclassified, and absent from this list) instead of
// silently defaulting to BASE.
//
// Keep sorted; TestSugarClassificationIsTotal checks the partition, not the
// order.
var baseASTTypes = []string{
	"AnnotationArrayValue",
	"AnnotationBoolValue",
	"AnnotationIdentValue",
	"AnnotationIntValue",
	"AnnotationRangeValue",
	"AnnotationStatement",
	"AnnotationStringValue",
	"AnnotationStructValue",
	"ArrayLiteral",
	"ArrayType",
	"AssignExpression",
	"AwaitExpression",
	"BlockStatement",
	"BooleanLiteral",
	"BreakStatement",
	"CallExpression",
	"CastExpression",
	"CharLiteral",
	"CoExpression",
	"ConditionalExpression",
	"ContinueStatement",
	"DotExpression",
	"EnumDefinition",
	"ExportStatement",
	"ExpressionStatement",
	"ExternStatement",
	"FloatLiteral",
	"ForStatement",
	"FunctionDefinition",
	"FunctionLiteral",
	"FunctionType",
	"Identifier",
	"IfExpression",
	"IndexExpression",
	"InfixExpression",
	"IntegerLiteral",
	"InterfaceDefinition",
	"IterationExpr",
	"LetStatement",
	"MapLiteral",
	"MapType",
	"NamedType",
	"NilLiteral",
	"NullableType",
	"Parameter",
	"PointerType",
	"PrefixExpression",
	"RangeExpression",
	"RegexLiteral",
	"ReturnStatement",
	"RunExpression",
	"SliceExpression",
	"SliceLiteral",
	"SliceType",
	"StringLiteral",
	"StructDefinition",
	"StructLiteral",
	"TaggedEnumDefinition",
	"TypeAlias",
	"UnionType",
	"UseStatement",
	"ViewType",
}

// sugarParserSources parses every non-test .go file of this package and returns
// the raw source of each (keyed by basename) together with the set of type
// names implementing a marker interface.
//
// Unlike astMarkerTypes (tohir_exhaustive_test.go), which reads only ast.go,
// this scans *every* file. That difference is load-bearing: SurfaceMatch is
// declared in lowering.go, so a scan limited to ast.go omits it from the marker
// set — and therefore from every exhaustiveness check built on that set. (That
// omission is why tohir.go's missing `case *SurfaceMatch` has gone unnoticed;
// see TestSugarNodesAreConsumedByLowering.)
func sugarParserSources(t *testing.T) (map[string]string, map[string]map[string]bool) {
	t.Helper()
	dir := thisDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	sources := map[string]string{}
	markers := map[string]map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		sources[name] = string(raw)

		f, err := goparser.ParseFile(token.NewFileSet(), path, raw, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if !sugarMarkerNames[fn.Name.Name] {
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
			if markers[id.Name] == nil {
				markers[id.Name] = map[string]bool{}
			}
			markers[id.Name][fn.Name.Name] = true
		}
	}
	if len(sources) == 0 {
		t.Fatal("no non-test .go files found in the parser package; the scan broke")
	}
	return sources, markers
}

func sortedSugarKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestSugarClassificationIsTotal is gate G2 (constraint C2): the classification
// is a total, explicit partition of the AST node types, and it is checked in
// both directions.
func TestSugarClassificationIsTotal(t *testing.T) {
	_, markers := sugarParserSources(t)

	base := map[string]bool{}
	for _, n := range baseASTTypes {
		if base[n] {
			t.Errorf("baseASTTypes lists %q twice", n)
		}
		base[n] = true
	}
	sugar := map[string]bool{}
	for n := range sugarASTTypes {
		sugar[n] = true
	}

	// Anti-vacuity. The repo's convention is that "0 violations" is not a result
	// until the detector is shown to be able to see the thing it is checking.
	if len(markers) < 60 {
		t.Fatalf("only %d marker types found across the parser package; the detection "+
			"heuristic broke, so every assertion below would be vacuous", len(markers))
	}
	if len(sugar) == 0 {
		t.Fatal("sugarASTTypes is empty, so this gate cannot fail")
	}
	if len(base) == 0 {
		t.Fatal("baseASTTypes is empty")
	}

	var unclassified, stale, both []string
	for n := range markers {
		if !base[n] && !sugar[n] {
			unclassified = append(unclassified, n)
		}
	}
	for n := range sugar {
		if markers[n] == nil {
			stale = append(stale, n)
		}
		if base[n] {
			both = append(both, n)
		}
	}
	for _, n := range baseASTTypes {
		if markers[n] == nil {
			stale = append(stale, n)
		}
	}
	sort.Strings(unclassified)
	sort.Strings(stale)
	sort.Strings(both)

	if len(unclassified) > 0 {
		t.Errorf("AST node type(s) not classified as BASE or SUGAR: %v\n"+
			"classify each in sugarASTTypes or baseASTTypes "+
			"(docs/design/nolang-parser-lowering-invariants.md §4, §6 C2)", unclassified)
	}
	if len(stale) > 0 {
		t.Errorf("classified type(s) that no longer implement an AST marker interface: %v\n"+
			"remove or rename them in the registry", stale)
	}
	if len(both) > 0 {
		t.Errorf("type(s) classified as BOTH base and sugar: %v", both)
	}

	t.Logf("%d marker types: %d base, %d sugar", len(markers), len(base), len(sugar))
}

// TestSugarRegistryEntriesAreLive is the anti-vacuity control for the registry:
// a registered sugar type that the parser never constructs is a dead entry, and
// a dead entry makes the C3 gate ("every sugar node has a handler") trivially
// satisfiable.
func TestSugarRegistryEntriesAreLive(t *testing.T) {
	sources, _ := sugarParserSources(t)

	var all strings.Builder
	for _, name := range sortedSugarKeys(sources) {
		all.WriteString(sources[name])
	}
	blob := all.String()

	for _, name := range sortedSugarKeys(sugarASTTypes) {
		if !strings.Contains(blob, "&"+name+"{") {
			t.Errorf("sugar type %q is registered but no non-test parser file constructs "+
				"it (no `&%s{`); a dead registry entry makes the C3 gate vacuous",
				name, name)
		}
	}
}

// TestSugarNodesAreConsumedByLowering is gate G2's second half (constraint C3):
// every registered sugar node type must be consumed by the lowering pass, and
// every lowering handler must correspond to a registered sugar type.
//
// SKIPPED until S2. Today the desugaring is lowering.go's reflection walk
// (`lowerProgram`), which has no dispatch table a registry could be compared
// against, and SurfaceMatch is expanded before ParseProgram even returns. S2
// folds the desugaring into tohir.go's streaming walk, at which point the
// comparison becomes mechanical:
//
//	sugarASTTypes ⊆ union(cases of hirConv.stmtNode, hirConv.exprNode)
//
// reusing switchCaseTypes from tohir_exhaustive_test.go for the case list.
//
// One finding to carry into S2, recorded here because it is the gap this test
// will close: tohir.go has NO `case *SurfaceMatch`. If the node ever reaches H0
// it lowers to hir.KUnknown — silently, since the ASTToHIR switch has no default
// diagnostic. That is harmless today only because lowering.go always expands it
// first, which is exactly what S1/S2 change.
//
// The gap is no longer unchecked: TestASTToHIRCoverageHasNoAstGoBlindSpot
// (tohir_exhaustive_test.go) joins the all-files marker scan to the ast.go-only
// coverage gates and pins `SurfaceMatch` as the single recorded exemption. When
// this test is enabled, that exemption entry must be gone.
func TestSugarNodesAreConsumedByLowering(t *testing.T) {
	t.Skip("S2 (docs/design/nolang-parser-lowering-invariants.md §8): enable once the " +
		"desugaring lives in tohir.go's dispatch instead of lowering.go's reflection walk")
}

// TestLoweringHandlersHaveNoOrphans is gate G2's third assertion (C3): a handler
// in the lowering dispatch that no registry entry claims is either a
// mis-classified base node or a stale handler.
//
// SKIPPED for the same reason as TestSugarNodesAreConsumedByLowering.
func TestLoweringHandlersHaveNoOrphans(t *testing.T) {
	t.Skip("S2 (docs/design/nolang-parser-lowering-invariants.md §8): no lowering " +
		"dispatch table exists yet to compare against")
}
