package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Package 表示 mod.jsonc 定義的專案套件
type Package struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Description     string            `json:"description,omitempty"`
	Keywords        []string          `json:"keywords,omitempty"`
	Author          string            `json:"author,omitempty"`
	Email           string            `json:"email,omitempty"`
	Organization    string            `json:"organization,omitempty"`
	Repository      string            `json:"repository,omitempty"`
	Homepage        string            `json:"homepage,omitempty"`
	License         string            `json:"license,omitempty"`
	Main            string            `json:"main,omitempty"`
	Dependencies    map[string]string `json:"dependencies,omitempty"`
	DevDependencies map[string]string `json:"dev-dependencies,omitempty"`
	Ignore          []string          `json:"ignore,omitempty"`
	Alias           map[string]string `json:"alias,omitempty"`
	Workspace       string            `json:"workspace,omitempty"` // 工作區路徑（相對於 mod.jsonc）
	Mirrors         []string          `json:"mirrors,omitempty"`   // 下載鏡像清單（依序嘗試）
	RootDir         string            // 套件根目錄（含 mod.jsonc）
	workspaceRoot   string            // 解析後的絕對工作區根目錄路徑
	lockFile        *LockFile         // 已載入的鎖檔案（可選）
	sumFile         *SumFile          // 已載入的總和檔案（可選）
	depGraph        *DependencyGraph  // 已解析的依賴圖（可選）
}

// stripJSONC 移除 JSONC 中的 // 和 /* */ 註解
func stripJSONC(raw []byte) []byte {
	s := string(raw)
	var out strings.Builder
	inStr := false
	inLine := false
	inBlock := false
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if inStr {
			out.WriteRune(ch)
			if ch == '"' && (i == 0 || runes[i-1] != '\\') {
				inStr = false
			}
			continue
		}

		if inLine {
			if ch == '\n' {
				inLine = false
				out.WriteRune(ch)
			}
			continue
		}

		if inBlock {
			if ch == '*' && i+1 < len(runes) && runes[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}

		if ch == '"' {
			inStr = true
			out.WriteRune(ch)
			continue
		}

		if ch == '/' && i+1 < len(runes) {
			next := runes[i+1]
			if next == '/' {
				inLine = true
				i++
				continue
			}
			if next == '*' {
				inBlock = true
				i++
				continue
			}
		}

		out.WriteRune(ch)
	}

	// 移除物件/陣列結尾前的尾隨逗號
	result := out.String()
	result = removeTrailingCommas(result)

	return []byte(result)
}

// removeTrailingCommas 移除 JSON 中物件/陣列結尾前的尾隨逗號
// 例如 {"a": 1,} → {"a": 1} 或 [1, 2,] → [1, 2]
func removeTrailingCommas(s string) string {
	var out strings.Builder
	inStr := false
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if inStr {
			out.WriteRune(ch)
			if ch == '"' && (i == 0 || runes[i-1] != '\\') {
				inStr = false
			}
			continue
		}

		if ch == '"' {
			inStr = true
			out.WriteRune(ch)
			continue
		}

		// 跳過 // 註解（防止 "key,//comment" 的情況，但這裡輸入已無註解）
		// 移除尾隨逗號：逗號後跟空白後跟 } 或 ]
		if ch == ',' {
			// 向前查找，忽略空白
			j := i + 1
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t' || runes[j] == '\n' || runes[j] == '\r') {
				j++
			}
			if j < len(runes) && (runes[j] == '}' || runes[j] == ']') {
				// 跳過逗號，不寫入
				continue
			}
		}

		out.WriteRune(ch)
	}

	return out.String()
}

// LoadPackage 從 dir 目錄尋找並解析 mod.jsonc
func LoadPackage(dir string) (*Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// 向上尋找 mod.jsonc
	root := abs
	for {
		candidate := filepath.Join(root, "mod.jsonc")
		if _, err := os.Stat(candidate); err == nil {
			raw, err := os.ReadFile(candidate)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", candidate, err)
			}
			cleaned := stripJSONC(raw)
			var pkg Package
			if err := json.Unmarshal(cleaned, &pkg); err != nil {
				return nil, fmt.Errorf("parsing %s: %w", candidate, err)
			}
			pkg.RootDir = root

			// 解析 workspace 路徑
			if pkg.Workspace != "" {
				wsPath := filepath.Join(root, pkg.Workspace)
				if absWS, err := filepath.Abs(wsPath); err == nil {
					pkg.workspaceRoot = absWS
				}
			}

			// 警告：workspace 內的依賴不應有版本號
			pkg.warnWorkspaceDepVersion()

			// 補上預設 alias
			if pkg.Alias == nil {
				pkg.Alias = make(map[string]string)
			}
			if _, ok := pkg.Alias["@"]; !ok {
				pkg.Alias["@"] = "./"
			}

			// 載入鎖檔案（可選）
			pkg.lockFile, _ = LoadLockFile(root)

			// 載入總和檔案（可選）
			pkg.sumFile, _ = LoadSumFile(root)

			return &pkg, nil
		}

		parent := filepath.Dir(root)
		if parent == root { // 到根目錄了
			break
		}
		root = parent
	}

	return nil, nil // 沒有找到套件
}

