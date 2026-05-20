//go:build linux

package process

import "syscall"

// setProcessGroupAttr 设置Linux特定的进程组属性
func setProcessGroupAttr(attr *syscall.SysProcAttr) {
	// Linux上使用Setpgid
	attr.Setpgid = true
}

// signalProcessGroup sends a signal to the entire process group identified by pid.
func signalProcessGroup(pid int, sig syscall.Signal) error {
	// Negative PID sends the signal to the process group PGID.
	return syscall.Kill(-pid, sig)
}
