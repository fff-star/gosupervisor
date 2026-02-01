//go:build windows

package process

import "syscall"

// setProcessGroupAttr 设置Windows特定的进程组属性
func setProcessGroupAttr(attr *syscall.SysProcAttr) {
	// Windows上使用CREATE_NEW_PROCESS_GROUP
	attr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP
}
