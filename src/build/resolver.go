package build

import (
	"fmt"
	"os"
	"path/filepath"
)

// DependencyGraph 表示完整的依賴圖，管理依賴解析狀態和循環檢測
type DependencyGraph struct {
	roots    []*DependencyNode          // 頂層依賴節點
	resolved map[string]*DependencyNode // "key@version" → node（全局唯一，避免重複解析）
	seen     map[string]bool            // 當前 DFS 路徑（用於循環檢測）
}

// DependencyNode 表示依賴樹中的一個節點
type DependencyNode struct {
	Key          string            // 依賴鍵，如 "github.com/lizongying/nolang/test2"
	Version      string            // 版本號
	PkgDir       string            // 本地下載目錄（套件源碼目錄）
	PkgRoot      string            // 包含 mod.jsonc 的目錄（可能與 PkgDir 相同）
	DownloadHash string            // 下載壓縮包的 SHA256 雜湊值
	Dependencies []*DependencyNode // 子依賴
	Depth        int               // 依賴深度
}

// NewDependencyGraph 創建一個新的依賴圖
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		roots:    make([]*DependencyNode, 0),
		resolved: make(map[string]*DependencyNode),
		seen:     make(map[string]bool),
	}
}

// Roots 返回頂層依賴節點
func (g *DependencyGraph) Roots() []*DependencyNode {
	return g.roots
}

// Resolved 返回所有已解析的依賴節點（key@version → node）
func (g *DependencyGraph) Resolved() map[string]*DependencyNode {
	return g.resolved
}

// ResolveAll 遞迴解析指定依賴及其所有傳遞依賴
// maxDepth 限制最大遞迴深度（0 表示不限制）
func (g *DependencyGraph) ResolveAll(rootKey, rootVersion string, maxDepth int) error {
	node, err := g.resolveNode(rootKey, rootVersion, 0, maxDepth)
	if err != nil {
		return err
	}
	g.roots = append(g.roots, node)
	return nil
}

// resolveNode 解析單個依賴節點（核心遞迴邏輯）
func (g *DependencyGraph) resolveNode(key, version string, depth, maxDepth int) (*DependencyNode, error) {
	keyWithVer := key + "@" + version

	// 1. 檢查是否已解析（全域去重）
	if existing, ok := g.resolved[keyWithVer]; ok {
		return existing, nil
	}

	// 2. 檢查循環依賴
	if g.seen[keyWithVer] {
		return nil, fmt.Errorf("circular dependency detected: %s", keyWithVer)
	}

	// 3. 檢查最大深度
	if maxDepth > 0 && depth >= maxDepth {
		return nil, fmt.Errorf("dependency depth limit exceeded (%d): %s@%s", maxDepth, key, version)
	}

	// 4. 標記為正在解析
	g.seen[keyWithVer] = true

	// 5. 下載/獲取快取路徑（源碼目錄）
	pkgDir, downloadHash, err := downloadPackage(key, version)
	if err != nil {
		delete(g.seen, keyWithVer)
		return nil, fmt.Errorf("downloading %s@%s: %w", key, version, err)
	}

	// 6. 尋找包含 mod.jsonc 的根目錄
	pkgRoot := findPackageRootForDep(key, version, pkgDir)

	// 7. 載入套件的 mod.jsonc 以讀取其依賴
	var deps map[string]string
	if hasNolangConfig(pkgRoot) {
		pkg, err := LoadPackage(pkgRoot)
		if err == nil && pkg != nil && len(pkg.Dependencies) > 0 {
			deps = pkg.Dependencies
		}
	}

	// 8. 創建節點
	node := &DependencyNode{
		Key:          key,
		Version:      version,
		PkgDir:       pkgDir,
		PkgRoot:      pkgRoot,
		DownloadHash: downloadHash,
		Dependencies: make([]*DependencyNode, 0),
		Depth:        depth,
	}

	// 9. 遞迴解析子依賴
	for depKey, depVersion := range deps {
		// 跳過標準庫依賴（短名稱，不包含 "."，不走遠端下載）
		if isStdDependency(depKey) {
			continue
		}

		childNode, err := g.resolveNode(depKey, depVersion, depth+1, maxDepth)
		if err != nil {
			delete(g.seen, keyWithVer)
			return nil, fmt.Errorf("resolving dependency %s of %s@%s: %w", depKey, key, version, err)
		}
		node.Dependencies = append(node.Dependencies, childNode)
	}

	// 10. 從路徑中移除標記
	delete(g.seen, keyWithVer)

	// 11. 存入全域 resolved map
	g.resolved[keyWithVer] = node

	return node, nil
}

