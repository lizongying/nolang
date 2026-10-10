package parser

import (
	"bytes"
	"encoding/gob"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

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
