package checker

import (
	"os"
	"reflect"
	"testing"
)

// TestStdSigCacheLifecycle verifies the whole Stage-1 cache path:
//  1. a cold collection (disk bypassed) yields the real signature tables,
//  2. the exact payload we persist survives gob encode/decode byte-for-byte,
//  3. corrupt input is rejected,
//  4. the file path (atomic write + load) round-trips correctly.
func TestStdSigCacheLifecycle(t *testing.T) {
	t.Setenv("NOLANG_NOCACHE_STD", "1") // force cold collection, no disk side effects

	funcSigs, fields := CollectStdModuleSignatures()
	aliases := CollectStdConcreteAliases()
	structMod := CollectStdStructModules()
	methodSigs := CollectStdMethodSigs()
	enumVariants := CollectStdEnumVariants()
	if len(funcSigs) == 0 || len(fields) == 0 {
		t.Fatal("collected signatures are empty")
	}
	tokens := gatherStdTokens()
	if len(tokens) == 0 {
		t.Fatal("gathered std tokens are empty (token cache not warmed)")
	}

	// (2) in-memory encode/decode preserves data exactly
	payload := &stdSigCachePayload{
		FuncSigs:     funcSigs,
		MethodSigs:   methodSigs,
		Fields:       fields,
		Aliases:      aliases,
		StructMod:    structMod,
		EnumVariants: enumVariants,
		Tokens:       tokens,
	}
	data, err := encodeStdSigCache(payload)
	if err != nil {
		t.Fatalf("encodeStdSigCache: %v", err)
	}
	dec, ok := decodeStdSigCache(data)
	if !ok {
		t.Fatal("decodeStdSigCache failed on valid payload")
	}
	if !reflect.DeepEqual(dec.FuncSigs, payload.FuncSigs) ||
		!reflect.DeepEqual(dec.MethodSigs, payload.MethodSigs) ||
		!reflect.DeepEqual(dec.Fields, payload.Fields) ||
		!reflect.DeepEqual(dec.Aliases, payload.Aliases) ||
		!reflect.DeepEqual(dec.StructMod, payload.StructMod) ||
		!reflect.DeepEqual(dec.EnumVariants, payload.EnumVariants) ||
		!reflect.DeepEqual(dec.Tokens, payload.Tokens) {
		t.Fatal("payload not preserved through encode/decode")
	}

	// (3) corrupt input is rejected
	if _, ok := decodeStdSigCache([]byte("this is not gob")); ok {
		t.Fatal("decodeStdSigCache should reject corrupt input")
	}

	// (4) file round-trip through the real cache path
	key, err := computeStdSigKey()
	if err != nil {
		t.Fatalf("computeStdSigKey: %v", err)
	}
	path, err := stdSigCachePath(key)
	if err != nil {
		t.Fatalf("stdSigCachePath: %v", err)
	}
	os.Remove(path)
	defer os.Remove(path)

	saveStdSigCache(funcSigs, methodSigs, fields, aliases, structMod, enumVariants, tokens)
	loaded, ok := tryLoadStdSigCache()
	if !ok {
		t.Fatal("tryLoadStdSigCache missed after save")
	}
	if !reflect.DeepEqual(loaded.FuncSigs, funcSigs) {
		t.Fatalf("file round-trip FuncSigs mismatch:\n got %d entries, want %d",
			len(loaded.FuncSigs), len(funcSigs))
	}
	if !reflect.DeepEqual(loaded.Fields, fields) ||
		!reflect.DeepEqual(loaded.MethodSigs, methodSigs) ||
		!reflect.DeepEqual(loaded.Aliases, aliases) ||
		!reflect.DeepEqual(loaded.StructMod, structMod) ||
		!reflect.DeepEqual(loaded.EnumVariants, enumVariants) ||
		!reflect.DeepEqual(loaded.Tokens, tokens) {
		t.Fatal("file round-trip mismatch on Fields/Aliases/StructMod/Tokens")
	}
}

// TestStdSigKeyDeterministic ensures the cache key is stable across calls so a
// warm cache is reliably hit.
func TestStdSigKeyDeterministic(t *testing.T) {
	k1, err := computeStdSigKey()
	if err != nil {
		t.Fatalf("computeStdSigKey: %v", err)
	}
	if k1 == "" {
		t.Fatal("empty cache key")
	}
	k2, err := computeStdSigKey()
	if err != nil {
		t.Fatalf("computeStdSigKey: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("cache key not deterministic: %s != %s", k1, k2)
	}
}
