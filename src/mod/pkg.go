package mod

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CompilerOptions 表示 mod.jsonc 的 compiler 區塊選項
type CompilerOptions struct {
	AnonymousFnType bool     `json:"anonymous-fn-type"`
	LinkLibs        []string `json:"link-libs,omitempty"`
	// Emit 控制輸出目標後端："js" 表示使用 JS 後端發射 JavaScript；
	// 空字串（預設）表示使用 LLVM 後端生成原生可執行檔。
	Emit string `json:"emit,omitempty"`
}

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
	Workspace       string            `json:"workspace,omitempty"` // 已廢棄：workspace 根目錄現由 workspace.jsonc 自動偵測
	Mirrors         []string          `json:"mirrors,omitempty"`   // 下載鏡像清單（依序嘗試）
	Compiler        CompilerOptions   `json:"compiler,omitempty"`
	Output          string            `json:"output,omitempty"` // 輸出目錄（如 "./dist"）
	RootDir         string            // 套件根目錄（含 mod.jsonc）
	workspaceRoot   string            // 解析後的絕對工作區根目錄路徑
	wsMap           WorkspaceMap      // 快取的 workspace.jsonc 映射
	warned          bool              // 是否已輸出過 workspace 版本警告
	lockFile        *LockFile         // 已載入的鎖檔案（可選）
	sumFile         *SumFile          // 已載入的總和檔案（可選）
	depGraph        *DependencyGraph  // 已解析的依賴圖（可選）
}

// StripJSONC 移除 JSONC 中的 // 和 /* */ 註解及尾隨逗號
func StripJSONC(raw []byte) []byte {
	return stripJSONC(raw)
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

			// 自動偵測 workspace 根目錄：向上搜尋 workspace.jsonc
			wsRoot := root
			for {
				candidate := filepath.Join(wsRoot, "workspace.jsonc")
				if _, err := os.Stat(candidate); err == nil {
					pkg.workspaceRoot = wsRoot
					break
				}
				parent := filepath.Dir(wsRoot)
				if parent == wsRoot {
					break
				}
				wsRoot = parent
			}

			// 快取 workspace.jsonc 映射，避免後續重複讀取磁碟
			if pkg.workspaceRoot != "" {
				pkg.wsMap, _ = loadWorkspaceMap(pkg.workspaceRoot)
			}
			// 警告依賴版本約束不當（本地包應用 *，線上包應用版本號）
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

// WorkspaceRoot 返回工作區根目錄（workspace.jsonc 所在目錄）。
// 若未偵測到 workspace.jsonc 則返回空字串。
func (p *Package) WorkspaceRoot() string {
	if p == nil {
		return ""
	}
	return p.workspaceRoot
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

// loadWorkspaceMap 從磁碟讀取並解析 workspace.jsonc，同時合併 .workspace.jsonc。
//
// 加載順序：先加載公共配置 workspace.jsonc，再加載私有配置 .workspace.jsonc。
// 相同的 key，私有配置覆蓋公共配置；新 key 相互疊加。
// 這分離了「工程標準化配置」與「個人本地調試配置」，避免臨時 fork 映射污染項目公共配置。
func loadWorkspaceMap(workspaceRoot string) (WorkspaceMap, error) {
	// 1. 加載公共配置 workspace.jsonc
	wsFile := filepath.Join(workspaceRoot, "workspace.jsonc")
	raw, err := os.ReadFile(wsFile)
	if err != nil {
		return nil, nil // workspace.jsonc 可選
	}

	cleaned := stripJSONC(raw)
	var ws WorkspaceMap
	if err := json.Unmarshal(cleaned, &ws); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", wsFile, err)
	}
	if ws == nil {
		ws = make(WorkspaceMap)
	}

	// 2. 加載私有配置 .workspace.jsonc（可選，覆蓋公共配置）
	privateFile := filepath.Join(workspaceRoot, ".workspace.jsonc")
	if privateRaw, pErr := os.ReadFile(privateFile); pErr == nil {
		privateCleaned := stripJSONC(privateRaw)
		var privateWs WorkspaceMap
		if err := json.Unmarshal(privateCleaned, &privateWs); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", privateFile, err)
		}
		// 合併：私有配置覆蓋公共配置，新 key 疊加
		for k, v := range privateWs {
			ws[k] = v
		}
	}

	return ws, nil
}

