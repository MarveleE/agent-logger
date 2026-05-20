//go:build !windows

package daemon

import (
	"errors"
	"os"
	"syscall"
)

// IsAlive reports whether a process with the given PID is currently running.
// On Unix this uses kill(pid, 0). EPERM (signal could not be delivered due to
// permissions) still implies the process exists.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

// stopRunning sends SIGTERM to the live process. The server installs a
// signal handler that cancels its root context for a graceful shutdown.
func stopRunning(pid int, _ *Paths) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}
