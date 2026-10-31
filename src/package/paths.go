package pkg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lizongying/nolang/i18n"
)

// FindWorkspaceRoot 從 start 向上查找 workspace.noml 所在的目錄。
// 找不到時回傳 ("", false)。
//
// 工作區根目錄是所有 embed / 導入路徑解析的單一基準：
// 無論源碼位於工作區內多深的子目錄，相對路徑都統一解析到這裡，
// 從根源消除「同一相對路徑因所在源檔不同而指向不同檔案」的快取碰撞。
func FindWorkspaceRoot(start string) (string, bool) {
	if start == "" {
		return "", false
	}
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "workspace.noml")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// FindPackageRoot 從 start 向上查找 package.noml 所在的目錄。
// 找不到時回傳空字串。用於沒有 workspace.noml 的專案作為退路。
func FindPackageRoot(start string) string {
	if start == "" {
		return ""
	}
	dir := filepath.Dir(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, "package.noml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ResolveToWorkspaceRoot 把源碼中書寫的導入/嵌入路徑解析為基於工作區根目錄的
// 規範絕對路徑：
//   - 絕對路徑：直接 Clean 回傳，不變動。
//   - 前置 "/" 表示「相對於工作區根目錄」，會被去除。
//   - wsRoot 為空（未偵測到工作區）時，退路為相對於當前目錄（舊行為）。
//
// 這是所有路徑解析唯一收斂的入口，編譯器不再以「當前源碼檔目錄」作為相對基準。
func ResolveToWorkspaceRoot(wsRoot, rel string) string {
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel)
	}
	rel = strings.TrimPrefix(rel, "/")
	if wsRoot == "" {
		return filepath.Clean(rel)
	}
	return filepath.Clean(filepath.Join(wsRoot, rel))
}

// ResolveEmbedBase 回傳相對 embed 路徑解析所依據的目錄：
// 工作區根目錄（優先）→ 包根目錄（package.noml）→ 源碼檔所在目錄（舊行為退路）。
//
// 優先使用工作區根目錄即符合「embed 路徑全部以工作區根目錄為基準」的約定；
// 保留包根/源碼目錄退路是為了相容沒有 workspace.noml 的舊專案，以及
// std / 內嵌模組（其 embed 應相對於自身位置，而非使用者工作區）。
func ResolveEmbedBase(sourcePath string) string {
	if sourcePath == "" {
		return ""
	}
	if ws, ok := FindWorkspaceRoot(sourcePath); ok {
		return ws
	}
	if pr := FindPackageRoot(sourcePath); pr != "" {
		return pr
	}
	return filepath.Dir(sourcePath)
}

// SourcePathSegmentRe 匹配合法的源碼路徑段：小寫字母 (a-z)、數字 (0-9) 與中劃線 (-)。
// 底線 (_)、大寫字母與其他字元皆被拒絕。
var SourcePathSegmentRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// isValidSourceSegment 檢查單一路徑段（目錄名或去副檔名的檔名）是否符合
// 源碼命名規範：只允許小寫字母、數字與中劃線，且不能以中劃線開頭/結尾、不能有連續中劃線。
// 底線與大寫字母因不在字元集內而自然被拒。
func isValidSourceSegment(name string) bool {
	if name == "" {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	if strings.Contains(name, "--") {
		return false
	}
	return SourcePathSegmentRe.MatchString(name)
}

// ValidateSourcePathName 強制執行 Nolang 源碼路徑命名規範：
// 路徑的每一段（目錄名與檔名主體；.no 副檔名不計入）都只能由小寫字母、數字與中劃線組成，
// 不得以中劃線開頭/結尾、不得出現連續中劃線。底線、大寫字母與其他字元皆直接報錯。
//
// 無論資料夾或檔案都會檢查：例如 "tmp/zz_stridx/r_index.no" 因 "zz_stridx" 與 "r_index"
// 含底線而被拒。
//
// 絕對 OS 路徑（如 /Users/me/proj/a.no）只驗證落在目前工作目錄下的部分，
// 因此 "Users" 等宿主目錄名不會觸發假陽性。
func ValidateSourcePathName(path string) error {
	rel := sourcePathRelativeToCwd(path)
	clean := filepath.ToSlash(filepath.Clean(rel))
	segs := strings.Split(clean, "/")
	for i, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		name := seg
		if i == len(segs)-1 && strings.HasSuffix(name, ".no") {
			name = strings.TrimSuffix(name, ".no")
		}
		if !isValidSourceSegment(name) {
			return i18n.Errorf("83xbz15x", "source file path is not a valid name: %s\n  the path segment %q contains disallowed characters (only lowercase letters, digits and hyphens are allowed; underscores are forbidden, and a hyphen may not lead, trail or repeat)\n  valid example: tmp/zz-stridx/r-index.no", path, seg)
		}
	}
	return nil
}

// sourcePathRelativeToCwd 把絕對路徑轉為相對於目前工作目錄的路徑；
// 若不在目前工作目錄下（宿主系統路徑），回傳空字串以跳過驗證，避免對 OS 路徑誤報。
func sourcePathRelativeToCwd(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return ""
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return rel
}
