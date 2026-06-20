package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SumFileVersion 是當前總和檔案格式版本
const SumFileVersion = "1"

// SumFile 表示 sum.jsonc 的結構
type SumFile struct {
	Version  string             `json:"version"`   // 總和檔案格式版本
	Packages map[string]SumPkg  `json:"packages"`  // key@version → SHA256 資訊
}

// SumPkg 表示總和檔案中的一個套件
type SumPkg struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"` // 目錄中所有檔案的 SHA256 雜湊值
}

// sumFilePath 返回 sum.jsonc 的路徑
func sumFilePath(dir string) string {
	return filepath.Join(dir, "sum.jsonc")
}

// LoadSumFile 載入 sum.jsonc
func LoadSumFile(dir string) (*SumFile, error) {
	path := sumFilePath(dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	// 總和檔案使用標準 JSON（無註解）
	var sum SumFile
	if err := json.Unmarshal(raw, &sum); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if sum.Version != SumFileVersion {
		return nil, fmt.Errorf("unsupported sum file version %q (expected %s)", sum.Version, SumFileVersion)
	}

	return &sum, nil
}

// SaveSumFile 保存 sum.jsonc（SHA256 來自下載的壓縮包）
func SaveSumFile(dir string, graph *DependencyGraph) error {
	if graph == nil || len(graph.resolved) == 0 {
		return nil
	}

	sum := &SumFile{
		Version:  SumFileVersion,
		Packages: make(map[string]SumPkg),
	}

	for key, node := range graph.resolved {
		pkg := SumPkg{
			Key:     node.Key,
			Version: node.Version,
		}

		// 使用下載壓縮包的 SHA256（來自 DependencyNode.DownloadHash）
		if node.DownloadHash != "" {
			pkg.SHA256 = node.DownloadHash
		}

		sum.Packages[key] = pkg
	}

	path := sumFilePath(dir)
	data, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling sum file: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// VerifySumFile 驗證 sum.jsonc 中的 SHA256 是否與快取中的壓縮包匹配
func VerifySumFile(graph *DependencyGraph, sum *SumFile) (bool, error) {
	if sum == nil || graph == nil {
		return true, nil
	}

	for key, expected := range sum.Packages {
		if expected.SHA256 == "" {
			continue
		}

		node, exists := graph.resolved[key]
		if !exists || node.PkgDir == "" {
			continue
		}

		// 優先使用節點上的 DownloadHash
		actualHash := node.DownloadHash
		if actualHash == "" {
			// 無快取 hash，嘗試從快取中的壓縮包重新計算
			owner, repo, ok := parseGitHubKey(node.Key)
			if !ok {
				continue
			}
			cachePath := filepath.Join(cacheDir(), owner, repo, node.Version)
			for _, name := range []string{"_archive.tar.gz", "_archive.zip"} {
				archivePath := filepath.Join(cachePath, name)
				if hash, err := archiveHashFromFile(archivePath); err == nil {
					actualHash = hash
					break
				}
			}
		}

		if actualHash == "" {
			// 無法獲取 hash，跳過驗證
			continue
		}

		if actualHash != expected.SHA256 {
			return false, nil
		}
	}

	return true, nil
}

// CheckSumFile 檢查總和檔案是否與 mod.jsonc 相容
// 返回 (是否需要重新解析, 錯誤)
func CheckSumFile(pkg *Package, sum *SumFile) (bool, error) {
	if pkg == nil || sum == nil {
		return true, nil
	}
	if len(pkg.Dependencies) == 0 {
		return false, nil
	}

	for key, version := range pkg.Dependencies {
		keyWithVer := key + "@" + version
		if _, exists := sum.Packages[keyWithVer]; !exists {
			// 總和檔案中缺少此依賴
			return true, nil
		}
	}

	// 所有依賴都在總和檔案中
	return false, nil
}