package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LockFileVersion 是當前鎖檔案格式版本
const LockFileVersion = "1"

// LockFile 表示 nolang.lock.json 的結構
type LockFile struct {
	Version  string              `json:"version"`  // 鎖檔案格式版本
	Packages map[string]LockPkg  `json:"packages"` // key@version → 詳細資訊
}

// LockPkg 表示鎖檔案中的一個套件
type LockPkg struct {
	Key          string            `json:"key"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"` // 子依賴 key → version
	Integrity    string            `json:"integrity,omitempty"` // SHA256 可選
}

// lockFilePath 返回 nolang.lock.json 的路徑
func lockFilePath(dir string) string {
	return filepath.Join(dir, "nolang.lock.json")
}

// LoadLockFile 載入 nolang.lock.json
func LoadLockFile(dir string) (*LockFile, error) {
	path := lockFilePath(dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	// 鎖檔案使用標準 JSON（無註解）
	var lock LockFile
	if err := json.Unmarshal(raw, &lock); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if lock.Version != LockFileVersion {
		return nil, fmt.Errorf("unsupported lock file version %q (expected %s)", lock.Version, LockFileVersion)
	}

	return &lock, nil
}

// SaveLockFile 保存 nolang.lock.json
func SaveLockFile(dir string, graph *DependencyGraph) error {
	if graph == nil || len(graph.resolved) == 0 {
		return nil
	}

	lock := &LockFile{
		Version:  LockFileVersion,
		Packages: make(map[string]LockPkg),
	}

	for key, node := range graph.resolved {
		deps := make(map[string]string)
		for _, child := range node.Dependencies {
			deps[child.Key] = child.Version
		}

		pkg := LockPkg{
			Key:          node.Key,
			Version:      node.Version,
			Dependencies: deps,
		}

		// 可選：計算完整性
		if node.PkgDir != "" {
			if integrity, err := dirSHA256(node.PkgDir); err == nil {
				pkg.Integrity = integrity
			}
		}

		lock.Packages[key] = pkg
	}

	path := lockFilePath(dir)
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling lock file: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// CheckLockFile 檢查鎖檔案是否與 mod.jsonc 相容
// 返回 (是否需要重新解析, 錯誤)
// 相容條件：
//   - mod.jsonc 中所有的 dependencies 都在鎖檔案中
//   - 版本號一致
func CheckLockFile(pkg *Package, lock *LockFile) (bool, error) {
	if pkg == nil || lock == nil {
		return true, nil
	}
	if len(pkg.Dependencies) == 0 {
		return false, nil
	}

	for key, version := range pkg.Dependencies {
		keyWithVer := key + "@" + version
		if _, exists := lock.Packages[keyWithVer]; !exists {
			// 鎖檔案中缺少此依賴
			return true, nil
		}
	}

	// 所有依賴都在鎖檔案中
	return false, nil
}

// dirSHA256 計算目錄中所有檔案的 SHA256 雜湊值
func dirSHA256(dir string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyIntegrity 驗證目錄的完整性是否與預期值匹配
func VerifyIntegrity(dir, expectedHash string) (bool, error) {
	if expectedHash == "" {
		return true, nil
	}
	actual, err := dirSHA256(dir)
	if err != nil {
		return false, err
	}
	return actual == expectedHash, nil
}