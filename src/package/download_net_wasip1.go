//go:build wasip1

package pkg

import "github.com/lizongying/nolang/i18n"

// DownloadPackage 在 wasip1 上是報錯樁。
//
// 瀏覽器沙箱沒有可用的 socket，套件下載在 playground 裡本來就沒有意義；
// 更關鍵的是不能為它把 net/http 鏈進 wasip1 產物——那會帶進
// sock_accept / sock_shutdown 兩個 wasmimport，而 @wasmer/wasi 沒有實作，
// `WebAssembly.Instance` 直接 LinkError，整個執行鏈路都會失效。
// 真實在 download_net.go（`!wasip1`）。
func DownloadPackage(key, version string, mirrors []string) (pkgDir string, archiveHash string, err error) {
	return "", "", i18n.Errorf("7p1k0dke", "package download not supported in WASM build (browser playground)")
}
