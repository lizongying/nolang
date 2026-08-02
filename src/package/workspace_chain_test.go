package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTempWorkspace creates a temporary workspace directory structure for testing.
// Returns the root temp dir (cleanup is caller's responsibility).
//
// Structure:
//   root/
//     workspace.jsonc        — maps "testkey" → "/pkgA"
//     pkgA/
//       workspace.jsonc      — maps "testkey" → "/pkgB"
//       pkgB/
//         main.no            — dummy file
//
// If cycleDirs is non-empty, creates a cycle: pkgB/workspace.jsonc maps back to pkgA.
func createTempWorkspace(t *testing.T, cycle bool) string {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Root workspace.jsonc: "testkey" → "/pkgA" (workspace-relative)
	writeWorkspaceJSONC(t, root, map[string]string{
		"testkey": "/pkgA",
	})

	// pkgA directory
	pkgA := filepath.Join(root, "pkgA")
	if err := os.MkdirAll(pkgA, 0755); err != nil {
		t.Fatal(err)
	}

	// pkgA/workspace.jsonc: "testkey" → "/pkgB" (workspace-relative to pkgA)
	pkgB := filepath.Join(pkgA, "pkgB")
	if err := os.MkdirAll(pkgB, 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceJSONC(t, pkgA, map[string]string{
		"testkey": "/pkgB",
	})

	// Create a dummy file in pkgB so it's a valid directory
	if err := os.WriteFile(filepath.Join(pkgB, "main.no"), []byte("// pkgB\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if cycle {
		// pkgB/workspace.jsonc: "testkey" → OS absolute path to pkgA (cycle back)
		writeWorkspaceJSONC(t, pkgB, map[string]string{
			"testkey": pkgA,
		})
	} else {
		// pkgC directory (final destination, no workspace.jsonc)
		pkgC := filepath.Join(pkgB, "pkgC")
		if err := os.MkdirAll(pkgC, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkgC, "main.no"), []byte("// pkgC\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// pkgB/workspace.jsonc maps to pkgC (workspace-relative to pkgB)
		writeWorkspaceJSONC(t, pkgB, map[string]string{
			"testkey": "/pkgC",
		})
	}

	return root
}

func writeWorkspaceJSONC(t *testing.T, dir string, m map[string]string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("{\n")
	first := true
	for k, v := range m {
		if !first {
			sb.WriteString(",\n")
		}
		sb.WriteString("  \"")
		sb.WriteString(k)
		sb.WriteString("\": \"")
		sb.WriteString(v)
		sb.WriteString("\"")
		first = false
	}
	sb.WriteString("\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "workspace.jsonc"), []byte(sb.String()), 0644); err != nil {
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
	writeWorkspaceJSONC(t, root, map[string]string{
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
//	root/workspace.jsonc:        "testkey" → "/pkgA"
//	pkgA/workspace.jsonc:        "testkey" → "/pkgB"
//	pkgB/workspace.jsonc:        "testkey" → "/pkgC"
//	pkgC has no workspace.jsonc  → final resolution
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

	// Root workspace.jsonc
	writeWorkspaceJSONC(t, root, map[string]string{
		"testkey": "/pkgA",
	})
	// pkgA/workspace.jsonc
	writeWorkspaceJSONC(t, pkgA, map[string]string{
		"testkey": "/pkgB",
	})
	// pkgB/workspace.jsonc
	writeWorkspaceJSONC(t, pkgB, map[string]string{
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
//	root/workspace.jsonc:        "testkey" → "/pkgA"
//	pkgA/workspace.jsonc:        "testkey" → "/pkgB"
//	pkgB/workspace.jsonc:        "testkey" → OS absolute path to pkgA (cycle)
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

	// Root workspace.jsonc
	writeWorkspaceJSONC(t, root, map[string]string{
		"testkey": "/pkgA",
	})
	// pkgA/workspace.jsonc
	writeWorkspaceJSONC(t, pkgA, map[string]string{
		"testkey": "/pkgB",
	})
	// pkgB/workspace.jsonc: cycle back to pkgA via OS absolute path
	writeWorkspaceJSONC(t, pkgB, map[string]string{
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
// when the target directory has no workspace.jsonc.
func TestResolveWorkspaceChain_NoWorkspaceAtTarget(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	pkgDir := filepath.Join(root, "mypkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	// No workspace.jsonc in pkgDir

	writeWorkspaceJSONC(t, root, map[string]string{
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
	writeWorkspaceJSONC(t, root, map[string]string{
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
	writeWorkspaceJSONC(t, root, map[string]string{
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

	// Each level (except the last) has a workspace.jsonc mapping to the next
	for i := 0; i < 3; i++ {
		writeWorkspaceJSONC(t, levels[i], map[string]string{
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
