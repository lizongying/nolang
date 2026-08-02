package pkg

import (
	"os"
	"path/filepath"
	"strings"
)

// FindWorkspaceRoot 從 start 向上查找 workspace.jsonc 所在的目錄。
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
		if _, err := os.Stat(filepath.Join(dir, "workspace.jsonc")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// FindPackageRoot 從 start 向上查找 package.jsonc 所在的目錄。
// 找不到時回傳空字串。用於沒有 workspace.jsonc 的專案作為退路。
func FindPackageRoot(start string) string {
	if start == "" {
		return ""
	}
	dir := filepath.Dir(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, "package.jsonc")); err == nil {
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
// 工作區根目錄（優先）→ 包根目錄（package.jsonc）→ 源碼檔所在目錄（舊行為退路）。
//
// 優先使用工作區根目錄即符合「embed 路徑全部以工作區根目錄為基準」的約定；
// 保留包根/源碼目錄退路是為了相容沒有 workspace.jsonc 的舊專案，以及
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
