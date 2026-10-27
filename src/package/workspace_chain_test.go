package pkg

import (
	"os"
	"sort"
	"path/filepath"
	"strings"
	"testing"
)

// createTempWorkspace creates a temporary workspace directory structure for testing.
// Returns the root temp dir (cleanup is caller's responsibility).
//
// Structure:
//
//	root/
//	  workspace.noml        — maps "testkey" → "/pkgA"
//	  pkgA/
//	    workspace.noml      — maps "testkey" → "/pkgB"
//	    pkgB/
//	      main.no            — dummy file
//
// If cycleDirs is non-empty, creates a cycle: pkgB/workspace.noml maps back to pkgA.
func createTempWorkspace(t *testing.T, cycle bool) string {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Root workspace.noml: "testkey" → "/pkgA" (workspace-relative)
	writeWorkspaceNoml(t, root, map[string]string{
		"testkey": "/pkgA",
	})

	// pkgA directory
	pkgA := filepath.Join(root, "pkgA")
	if err := os.MkdirAll(pkgA, 0755); err != nil {
		t.Fatal(err)
	}

	// pkgA/workspace.noml: "testkey" → "/pkgB" (workspace-relative to pkgA)
	pkgB := filepath.Join(pkgA, "pkgB")
	if err := os.MkdirAll(pkgB, 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceNoml(t, pkgA, map[string]string{
		"testkey": "/pkgB",
	})

	// Create a dummy file in pkgB so it's a valid directory
	if err := os.WriteFile(filepath.Join(pkgB, "main.no"), []byte("// pkgB\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if cycle {
		// pkgB/workspace.noml: "testkey" → OS absolute path to pkgA (cycle back)
		writeWorkspaceNoml(t, pkgB, map[string]string{
			"testkey": pkgA,
		})
	} else {
		// pkgC directory (final destination, no workspace.noml)
		pkgC := filepath.Join(pkgB, "pkgC")
		if err := os.MkdirAll(pkgC, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkgC, "main.no"), []byte("// pkgC\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// pkgB/workspace.noml maps to pkgC (workspace-relative to pkgB)
		writeWorkspaceNoml(t, pkgB, map[string]string{
			"testkey": "/pkgC",
		})
	}

	return root
}

func writeWorkspaceNoml(t *testing.T, dir string, m map[string]string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("{\n")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString("  ")
		if IsNomlIdentifierKey(k) {
			sb.WriteString(k)
		} else {
			sb.WriteString(NomlQuote(k))
		}
		sb.WriteString(": ")
		sb.WriteString(NomlQuote(m[k]))
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, WorkspaceFileName), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveWorkspaceChain_SingleLevel tests basic single-level workspace lookup.
func TestResolveWorkspaceChain_SingleLevel(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	pkgDir := filepath.Join(root, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceNoml(t, root, map[string]string{
		"testkey": "/mypkg",
	})

	// Single-level lookup
	dir, found, err := resolveWorkspaceChain("testkey", root, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected to find testkey")
	}
	expected, _ := filepath.Abs(filepath.Join(root, "mypkg"))
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}
}

// TestResolveWorkspaceChain_Recursive tests recursive resolution through nested workspaces.
//
//	root/workspace.noml:        "testkey" → "/pkgA"
//	pkgA/workspace.noml:        "testkey" → "/pkgB"
//	pkgB/workspace.noml:        "testkey" → "/pkgC"
//	pkgC has no workspace.noml  → final resolution
//
// Expected: resolveWorkspaceChain("testkey", root) → pkgC directory
func TestResolveWorkspaceChain_Recursive(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create directory structure
	pkgA := filepath.Join(root, "pkgA")
	pkgB := filepath.Join(pkgA, "pkgB")
	pkgC := filepath.Join(pkgB, "pkgC")
	for _, dir := range []string{pkgA, pkgB, pkgC} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Root workspace.noml
	writeWorkspaceNoml(t, root, map[string]string{
		"testkey": "/pkgA",
	})
	// pkgA/workspace.noml
	writeWorkspaceNoml(t, pkgA, map[string]string{
		"testkey": "/pkgB",
	})
	// pkgB/workspace.noml
	writeWorkspaceNoml(t, pkgB, map[string]string{
		"testkey": "/pkgC",
	})

	// Resolve: should follow chain root → pkgA → pkgB → pkgC
	dir, found, err := resolveWorkspaceChain("testkey", root, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected to find testkey through recursive resolution")
	}
	expected, _ := filepath.Abs(pkgC)
	if dir != expected {
		t.Errorf("dir = %q, want %q (pkgC)", dir, expected)
	}
}

// TestResolveWorkspaceChain_CycleDetection tests that circular mappings are detected.
//
//	root/workspace.noml:        "testkey" → "/pkgA"
//	pkgA/workspace.noml:        "testkey" → "/pkgB"
//	pkgB/workspace.noml:        "testkey" → OS absolute path to pkgA (cycle)
//
// Expected: error with "circular workspace mapping detected" and full chain in message
func TestResolveWorkspaceChain_CycleDetection(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create directory structure
	pkgA := filepath.Join(root, "pkgA")
	pkgB := filepath.Join(pkgA, "pkgB")
	for _, dir := range []string{pkgA, pkgB} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Root workspace.noml
	writeWorkspaceNoml(t, root, map[string]string{
		"testkey": "/pkgA",
	})
	// pkgA/workspace.noml
	writeWorkspaceNoml(t, pkgA, map[string]string{
		"testkey": "/pkgB",
	})
	// pkgB/workspace.noml: cycle back to pkgA via OS absolute path
	writeWorkspaceNoml(t, pkgB, map[string]string{
		"testkey": pkgA,
	})

	// Resolve: should detect cycle and return error
	_, _, err := resolveWorkspaceChain("testkey", root, nil, nil)
	if err == nil {
		t.Fatal("expected circular mapping error, got nil")
	}
	if !strings.Contains(err.Error(), "circular workspace mapping detected") {
		t.Errorf("error should mention 'circular workspace mapping detected', got: %v", err)
	}
	// Error message should contain the chain for debugging
	if !strings.Contains(err.Error(), "→") {
		t.Errorf("error should contain the chain (→), got: %v", err)
	}
	t.Logf("cycle error: %v", err)
}

// TestResolveWorkspaceChain_NoWorkspaceAtTarget tests that resolution stops
// when the target directory has no workspace.noml.
func TestResolveWorkspaceChain_NoWorkspaceAtTarget(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	pkgDir := filepath.Join(root, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// No workspace.noml in pkgDir

	writeWorkspaceNoml(t, root, map[string]string{
		"testkey": "/mypkg",
	})

	dir, found, err := resolveWorkspaceChain("testkey", root, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected to find testkey")
	}
	expected, _ := filepath.Abs(pkgDir)
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}
}

// TestResolveWorkspaceChain_KeyNotFound tests that a missing key returns not found.
func TestResolveWorkspaceChain_KeyNotFound(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	writeWorkspaceNoml(t, root, map[string]string{
		"otherkey": "/otherpkg",
	})

	_, found, err := resolveWorkspaceChain("nonexistent", root, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected not found for nonexistent key")
	}
}

// TestResolveWorkspaceChain_PreloadedMap tests that a pre-loaded workspace map
// is used for the first level (optimization).
func TestResolveWorkspaceChain_PreloadedMap(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	pkgDir := filepath.Join(root, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceNoml(t, root, map[string]string{
		"testkey": "/mypkg",
	})

	// Pre-load the workspace map
	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("loadWorkspaceMap error: %v", err)
	}

	// Pass pre-loaded map
	dir, found, err := resolveWorkspaceChain("testkey", root, ws, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected to find testkey")
	}
	expected, _ := filepath.Abs(pkgDir)
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}
}

// TestResolveWorkspaceChain_DeepChain tests a 4-level deep resolution chain.
func TestResolveWorkspaceChain_DeepChain(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create 4-level chain: root → L1 → L2 → L3 (final, no workspace)
	levels := make([]string, 4)
	levels[0] = root
	for i := 1; i <= 3; i++ {
		levels[i] = filepath.Join(levels[i-1], "L"+string(rune('0'+i)))
		if err := os.MkdirAll(levels[i], 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Each level (except the last) has a workspace.noml mapping to the next
	for i := 0; i < 3; i++ {
		writeWorkspaceNoml(t, levels[i], map[string]string{
			"deepkey": "/L" + string(rune('0'+i+1)),
		})
	}

	// Resolve: should follow chain root → L1 → L2 → L3
	dir, found, err := resolveWorkspaceChain("deepkey", root, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected to find deepkey through recursive resolution")
	}
	expected, _ := filepath.Abs(levels[3])
	if dir != expected {
		t.Errorf("dir = %q, want %q (L3)", dir, expected)
	}
}
