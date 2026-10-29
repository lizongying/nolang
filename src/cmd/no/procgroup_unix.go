//go:build unix

package main

import (
	"syscall"
)

// procGroupAttr 让被测子进程以独立 process group 运行（Setpgid），
// 配合 killProcGroup 在超时时把被测二进制及其孙进程一并杀掉。
// unix 分支：wasip1 的 syscall.SysProcAttr 没有 Setpgid 字段，故拆出本文件。
func procGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcGroup 杀掉整个进程组（-pid 表示进程组）；失败时退回只杀直接子进程。
func killProcGroup(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}
