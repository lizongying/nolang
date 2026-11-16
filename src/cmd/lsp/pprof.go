//go:build !wasip1

package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
)

// startPprofServer 按需启动 pprof HTTP 服务：NOLANG_LSP_PPROF=<addr> 时在 addr 上
// 提供 /debug/pprof，用于压测定位常驻缓存增长（默认不监听，零开销）。
//
// 这个文件刻意带 `!wasip1` 约束：net/http 会把 Go 的 socket 系 wasmimport
// （sock_accept / sock_shutdown）链进 wasip1 产物，而浏览器里的 WASI 实现
// （@wasmer/wasi）没有这两个导入，实例化会直接 LinkError——
// "Import #28 wasi_snapshot_preview1.sock_accept: function import requires a
// callable"，整片语言服务随之初始化失败。浏览器 playground 里没有真实 socket，
// 所以 wasip1 侧用同名的空实现（pprof_wasip1.go）替掉，产物里就再无 socket 导入。
func startPprofServer() {
	addr := os.Getenv("NOLANG_LSP_PPROF")
	if addr == "" {
		return
	}
	go func() {
		log.Printf("pprof listening on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("pprof server: %v", err)
		}
	}()
}
