package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadWorkspaceMap_PublicOnly tests loading only workspace.jsonc (no .workspace.jsonc).
func TestLoadWorkspaceMap_PublicOnly(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create workspace.jsonc — paths use "/" prefix (workspace-relative)
	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{
  "foo": "/foo",
  "bar": "/bar"
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("loadWorkspaceMap error: %v", err)
	}
	if len(ws) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(ws))
	}
	if ws["foo"] != "/foo" {
		t.Errorf("ws[foo] = %q, want %q", ws["foo"], "/foo")
	}
	if ws["bar"] != "/bar" {
		t.Errorf("ws[bar] = %q, want %q", ws["bar"], "/bar")
	}
}

// TestLoadWorkspaceMap_PrivateOverride tests that .workspace.jsonc overrides workspace.jsonc.
func TestLoadWorkspaceMap_PrivateOverride(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Public config
	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{
  "foo": "/foo",
  "bar": "/bar"
}`)

	// Private config: override "foo", keep "bar" from public
	writeFile(t, filepath.Join(root, ".workspace.jsonc"), `{
  "foo": "/my-foo"
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("loadWorkspaceMap error: %v", err)
	}
	if len(ws) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(ws))
	}
	// foo should be overridden by private config
	if ws["foo"] != "/my-foo" {
		t.Errorf("ws[foo] = %q, want %q (private override)", ws["foo"], "/my-foo")
	}
	// bar should remain from public config
	if ws["bar"] != "/bar" {
		t.Errorf("ws[bar] = %q, want %q (from public)", ws["bar"], "/bar")
	}
}

// TestLoadWorkspaceMap_PrivateNewKey tests that .workspace.jsonc adds new keys.
func TestLoadWorkspaceMap_PrivateNewKey(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Public config
	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{
  "foo": "/foo"
}`)

	// Private config: add new key "baz"
	writeFile(t, filepath.Join(root, ".workspace.jsonc"), `{
  "baz": "/baz"
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("loadWorkspaceMap error: %v", err)
	}
	if len(ws) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(ws))
	}
	if ws["foo"] != "/foo" {
		t.Errorf("ws[foo] = %q, want %q", ws["foo"], "/foo")
	}
	if ws["baz"] != "/baz" {
		t.Errorf("ws[baz] = %q, want %q", ws["baz"], "/baz")
	}
}

// TestLoadWorkspaceMap_PrivateOnly tests that .workspace.jsonc alone (no workspace.jsonc) returns nil.
// This is because workspace.jsonc is the primary config; .workspace.jsonc is supplementary.
func TestLoadWorkspaceMap_PrivateOnly(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Only private config, no public workspace.jsonc
	writeFile(t, filepath.Join(root, ".workspace.jsonc"), `{
  "baz": "/baz"
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("loadWorkspaceMap error: %v", err)
	}
	// Without workspace.jsonc, loadWorkspaceMap returns nil (public config is required)
	if ws != nil {
		t.Fatalf("expected nil when no workspace.jsonc exists, got %v", ws)
	}
}

// TestLoadWorkspaceMap_BothEmpty tests that empty workspace.jsonc works with .workspace.jsonc.
func TestLoadWorkspaceMap_BothEmpty(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Empty public config
	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{}`)

	// Private config adds a key
	writeFile(t, filepath.Join(root, ".workspace.jsonc"), `{
  "override": "/override"
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("loadWorkspaceMap error: %v", err)
	}
	if len(ws) != 1 {
		t.Fatalf("expected 1 key, got %d", len(ws))
	}
	if ws["override"] != "/override" {
		t.Errorf("ws[override] = %q, want %q", ws["override"], "/override")
	}
}

// TestLoadWorkspaceMap_JSONCComments tests that JSONC comments work in both files.
func TestLoadWorkspaceMap_JSONCComments(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Public config with comments
	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{
  // Public mapping
  "foo": "/foo",
}`)

	// Private config with comments
	writeFile(t, filepath.Join(root, ".workspace.jsonc"), `{
  // Private override
  "foo": "/my-foo", // personal fork
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("loadWorkspaceMap error: %v", err)
	}
	if ws["foo"] != "/my-foo" {
		t.Errorf("ws[foo] = %q, want %q", ws["foo"], "/my-foo")
	}
}

// TestLoadWorkspaceMap_PrivateBadJSON tests that invalid .workspace.jsonc returns an error.
func TestLoadWorkspaceMap_PrivateBadJSON(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{"foo": "/foo"}`)
	writeFile(t, filepath.Join(root, ".workspace.jsonc"), `{invalid json`)

	_, err := loadWorkspaceMap(root)
	if err == nil {
		t.Fatal("expected error for invalid .workspace.jsonc, got nil")
	}
}

// --- 工作區邊界約束測試 ---

// TestLoadWorkspaceMap_AcceptRelativePath_Public tests that workspace.jsonc accepts "./" and "../" paths.
func TestLoadWorkspaceMap_AcceptRelativePath_Public(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{
  "foo": "./foo"
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("expected no error for ./ path in workspace.jsonc, got: %v", err)
	}
	if ws["foo"] != "./foo" {
		t.Errorf("ws[foo] = %q, want %q", ws["foo"], "./foo")
	}
}

