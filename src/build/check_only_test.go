package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckOnlyWritesNoArtifacts pins the contract of `no build --check`: the
// full compile pipeline runs (so codegen errors still surface), but the output
// directory, binary, C header and JS/WASM sidecars are never created.
//
// The test uses a temporary package directory and an explicit -o path in a
// fresh subdirectory, so any write the check path forgot lands somewhere the
// assertions can see it.
func TestCheckOnlyWritesNoArtifacts(t *testing.T) {
	skipWithoutStdSigKey(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "main.no")
	if err := os.WriteFile(src, []byte("x i64 = 1\nprint(x)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// -o points at a subdirectory that does NOT exist. A normal build would
	// create it; --check must not.
	out := filepath.Join(dir, "would-be", "app")

	outPath, err := BuildFile(src, BuildOptions{CC: "clang", Output: out, CheckOnly: true})
	if err != nil {
		t.Fatalf("--check on a valid source failed: %v", err)
	}
	if outPath != "" {
		t.Fatalf("--check returned an artifact path %q; must be empty", outPath)
	}
	if _, err := os.Stat(filepath.Dir(out)); !os.IsNotExist(err) {
		t.Fatalf("--check created the output directory %s", filepath.Dir(out))
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("--check wrote the output binary %s", out)
	}
}

// TestCheckOnlyStillReportsCompileErrors is the anti-vacuity counterpart: a
// check that silently skipped compilation would pass every source. Here the
// codegen/checker must still reject a bad program with a diagnostic.
func TestCheckOnlyStillReportsCompileErrors(t *testing.T) {
	skipWithoutStdSigKey(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "main.no")
	if err := os.WriteFile(src, []byte("x i64 = \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := BuildFile(src, BuildOptions{CC: "clang", CheckOnly: true})
	if err == nil {
		t.Fatal("--check accepted a source with a syntax error; compilation was skipped")
	}
	if !strings.Contains(err.Error(), "not defined") && !strings.Contains(err.Error(), "expected") {
		t.Fatalf("--check error lost the diagnostic: %v", err)
	}
}

// TestCheckOnlyJSWritesNoArtifacts covers the JS backend path, where codegen
// produces a string rather than IR but the write guard is a different branch.
func TestCheckOnlyJSWritesNoArtifacts(t *testing.T) {
	skipWithoutStdSigKey(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "main.no")
	if err := os.WriteFile(src, []byte("x i64 = 1\nprint(x)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "would-be", "app.js")

	outPath, err := buildJSBackend(src, BuildOptions{CheckOnly: true, Output: out})
	if err != nil {
		t.Fatalf("--check --js on a valid source failed: %v", err)
	}
	if outPath != "" {
		t.Fatalf("--check --js returned an artifact path %q; must be empty", outPath)
	}
	if _, err := os.Stat(filepath.Dir(out)); !os.IsNotExist(err) {
		t.Fatalf("--check --js created the output directory %s", filepath.Dir(out))
	}
}
