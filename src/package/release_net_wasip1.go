//go:build wasip1

package pkg

import "fmt"

// wasip1 側的 GitHub Releases 訪問都是報錯樁。
//
// 原因有兩層：能力上，瀏覽器沙箱沒有 socket，安裝原生工具本就無從落地；
// 鏈接上，真實現在 release_net.go（`!wasip1`）裡用了 net/http，會把
// sock_accept / sock_shutdown 這兩個 wasmimport 帶進產物，而 @wasmer/wasi
// 沒有實作它們 ⇒ `WebAssembly.Instance` 直接 LinkError ⇒ 整個 wasm 程式
// （playground 的編譯器 / 語言服務）連啟動都做不到。
// 所以這裡只保留簽名相同的樁，讓 wasip1 產物的導入表裡只剩檔案系統類函式。
//
// 訊息刻意不掛 i18n ID：這個分支只在瀏覽器沙箱裡可達，而每一個新 ID 都要
// 同步十個語系的 .noml（見 i18n/catalog 的 en 新鮮度與全語系覆蓋門禁），
// 為一條用戶永遠看不到的錯誤加十檔翻譯不划算。

func LatestReleaseTag(host, owner, repo string) (string, error) {
	return "", fmt.Errorf("network access is not available in WASM build (browser playground)")
}

func DownloadReleaseAsset(url string) ([]byte, error) {
	return nil, fmt.Errorf("network access is not available in WASM build (browser playground)")
}