// TestLoadWorkspaceMap_AcceptRelativePath_Private tests that .workspace.jsonc accepts "./" and "../" paths.
func TestLoadWorkspaceMap_AcceptRelativePath_Private(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{"foo": "/foo"}`)
	writeFile(t, filepath.Join(root, ".workspace.jsonc"), `{
  "bar": "../bar"
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("expected no error for ../ path in .workspace.jsonc, got: %v", err)
	}
	if ws["bar"] != "../bar" {
		t.Errorf("ws[bar] = %q, want %q", ws["bar"], "../bar")
	}
}

// TestLoadWorkspaceMap_AcceptOSAbsolutePath tests that OS absolute paths are allowed in workspace.jsonc.
func TestLoadWorkspaceMap_AcceptOSAbsolutePath(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create an external directory to simulate a fork
	externalDir := t.TempDir()
	externalDir, _ = filepath.EvalSymlinks(externalDir)

	writeFile(t, filepath.Join(root, "workspace.jsonc"), `{
  "github.com/user/repo/core": "`+externalDir+`"
}`)

	ws, err := loadWorkspaceMap(root)
	if err != nil {
		t.Fatalf("loadWorkspaceMap error: %v", err)
	}
	if ws["github.com/user/repo/core"] != externalDir {
		t.Errorf("expected OS absolute path %q, got %q", externalDir, ws["github.com/user/repo/core"])
	}
}

// TestResolveWorkspaceMapValue_RelativeDotSlash tests ./ relative path resolution.
func TestResolveWorkspaceMapValue_RelativeDotSlash(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create a directory inside workspace
	subDir := filepath.Join(root, "vendor", "lib")
	os.MkdirAll(subDir, 0755)

	dir, ok := resolveWorkspaceMapValue("./vendor/lib", root)
	if !ok {
		t.Fatal("expected to find ./vendor/lib as relative path")
	}
	if dir != subDir {
		t.Errorf("got %q, want %q", dir, subDir)
	}
}

// TestResolveWorkspaceMapValue_RelativeDotDotSlash tests ../ relative path resolution (can escape workspace).
func TestResolveWorkspaceMapValue_RelativeDotDotSlash(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create a sibling directory outside workspace
	parentDir := filepath.Dir(root)
	siblingDir := filepath.Join(parentDir, "sibling-fork")
	os.MkdirAll(siblingDir, 0755)
	defer os.RemoveAll(siblingDir)

	dir, ok := resolveWorkspaceMapValue("../sibling-fork", root)
	if !ok {
		t.Fatal("expected to find ../sibling-fork as relative path escaping workspace")
	}
	expected, _ := filepath.EvalSymlinks(siblingDir)
	if dir != expected {
		t.Errorf("got %q, want %q", dir, expected)
	}
}

// TestResolveWorkspaceMapValue_WorkspaceRelative tests workspace-relative path resolution.
func TestResolveWorkspaceMapValue_WorkspaceRelative(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create a directory inside workspace
	subDir := filepath.Join(root, "vendor", "lib")
	os.MkdirAll(subDir, 0755)

	dir, ok := resolveWorkspaceMapValue("/vendor/lib", root)
	if !ok {
		t.Fatal("expected to find /vendor/lib as workspace-relative")
	}
	if dir != subDir {
		t.Errorf("got %q, want %q", dir, subDir)
	}
}

// TestResolveWorkspaceMapValue_OSAbsolute tests OS absolute path resolution.
func TestResolveWorkspaceMapValue_OSAbsolute(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	// Create an external directory
	externalDir := t.TempDir()
	externalDir, _ = filepath.EvalSymlinks(externalDir)

	// Path that doesn't exist as workspace-relative, but exists as OS absolute
	dir, ok := resolveWorkspaceMapValue(externalDir, root)
	if !ok {
		t.Fatal("expected to find external dir as OS absolute path")
	}
	if dir != externalDir {
		t.Errorf("got %q, want %q", dir, externalDir)
	}
}

// TestResolveWorkspaceMapValue_NotFound tests that non-existent paths return false.
func TestResolveWorkspaceMapValue_NotFound(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)

	_, ok := resolveWorkspaceMapValue("/nonexistent", root)
	if ok {
		t.Fatal("expected false for non-existent path")
	}
}

// TestIsWithinWorkspace tests the workspace boundary check.
func TestIsWithinWorkspace(t *testing.T) {
	root := "/code/project"

	tests := []struct {
		target string
		want   bool
	}{
		{"/code/project/src", true},
		{"/code/project", true},
		{"/code/project/vendor/lib", true},
		{"/code/other", false},
		{"/home/user", false},
		{"/code", false},
	}

	for _, tt := range tests {
		got := isWithinWorkspace(tt.target, root)
		if got != tt.want {
			t.Errorf("isWithinWorkspace(%q, %q) = %v, want %v", tt.target, root, got, tt.want)
		}
	}
}

// TestIsRelativeJumpPath tests the relative jump path detector.
func TestIsRelativeJumpPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"./foo", true},
		{"../bar", true},
		{"/foo", false},
		{"/home/user/code", false},
		{"foo/bar", false},
		{"github.com/user/repo", false},
	}

	for _, tt := range tests {
		got := isRelativeJumpPath(tt.path)
		if got != tt.want {
			t.Errorf("isRelativeJumpPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
