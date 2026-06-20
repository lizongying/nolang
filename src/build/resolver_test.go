package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewDependencyGraph(t *testing.T) {
	g := NewDependencyGraph()
	if g == nil {
		t.Fatal("NewDependencyGraph() returned nil")
	}
	if len(g.Roots()) != 0 {
		t.Errorf("expected 0 roots, got %d", len(g.Roots()))
	}
	if len(g.Resolved()) != 0 {
		t.Errorf("expected 0 resolved, got %d", len(g.Resolved()))
	}
}

func TestIsStdDependency(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"fmt", true},
		{"math", true},
		{"strconv", true},
		{"os", true},
		{"github.com/lizongying/nolang/test2", false},
		{"github.com/user/repo", false},
		{"github.com/user/repo/sub/pkg", false},
	}
	for _, tt := range tests {
		got := isStdDependency(tt.key)
		if got != tt.want {
			t.Errorf("isStdDependency(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestContainsDot(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"fmt", false},
		{"math", false},
		{"github.com/user/repo", true},
		{"std/math", false},
		{"a.b", true},
	}
	for _, tt := range tests {
		got := containsDot(tt.s)
		if got != tt.want {
			t.Errorf("containsDot(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestDetectCycles(t *testing.T) {
	// 構建一個有循環的依賴圖：A → B → C → A
	g := NewDependencyGraph()

	// 手動構建循環
	nodeA := &DependencyNode{Key: "github.com/a/a", Version: "v1.0.0"}
	nodeB := &DependencyNode{Key: "github.com/b/b", Version: "v1.0.0"}
	nodeC := &DependencyNode{Key: "github.com/c/c", Version: "v1.0.0"}

	nodeA.Dependencies = []*DependencyNode{nodeB}
	nodeB.Dependencies = []*DependencyNode{nodeC}
	nodeC.Dependencies = []*DependencyNode{nodeA} // 循環！

	g.roots = append(g.roots, nodeA)
	g.resolved["github.com/a/a@v1.0.0"] = nodeA
	g.resolved["github.com/b/b@v1.0.0"] = nodeB
	g.resolved["github.com/c/c@v1.0.0"] = nodeC

	err := g.DetectCycles()
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
	t.Logf("Cycle detected correctly: %v", err)
}

func TestDetectNoCycles(t *testing.T) {
	// 正常的依賴樹：A → B → C
	g := NewDependencyGraph()

	nodeA := &DependencyNode{Key: "github.com/a/a", Version: "v1.0.0"}
	nodeB := &DependencyNode{Key: "github.com/b/b", Version: "v1.0.0"}
	nodeC := &DependencyNode{Key: "github.com/c/c", Version: "v1.0.0"}

	nodeA.Dependencies = []*DependencyNode{nodeB}
	nodeB.Dependencies = []*DependencyNode{nodeC}

	g.roots = append(g.roots, nodeA)
	g.resolved["github.com/a/a@v1.0.0"] = nodeA
	g.resolved["github.com/b/b@v1.0.0"] = nodeB
	g.resolved["github.com/c/c@v1.0.0"] = nodeC

	err := g.DetectCycles()
	if err != nil {
		t.Fatalf("unexpected cycle detection: %v", err)
	}
}

func TestLoadLockFile(t *testing.T) {
	// 建立臨時目錄和鎖檔案
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "nolang.lock.json")

	content := `{
  "version": "1",
  "packages": {
    "github.com/a/a@v1.0.0": {
      "key": "github.com/a/a",
      "version": "v1.0.0",
      "dependencies": {
        "github.com/b/b": "v1.0.0"
      }
    }
  }
}`
	if err := os.WriteFile(lockPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	lock, err := LoadLockFile(tmpDir)
	if err != nil {
		t.Fatalf("LoadLockFile error: %v", err)
	}
	if lock == nil {
		t.Fatal("LoadLockFile returned nil")
	}
	if lock.Version != "1" {
		t.Errorf("Version = %q, want %q", lock.Version, "1")
	}
	pkg, exists := lock.Packages["github.com/a/a@v1.0.0"]
	if !exists {
		t.Fatal("expected package in lock file")
	}
	if pkg.Key != "github.com/a/a" {
		t.Errorf("Key = %q, want %q", pkg.Key, "github.com/a/a")
	}
	if pkg.Version != "v1.0.0" {
		t.Errorf("Version = %q, want %q", pkg.Version, "v1.0.0")
	}
	if pkg.Dependencies["github.com/b/b"] != "v1.0.0" {
		t.Errorf("Dependencies[github.com/b/b] = %q, want %q", pkg.Dependencies["github.com/b/b"], "v1.0.0")
	}
}

func TestLoadLockFileNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	lock, err := LoadLockFile(tmpDir)
	if err != nil {
		t.Fatalf("LoadLockFile error: %v", err)
	}
	if lock != nil {
		t.Fatal("expected nil for non-existent lock file")
	}
}

func TestCheckLockFile(t *testing.T) {
	pkg := &Package{
		Dependencies: map[string]string{
			"github.com/a/a": "v1.0.0",
		},
	}

	lock := &LockFile{
		Version: "1",
		Packages: map[string]LockPkg{
			"github.com/a/a@v1.0.0": {
				Key:     "github.com/a/a",
				Version: "v1.0.0",
			},
		},
	}

	needsResolve, err := CheckLockFile(pkg, lock)
	if err != nil {
		t.Fatalf("CheckLockFile error: %v", err)
	}
	if needsResolve {
		t.Error("expected false (lock is compatible)")
	}
}

func TestCheckLockFileMissingDep(t *testing.T) {
	pkg := &Package{
		Dependencies: map[string]string{
			"github.com/a/a": "v1.0.0",
			"github.com/c/c": "v2.0.0",
		},
	}

	lock := &LockFile{
		Version: "1",
		Packages: map[string]LockPkg{
			"github.com/a/a@v1.0.0": {
				Key:     "github.com/a/a",
				Version: "v1.0.0",
			},
		},
	}

	needsResolve, err := CheckLockFile(pkg, lock)
	if err != nil {
		t.Fatalf("CheckLockFile error: %v", err)
	}
	if !needsResolve {
		t.Error("expected true (lock is missing dependency)")
	}
}

func TestSaveLockFile(t *testing.T) {
	tmpDir := t.TempDir()

	graph := NewDependencyGraph()
	node := &DependencyNode{
		Key:     "github.com/a/a",
		Version: "v1.0.0",
		PkgDir:  "/tmp/test",
	}
	child := &DependencyNode{
		Key:     "github.com/b/b",
		Version: "v1.0.0",
		PkgDir:  "/tmp/test2",
	}
	node.Dependencies = []*DependencyNode{child}
	graph.roots = append(graph.roots, node)
	graph.resolved["github.com/a/a@v1.0.0"] = node
	graph.resolved["github.com/b/b@v1.0.0"] = child

	if err := SaveLockFile(tmpDir, graph); err != nil {
		t.Fatalf("SaveLockFile error: %v", err)
	}

	// 驗證鎖檔案存在
	lockPath := filepath.Join(tmpDir, "nolang.lock.json")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	// 重新載入驗證
	lock, err := LoadLockFile(tmpDir)
	if err != nil {
		t.Fatalf("LoadLockFile error: %v", err)
	}
	if lock == nil {
		t.Fatal("LoadLockFile returned nil")
	}
	aPkg, exists := lock.Packages["github.com/a/a@v1.0.0"]
	if !exists {
		t.Fatal("expected github.com/a/a@v1.0.0 in lock file")
	}
	if aPkg.Dependencies["github.com/b/b"] != "v1.0.0" {
		t.Errorf("Dependencies mismatch: %v", aPkg.Dependencies)
	}
}

func TestEnsureDependenciesNoDeps(t *testing.T) {
	pkg := &Package{
		Name:         "test",
		Dependencies: nil,
	}

	graph, err := pkg.EnsureDependencies(10)
	if err != nil {
		t.Fatalf("EnsureDependencies error: %v", err)
	}
	if graph == nil {
		t.Fatal("EnsureDependencies returned nil graph")
	}
	if len(graph.Roots()) != 0 {
		t.Errorf("expected 0 roots, got %d", len(graph.Roots()))
	}
}

func TestEnsureDependenciesEmptyDeps(t *testing.T) {
	pkg := &Package{
		Name:         "test",
		Dependencies: map[string]string{},
	}

	graph, err := pkg.EnsureDependencies(10)
	if err != nil {
		t.Fatalf("EnsureDependencies error: %v", err)
	}
	if graph == nil {
		t.Fatal("EnsureDependencies returned nil graph")
	}
}

func TestHasNolangConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// 沒有 mod.jsonc
	if hasNolangConfig(tmpDir) {
		t.Error("expected false for dir without mod.jsonc")
	}

	// 建立 mod.jsonc
	cfgPath := filepath.Join(tmpDir, "mod.jsonc")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	if !hasNolangConfig(tmpDir) {
		t.Error("expected true for dir with mod.jsonc")
	}
}

func TestFindPackageRootForDep(t *testing.T) {
	tmpDir := t.TempDir()

	// 建立一個模擬的套件目錄結構
	pkgDir := filepath.Join(tmpDir, "test-pkg")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 在 pkgDir 中建立 mod.jsonc
	cfgPath := filepath.Join(pkgDir, "mod.jsonc")
	if err := os.WriteFile(cfgPath, []byte(`{"name":"test-pkg"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// findPackageRootForDep 應該返回 pkgDir 因為它包含 mod.jsonc
	result := findPackageRootForDep("github.com/user/test-pkg", "v1.0.0", pkgDir)
	if result != pkgDir {
		t.Errorf("expected %q, got %q", pkgDir, result)
	}

	// 如果 pkgDir 中沒有 mod.jsonc，應該返回 pkgDir 本身
	os.Remove(cfgPath)
	result2 := findPackageRootForDep("github.com/user/test-pkg", "v1.0.0", pkgDir)
	if result2 == "" {
		t.Error("expected non-empty result")
	}
}