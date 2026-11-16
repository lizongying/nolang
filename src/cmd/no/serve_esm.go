//go:build !wasip1

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/lizongying/nolang/i18n"
)

// serveESMAndOpen serves dir over http on 127.0.0.1 (ephemeral port), opens
// htmlPath's basename in the default browser, and blocks until interrupted.
// Browser ESM output needs an http origin: module scripts (and the import map
// resolving bare specifiers to a CDN) are rejected under file:// by CORS.
//
// 这个文件带 `!wasip1` 约束：net / net/http 会把 sock_accept / sock_shutdown
// 链进 wasip1 产物，浏览器里的 @wasmer/wasi 没有这两个导入，实例化直接
// LinkError（playground 的「运行」按钮会因此整片失败）。wasip1 侧只保留一个
// 报错桩（serve_esm_wasip1.go），产物导入表里就不会再有 socket 函数。
func serveESMAndOpen(dir, htmlPath string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot start local server (%v); open manually: %s\n", err, htmlPath)
		return
	}
	srv := &http.Server{Handler: http.FileServer(http.Dir(dir))}
	port := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", port, filepath.Base(htmlPath))
	go func() { _ = srv.Serve(ln) }()
	fmt.Printf(i18n.P("lvi02faz", "Serving ESM output at %s (Ctrl+C to stop)\n"), url)
	opened := false
	switch runtime.GOOS {
	case "darwin":
		opened = exec.Command("open", url).Run() == nil
	case "linux":
		opened = exec.Command("xdg-open", url).Run() == nil
	case "windows":
		opened = exec.Command("cmd", "/c", "start", url).Run() == nil
	}
	if !opened {
		fmt.Printf(i18n.P("n48cusda", "Open manually: %s\n"), url)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	_ = srv.Close()
}
