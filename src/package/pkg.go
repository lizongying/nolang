package pkg

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lizongying/nolang/i18n"
)

// manifestWarned 讓同一份 package.noml 的欄位集警告在單一 no 進程裡只印一次：
// LoadPackage 會被簽名表 / LSP / builder 反覆呼叫，重複警告只會淹沒使用者。
var manifestWarned sync.Map

// CompilerOptions 表示 package.noml 的 compiler 區塊選項
type CompilerOptions struct {
	AnonymousFnType bool   `json:"anonymous-fn-type"`
	Version         string `json:"version,omitempty"`
	// Emit 控制輸出目標後端："js" 表示使用 JS 後端發射 JavaScript；
	// 空字串（預設）表示使用 LLVM 後端生成原生可執行檔。
	Emit     string   `json:"emit,omitempty"`
	LinkLibs []string `json:"link-libs,omitempty"`
	// OptionPayloadInlineThreshold 是 `?T` 载荷内联的字节阈值，只按**载荷**计，
	// 不含 8 字节标签区（见 src/mir/layout.go：`%option_<key> = { i8 tag, [K x i64] }`）。
	// 默认 256：载荷不超过 256 字节就内联，`?txt`（256 字节缓冲）正好在边界内；
	// `?str`（16 字节描述符）与 `?vec`（16 字节）更不在话下。最低 8（一个载荷
	// 字），低于 8 会被夹到 8；要内联 16 字节的 ?str 需阈值 >= 16。err 载荷恒装箱。
	// 这是可选字段：未配置时直接用默认值，package.noml 不需要写出来。
	OptionPayloadInlineThreshold int `json:"option-payload-inline-threshold,omitempty"`
	// ESMCDN 是 browser 模式下第三方 ESM 導入（# npm/…）的 import map CDN
	// 前綴；空字串表示使用內建預設 https://esm.sh。
	ESMCDN string `json:"esm-cdn,omitempty"`
	// Lang 是本專案預設的消息語言（`compiler.lang = 'zh-tw'`）。空字串表示
	// 跟隨環境；--lang 與 NO_LANG 永遠優先於它，見 i18n.Resolve 的優先級。
	// 值的小寫中劃線形式與 --lang 相同，`zh_TW` 這類寫法由 i18n 正規化。
	Lang string `json:"lang,omitempty"`
}

// Package 表示 package.noml 定義的專案套件
type Package struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Category     string   `json:"category,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`
	Author       string   `json:"author,omitempty"`
	Email        string   `json:"email,omitempty"`
	Organization string   `json:"organization,omitempty"`
	Repository   string   `json:"repository,omitempty"`
	Homepage     string   `json:"homepage,omitempty"`
	License      string   `json:"license,omitempty"`
	// Publish 控制 nohub 收录时是否公开此包；缺省 true（与 nohub 索引一致）。
	Publish         bool              `json:"publish"`
	Dependencies    map[string]string `json:"dependencies,omitempty"`
	DevDependencies map[string]string `json:"dev-dependencies,omitempty"`
	Ignore          []string          `json:"ignore,omitempty"`
	Workspace       string            `json:"workspace,omitempty"` // 已廢棄：workspace 根目錄現由 workspace.noml 自動偵測
	Mirrors         []string          `json:"mirrors,omitempty"`   // 下載鏡像清單（依序嘗試）
	Compiler        CompilerOptions   `json:"compiler,omitempty"`
	Output          string            `json:"output,omitempty"` // 輸出目錄（如 "./dist"）
	RootDir         string            // 套件根目錄（含 package.noml）
	workspaceRoot   string            // 解析後的絕對工作區根目錄路徑
	wsMap           WorkspaceMap      // 快取的 workspace.noml 映射
	warned          bool              // 是否已輸出過 workspace 版本警告
	lockFile        *LockFile         // 已載入的鎖檔案（可選）
	sumFile         *SumFile          // 已載入的總和檔案（可選）
	depGraph        *DependencyGraph  // 已解析的依賴圖（可選）
}

// StripJSONC 已移除：配置檔格式硬切 noml（見 noml.go）。

