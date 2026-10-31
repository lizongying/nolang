package pkg

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/lizongying/nolang/i18n"
)

// LockFileVersion 是當前鎖檔案格式版本
const LockFileVersion = "1"

// LockFile 表示 nolang.lock.noml 的結構
type LockFile struct {
	Version  string             `json:"version"`  // 鎖檔案格式版本
	Packages map[string]LockPkg `json:"packages"` // key@version → 詳細資訊
}

// LockPkg 表示鎖檔案中的一個套件
type LockPkg struct {
	Key          string            `json:"key"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`        // 子依賴（noml 中序列化為 'key:version' 字串陣列）
	Integrity    string            `json:"integrity,omitempty"` // SHA256 可選
}

// lockFilePath 返回 nolang.lock.noml 的路徑
func lockFilePath(dir string) string {
	return filepath.Join(dir, LockFileName)
}

// LoadLockFile 載入 nolang.lock.noml
func LoadLockFile(dir string) (*LockFile, error) {
	path := lockFilePath(dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, i18n.Errorf("k1h5kums", "reading %s: %w", path, err)
	}

	m, err := ParseNomlMap(raw)
	if err != nil {
		return nil, i18n.Errorf("o9bv21zu", "parsing %s: %w", path, err)
	}
	var lock LockFile
	if err := DecodeInto(m, &lock); err != nil {
		return nil, i18n.Errorf("o9bv21zu", "parsing %s: %w", path, err)
	}

	if lock.Version != LockFileVersion {
		return nil, i18n.Errorf("7riv9hah", "unsupported lock file version %q (expected %s)", lock.Version, LockFileVersion)
	}

	return &lock, nil
}

// SaveLockFile 保存 nolang.lock.noml
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

	data, err := marshalLock(lock)
	if err != nil {
		return i18n.Errorf("m4mu624f", "marshaling lock file: %w", err)
	}

	path := lockFilePath(dir)
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		return i18n.Errorf("zae1l26t", "writing %s: %w", path, err)
	}

	return nil
}

// marshalLock 把鎖檔案序列化為 noml 文字。dependencies 以 'key:version'
// 字串陣列落地（noml 的 map[string]string 表示法），並排序以求確定性。
func marshalLock(lock *LockFile) (string, error) {
	pkgs := make(map[string]any, len(lock.Packages))
	for name, lp := range lock.Packages {
		pkgs[name] = lockPkgToNoml(lp)
	}
	return MarshalNoml(map[string]any{
		"version":  lock.Version,
		"packages": pkgs,
	})
}

func lockPkgToNoml(lp LockPkg) map[string]any {
	m := map[string]any{
		"key":          lp.Key,
		"version":      lp.Version,
		"dependencies": depSpecArray(lp.Dependencies),
	}
	if lp.Integrity != "" {
		m["integrity"] = lp.Integrity
	}
	return m
}

// depSpecArray 把 key→version 映射轉成排序後的 'key:version' 字串陣列。
func depSpecArray(deps map[string]string) []any {
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, FormatDepSpec(k, deps[k]))
	}
	return out
}

// CheckLockFile 檢查鎖檔案是否與 package.noml 相容
// 返回 (是否需要重新解析, 錯誤)
// 相容條件：
//   - package.noml 中所有的 dependencies 都在鎖檔案中
//   - 版本號一致
func CheckLockFile(pkg *Package, lock *LockFile) (bool, error) {
	if pkg == nil || lock == nil {
		return true, nil
	}
	if len(pkg.Dependencies) == 0 {
		return false, nil
	}

	for key, version := range pkg.Dependencies {
		// 跳過 workspace 本地套件，不需要在鎖檔案中
		if pkg.isWorkspaceLocalDep(key) {
			continue
		}
		keyWithVer := key + "@" + version
		if _, exists := lock.Packages[keyWithVer]; !exists {
			// 鎖檔案中缺少此依賴
			return true, nil
		}
	}

	// 所有非 workspace 本地依賴都在鎖檔案中
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
