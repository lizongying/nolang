package dump

// ─────────────────────────────────────────────────────────────────────────────
// S2 preparation: the RUNTIME half of "HIR must contain no sugar" (C4 / G3).
//
// src/parser/tohir.go has three fallbacks that intern fmt.Sprintf("%T", node)
// into hir.KUnknown — stmtNode :506, annValue :735, exprNode :1067. They are
// the difference between "H0 does not know this construct" and a silently
// wrong tree.
//
// tohir_exhaustive_test.go's coverage gates answer the STATIC question: "is
// there a `case` for every marker type?" They cannot see a type whose case
// exists but whose sub-branch falls through, and — because astMarkerTypes
// reads ast.go only — they were blind to SurfaceMatch entirely. TestASTToHIR-
// CoverageHasNoAstGoBlindSpot (design doc §15a) closes that half.
//
// This file answers the RUNTIME question: feed the whole corpus to H0 under
// both the shipped shape and the sugar shape, and count what actually falls
// through to KUnknown, naming the Go type. Under `lowered` the answer must be
// empty — nothing on the shipped path is dropped. Under `sugar` it must be
// exactly SurfaceMatch, the one sugar node H0 cannot lower yet.
//
// The two halves are complementary and neither subsumes the other:
//
//	static (§15a)   case exists for every type?          — catches a type nobody
//	                                                         wrote a case for
//	runtime (here)  anything actually fell through?      — catches a type WITH a
//	                                                         case whose branch
//	                                                         still drops
// ─────────────────────────────────────────────────────────────────────────────

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// h0ExpectedDrops records, per parsing shape, which AST node types ASTToHIR is
// *allowed* to fail to lower. The measured set must equal the recorded set
// EXACTLY in both directions: a new drop fails and is named, and a drop that
// has been fixed must be deleted here (which is what makes landing
// `case *SurfaceMatch` a deliberate act rather than a silent improvement).
//
// On the count itself: TestCorpusSugarCensus (sugar_seam_test.go) reflectively
// tallies 697 DISTINCT SurfaceMatch nodes in the sugar AST, while this gate
// measures how many KUnknown nodes H0 actually emits. The latter is smaller
// because HIR only gets a node for a SurfaceMatch its traversal actually
// reaches — the rest sit in subtrees H0 does not descend into. The gap is the
// reachable subset, not a counting error; what this gate pins is the *set* of
// dropped types, and the count is a monotone tripwire.
//
// BOTH entries are empty. The shipped shape must never drop anything (H0 is the
// last stage before codegen, and a KUnknown here becomes a mysterious MIR
// failure downstream). The sugar shape dropped exactly SurfaceMatch, 627 nodes,
// until S2 core landed `case *SurfaceMatch` in tohir.go — H0 now desugars the
// node itself, so nothing falls through. See
// parser/dump/hir_equivalence_test.go, which proves the desugar produces the
// SAME HIR as the parser's lowering did; this gate only proves nothing is lost.
var h0ExpectedDrops = map[string]map[string]string{
	"lowered": {},
	"sugar":   {},
}

// h0DroppedTypes returns the Go type names of every AST node ASTToHIR could not
// lower, i.e. every hir.KUnknown in the package, keyed by the S string the
// fallback interned. KUnknown's S is populated by exactly the three fallbacks,
// so a non-empty result is always a real drop, never a legitimate node kind.
//
// ⚠️ It iterates pkg.Nodes directly (the same way hir.Package.KindCounts does)
// instead of using Walk/WalkTop: HIR is a DAG and Walk visits a node once per
// PARENT, so a dropped node reachable from two parents would be counted twice.
// That mistake showed up as 2362 "drops" against the reflective census's 697
// distinct SurfaceMatch nodes — a 3.4× inflation from double-visiting, not a
// real difference in how many nodes fell through.
func h0DroppedTypes(pkg *hir.Package) map[string]int {
	out := map[string]int{}
	if pkg == nil {
		return out
	}
	for i := 1; i < len(pkg.Nodes); i++ {
		if pkg.Nodes[i].Kind == hir.KUnknown {
			out[pkg.Str(pkg.Nodes[i].S)]++
		}
	}
	return out
}

// countSurfaceMatches counts *parser.SurfaceMatch nodes reachable from prog.
//
// This is the positive half of non-vacuity for the sugar shape. h0ExpectedDrops
// is empty (S2 core landed `case *SurfaceMatch`), so "0 KUnknown" is the
// expected result — and a gate whose expected result is "nothing happened" is
// worthless unless something was there to happen. Counts, does not assert.
//
// It duplicates the reflective walk in parser/sugar_seam_test.go's
// sugarNodeCensus rather than exporting that helper: the census tallies every
// sugar type keyed by name, this counts one type and needs to be callable from
// the dump package.
func countSurfaceMatches(prog *parser.Program) int {
	if prog == nil {
		return 0
	}
	seen := map[uintptr]bool{}
	n := 0
	var walk func(v reflect.Value, depth int)
	walk = func(v reflect.Value, depth int) {
		if !v.IsValid() || depth > 400 {
			return
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			addr := v.Pointer()
			if seen[addr] {
				return
			}
			seen[addr] = true
			if v.Elem().Type().Name() == "SurfaceMatch" {
				n++
			}
			walk(v.Elem(), depth+1)
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), depth+1)
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), depth+1)
			}
		case reflect.Struct:
			// Side tables are not part of the node tree; Sem in particular is a
			// map keyed by Node and would dominate the walk. Same carve-out as
			// parser/sugar_seam_test.go's sugarNodeCensus.
			isProgram := v.Type() == programReflectType
			for i := 0; i < v.NumField(); i++ {
				if isProgram {
					switch v.Type().Field(i).Name {
					case "Sem", "Warnings", "BuiltinFuncNames":
						continue
					}
				}
				walk(v.Field(i), depth+1)
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				walk(it.Value(), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(prog), 0)
	return n
}

