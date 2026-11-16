//go:build wasip1

package main

// startPprofServer 在 wasip1 上是空操作。
//
// 浏览器沙箱里既没有可监听的 socket，也无法把端口暴露给宿主浏览器的开发者工具，
// 所以 pprof 服务在 wasm 构建里没有意义。更重要的是「不导入」本身：
// net/http 会把 sock_accept / sock_shutdown 这两个 wasmimport 带进产物，
// 而 @wasmer/wasi 没有实现它们，实例化会 LinkError（见 pprof.go 的说明）。
// 这里是空实现，wasip1 产物的导入表里因此不含任何 socket 函数。
func startPprofServer() {}
