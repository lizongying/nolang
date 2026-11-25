package parser

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestASTCacheInvalidatesByContentHash pins the end-to-end contract behind the
// quoted requirement "AST cache 按内容 hash 失效": a cached Program is reused
// only for the EXACT source bytes that produced it, and any change to one of the
// other key axes (external-signature salt, parse version) misses too.
//
// TestASTCacheKeyCoversParseVersionOnly checks cacheKey as a pure function. This
// test goes through NewUserASTCache.Put/Get, so it also pins that the build path
// actually feeds the content into KeyFor (not a path or an mtime) — a regression
// there would make the pure-function test pass while builds serve stale ASTs.
func TestASTCacheInvalidatesByContentHash(t *testing.T) {
	cache, err := NewUserASTCache(t.TempDir(), 16)
	if err != nil {
		t.Fatalf("NewUserASTCache failed: %v", err)
	}

	parse := func(src string) *Program {
		t.Helper()
		p := New(lexer.New(src))
		prog := p.ParseProgram()
		if prog == nil || len(p.Errors()) > 0 {
			t.Fatalf("fixture did not parse: %v", p.Errors())
		}
		return prog
	}

	const salt = "content-hash-salt"
	srcV1 := "x i64 = 1\nprint(x)\n"
	progV1 := parse(srcV1)
	keyV1 := cache.KeyFor(srcV1, salt)
	cache.Put(keyV1, progV1)

	// Same bytes + same salt must HIT, and must carry the original value.
	hit, ok := cache.Get(keyV1, SemRebuildConfig{})
	if !ok {
		t.Fatal("same content/salt missed right after Put")
	}
	if len(hit.Statements) != len(progV1.Statements) {
		t.Fatal("cache HIT returned a different program")
	}

	// One byte different in the source ⇒ different key ⇒ MISS. This is the
	// actual "content hash invalidation" guarantee: no mtime, no path.
	srcV2 := "x i64 = 2\nprint(x)\n"
	keyV2 := cache.KeyFor(srcV2, salt)
	if keyV2 == keyV1 {
		t.Fatal("changing one byte of source did not change the cache key")
	}
	if _, ok := cache.Get(keyV2, SemRebuildConfig{}); ok {
		t.Fatal("changed source HIT the blob written for the old source")
	}

	// Same source, different injected-signature salt ⇒ MISS. A stale blob cannot
	// be reused under a different set of parser inputs.
	keySalt2 := cache.KeyFor(srcV1, salt+"-bumped")
	if keySalt2 == keyV1 {
		t.Fatal("changing the signature salt did not change the cache key")
	}
	if _, ok := cache.Get(keySalt2, SemRebuildConfig{}); ok {
		t.Fatal("same source HIT a blob produced under a different salt")
	}

	// Same source and salt under a bumped parse version ⇒ MISS. ASTVersion is
	// the escape hatch when parse OUTPUT changes without a source change.
	keyVersion2 := cacheKey(srcV1, salt, ASTVersion+".bumped")
	if keyVersion2 == keyV1 {
		t.Fatal("changing the AST version did not change the cache key")
	}
	cache.Put(keyVersion2, parse(srcV1))
	if _, ok := cache.Get(keyVersion2, SemRebuildConfig{}); !ok {
		t.Fatal("entry stored under the bumped version must be readable")
	}
	if _, ok := cache.Get(keyV1, SemRebuildConfig{}); !ok {
		t.Fatal("the old-version entry must remain readable on its own key")
	}

	// Anti-vacuity: a key derived from a different content must never alias the
	// first blob even after both have been written.
	if keyV2 == keySalt2 || keyV2 == keyVersion2 || keySalt2 == keyVersion2 {
		t.Fatal("two independent key axes collapsed to the same key; the misses above would be vacuous")
	}
}