var programReflectType = reflect.TypeOf((*parser.Program)(nil))

// TestH0DropsNothingUnknown lowers the whole tests/ corpus under every shape
// with a recorded expectation and asserts the measured drop set matches.
func TestH0DropsNothingUnknown(t *testing.T) {
	if _, err := os.Stat(corpusRoot); err != nil {
		t.Skipf("no corpus at %s: %v", corpusRoot, err)
	}

	for _, sh := range []shape{loweredShape(), sugarShape()} {
		want, ok := h0ExpectedDrops[sh.name]
		if !ok {
			t.Fatalf("shape %q has no recorded expectation in h0ExpectedDrops", sh.name)
		}

		dropped := map[string]int{}
		var compared int
		var sugarNodes int
		walkErr := filepath.WalkDir(corpusRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".no") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			p := parser.New(lexer.NewCached(path, string(raw)))
			p.Filename = path
			sh.configure(p)
			prog := p.ParseProgram()
			// Only programs that parse cleanly: a parse error means the shape
			// itself is unsupported for that file, which is a different question.
			if len(p.Errors()) > 0 {
				return nil
			}
			// Positive non-vacuity: the sugar shape must actually CONTAIN the
			// node it is expected to lower. With h0ExpectedDrops empty, "0
			// KUnknown" would otherwise be equally consistent with a corpus
			// that never writes a match.
			if sh.name == "sugar" {
				sugarNodes += countSurfaceMatches(prog)
			}
			var pkg *hir.Package
			func() {
				defer func() {
					if r := recover(); r != nil {
						pkg = nil
					}
				}()
				pkg = parser.ASTToHIR(prog)
			}()
			if pkg == nil {
				return nil
			}
			compared++
			for k, n := range h0DroppedTypes(pkg) {
				dropped[k] += n
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walking %s: %v", corpusRoot, walkErr)
		}
		if compared < 200 {
			t.Fatalf("only %d programs lowered to HIR under the %s shape; the census "+
				"would be vacuous", compared, sh.name)
		}
		if sh.name == "sugar" && sugarNodes == 0 {
			t.Fatalf("the sugar shape contains no SurfaceMatch nodes, so \"0 drops\" " +
				"proves nothing about whether H0 can lower them; either the corpus " +
				"stopped producing the construct or the scan broke")
		}

		// Anti-vacuity: if a shape expects drops, it must actually produce them,
		// or the gate is asserting 0 == 0.
		var total int
		for _, n := range dropped {
			total += n
		}
		if len(want) > 0 && total == 0 {
			t.Errorf("%s shape: %d programs lowered, but nothing fell through to "+
				"KUnknown while %v was expected. Either S2 landed (delete the "+
				"expectation) or the corpus stopped producing the construct.", sh.name, compared, keysOf(want))
		}
		if len(want) == 0 && total != 0 {
			t.Errorf("%s shape: %d programs lowered and %d nodes fell through to "+
				"KUnknown, but nothing is recorded as droppable: %v", sh.name, compared, total, keysOfInt(dropped))
		}

		// The measured set must equal the recorded set, both ways.
		for typ, n := range dropped {
			if _, ok := want[typ]; !ok {
				t.Errorf("%s shape: ASTToHIR dropped %d × %s, which is not recorded in "+
					"h0ExpectedDrops. Add a case to tohir.go, or record it with a "+
					"rationale", sh.name, n, typ)
			}
		}
		for typ := range want {
			if _, ok := dropped[typ]; !ok {
				t.Errorf("%s shape: recorded drop %s no longer happens — ASTToHIR covers "+
					"it now. Delete it from h0ExpectedDrops", sh.name, typ)
			}
		}

	if sh.name == "sugar" {
		t.Logf("%s shape: %d programs, %d KUnknown nodes, drop set %v; "+
			"the shape contained %d SurfaceMatch nodes, all lowered (non-vacuity: "+
			"a corpus with 0 matches would make \"0 drops\" meaningless)",
			sh.name, compared, total, keysOfInt(dropped), sugarNodes)
	} else {
		t.Logf("%s shape: %d programs, %d KUnknown nodes, drop set %v",
			sh.name, compared, total, keysOfInt(dropped))
	}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keysOfInt(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, fmt.Sprintf("%s=%d", k, m[k]))
	}
	sort.Strings(out)
	return out
}
