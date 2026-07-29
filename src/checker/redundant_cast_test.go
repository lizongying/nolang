package checker

import (
	"testing"
)

// TestGetModuleExportsCached verifies the per-module export cache returns
// identical results on repeated calls (correctness + cache works).
func TestGetModuleExportsCached(t *testing.T) {
	names := []string{"hash", "str"}
	first := GetModuleExports(names)
	second := GetModuleExports(names)
	if len(first) == 0 {
		t.Skip("no std modules available in this environment")
	}
	if len(first) != len(second) {
		t.Fatalf("cache returned different lengths: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("cache mismatch at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
}

// TestCollectStdModuleSignaturesCached verifies the signature cache returns
// identical results on repeated calls.
func TestCollectStdModuleSignaturesCached(t *testing.T) {
	sigs1, fields1 := CollectStdModuleSignatures()
	sigs2, fields2 := CollectStdModuleSignatures()
	if len(sigs1) == 0 {
		t.Skip("no std module signatures available")
	}
	if len(sigs1) != len(sigs2) {
		t.Fatalf("sigs cache mismatch: %d vs %d", len(sigs1), len(sigs2))
	}
	if len(fields1) != len(fields2) {
		t.Fatalf("fields cache mismatch: %d vs %d", len(fields1), len(fields2))
	}
}
