//go:build wasip1

package main

import (
	"fmt"
	"os"
)

// serveESMAndOpen 在 wasip1 上是「不支持」桩。
//
// 浏览器沙箱既不能 listen socket，也无法在宿主机上拉起浏览器；更关键的是
// 不能为了这个功能把 net / net/http 链进 wasip1 产物——它们会带进
// sock_accept / sock_shutdown 两个 wasmimport，而 @wasmer/wasi 没有实现，
// 实例化即 LinkError，整个 playground 运行链路都会挂掉。
// 原实现在 serve_esm.go（`!wasip1`）。
//
// 訊息不掛 i18n ID：這條分支只在瀏覽器沙箱可達，而新增 ID 要同步十個語系的
// .noml（i18n/catalog 有 en 新鮮度與全語系覆蓋門禁），不值得為一條用戶看不到
// 的錯誤加十個翻譯檔。
func serveESMAndOpen(dir, htmlPath string) {
	fmt.Fprintf(os.Stderr, "Error: serving ESM output over http is not supported in WASM builds (browser playground): %s\n", htmlPath)
}
