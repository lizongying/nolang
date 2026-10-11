package parser

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestProgramGobRoundTripPreservesNodeSemMapping is the DEEP counterpart of
// TestProgramGobRoundTrip above: it compares the annotation attachment of EVERY
// node, not just the top-level statements.
//
// The shallow test cannot see this class of defect. nodeSem is restored by
// walking the decoded program in the same order used to serialize it, so the
// two walks must enumerate the SAME number of nodes. Encoding/gob flattens
// pointers, so a node shared by two fields (the lowering shares several: e.g.
// the index-out desugar reuses one default-value literal for both match arms
// and one temp identifier in two nodes) is decoded as two distinct objects.
// A walk that deduplicates by pointer identity therefore skips fewer duplicates
// after decoding than before, every index after the first divergence shifts,
// and the restored entries land on unrelated statements.
//
// Measured symptom before the fix: `no vet src/std/archive/bzip2.no` reported
// `nolang-overflow-ineffective` for a perfectly valid `#{overflow=wrap}`, but
// only on an AST-cache HIT — with NOLANG_AST_CACHE_OFF=1 the warning vanished.
//
// The fixture asserts its own non-vacuity: if the program stops containing
// aliased nodes, the test fails rather than silently proving nothing.
func TestProgramGobRoundTripPreservesNodeSemMapping(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// The `#{index-out=zero}` desugar shares a default-value literal
			// between its two match arms and a temp identifier between the
			// binding and the match subject.
			"index-out read",
			"f = (a []i64, i i64) {\n" +
				"    #{index-out=zero}\n" +
				"    x = a[i]\n" +
				"    print(x)\n" +
				"}\n",
		},
		{
			// Same, plus an annotated integer operation whose annotation must
			// survive on the statement it governs.
			"index-out read + annotated arith",
			"f = (a []i64, i i64) {\n" +
				"    n [24]i64\n" +
				"    #{index-out=zero}\n" +
				"    l = a[i]\n" +
				"    #{index-out=zero, overflow=wrap}\n" +
				"    n[l] = n[l] + 1\n" +
				"}\n",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			p := New(lexer.NewCached("gobmap.no", c.src))
			orig := p.ParseProgram()
			if orig == nil || len(p.Errors()) > 0 {
				t.Fatalf("parse failed: %v", p.Errors())
			}

			// Non-vacuity: the fixture must actually contain a shared node,
			// otherwise the walk order cannot diverge and this test would pass
			// even with the deduplicating walk it exists to reject.
			occ := countNodeOccurrences(orig)
			uniq := countUniqueNodes(orig)
			if occ <= uniq {
				t.Fatalf("fixture is stale: no aliased nodes (occurrences=%d unique=%d); "+
					"the regression it guards cannot be exercised", occ, uniq)
			}

			cp := &CachedProgram{Prog: orig, NodeSem: SerializeNodeSem(orig)}
			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(cp); err != nil {
				t.Fatalf("encode: %v", err)
			}
			var dcp CachedProgram
			if err := gob.NewDecoder(&buf).Decode(&dcp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			RestoreNodeSem(dcp.Prog, dcp.NodeSem)

			before := nodeSemSequence(orig)
			after := nodeSemSequence(dcp.Prog)
			if len(before) != len(after) {
				t.Fatalf("walk length changed across the round-trip: before=%d after=%d "+
					"(the restore walk must enumerate exactly the occurrences the "+
					"serialize walk enumerated)", len(before), len(after))
			}
			mismatches := 0
			for i := range before {
				if before[i] != after[i] {
					if mismatches < 5 {
						t.Errorf("index %d: before=%q after=%q", i, before[i], after[i])
					}
					mismatches++
				}
			}
			if mismatches > 0 {
				t.Errorf("%d of %d node/annotation slots were restored onto the wrong node",
					mismatches, len(before))
			}
		})
	}
}

// nodeSemSequence renders every node occurrence (type, position) together with
// the annotations attached to it, in walk order. Two programs whose nodeSem
// side-tables are equivalent produce identical sequences; a shifted mapping
// shows up as a mismatch.
func nodeSemSequence(prog *Program) []string {
	var out []string
	walkProgramNodes(prog, func(n Node) {
		var sb strings.Builder
		sb.WriteString(reflect.TypeOf(n).String())
		sb.WriteString("@")
		sb.WriteString(strconv.Itoa(n.Pos().Line))
		sb.WriteString(":")
		sb.WriteString(strconv.Itoa(n.Pos().Column))
		for _, e := range prog.Sem.AnnotationsOf(n) {
			sb.WriteString("|")
			sb.WriteString(e.Key)
			sb.WriteString("=")
			if e.Value != nil {
				sb.WriteString(e.Value.String())
			}
			sb.WriteString("@")
			sb.WriteString(strconv.Itoa(e.Pos().Line))
			sb.WriteString(":")
			sb.WriteString(strconv.Itoa(e.Pos().Column))
		}
		out = append(out, sb.String())
	})
	return out
}

// countNodeOccurrences counts how many node OCCURRENCES the program contains.
//
// It deliberately does NOT call walkProgramNodes: the non-vacuity check must
// stay independent of the function under test, otherwise a deduplicating
// walkProgramNodes makes occurrences == unique, the guard fires, and the test
// blames the fixture instead of the regression it is meant to report.
func countNodeOccurrences(prog *Program) int {
	n := 0
	onPath := make(map[Node]bool)
	var rec func(v reflect.Value)
	rec = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Invalid:
			return
		case reflect.Interface:
			if !v.IsNil() {
				rec(v.Elem())
			}
		case reflect.Ptr:
			if v.IsNil() {
				return
			}
			if v.CanInterface() {
				if nd, ok := v.Interface().(Node); ok {
					if onPath[nd] {
						return
					}
					onPath[nd] = true
					n++
					rec(v.Elem())
					delete(onPath, nd)
					return
				}
			}
			rec(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				rec(v.Index(i))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if f := v.Field(i); f.CanInterface() {
					rec(f)
				}
			}
		}
	}
	for _, s := range prog.Statements {
		rec(reflect.ValueOf(s))
	}
	return n
}

