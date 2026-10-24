package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestUserASTCacheRoundTrip is the verification gate for Tier 1 (on-disk
// content-addressed user-module AST cache). It proves that a parsed *Program
// survives a full gzip+gob file write/read round-trip through UserASTCache
// and that:
//   - two Gets return distinct (freshly decoded) instances, so the "parse fresh
//     per load" codegen semantics hold across builds;
//   - the three pointer-keyed node-keyed side tables survive
//     (annotation fingerprint identical to the in-memory original);
//   - the name-keyed semantic maps (VarTypes / EnumVariants, consumed during
//     lowering) survive intact.
func TestUserASTCacheRoundTrip(t *testing.T) {
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
	orig := New(l).ParseProgram()
	if orig == nil {
		t.Fatal("ParseProgram returned nil")
	}
	ResolveProgram(orig)

	cache, err := NewUserASTCache(t.TempDir(), 16)
	if err != nil {
		t.Fatalf("NewUserASTCache failed: %v", err)
	}
	key := cache.KeyFor(src, "salt-v1")
	cache.Put(key, orig)

	got1, ok := cache.Get(key)
	if !ok {
		t.Fatal("cache Get returned false on a key that was just Put")
	}
	got2, ok := cache.Get(key)
	if !ok {
		t.Fatal("second cache Get returned false")
	}
	if got1 == got2 {
		t.Fatal("two Gets returned the same pointer; expected fresh decode per load")
	}
	if got1 == orig {
		t.Fatal("Get returned the original in-memory instance; expected a fresh decode")
	}

	ResolveProgram(got1)
	ResolveProgram(got2)

	if len(orig.Statements) != len(got1.Statements) {
		t.Fatalf("statement count mismatch: orig=%d got=%d", len(orig.Statements), len(got1.Statements))
	}
	if fp := annotationFingerprint(orig); fp != annotationFingerprint(got1) {
		t.Fatalf("annotation fingerprint mismatch:\n orig=%q\n got =%q", fp, annotationFingerprint(got1))
	}
	if fp := annotationFingerprint(orig); fp != annotationFingerprint(got2) {
		t.Fatalf("annotation fingerprint mismatch (2nd get):\n orig=%q\n got =%q", fp, annotationFingerprint(got2))
	}
	if !mapStrEqual(orig.Sem.VarTypes, got1.Sem.VarTypes) {
		t.Fatalf("Sem.VarTypes mismatch:\n orig=%v\n got =%v", orig.Sem.VarTypes, got1.Sem.VarTypes)
	}
	if !mapSliceEqual(orig.Sem.EnumVariants, got1.Sem.EnumVariants) {
		t.Fatalf("Sem.EnumVariants mismatch")
	}

	// Missing key must miss, not panic.
	if _, ok := cache.Get("deadbeef"); ok {
		t.Fatal("Get on unknown key should miss")
	}
}

func TestUserASTCacheRoundTripPreservesInBoundsProof(t *testing.T) {
	src := `f = () {
    a [3]i64 = [0, 0, 0]
    x = a[1]
}`
	orig := parseSurface(t, src)
	var origIdx *IndexExpression
	walkNodes(orig, func(n interface{}) {
		if idx, ok := n.(*IndexExpression); ok {
			origIdx = idx
		}
	})
	if origIdx == nil || !orig.Sem.IndexIsInBounds(origIdx) {
		t.Fatal("fixture did not carry its in-bounds proof in Program.Sem")
	}

	cache, err := NewUserASTCache(t.TempDir(), 4)
	if err != nil {
		t.Fatalf("NewUserASTCache failed: %v", err)
	}
	key := cache.KeyFor(src, "inbounds-side-table")
	cache.Put(key, orig)
	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("cache Get returned false after Put")
	}

	var gotIdx *IndexExpression
	walkNodes(got, func(n interface{}) {
		if idx, ok := n.(*IndexExpression); ok {
			gotIdx = idx
		}
	})
	if gotIdx == nil || !got.Sem.IndexIsInBounds(gotIdx) {
		t.Fatal("cache round-trip lost the IndexExpression's side-table proof")
	}
}
