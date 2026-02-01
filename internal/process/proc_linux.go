//go:build linux

package process

import "syscall"

// setProcessGroupAttr 设置Linux特定的进程组属性
func setProcessGroupAttr(attr *syscall.SysProcAttr) {
	// Linux上使用Setpgid
	attr.Setpgid = true
}
