package cache

import "testing"

func TestLRU_BasicPutGet(t *testing.T) {
	c := NewLRU[int](4)
	if _, ok := c.Get("a"); ok {
		t.Fatal("empty cache should miss")
	}
	c.Put("a", 1)
	c.Put("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("expected a=1, got %v ok=%v", v, ok)
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("expected b=2, got %v ok=%v", v, ok)
	}
}

func TestLRU_EvictionAtCapacity(t *testing.T) {
	c := NewLRU[int](3)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	// touch "a" so "b" becomes least recently used
	c.Get("a")
	c.Put("d", 4) // should evict "b"
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("a should survive (recently used), got %v ok=%v", v, ok)
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("c should survive, got %v ok=%v", v, ok)
	}
	if v, ok := c.Get("d"); !ok || v != 4 {
		t.Fatalf("d should be present, got %v ok=%v", v, ok)
	}
}

func TestLRU_UpdateExisting(t *testing.T) {
	// capacity 1: updating an existing key must NOT consume an extra slot.
	c := NewLRU[int](1)
	c.Put("a", 1)
	c.Put("a", 9) // in-place update
	if v, ok := c.Get("a"); !ok || v != 9 {
		t.Fatalf("expected updated a=9, got %v ok=%v", v, ok)
	}
	// inserting a distinct key must evict the single slot (the old "a").
	c.Put("b", 3)
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be evicted once b is inserted (cap 1)")
	}
	if v, ok := c.Get("b"); !ok || v != 3 {
		t.Fatalf("b must be present with value 3, got %v ok=%v", v, ok)
	}
}

func TestLRU_Clear(t *testing.T) {
	c := NewLRU[int](2)
	c.Put("a", 1)
	c.Clear()
	if _, ok := c.Get("a"); ok {
		t.Fatal("Clear should empty the cache")
	}
}

func TestKey_ContentAddressed(t *testing.T) {
	// same path + same content => identical key (cache hit across builds)
	if k1, k2 := Key("/p/x.no", "abc"), Key("/p/x.no", "abc"); k1 != k2 {
		t.Fatalf("same content must yield same key: %q vs %q", k1, k2)
	}
	// same path + different content => different key (no stale hit)
	if k1, k2 := Key("/p/x.no", "abc"), Key("/p/x.no", "abcd"); k1 == k2 {
		t.Fatal("different content at same path must yield different keys")
	}
	// different paths + same content => different keys (no cross-file collision)
	if k1, k2 := Key("/p/x.no", "abc"), Key("/p/y.no", "abc"); k1 == k2 {
		t.Fatal("different paths must not collide even with identical content")
	}
}