// ResolveFromLock 從鎖檔案解析依賴（跳過下載，直接從快取載入）
func (g *DependencyGraph) ResolveFromLock(key, version, pkgDir, downloadHash string, lockDeps map[string]string, depth, maxDepth int) (*DependencyNode, error) {
	keyWithVer := key + "@" + version

	if existing, ok := g.resolved[keyWithVer]; ok {
		return existing, nil
	}

	if maxDepth > 0 && depth >= maxDepth {
		return nil, fmt.Errorf("dependency depth limit exceeded (%d): %s@%s", maxDepth, key, version)
	}

	pkgRoot := findPackageRootForDep(key, version, pkgDir)

	node := &DependencyNode{
		Key:          key,
		Version:      version,
		PkgDir:       pkgDir,
		PkgRoot:      pkgRoot,
		DownloadHash: downloadHash,
		Dependencies: make([]*DependencyNode, 0),
		Depth:        depth,
	}

	// 遞迴解析鎖檔案中記錄的子依賴
	for depKey, depVersion := range lockDeps {
		if isStdDependency(depKey) {
			continue
		}

		// 下載/從快取獲取套件目錄及壓縮包 SHA256
		depPkgDir, depDownloadHash, err := downloadPackage(depKey, depVersion)
		if err != nil {
			return nil, fmt.Errorf("downloading %s@%s from lock: %w", depKey, depVersion, err)
		}

		// 從快取載入該依賴的子依賴列表
		childDep := make(map[string]string)
		depPkgRoot := findPackageRootForDep(depKey, depVersion, depPkgDir)
		if hasNolangConfig(depPkgRoot) {
			if p := loadCachedPackageFromDir(depPkgRoot); p != nil && p.Dependencies != nil {
				for k, v := range p.Dependencies {
					if !isStdDependency(k) {
						childDep[k] = v
					}
				}
			}
		}

		childNode, err := g.ResolveFromLock(depKey, depVersion, depPkgDir, depDownloadHash, childDep, depth+1, maxDepth)
		if err != nil {
			return nil, fmt.Errorf("resolving %s from lock: %w", depKey, err)
		}
		node.Dependencies = append(node.Dependencies, childNode)
	}

	g.resolved[keyWithVer] = node
	return node, nil
}

// DetectCycles 對已解析的依賴圖進行循環檢測（DFS）
func (g *DependencyGraph) DetectCycles() error {
	visited := make(map[string]bool)
	path := make(map[string]bool)

	var dfs func(node *DependencyNode) error
	dfs = func(node *DependencyNode) error {
		key := node.Key + "@" + node.Version
		if path[key] {
			return fmt.Errorf("circular dependency detected: %s", key)
		}
		if visited[key] {
			return nil
		}
		visited[key] = true
		path[key] = true
		for _, child := range node.Dependencies {
			if err := dfs(child); err != nil {
				return err
			}
		}
		delete(path, key)
		return nil
	}

	for _, root := range g.roots {
		if err := dfs(root); err != nil {
			return err
		}
	}
	return nil
}

// findPackageRootForDep 根據依賴鍵和源碼目錄，尋找包含 mod.jsonc 的根目錄
func findPackageRootForDep(key, version, pkgDir string) string {
	// 先檢查 pkgDir 自身是否包含 mod.jsonc
	if hasNolangConfig(pkgDir) {
		return pkgDir
	}

	// 嘗試通過快取路徑尋找
	owner, repo, ok := parseGitHubKey(key)
	if !ok {
		return pkgDir
	}
	cachePath := filepath.Join(cacheDir(), owner, repo, version)
	shortName := packageShortName(key)
	root := findPackageRoot(cachePath, shortName)
	if root != "" {
		return root
	}

	return pkgDir
}

// isStdDependency 判斷是否為標準庫依賴（不參與遠端解析）
func isStdDependency(key string) bool {
	// 標準庫依賴為短名稱（不包含 "."）
	// 例如 "fmt", "math", "strconv"
	// 遠端依賴包含 "."，例如 "github.com/..."
	return !containsDot(key)
}

// containsDot 檢查字串是否包含 "."
func containsDot(s string) bool {
	for _, ch := range s {
		if ch == '.' {
			return true
		}
	}
	return false
}

// loadCachedPackageFromDir 從指定目錄載入套件配置
func loadCachedPackageFromDir(pkgDir string) *Package {
	// 先檢查 pkgDir 自身
	pkg, err := LoadPackage(pkgDir)
	if err == nil && pkg != nil {
		return pkg
	}

	// LoadPackage 向上尋找 mod.jsonc，如果還是 nil，嘗試子目錄
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			subPkg, err := LoadPackage(filepath.Join(pkgDir, entry.Name()))
			if err == nil && subPkg != nil {
				return subPkg
			}
		}
	}
	return nil
}