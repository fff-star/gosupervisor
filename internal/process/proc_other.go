//go:build !linux

package process

import (
	"fmt"
	"syscall"
)

// setProcessGroupAttr is a no-op on non-Linux platforms.
func setProcessGroupAttr(attr *syscall.SysProcAttr) {}

// signalProcessGroup falls back to signaling the individual process on non-Linux.
func signalProcessGroup(pid int, sig syscall.Signal) error {
	return fmt.Errorf("process group signaling not supported on this platform")
}