// IsIgnored 判斷給定的**檔案**路徑是否匹配 ignore 列表中的任一模式。
//
// 支援以下模式格式（一律相對套件根目錄、以 / 分隔）：
//   - 檔名精確匹配：如 "main.no" 匹配任意目錄下的 main.no
//   - Glob 萬用字元：如 "*.no"、"*_skip.no" 匹配檔名
//   - 相對路徑匹配：如 "src/utils.no" 匹配套件根目錄下的相應路徑
//   - 目錄前綴匹配：如 "tests/" 或 "tests" 匹配該目錄下所有檔案
//   - 路徑／目錄 Glob：如 "src/*.no"、"*_skip"、"src/*_skip" 匹配相對路徑或任一上層目錄
//
// filePath 可以是絕對路徑或相對路徑；相對於 RootDir 的路徑直接匹配，絕對路徑
// 會先轉成相對於套件根目錄的路徑。
func (p *Package) IsIgnored(filePath string) bool {
	return p.isIgnored(filePath, false)
}

// IsIgnoredDir 與 IsIgnored 相同，差別只在於 path 本身是目錄：以 / 結尾的模式
// （如 "dist/"）會把該目錄本身也視為命中，方便 WalkDir 直接 SkipDir。
func (p *Package) IsIgnoredDir(dirPath string) bool {
	return p.isIgnored(dirPath, true)
}

// IsIgnoredSourceDir 在持有 Package 的走訪點（如 builder 掃 tests/）沿用同一條
// 「底線開頭目錄 = 忽略」慣例，免得呼叫端為了取用上層套件函式而把 pkg 匯入
// 名稱別名化。語意同 IsIgnoredSourceDir。
func (p *Package) IsIgnoredSourceDir(name string) bool {
	return IsIgnoredSourceDir(name)
}

func (p *Package) isIgnored(filePath string, isDir bool) bool {
	if p == nil || len(p.Ignore) == 0 {
		return false
	}

	relPath := filepath.ToSlash(filepath.Clean(filePath))
	if filepath.IsAbs(filePath) {
		if p.RootDir != "" {
			if rel, err := filepath.Rel(p.RootDir, filePath); err == nil {
				relPath = filepath.ToSlash(rel)
			}
		} else {
			relPath = strings.TrimPrefix(relPath, "/")
		}
	}
	relPath = strings.TrimPrefix(relPath, "./")
	if relPath == "." {
		relPath = ""
	}
	base := pathpkg.Base(relPath)

	for _, raw := range p.Ignore {
		if p.matchIgnorePattern(raw, relPath, base, isDir) {
			return true
		}
	}
	return false
}

func (p *Package) matchIgnorePattern(raw, relPath, base string, isDir bool) bool {
	pattern := filepath.ToSlash(strings.TrimSpace(raw))
	pattern = strings.TrimPrefix(pattern, "./")
	if pattern == "" {
		return false
	}
	dirOnly := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "./")
	if pattern == "" {
		return false
	}

	match := func(s string) bool {
		if s == "" {
			return false
		}
		ok, err := pathpkg.Match(pattern, s)
		return err == nil && ok
	}

	// 精確／Glob 命中檔案或目錄本身。以 / 結尾的模式不命中同名檔案。
	if (match(base) && (!dirOnly || isDir)) || (match(relPath) && (!dirOnly || isDir)) {
		return true
	}
	if (!dirOnly || isDir) && relPath == pattern {
		return true
	}

	// 前綴命中整棵子樹：tests/ 或 tests 都覆蓋 tests/a.no。
	if strings.HasPrefix(relPath, pattern+"/") {
		return true
	}

	// 任一上層目錄命中（含萬用目錄，如 *_skip / src/*_skip）。
	for dir := pathpkg.Dir(relPath); dir != "." && dir != "/" && dir != ""; dir = pathpkg.Dir(dir) {
		if match(dir) || match(pathpkg.Base(dir)) || dir == pattern || strings.HasPrefix(dir, pattern+"/") {
			return true
		}
	}
	return false
}