// LoadWorkspace 載入 workspace.jsonc（優先使用快取）
func (p *Package) LoadWorkspace() (WorkspaceMap, error) {
	if p == nil || p.workspaceRoot == "" {
		return nil, nil
	}
	// 使用快取避免重複讀取磁碟
	if p.wsMap != nil {
		return p.wsMap, nil
	}
	return loadWorkspaceMap(p.workspaceRoot)
}

// MatchDependency 在依賴中尋找最長前綴匹配
// 例如 import path="github.com/lizongying/nolang/test2/utils"
// 依賴 "github.com/lizongying/nolang/test2": "v0.1.0" 匹配
// 返回 (依賴鍵, 版本號, 是否匹配)
func (p *Package) MatchDependency(importPath string) (string, string, bool) {
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

// PackageShortName 從依賴鍵中提取短名稱（最後一段）
// "github.com/lizongying/nolang/test2" → "test2"
func PackageShortName(depKey string) string {
	if idx := strings.LastIndex(depKey, "/"); idx >= 0 {
		return depKey[idx+1:]
	}
	return depKey
}

// resolveDependency 解析依賴路徑，返回本地套件目錄
//
// 工作區邊界約束：
//   - package.jsonc 中的本地路徑只能以 "/" 開頭（工作區相對），
//     禁止 "./"、"../" 和作業系統絕對路徑。
//   - 唯一允許突破工作區邊界的入口是 workspace.jsonc / .workspace.jsonc 映射。
//
// 解析順序：
// 1. "/" 前綴 → 工作區相對路徑解析（嚴格限制在工作區內）
// 2. workspace.jsonc 中有匹配（短名稱或直接鍵）→ 遞迴解析（支援跨包映射鏈，允許逃逸工作區）
// 3. 無本地覆蓋 → 下載線上套件
func (p *Package) resolveDependency(importPath string) (string, error) {
	key, version, ok := p.MatchDependency(importPath)
	if !ok {
		return "", nil
	}

	// 禁止 "./" 和 "../" 前綴（package.jsonc 不允許相對跳轉）
	if strings.HasPrefix(key, "./") || strings.HasPrefix(key, "../") {
		return "", fmt.Errorf("dependency %q: relative paths (./ ../) are not allowed in package.jsonc; use `/` prefix for workspace-relative paths", key)
	}

	// "/" 前綴：相對於 workspace 根目錄解析（嚴格限制在工作區內）
	if strings.HasPrefix(key, "/") {
		relPath := strings.TrimPrefix(key, "/")
		localDir := filepath.Join(p.workspaceRoot, relPath)
		// 安全檢查：解析後的路徑必須在工作區根目錄內
		if !isWithinWorkspace(localDir, p.workspaceRoot) {
			return "", fmt.Errorf("dependency %q: path escapes workspace root; package.jsonc dependencies must stay within the workspace", key)
		}
		if info, err := os.Stat(localDir); err == nil && info.IsDir() {
			return filepath.Clean(localDir), nil
		}
		return "", fmt.Errorf("workspace-local dependency path %q not found", key)
	}

	// 檢查 workspace.jsonc 是否有本地覆蓋（遞迴解析，支援跨包映射鏈）
	// workspace.jsonc / .workspace.jsonc 是唯一允許逃逸工作區邊界的入口
	ws, _ := p.LoadWorkspace()
	localDir, found, err := resolveWorkspaceChain(key, p.workspaceRoot, ws, nil)
	if err != nil {
		return "", err // 循環映射等硬錯誤
	}
	if found {
		return filepath.Clean(localDir), nil
	}

	// 無本地覆蓋，需要下載
	pkgDir, _, err := DownloadPackage(key, version, p.Mirrors)
	return pkgDir, err
}

// warnWorkspaceDepVersion 警告依賴版本約束不當（僅首次）
//
// 規則：
//   - 本地包（在 workspace.jsonc 中存在，或依賴鍵以 "/" 開頭）：
//     應使用 "*"，若指定了版本號則警告。
//   - "./" 和 "../" 前綴的依賴鍵：報錯（不允許在 package.jsonc 中使用）。
//   - 線上包（不在 workspace.jsonc 中且非路徑形式）：
//     版本號和 "*" 均可，不警告。
func (p *Package) warnWorkspaceDepVersion() {
	if p == nil || p.warned || len(p.Dependencies) == 0 {
		return
	}
	p.warned = true
	ws, _ := p.LoadWorkspace()
	for key, version := range p.Dependencies {
		// 禁止 "./" 和 "../" 前綴
		if strings.HasPrefix(key, "./") || strings.HasPrefix(key, "../") {
			fmt.Fprintf(os.Stderr, "Error: dependency %q: relative paths (./ ../) are not allowed in package.jsonc; use `/` prefix for workspace-relative paths\n", key)
			continue
		}
		// 先判斷是否為本地包（路徑形式或 workspace.jsonc 中直接匹配）
		isLocal := isLocalPathDep(key) || isWorkspaceLocalDepStatic(ws, key, p.workspaceRoot)
		if isLocal {
			if version != "" && version != "*" {
				fmt.Fprintf(os.Stderr, "Warning: dependency %q specifies version %q but is a workspace-local package. Use \"*\" instead.\n", key, version)
			}
			continue
		}
		// 標準庫依賴不檢查
		if isStdDependency(key) {
			continue
		}
		// 線上包：版本號和 "*" 均可，不警告
	}
}

// isLocalPathDep 判斷依賴鍵是否為本地路徑形式（以 "/" 開頭）
// 注意："./" 和 "../" 已被禁止，不再視為合法本地路徑。
func isLocalPathDep(key string) bool {
	return strings.HasPrefix(key, "/")
}

// isRelativeJumpPath 判斷路徑是否以 "./" 或 "../" 開頭（禁止的相對跳轉形式）。
func isRelativeJumpPath(path string) bool {
	return strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

// isWorkspaceLocalDepStatic 判斷依賴鍵是否為 workspace 本地套件（無需 *Package 接收者）
// 使用遞迴解析，支援被依賴包自帶 workspace.jsonc 的跨包映射。
func isWorkspaceLocalDepStatic(ws WorkspaceMap, key, workspaceRoot string) bool {
	if workspaceRoot == "" {
		// 無工作區根目錄時，退路為單層查找
		_, found := lookupWorkspaceMap(ws, key, workspaceRoot)
		return found
	}
	_, found, err := resolveWorkspaceChain(key, workspaceRoot, ws, nil)
	if err != nil {
		return true // 循環映射 = 本地套件（但有問題，錯誤會在 resolveDependency 中報告）
	}
	return found
}

// lookupWorkspaceMap 在 workspace.jsonc 中查找依賴鍵對應的本地路徑。
// 查找順序：短名稱匹配 → 直接鍵匹配。
//
// 映射值（localPath）的解析規則：
//   - "/xxx"：先嘗試工作區相對路徑（workspaceRoot + path），若目錄存在則使用；
//     若不存在，嘗試作為作業系統絕對路徑使用（允許逃逸工作區邊界）。
//   - 作業系統絕對路徑（如 /home/user/code/foo）：直接使用。
//   - "./" 和 "../"：禁止（已由 loadWorkspaceMap 驗證）。
//
// 返回 (本地目錄絕對路徑, 是否找到)。
func lookupWorkspaceMap(ws WorkspaceMap, key, workspaceRoot string) (string, bool) {
	if ws == nil || workspaceRoot == "" {
		return "", false
	}
	// 1. 短名稱匹配（如 github.com/lizongying/nolang/test2 → test2）
	shortName := PackageShortName(key)
	if localPath, exists := ws[shortName]; exists {
		if dir, ok := resolveWorkspaceMapValue(localPath, workspaceRoot); ok {
			return dir, true
		}
	}
	// 2. 直接鍵匹配（workspace.jsonc 可直接以完整依賴鍵註冊）
	if localPath, exists := ws[key]; exists {
		if dir, ok := resolveWorkspaceMapValue(localPath, workspaceRoot); ok {
			return dir, true
		}
	}
	return "", false
}

// resolveWorkspaceMapValue 解析 workspace.jsonc 映射值為本地目錄。
//
// 支援三種路徑形式：
//   - 工作區相對路徑（以 "/" 開頭）：workspaceRoot + path
//   - 相對跳轉路徑（以 "./" 或 "../" 開頭）：相對於 workspaceRoot 解析，允許逃逸工作區邊界
//   - 作業系統絕對路徑：直接使用（允許指向工作區外部，用於 fork、外部組件）
//
// workspace.jsonc / .workspace.jsonc 是唯一允許突破工作區邊界的入口。
func resolveWorkspaceMapValue(localPath, workspaceRoot string) (string, bool) {
	// 相對跳轉路徑（"./" 或 "../"）：相對於 workspaceRoot 解析
	if strings.HasPrefix(localPath, "./") || strings.HasPrefix(localPath, "../") {
		resolved := filepath.Join(workspaceRoot, localPath)
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			return filepath.Clean(resolved), true
		}
		return "", false
	}
	// 工作區相對路徑（"/" 前綴）：先嘗試 workspaceRoot + path
	wsRelative := filepath.Join(workspaceRoot, localPath)
	if info, err := os.Stat(wsRelative); err == nil && info.IsDir() {
		return wsRelative, true
	}
	// 嘗試作為作業系統絕對路徑（允許逃逸工作區邊界）
	if info, err := os.Stat(localPath); err == nil && info.IsDir() {
		return filepath.Clean(localPath), true
	}
	return "", false
}

// isWithinWorkspace 檢查 target 是否在 workspaceRoot 內（防止路徑逃逸）。
func isWithinWorkspace(target, workspaceRoot string) bool {
	if workspaceRoot == "" {
		return false
	}
	rel, err := filepath.Rel(workspaceRoot, target)
	if err != nil {
		return false
	}
	// 如果相對路徑以 ".." 開頭，表示逃逸了工作區
	return rel != ".." && !strings.HasPrefix(rel, "../")
}

// lookupWorkspaceLocal 是 lookupWorkspaceMap 的 *Package 方法版本，使用遞迴解析。
func (p *Package) lookupWorkspaceLocal(key string) (string, bool) {
	if p == nil || p.workspaceRoot == "" {
		return "", false
	}
	ws, _ := p.LoadWorkspace()
	dir, found, err := resolveWorkspaceChain(key, p.workspaceRoot, ws, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return "", false
	}
	return dir, found
}

// resolveWorkspaceChain 遞迴解析依賴鍵通過多層 workspace.jsonc 映射。
//
// 當依賴鍵在當前 workspace.jsonc 中找到映射後，檢查目標目錄是否自帶 workspace.jsonc。
// 若有，則繼續在同一鍵的基礎上查找，形成自然的解析鏈。
//
// 這是 Nolang 的核心差異化能力：被依賴的包可以自帶 workspace.jsonc，
// 內部繼續存在映射規則，形成自然的跨包解析鏈。
// 適用場景：基礎庫統一內部別名、標準化導入路徑、多層庫兼容遷移。
// 主流 Go/Cargo 不具備此能力，它們的 replace/patch 僅生效於當前項目。
//
// 使用 visitStack 跟蹤已訪問的工作區根目錄，防止循環映射。
// 一旦檢測到循環（如 A→B, B→A），立刻報錯並輸出完整鏈路。
//
// 參數:
//   - key: 依賴鍵（如 "github.com/foo/bar" 或短名稱 "bar"）
//   - workspaceRoot: 當前層的工作區根目錄路徑
//   - ws: 預載入的 workspace 映射（僅第一層使用，可為 nil）
//   - visitStack: 已訪問的工作區根目錄棧（用於循環檢測）
//
// 返回:
//   - localDir: 最終解析到的本地目錄絕對路徑
//   - found: 是否找到映射
//   - err: 循環映射等錯誤
func resolveWorkspaceChain(key, workspaceRoot string, ws WorkspaceMap, visitStack []string) (string, bool, error) {
	// 循環檢測：檢查當前工作區根目錄是否已在訪問棧中
	for i, visited := range visitStack {
		if visited == workspaceRoot {
			chain := strings.Join(visitStack[i:], " → ") + " → " + workspaceRoot
			return "", false, fmt.Errorf("circular workspace mapping detected: %s", chain)
		}
	}

	// 使用預載入的映射（第一層），或從磁碟載入
	currentWs := ws
	if currentWs == nil {
		loaded, err := loadWorkspaceMap(workspaceRoot)
		if err != nil || loaded == nil {
			return "", false, nil
		}
		currentWs = loaded
	}

	// 單層查找（短名稱匹配 → 直接鍵匹配）
	localDir, found := lookupWorkspaceMap(currentWs, key, workspaceRoot)
	if !found {
		return "", false, nil
	}

	// 檢查目標目錄是否有自己的 workspace.jsonc（遞迴解析）
	nestedWsFile := filepath.Join(localDir, "workspace.jsonc")
	if _, err := os.Stat(nestedWsFile); err == nil {
		// 遞迴：在同一鍵的基礎上繼續查找
		deeperDir, deeperFound, err := resolveWorkspaceChain(key, localDir, nil, append(visitStack, workspaceRoot))
		if err != nil {
			return "", false, err
		}
		if deeperFound {
			return deeperDir, true, nil
		}
	}

	// 沒有更深的映射，返回當前解析結果
	return localDir, true, nil
}

// ResolveDependencyModule 解析依賴中的模組完整路徑
// 返回模組 .no 檔案的絕對路徑
func (p *Package) ResolveDependencyModule(importPath string) (string, error) {
	key, _, ok := p.MatchDependency(importPath)
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

// isWorkspaceLocalDep 判斷依賴鍵是否為本地套件
// 如果依賴鍵為本地路徑形式（以 "./" 或 "/" 開頭），或在 workspace.jsonc 中有匹配（含遞迴），則返回 true
func (p *Package) isWorkspaceLocalDep(key string) bool {
	if isLocalPathDep(key) {
		return true
	}
	ws, _ := p.LoadWorkspace()
	_, found, err := resolveWorkspaceChain(key, p.workspaceRoot, ws, nil)
	if err != nil {
		return true // 循環映射 = 本地套件（但有問題）
	}
	return found
}

// resolveFromScratch 從頭解析所有依賴（無鎖檔案）
func (p *Package) resolveFromScratch(graph *DependencyGraph, maxDepth int) (*DependencyGraph, error) {
	for key, version := range p.Dependencies {
		// 跳過標準庫依賴
		if isStdDependency(key) {
			continue
		}
		// 跳過 workspace 本地套件，由 transpiler 在編譯時解析
		if p.isWorkspaceLocalDep(key) {
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
		// 跳過 workspace 本地套件，由 transpiler 在編譯時解析
		if p.isWorkspaceLocalDep(key) {
			continue
		}

		keyWithVer := key + "@" + version
		lockPkg, exists := p.lockFile.Packages[keyWithVer]
		if !exists {
			// 鎖檔案中沒有此依賴，回退到從頭解析
			return p.resolveFromScratch(graph, maxDepth)
		}

		// 從快取或下載取得套件目錄及壓縮包 SHA256
		pkgDir, downloadHash, err := DownloadPackage(key, version, p.Mirrors)
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