// ResolvePath 根據 alias 解析路徑
func (p *Package) ResolvePath(inputPath string) string {
	if p == nil {
		return inputPath
	}

	// 嘗試所有 alias 前綴
	for prefix, alias := range p.Alias {
		prefixStr := prefix
		if strings.HasPrefix(inputPath, prefixStr) {
			rel := strings.TrimPrefix(inputPath, prefixStr)
			aliasPath := filepath.Join(p.RootDir, alias, rel)
			return filepath.Clean(aliasPath)
		}
	}

	// 沒有匹配 alias，相對於套件根目錄
	return filepath.Clean(filepath.Join(p.RootDir, inputPath))
}

// WorkspaceMap 表示 workspace.jsonc 解析結果
// key 為套件短名稱，value 為相對於 workspaceRoot 的本地路徑
type WorkspaceMap map[string]string

// LoadWorkspace 載入 workspace.jsonc
func (p *Package) LoadWorkspace() (WorkspaceMap, error) {
	if p == nil || p.workspaceRoot == "" {
		return nil, nil
	}

	wsFile := filepath.Join(p.workspaceRoot, "workspace.jsonc")
	raw, err := os.ReadFile(wsFile)
	if err != nil {
		return nil, nil // workspace.jsonc 可選
	}

	cleaned := stripJSONC(raw)
	var ws WorkspaceMap
	if err := json.Unmarshal(cleaned, &ws); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", wsFile, err)
	}
	return ws, nil
}

// matchDependency 在依賴中尋找最長前綴匹配
// 例如 import path="github.com/lizongying/nolang/test2/utils"
// 依賴 "github.com/lizongying/nolang/test2": "v0.1.0" 匹配
// 返回 (依賴鍵, 版本號, 是否匹配)
func (p *Package) matchDependency(importPath string) (string, string, bool) {
	if p == nil || len(p.Dependencies) == 0 {
		return "", "", false
	}

	var matchedKey string
	var matchedVer string

	for key, version := range p.Dependencies {
		// 依賴鍵必須是 importPath 的前綴
		// 且 importPath 中的後續部分應以 / 開頭
		if strings.HasPrefix(importPath, key) {
			remainder := strings.TrimPrefix(importPath, key)
			if remainder == "" || strings.HasPrefix(remainder, "/") {
				if len(key) > len(matchedKey) {
					matchedKey = key
					matchedVer = version
				}
			}
		}
	}

	if matchedKey == "" {
		return "", "", false
	}
	return matchedKey, matchedVer, true
}

// packageShortName 從依賴鍵中提取短名稱（最後一段）
// "github.com/lizongying/nolang/test2" → "test2"
func packageShortName(depKey string) string {
	if idx := strings.LastIndex(depKey, "/"); idx >= 0 {
		return depKey[idx+1:]
	}
	return depKey
}

// resolveDependency 解析依賴路徑，返回本地套件目錄
// 先檢查 workspace.jsonc 是否有本地覆蓋，否則回退到下載
func (p *Package) resolveDependency(importPath string) (string, error) {
	key, version, ok := p.matchDependency(importPath)
	if !ok {
		return "", nil
	}

	shortName := packageShortName(key)

	// 檢查 workspace.jsonc 是否有本地覆蓋
	if p.workspaceRoot != "" {
		ws, err := p.LoadWorkspace()
		if err == nil && ws != nil {
			if localPath, exists := ws[shortName]; exists {
				localDir := filepath.Join(p.workspaceRoot, localPath)
				if info, err := os.Stat(localDir); err == nil && info.IsDir() {
					return filepath.Clean(localDir), nil
				}
			}
		}
	}

	// 無本地覆蓋，需要下載
	pkgDir, _, err := downloadPackage(key, version, p.Mirrors)
	return pkgDir, err
}

// warnWorkspaceDepVersion 警告 workspace 內的依賴不應指定版本號
func (p *Package) warnWorkspaceDepVersion() {
	if p == nil || p.workspaceRoot == "" || len(p.Dependencies) == 0 {
		return
	}
	ws, err := p.LoadWorkspace()
	if err != nil || ws == nil {
		return
	}
	for key, version := range p.Dependencies {
		if version == "" || version == "*" {
			continue
		}
		shortName := packageShortName(key)
		if _, exists := ws[shortName]; exists {
			fmt.Printf("Warning: dependency %q specifies version %q but is a workspace-local package. Remove the version constraint (use \"*\").\n", key, version)
		}
	}
}