// LoadPackage 從 dir 目錄尋找並解析 package.noml
func LoadPackage(dir string) (*Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// 向上尋找 package.noml
	root := abs
	for {
		candidate := filepath.Join(root, PackageManifestName)
		if _, err := os.Stat(candidate); err == nil {
			raw, err := os.ReadFile(candidate)
			if err != nil {
				return nil, i18n.Errorf("k1h5kums", "reading %s: %w", candidate, err)
			}
			warnManifestFieldIssues(candidate, CheckManifestFields(raw))
			m, err := ParseNomlMap(raw)
			if err != nil {
				return nil, i18n.Errorf("o9bv21zu", "parsing %s: %w", candidate, err)
			}
			// publish 缺省 true（與 nohub 索引一致）；DecodeInto 只會在鍵存在時覆寫。
			pkg := Package{Publish: true}
			if err := DecodeInto(m, &pkg); err != nil {
				return nil, i18n.Errorf("o9bv21zu", "parsing %s: %w", candidate, err)
			}
			pkg.RootDir = root

			// 自動偵測 workspace 根目錄：向上搜尋 workspace.noml
			wsRoot := root
			for {
				candidate := filepath.Join(wsRoot, WorkspaceFileName)
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

			// 快取 workspace.noml 映射，避免後續重複讀取磁碟
			if pkg.workspaceRoot != "" {
				pkg.wsMap, _ = loadWorkspaceMap(pkg.workspaceRoot)
			}
			// 警告依賴版本約束不當（本地包應用 *，線上包應用版本號）
			pkg.warnWorkspaceDepVersion()

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

// warnManifestFieldIssues 在載入期回報 package.noml 的缺欄位／多欄位。
// 非致命：舊專案仍可 build，但會被提醒跑 no fmt -w 讓欄位集對齊。
func warnManifestFieldIssues(path string, issues ManifestFieldIssues) {
	if issues.Empty() {
		return
	}
	if _, seen := manifestWarned.LoadOrStore(path, true); seen {
		return
	}
	fmt.Fprintf(os.Stderr, "Warning: %s: package.noml field set: %s; run 'no fmt -w %s' to fix\n",
		path, issues.Describe(), filepath.Dir(path))
}

// CompilerLang returns the `compiler.lang` declared by the nearest package.noml
// at or above dir, or "" when there is no manifest or the field is unset.
//
// It deliberately does NOT go through LoadPackage. That function also resolves
// the workspace map, the lock file and the sum file — none of which choosing a
// message language needs — and this runs before i18n is initialised, i.e. on
// every `no` invocation, so the startup cost has to stay at "read one small
// file". A manifest that fails to parse returns "" rather than an error: the
// command about to run will report the parse failure itself, and a broken
// config must not make `no` unable to print its own diagnostics.
//
// The value is returned as written; i18n normalises it (case, `_` vs `-`) the
// same way it normalises --lang and $LANG.
func CompilerLang(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for {
		if data, readErr := os.ReadFile(filepath.Join(abs, PackageManifestName)); readErr == nil {
			m, parseErr := ParseNomlMap(data)
			if parseErr != nil {
				return ""
			}
			comp, _ := nomlAsPlainMap(m["compiler"])
			lang, _ := comp["lang"].(string)
			return strings.TrimSpace(lang)
		}
		parent := filepath.Dir(abs)
		if parent == abs { // reached the filesystem root
			return ""
		}
		abs = parent
	}
}

// WorkspaceRoot 返回工作區根目錄（workspace.noml 所在目錄）。
// 若未偵測到 workspace.noml 則返回空字串。
func (p *Package) WorkspaceRoot() string {
	if p == nil {
		return ""
	}
	return p.workspaceRoot
}

// ResolvePath 把套件內的相對路徑解析到套件根目錄。
// package.noml 不再提供 alias，路徑一律相對 RootDir。
func (p *Package) ResolvePath(inputPath string) string {
	if p == nil {
		return inputPath
	}
	return filepath.Clean(filepath.Join(p.RootDir, inputPath))
}

// ResolveOutputDir 回傳套件的輸出目錄（絕對或相對目前工作目錄）。
//
// output 是可選欄位，且一律以 workspace 根目錄為基準：`/tests/dist` 與
// `./dist` 的前綴都会去掉再接到 workspace 根。省略时默认当前包目录下的
// dist（例如 tests/package.noml → <workspace>/tests/dist，根包 →
// <workspace>/dist）。找不到 workspace.noml 时退回套件根目录。
func (p *Package) ResolveOutputDir() string {
	if p == nil {
		return "dist"
	}

	output := strings.TrimSpace(p.Output)
	if output == "" {
		rel := "dist"
		if p.workspaceRoot != "" && p.RootDir != "" {
			if r, err := filepath.Rel(p.workspaceRoot, p.RootDir); err == nil && r != "." &&
				r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				rel = filepath.Join(r, "dist")
			}
		}
		if p.workspaceRoot == "" && p.RootDir != "" {
			return filepath.Clean(filepath.Join(p.RootDir, rel))
		}
		if p.workspaceRoot == "" {
			return filepath.Clean(rel)
		}
		return filepath.Clean(filepath.Join(p.workspaceRoot, rel))
	}

	// 显式 output 也相对 workspace：前导 / 或 ./ 只是写法差异。
	output = strings.TrimPrefix(output, "./")
	output = strings.TrimPrefix(output, "/")
	output = filepath.FromSlash(output)
	if output == "" {
		output = "dist"
	}
	if p.workspaceRoot == "" && p.RootDir != "" {
		return filepath.Clean(filepath.Join(p.RootDir, output))
	}
	return filepath.Clean(filepath.Join(p.workspaceRoot, output))
}

// WorkspaceMap 表示 workspace.noml 解析結果
// key 為套件短名稱，value 為相對於 workspaceRoot 的本地路徑
type WorkspaceMap map[string]string

// loadWorkspaceMap 從磁碟讀取並解析 workspace.noml，同時合併 .workspace.noml。
//
// 加載順序：先加載公共配置 workspace.noml，再加載私有配置 .workspace.noml。
// 相同的 key，私有配置覆蓋公共配置；新 key 相互疊加。
// 這分離了「工程標準化配置」與「個人本地調試配置」，避免臨時 fork 映射污染項目公共配置。
func loadWorkspaceMap(workspaceRoot string) (WorkspaceMap, error) {
	// 1. 加載公共配置 workspace.noml
	wsFile := filepath.Join(workspaceRoot, WorkspaceFileName)
	raw, err := os.ReadFile(wsFile)
	if err != nil {
		return nil, nil // workspace.noml 可選
	}

	m, err := ParseNomlMap(raw)
	if err != nil {
		return nil, i18n.Errorf("o9bv21zu", "parsing %s: %w", wsFile, err)
	}
	var ws WorkspaceMap
	if err := DecodeInto(m, &ws); err != nil {
		return nil, i18n.Errorf("o9bv21zu", "parsing %s: %w", wsFile, err)
	}
	if ws == nil {
		ws = make(WorkspaceMap)
	}

	// 2. 加載私有配置 .workspace.noml（可選，覆蓋公共配置）
	privateFile := filepath.Join(workspaceRoot, WorkspacePrivateName)
	if privateRaw, pErr := os.ReadFile(privateFile); pErr == nil {
		privateM, err := ParseNomlMap(privateRaw)
		if err != nil {
			return nil, i18n.Errorf("o9bv21zu", "parsing %s: %w", privateFile, err)
		}
		var privateWs WorkspaceMap
		if err := DecodeInto(privateM, &privateWs); err != nil {
			return nil, i18n.Errorf("o9bv21zu", "parsing %s: %w", privateFile, err)
		}
		// 合併：私有配置覆蓋公共配置，新 key 疊加
		for k, v := range privateWs {
			ws[k] = v
		}
	}

	return ws, nil
}

// LoadWorkspace 載入 workspace.noml（優先使用快取）
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
		// 依賴鍵必須是 importPath 的前綴，且後續部分以 / 開頭。
		//
		// 預編 AST 依賴的鍵形如 `nogit@github.com/lizongying/notools`：原始碼裡寫的是
		// `use nogit`（工具名）或 `use github.com/lizongying/notools/nogit`（倉庫路徑），
		// 兩者都不是鍵的字面前綴，所以對這種鍵額外展開兩個候選前綴再比。
		for _, cand := range dependencyPrefixes(key) {
			if !strings.HasPrefix(importPath, cand) {
				continue
			}
			remainder := strings.TrimPrefix(importPath, cand)
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

// dependencyPrefixes 返回依賴鍵在 import path 上可比對的前綴集合。
// 普通鍵（github.com/o/r）就是它自己；預編 AST 鍵（tool@host/owner/repo）另外
// 展開成工具名與 host/owner/repo 兩種寫法，讓 `use nogit` 與
// `use github.com/lizongying/notools/nogit` 都能命中。
func dependencyPrefixes(key string) []string {
	at := strings.Index(key, "@")
	if at <= 0 {
		return []string{key}
	}
	tool := key[:at]
	repo := key[at+1:]
	out := []string{tool, repo}
	return out
}

// IsASTDependencyKey 報告依賴鍵是否為預編 AST 依賴（<tool>@<host>/<owner>/<repo>）。
func IsASTDependencyKey(key string) bool {
	at := strings.Index(key, "@")
	return at > 0 && strings.Count(key[at+1:], "/") >= 2
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
//   - package.noml 中的本地路徑只能以 "/" 開頭（工作區相對），
//     禁止 "./"、"../" 和作業系統絕對路徑。
//   - 唯一允許突破工作區邊界的入口是 workspace.noml / .workspace.noml 映射。
//
// 解析順序：
// 1. "/" 前綴 → 工作區相對路徑解析（嚴格限制在工作區內）
// 2. workspace.noml 中有匹配（短名稱或直接鍵）→ 遞迴解析（支援跨包映射鏈，允許逃逸工作區）
// 3. 無本地覆蓋 → 下載線上套件
func (p *Package) resolveDependency(importPath string) (string, error) {
	key, version, ok := p.MatchDependency(importPath)
	if !ok {
		return "", nil
	}

	// 禁止 "./" 和 "../" 前綴（package.noml 不允許相對跳轉）
	if strings.HasPrefix(key, "./") || strings.HasPrefix(key, "../") {
		return "", i18n.Errorf("9e82usj6", "dependency %q: relative paths (./ ../) are not allowed in package.noml; use `/` prefix for workspace-relative paths", key)
	}

	// "/" 前綴：相對於 workspace 根目錄解析（嚴格限制在工作區內）
	if strings.HasPrefix(key, "/") {
		relPath := strings.TrimPrefix(key, "/")
		localDir := filepath.Join(p.workspaceRoot, relPath)
		// 安全檢查：解析後的路徑必須在工作區根目錄內
		if !isWithinWorkspace(localDir, p.workspaceRoot) {
			return "", i18n.Errorf("h1vvbufz", "dependency %q: path escapes workspace root; package.noml dependencies must stay within the workspace", key)
		}
		if info, err := os.Stat(localDir); err == nil && info.IsDir() {
			return filepath.Clean(localDir), nil
		}
		return "", i18n.Errorf("zql6tu9p", "workspace-local dependency path %q not found", key)
	}

	// 檢查 workspace.noml 是否有本地覆蓋（遞迴解析，支援跨包映射鏈）
	// workspace.noml / .workspace.noml 是唯一允許逃逸工作區邊界的入口
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
//   - 本地包（在 workspace.noml 中存在，或依賴鍵以 "/" 開頭）：
//     應使用 "*"，若指定了版本號則警告。
//   - "./" 和 "../" 前綴的依賴鍵：報錯（不允許在 package.noml 中使用）。
//   - 線上包（不在 workspace.noml 中且非路徑形式）：
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
			fmt.Fprintf(os.Stderr, "Error: dependency %q: relative paths (./ ../) are not allowed in package.noml; use `/` prefix for workspace-relative paths\n", key)
			continue
		}
		// 先判斷是否為本地包（路徑形式或 workspace.noml 中直接匹配）
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
// 使用遞迴解析，支援被依賴包自帶 workspace.noml 的跨包映射。
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

// lookupWorkspaceMap 在 workspace.noml 中查找依賴鍵對應的本地路徑。
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
	// 2. 直接鍵匹配（workspace.noml 可直接以完整依賴鍵註冊）
	if localPath, exists := ws[key]; exists {
		if dir, ok := resolveWorkspaceMapValue(localPath, workspaceRoot); ok {
			return dir, true
		}
	}
	return "", false
}

// resolveWorkspaceMapValue 解析 workspace.noml 映射值為本地目錄。
//
// 支援三種路徑形式：
//   - 工作區相對路徑（以 "/" 開頭）：workspaceRoot + path
//   - 相對跳轉路徑（以 "./" 或 "../" 開頭）：相對於 workspaceRoot 解析，允許逃逸工作區邊界
//   - 作業系統絕對路徑：直接使用（允許指向工作區外部，用於 fork、外部組件）
//
// workspace.noml / .workspace.noml 是唯一允許突破工作區邊界的入口。
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

// resolveWorkspaceChain 遞迴解析依賴鍵通過多層 workspace.noml 映射。
//
// 當依賴鍵在當前 workspace.noml 中找到映射後，檢查目標目錄是否自帶 workspace.noml。
// 若有，則繼續在同一鍵的基礎上查找，形成自然的解析鏈。
//
// 這是 Nolang 的核心差異化能力：被依賴的包可以自帶 workspace.noml，
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
	// 循環檢檢測：檢查當前工作區根目錄是否已在訪問棧中
	for i, visited := range visitStack {
		if visited == workspaceRoot {
			chain := strings.Join(visitStack[i:], " → ") + " → " + workspaceRoot
			return "", false, i18n.Errorf("yrdjmq57", "circular workspace mapping detected: %s", chain)
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

	// 檢查目標目錄是否有自己的 workspace.noml（遞迴解析）
	nestedWsFile := filepath.Join(localDir, WorkspaceFileName)
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

// ResolveWorkspaceAlias 把 workspace.noml 中註冊的短名（或完整鍵）解析成
// 本地套件目錄。與 Package.MatchDependency 不同，此處不要求該名字也出現在
// package.noml 的 dependencies 裡 —— workspace.noml 本身就是本地包的權威映射，
// 這正是 `# test/assert` 中 `test` 能被解析到 `/test` 的依據。
//
// 解析沿用 resolveWorkspaceChain，因此支援多層 workspace.noml 映射鏈與循環
// 偵測；找不到映射時回報 ("", false)。
func ResolveWorkspaceAlias(alias, workspaceRoot string) (string, bool) {
	if alias == "" || workspaceRoot == "" {
		return "", false
	}
	dir, found, err := resolveWorkspaceChain(alias, workspaceRoot, nil, nil)
	if err != nil || !found {
		return "", false
	}
	return dir, true
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
		return "", i18n.Errorf("1qz0p808", "no module path after dependency key %s", key)
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

	return "", i18n.Errorf("c33l3vql", "module file not found: %s or %s", fullPath, srcPath)
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
// 如果依賴鍵為本地路徑形式（以 "/" 開頭），或在 workspace.noml 中有匹配（含遞迴），則返回 true
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
		// 跳過預編 AST 依賴（nogit@host/owner/repo）：發布產物是 <tool>-ast.zip，
		// 沒有原始碼套件可下載，由 transpiler 在遇到 use 時按需抓取（見 mmast.EnsureModuleAST）。
		if IsASTDependencyKey(key) {
			continue
		}
		// 跳過 workspace 本地套件，由 transpiler 在編譯時解析
		if p.isWorkspaceLocalDep(key) {
			continue
		}
		if err := graph.ResolveAll(key, version, maxDepth); err != nil {
			return nil, i18n.Errorf("3vbgvipn", "resolving dependency %s@%s: %w", key, version, err)
		}
	}

	// 循環依賴檢測
	if err := graph.DetectCycles(); err != nil {
		return nil, i18n.Errorf("ibaimz2h", "cycle detection: %w", err)
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
			return nil, i18n.Errorf("cpyd2teh", "downloading %s@%s: %w", key, version, err)
		}

		if _, err := graph.ResolveFromLock(key, version, pkgDir, downloadHash, lockPkg.Dependencies, 0, maxDepth); err != nil {
			return nil, i18n.Errorf("kmm43ypb", "resolving %s from lock: %w", key, err)
		}
	}

	// 循環依賴檢測
	if err := graph.DetectCycles(); err != nil {
		return nil, i18n.Errorf("ibaimz2h", "cycle detection: %w", err)
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