// countUniqueNodes counts distinct node POINTERS reachable from the program,
// mirroring the deduplicating walk the fix replaced. A difference between the
// two counts is exactly the aliasing that used to shift the index space.
func countUniqueNodes(prog *Program) int {
	seen := make(map[Node]bool)
	var rec func(v reflect.Value)
	rec = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Invalid:
			return
		case reflect.Interface:
			if !v.IsNil() {
				rec(v.Elem())
			}
		case reflect.Ptr:
			if v.IsNil() {
				return
			}
			if v.CanInterface() {
				if n, ok := v.Interface().(Node); ok {
					seen[n] = true
				}
			}
			rec(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				rec(v.Index(i))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if f := v.Field(i); f.CanInterface() {
					rec(f)
				}
			}
		}
	}
	for _, s := range prog.Statements {
		rec(reflect.ValueOf(s))
	}
	return len(seen)
}

// TestProgramGobRoundTrip is the feasibility gate for the incremental-build AST
// cache (Tier 0 baked std AST + Tier 1 on-disk user-module cache).
//
// It proves that a fully parsed *parser.Program — including the pointer-keyed
// nodeSem side-table and per-node annotations — can survive a gob encode/decode
// round-trip without data loss, so the syntactic AST can be cached instead of
// re-lexed + re-parsed on every build.
func TestProgramGobRoundTrip(t *testing.T) {
	src := `
#{derive}
struct Point {
  x f64
  y f64
}

#{max=10}
func add (a i64) i64 {
  let s = a + 1
  if s > 5: {
    s = s * 2
  } else: {
    s = 0
  }
  return s
}

func make-point (x f64) Point {
  return Point { x: x, y: 0.0 }
}

func main () i64 {
  let p = make-point(3.0)
  let v = p.x
  let arr = [1, 2, 3]
  let m = { "a": 1 }
  for i in arr: {
    let _ = i
  }
  match v: {
    _ -> return 0
  }
  return add(2)
}
`
	l := lexer.NewCached("spike.no", src)
	p := New(l)
	orig := p.ParseProgram()
	if orig == nil {
		t.Fatal("ParseProgram returned nil")
	}

	// 1) encode original via the cache wrapper (nodeSem flattened to uint32 keys)
	cp := &CachedProgram{Prog: orig, NodeSem: SerializeNodeSem(orig)}
	var b1 bytes.Buffer
	if err := gob.NewEncoder(&b1).Encode(cp); err != nil {
		t.Fatalf("gob encode original failed: %v", err)
	}

	// 2) decode
	var dcp CachedProgram
	if err := gob.NewDecoder(&b1).Decode(&dcp); err != nil {
		t.Fatalf("gob decode failed: %v", err)
	}
	RestoreNodeSem(dcp.Prog, dcp.NodeSem)

	// 3) re-encode decoded — proves the decoded structure is fully gob-encodable
	// (no leaked unregistered dynamic type / unexported field surfaced only on
	// the second pass).
	var b2 bytes.Buffer
	if err := gob.NewEncoder(&b2).Encode(&dcp); err != nil {
		t.Fatalf("gob re-encode decoded failed: %v", err)
	}

	// 4) run ResolveProgram on both (populates the nodeSem side-table from raw
	// annotations) and compare.
	ResolveProgram(orig)
	ResolveProgram(dcp.Prog)

	if len(orig.Statements) != len(dcp.Prog.Statements) {
		t.Fatalf("statement count mismatch: orig=%d dec=%d", len(orig.Statements), len(dcp.Prog.Statements))
	}

	origFP := annotationFingerprint(orig)
	decFP := annotationFingerprint(dcp.Prog)
	if origFP != decFP {
		t.Fatalf("annotation fingerprint mismatch:\n orig=%q\n dec =%q", origFP, decFP)
	}

	// 5) deterministic name-keyed semantic maps must match exactly.
	if !mapStrEqual(orig.Sem.VarTypes, dcp.Prog.Sem.VarTypes) {
		t.Fatalf("Sem.VarTypes mismatch:\n orig=%v\n dec =%v", orig.Sem.VarTypes, dcp.Prog.Sem.VarTypes)
	}
	if !mapSliceEqual(orig.Sem.EnumVariants, dcp.Prog.Sem.EnumVariants) {
		t.Fatalf("Sem.EnumVariants mismatch")
	}

	t.Logf("gob round-trip OK: orig bytes=%d, dec bytes=%d, statements=%d, annotations=%q",
		b1.Len(), b2.Len(), len(orig.Statements), origFP)
}

// annotationFingerprint concatenates, for every statement, the annotations
// attached to it (key + value string) so two programs with identical annotation
// side-tables produce identical fingerprints.
func annotationFingerprint(prog *Program) string {
	var sb strings.Builder
	for _, s := range prog.Statements {
		for _, e := range prog.Sem.AnnotationsOf(s) {
			sb.WriteString(e.Key)
			sb.WriteByte('=')
			if e.Value != nil {
				sb.WriteString(e.Value.String())
			}
			sb.WriteByte(';')
		}
	}
	return sb.String()
}

func mapStrEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func mapSliceEqual(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok || len(bv) != len(v) {
			return false
		}
		for i := range v {
			if bv[i] != v[i] {
				return false
			}
		}
	}
	return true
}
