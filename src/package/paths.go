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

// IsIgnoredSourceDir 回報目錄名 name 是否為「被忽略」段：必須「以底線 '_' 開頭」
// 且「去掉首個底線後的剩餘部分」符合命名規範（例如 _drafts→drafts、_tmp-vet-test→
// tmp-vet-test 皆合法，但 _tmp_vet_test 去掉首 '_' 後仍是 tmp_vet_test 含底線，非法）。
// 恰好為 "_" 的段（空名）不放行。
func IsIgnoredSourceDir(name string) bool {
	if !strings.HasPrefix(name, "_") || name == "_" {
		return false
	}
	return isValidSourceSegment(name[1:])
}

// IsIgnoredSourcePath 回報 path 是否落在「被忽略」範圍內：
//
//   - 末段是 .no 檔，且其檔名主體「以 '_' 開頭且去掉首底線後剩餘部分合法」
//     （單一草稿檔，如 tests/_jp7.no、_tmp-vet-test/no.no）；
//   - 或路徑中任一「中間段」（皆為目錄）是 IsIgnoredSourceDir（如 _drafts/x.no）。
//
// 關鍵：領先底線只是「忽略標記」，剩餘部分仍須合法；否則（如 _tmp_vet_test/no.no
// 去掉首 '_' 後仍是 tmp_vet_test，含底線）視為非法名，不忽略、照常報錯。
// 非 .no 的末段（如 _readme.md）不是源檔，不計入忽略慣例。
func IsIgnoredSourcePath(path string) bool {
	segs := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for i, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		isLast := i == len(segs)-1
		if isLast && strings.HasSuffix(seg, ".no") {
			base := strings.TrimSuffix(seg, ".no")
			// 末段 .no 檔：以 '_' 開頭、且去掉首底線後剩餘合法，才算被忽略。
			if base != "_" && strings.HasPrefix(base, "_") && isValidSourceSegment(base[1:]) {
				return true
			}
			continue
		}
		// 中間段皆為目錄：是被忽略目錄即整棵忽略。
		// 末段非 .no（如 _readme.md）不算源檔，不在此列。
		if !isLast && IsIgnoredSourceDir(seg) {
			return true
		}
	}
	return false
}

// IsIgnoredSourceFile 是 IsIgnoredSourcePath 的檔名別名：path 為 .no 檔且其檔名主體
// （或任一上層目錄）以底線開頭時回傳 true。保留以相容既有呼叫點與測試。
func IsIgnoredSourceFile(path string) bool {
	return IsIgnoredSourcePath(path)
}

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
	return validatePathSegments(path, false)
}

// ValidateMarkdownDirName 是 Markdown / MDX 檔案的路徑命名檢查：只驗證**目錄段**，
// 且跳過以 `.` 開頭的隱藏目錄（`.agents` / `.github` / `.workbuddy` 是工具鏈約定名，
// 不受源碼命名規範約束）。檔名本身不檢查 —— `README.md` / `CHANGELOG.md` 這類
// 大寫慣例名不構成模組。
func ValidateMarkdownDirName(path string) error {
	return ValidateDirPathName(filepath.Dir(path))
}

// ValidateDirPathName 校驗**目錄**路徑的每一段命名，放行隱藏目錄。
//
// 目錄不是模組，也不參與符號解析；`.agents` 這類工具鏈約定名因此不該被源碼
// 命名規範攔下（`no fmt -md .agents` 必須能跑）。
func ValidateDirPathName(path string) error {
	return validatePathSegments(path, true)
}

// validatePathSegments 是路徑段命名檢查的共同實作。skipHidden 為真時放行以 `.`
// 開頭的隱藏段；`.no` 副檔名不計入（被規範的是檔名主體）。
func validatePathSegments(path string, skipHidden bool) error {
	rel := sourcePathRelativeToCwd(path)
	clean := filepath.ToSlash(filepath.Clean(rel))
	segs := strings.Split(clean, "/")
	for i, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		if skipHidden && strings.HasPrefix(seg, ".") {
			continue
		}
		name := seg
		if i == len(segs)-1 && strings.HasSuffix(name, ".no") {
			name = strings.TrimSuffix(name, ".no")
		}
		// 底線 '_' 開頭的段是被忽略慣例的「標記」：命名校驗針對「去掉首個底線後」的
		// 剩餘部分。剩餘部分合法 → 整段是「合法但被忽略」；剩餘仍含非法字元
		// （如 _tmp_vet_test 去掉首 '_' 後仍是 tmp_vet_test，含底線）→ 照常報命名錯誤，
		// 不當成忽略。恰好為 "_" 的段去掉首底線後為空，視為非法。
		if strings.HasPrefix(name, "_") {
			name = name[1:]
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
