package pkg

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lizongying/nolang/i18n"
)

// SumFileVersion 是當前總和檔案格式版本
const SumFileVersion = "1"

// sumFileHeader 寫在 sum.noml 第一行，提醒這是工具生成檔。
const sumFileHeader = "; auto-generated, do not edit manually\n"

// SumFile 表示 sum.noml 的結構
type SumFile struct {
	Version  string            `json:"version"`  // 總和檔案格式版本
	Packages map[string]SumPkg `json:"packages"` // key@version → SHA256 資訊
}

// SumPkg 表示總和檔案中的一個套件
type SumPkg struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"` // 目錄中所有檔案的 SHA256 雜湊值
}

// sumFilePath 返回 sum.noml 的路徑
func sumFilePath(dir string) string {
	return filepath.Join(dir, SumFileName)
}

// encodeSumPackage 把 (key, version) 編成 sum.noml 的 package 欄位。
// AST 鍵 `<tool>@<host>/<owner>/<repo>` 寫成 `<tool>:<version>@<host>/<owner>/<repo>`，
// 其餘鍵寫成 `<key>:<version>`；版本為空或 '*' 時省略版本段。
func encodeSumPackage(key, version string) string {
	if version == "" || version == defaultDependencySpec {
		return key
	}
	if at := strings.Index(key, "@"); at > 0 && strings.Count(key[at+1:], "/") >= 2 {
		return key[:at] + ":" + version + key[at:]
	}
	return key + ":" + version
}

// decodeSumPackage 把 sum.noml 的 package 欄位還原成 (key, version)。
// 先用 ParseToolSpec 認 `<tool>:<version>@<host>/<owner>/<repo>`（對所有 tool 名通用），
// 再退回 SplitDepSpec 處理普通 `<key>:<version>`。版本省略時補回 '*'。
func decodeSumPackage(spec string) (string, string) {
	if spec == "" {
		return "", ""
	}
	if s, err := ParseToolSpec(spec); err == nil {
		ver := s.Version
		if ver == "" {
			ver = defaultDependencySpec
		}
		return s.DepKey(), ver
	}
	key, ver := SplitDepSpec(spec)
	if ver == "" {
		ver = defaultDependencySpec
	}
	return key, ver
}

// LoadSumFile 載入 sum.noml（新陣列格式；舊 map 格式仍可向後相容讀取）
func LoadSumFile(dir string) (*SumFile, error) {
	path := sumFilePath(dir)
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

	sum := &SumFile{Version: SumFileVersion, Packages: make(map[string]SumPkg)}
	if v, ok := m["version"].(string); ok {
		sum.Version = v
	}

	switch pkgs := m["packages"].(type) {
	case []any:
		// 新格式：每項 { package: '<spec>', sha256: '<hex>' }
		for _, elem := range pkgs {
			obj, ok := nomlAsPlainMap(elem)
			if !ok {
				continue
			}
			spec, _ := obj["package"].(string)
			sha, _ := obj["sha256"].(string)
			key, ver := decodeSumPackage(spec)
			if key == "" {
				continue
			}
			sum.Packages[key+"@"+ver] = SumPkg{Key: key, Version: ver, SHA256: sha}
		}
	default:
		// 舊格式：<key@version> → { key, version, sha256 }
		// 外層鍵帶引號時是 NomlMap，裸鍵時是 map[string]any，兩者都要認。
		if plain, ok := nomlAsPlainMap(m["packages"]); ok {
			for name, val := range plain {
				obj, ok := nomlAsPlainMap(val)
				if !ok {
					continue
				}
				sp := SumPkg{}
				sp.Key, _ = obj["key"].(string)
				sp.Version, _ = obj["version"].(string)
				sp.SHA256, _ = obj["sha256"].(string)
				sum.Packages[name] = sp
			}
		}
	}

	if sum.Version != SumFileVersion {
		return nil, i18n.Errorf("39o3yr8x", "unsupported sum file version %q (expected %s)", sum.Version, SumFileVersion)
	}

	return sum, nil
}

// SaveSumPackages 以新格式寫出 sum.noml：packages 是陣列，每項只有
// package（含版本的完整 spec）與 sha256；首行附 auto-generated 註解。
// 條目按 package 字串排序輸出，保證可重現。
func SaveSumPackages(dir string, packages map[string]SumPkg) error {
	sum := &SumFile{Version: SumFileVersion, Packages: packages}
	type entry struct {
		name string
		sha  string
	}
	list := make([]entry, 0, len(packages))
	for _, sp := range packages {
		list = append(list, entry{name: encodeSumPackage(sp.Key, sp.Version), sha: sp.SHA256})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })

	entries := make([]any, 0, len(list))
	for _, e := range list {
		entries = append(entries, map[string]any{
			"package": e.name,
			"sha256":  e.sha,
		})
	}
	data, err := MarshalNoml(map[string]any{
		"version":  sum.Version,
		"packages": entries,
	})
	if err != nil {
		return i18n.Errorf("pibk34ed", "marshaling sum file: %w", err)
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data += "\n"
	}
	path := sumFilePath(dir)
	if err := os.WriteFile(path, []byte(sumFileHeader+data), 0644); err != nil {
		return i18n.Errorf("zae1l26t", "writing %s: %w", path, err)
	}
	return nil
}

// SaveSumFile 保存 sum.noml（SHA256 來自下載的壓縮包）
func SaveSumFile(dir string, graph *DependencyGraph) error {
	if graph == nil || len(graph.resolved) == 0 {
		return nil
	}

	packages := make(map[string]SumPkg, len(graph.resolved))
	for key, node := range graph.resolved {
		pkg := SumPkg{
			Key:     node.Key,
			Version: node.Version,
		}
		// 使用下載壓縮包的 SHA256（來自 DependencyNode.DownloadHash）
		if node.DownloadHash != "" {
			pkg.SHA256 = node.DownloadHash
		}
		packages[key] = pkg
	}
	return SaveSumPackages(dir, packages)
}

// VerifySumFile 驗證 sum.noml 中的 SHA256 是否與快取中的壓縮包匹配
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

// CheckSumFile 檢查總和檔案是否與 package.noml 相容
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
