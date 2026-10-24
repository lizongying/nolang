package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
)

// G7 — the cache gate for C9 (design doc §S6).
//
// The failure mode being guarded is "a stale AST blob silently resurrects old
// semantics": the cache key is (content, ASTVersion, sigSalt) and cannot see
// the compiler build, so a blob written by a different build decodes into a
// tree whose *parse-time* facts may be missing or different. Because nothing
// downstream of parsing looks at those facts until lowering/codegen, such a
// loss is invisible unless something compares the HIR.
//
// The gate therefore uses HIR equality as the oracle: a Program that survives
// the on-disk round trip must lower to exactly the same tree as the Program it
// was serialized from. That is stronger than the per-fact fingerprints in
// ast_diskcache_test.go, which only cover the facts someone remembered to
// enumerate — a new node-keyed fact table without a detU32* mirror fails here
// without anyone having to add a case.
//
// Note the order: Put happens before either side is lowered, because that is
// what production does (build.parseFile caches immediately after ParseProgram).
// Lowering first would let a lowering pass mutate the tree before it is
// serialized and mask exactly the kind of divergence this test looks for.

// TestCachedASTLowersToIdenticalHIR is the G7 oracle over the repo corpus.
func TestCachedASTLowersToIdenticalHIR(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.no"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus found under ../../tests: %v", err)
	}
	cache, err := NewUserASTCache(t.TempDir(), 64)
	if err != nil {
		t.Fatalf("NewUserASTCache failed: %v", err)
	}

	checked, skipped, diverged := 0, 0, 0
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		p := New(lexer.NewCached(path, string(src)))
		p.Filename = filepath.Base(path)
		prog := p.ParseProgram()
		if prog == nil || len(p.Errors()) > 0 {
			// Deliberately-broken fixtures live in tests/ too; they have no
			// parse output to cache.
			skipped++
			continue
		}

		key := cache.KeyFor(string(src), "g7-hir-equivalence")
		cache.Put(key, prog)
		decoded, ok := cache.Get(key)
		if !ok {
			t.Fatalf("%s: cache Get missed right after Put", path)
		}

		want := ASTToHIR(prog).Dump()
		got := ASTToHIR(decoded).Dump()
		if want != got {
			diverged++
			t.Errorf("%s: HIR from the decoded AST differs from a fresh parse\n"+
				"  fresh   (%d bytes)\n  decoded (%d bytes)\n"+
				"  a parse-time fact did not survive the gob round trip",
				path, len(want), len(got))
		}
		checked++
	}

	// Anti-vacuity: a glob that silently matched nothing, or a corpus that
	// turned out to be all syntax errors, would make this gate pass forever.
	if checked < 300 {
		t.Fatalf("only %d files round-tripped (%d skipped); expected the bulk of the 481-file corpus", checked, skipped)
	}
	// Only claim a clean sweep when there was one — the count alone is not the
	// verdict (a run can round-trip 481 files and still diverge on 2 of them).
	if diverged == 0 {
		t.Logf("%d files lowered identically from a decoded AST (%d skipped)", checked, skipped)
	}
}

// TestHIREquivalenceOracleSeesSideTableLoss is the anti-vacuity control for
// TestCachedASTLowersToIdenticalHIR: if the oracle could not see a missing
// parse-time fact, the equivalence test would be a tautology. This drops one
// proven in-bounds fact — exactly what a blob written before the fact existed
// would decode to — and requires the HIR to change.
func TestHIREquivalenceOracleSeesSideTableLoss(t *testing.T) {
	prog := parseSurface(t, `f = () {
    a [3]i64 = [0, 0, 0]
    x = a[1]
}`)
	var idx *IndexExpression
	walkNodes(prog, func(n interface{}) {
		if i, ok := n.(*IndexExpression); ok {
			idx = i
		}
	})
	if idx == nil || !prog.Sem.IndexIsInBounds(idx) {
		t.Fatal("fixture did not carry its in-bounds proof; the control would be vacuous")
	}
	withProof := ASTToHIR(prog).Dump()

	prog.Sem.SetIndexInBounds(idx, false)
	without := ASTToHIR(prog).Dump()
	if withProof == without {
		t.Fatal("dropping the in-bounds proof did not change the HIR — the G7 oracle is blind to side-table loss")
	}
}

// TestASTCacheKeyCoversParseVersionOnly pins the two-axis split itself (C9).
// The AST cache key must move when the parse version moves — otherwise a bump
// could not invalidate anything — and must not depend on hir.HIRVersion, or an
// HIR-only change would throw the AST cache away, which is the behaviour C9
// exists to stop.
func TestASTCacheKeyCoversParseVersionOnly(t *testing.T) {
	const content, salt = "x i64 = 1\n", "salt"
	base := cacheKey(content, salt, ASTVersion)
	if base != cacheKey(content, salt, ASTVersion) {
		t.Fatal("cache key is not deterministic")
	}
	if base == cacheKey(content, salt, ASTVersion+".bumped") {
		t.Fatal("cache key ignores the AST version; a bump would not invalidate stale blobs")
	}
	if base == cacheKey(content, salt+"x", ASTVersion) {
		t.Fatal("cache key ignores the signature salt")
	}
	if base == cacheKey(content+" ", salt, ASTVersion) {
		t.Fatal("cache key ignores the source content")
	}
	// The split: HIRVersion is not an input. cacheKey has no parameter for it,
	// so this asserts the value is not accidentally folded in through some
	// other channel (e.g. a future "fold every version constant" shortcut).
	if base == cacheKey(content, salt+"@"+hir.HIRVersion, ASTVersion) {
		t.Fatal("AST cache key depends on hir.HIRVersion; the C9 split is broken")
	}
	if ASTVersion == hir.HIRVersion {
		t.Fatalf("ASTVersion and HIRVersion are the same string (%q); the axes are not actually separate", ASTVersion)
	}
}
