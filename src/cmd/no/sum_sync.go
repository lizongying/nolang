package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/lizongying/nolang/mmast"
	pkg "github.com/lizongying/nolang/package"
)

// refreshSumFile 依目前 package.noml 的依賴集合全量重建 sum.noml。
//
// 條目 key 是 "<key>@<version>"，與 pkg.CheckSumFile 的查法一致。舊 sum.noml
// 中同 key 的 SHA256 會保留（源碼依賴的 hash 由 EnsureDependencies 寫入，這裡
// 不重算）；AST 依賴的 SHA256 取 <tool>-ast.zip 快取檔的內容，快取不存在才下載。
//
// 全量重建（而非增量追加）是必須的：remove 掉的依賴、update 前的舊版本鍵都要
// 從 sum.noml 消失，否則 sum 與 manifest 會漂移。
func refreshSumFile(deps map[string]string) error {
	old, _ := pkg.LoadSumFile(".")
	packages := make(map[string]pkg.SumPkg, len(deps))
	for key, version := range deps {
		name := key + "@" + version
		entry := pkg.SumPkg{Key: key, Version: version}
		if old != nil {
			if prev, ok := old.Packages[name]; ok {
				entry.SHA256 = prev.SHA256
			}
		}
		if pkg.IsASTDependencyKey(key) {
			if hash, err := astZipSHA256(key, version); err != nil {
				// 離線 / 下載失敗不阻斷 add/remove/update：保留舊 hash，
				// sum.noml 仍然寫出，下次 sync 或 build 可補齊。
				fmt.Printf("Warning: failed to compute checksum for %s: %v\n", name, err)
			} else if hash != "" {
				entry.SHA256 = hash
			}
		}
		packages[name] = entry
	}
	return pkg.SaveSumPackages(".", packages)
}

// refreshSumAfterChange 在 add/remove/update 改完 package.noml 後同步 sum.noml。
// 寫檔失敗只警告，不讓依赖管理命令本身失敗（manifest 已經寫入）。
func refreshSumAfterChange(deps map[string]string) {
	if err := refreshSumFile(deps); err != nil {
		fmt.Printf("Warning: failed to update %s: %v\n", pkg.SumFileName, err)
	}
}

// astZipSHA256 回傳 AST 依賴 <key>@<version> 的 -ast.zip SHA256。
// 版本尚未解析（""/"*"）時回傳空字串；快取命中直接讀檔，不重複解碼 gob。
func astZipSHA256(key, version string) (string, error) {
	if version == "" || version == "*" {
		return "", nil
	}
	spec, err := pkg.ParseToolSpec(key)
	if err != nil {
		return "", err
	}
	cache := mmast.ASTZipCachePath(spec, version)
	data, err := os.ReadFile(cache)
	if err != nil {
		// FetchModuleSet 內部處理快取目錄、下載與寫入；這裡只deal 快取 miss。
		if _, err := mmast.FetchModuleSet(spec, version); err != nil {
			return "", err
		}
		data, err = os.ReadFile(cache)
		if err != nil {
			return "", err
		}
	}
	if len(data) == 0 {
		return "", fmt.Errorf("empty ast zip cache: %s", cache)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}
