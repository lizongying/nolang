//go:build !unix

package main

import (
	"os"
	"syscall"
)

// procGroupAttr 非 unix 平台（windows 等）没有 Setpgid 概念，
// 返回 nil 让 exec.Cmd 用默认属性启动子进程。
func procGroupAttr() *syscall.SysProcAttr { return nil }

// killProcGroup 非 unix 平台只能杀掉直接子进程，可能残留孙进程。
func killProcGroup(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