// ResolveDependencyModule 解析依賴中的模組完整路徑
// 返回模組 .no 檔案的絕對路徑
func (p *Package) ResolveDependencyModule(importPath string) (string, error) {
	key, _, ok := p.matchDependency(importPath)
	if !ok {
		return "", nil
	}

	pkgDir, err := p.resolveDependency(importPath)
	if err != nil {
		return "", err
	}
	if pkgDir == "" {
		return "", nil
	}

	// 提取依賴鍵後面的模組路徑
	modulePart := strings.TrimPrefix(importPath, key)
	modulePart = strings.TrimPrefix(modulePart, "/")

	if modulePart == "" {
		return "", fmt.Errorf("no module path after dependency key %s", key)
	}

	fullPath := filepath.Join(pkgDir, modulePart) + ".no"
	if _, err := os.Stat(fullPath); err == nil {
		return filepath.Clean(fullPath), nil
	}

	// Fallback: try src/{module}.no
	srcPath := filepath.Join(pkgDir, "src", modulePart) + ".no"
	if _, err := os.Stat(srcPath); err == nil {
		return filepath.Clean(srcPath), nil
	}

	return "", fmt.Errorf("module file not found: %s or %s", fullPath, srcPath)
}

// EnsureDependencies 確保所有傳遞依賴已解析
// 在 BuildFile 中編譯前調用
// maxDepth 限制最大遞迴深度（0=不限制，建議值 10）
func (p *Package) EnsureDependencies(maxDepth int) (*DependencyGraph, error) {
	if p == nil || len(p.Dependencies) == 0 {
		return NewDependencyGraph(), nil
	}

	// 如果已經解析過，直接返回
	if p.depGraph != nil {
		return p.depGraph, nil
	}

	graph := NewDependencyGraph()
	graph.mirrors = p.Mirrors

	// 檢查是否有鎖檔案
	if p.lockFile != nil {
		needsResolve, err := CheckLockFile(p, p.lockFile)
		if err != nil || needsResolve {
			// 鎖檔案不相容，從頭解析
			return p.resolveFromScratch(graph, maxDepth)
		}
		// 使用鎖檔案解析（從快取載入）
		return p.resolveFromLock(graph, maxDepth)
	}

	// 無鎖檔案，從頭解析
	return p.resolveFromScratch(graph, maxDepth)
}

// GetDependencyGraph 返回已解析的依賴圖（如果尚未解析則返回 nil）
func (p *Package) GetDependencyGraph() *DependencyGraph {
	return p.depGraph
}

// resolveFromScratch 從頭解析所有依賴（無鎖檔案）
func (p *Package) resolveFromScratch(graph *DependencyGraph, maxDepth int) (*DependencyGraph, error) {
	for key, version := range p.Dependencies {
		// 跳過標準庫依賴
		if isStdDependency(key) {
			continue
		}
		if err := graph.ResolveAll(key, version, maxDepth); err != nil {
			return nil, fmt.Errorf("resolving dependency %s@%s: %w", key, version, err)
		}
	}

	// 循環依賴檢測
	if err := graph.DetectCycles(); err != nil {
		return nil, fmt.Errorf("cycle detection: %w", err)
	}

	// 解析成功，保存鎖檔案
	if p.RootDir != "" {
		if err := SaveLockFile(p.RootDir, graph); err != nil {
			// 僅記錄警告，不阻止編譯
			fmt.Printf("Warning: failed to save lock file: %v\n", err)
		}
		if err := SaveSumFile(p.RootDir, graph); err != nil {
			// 僅記錄警告，不阻止編譯
			fmt.Printf("Warning: failed to save sum file: %v\n", err)
		}
	}

	p.depGraph = graph
	return graph, nil
}

// resolveFromLock 從鎖檔案解析依賴（跳過下載，直接從快取載入）
func (p *Package) resolveFromLock(graph *DependencyGraph, maxDepth int) (*DependencyGraph, error) {
	if p.lockFile == nil {
		return p.resolveFromScratch(graph, maxDepth)
	}

	for key, version := range p.Dependencies {
		if isStdDependency(key) {
			continue
		}

		keyWithVer := key + "@" + version
		lockPkg, exists := p.lockFile.Packages[keyWithVer]
		if !exists {
			// 鎖檔案中沒有此依賴，回退到下載
			return p.resolveFromScratch(graph, maxDepth)
		}

		// 從快取或下載取得套件目錄及壓縮包 SHA256
		pkgDir, downloadHash, err := downloadPackage(key, version, p.Mirrors)
		if err != nil {
			return nil, fmt.Errorf("downloading %s@%s: %w", key, version, err)
		}

		if _, err := graph.ResolveFromLock(key, version, pkgDir, downloadHash, lockPkg.Dependencies, 0, maxDepth); err != nil {
			return nil, fmt.Errorf("resolving %s from lock: %w", key, err)
		}
	}

	// 循環依賴檢測
	if err := graph.DetectCycles(); err != nil {
		return nil, fmt.Errorf("cycle detection: %w", err)
	}

	// 解析成功，保存鎖檔案和總和檔案
	if p.RootDir != "" {
		if err := SaveLockFile(p.RootDir, graph); err != nil {
			fmt.Printf("Warning: failed to save lock file: %v\n", err)
		}
		if err := SaveSumFile(p.RootDir, graph); err != nil {
			fmt.Printf("Warning: failed to save sum file: %v\n", err)
		}
	}

	p.depGraph = graph
	return graph, nil
}
