//go:build !windows

package tool

import (
	"os"
	"syscall"
)

// signalProcess sends SIGTERM to the process.
func signalProcess(proc *os.Process, isWindows bool) error {
	return proc.Signal(syscall.SIGTERM)
}

// escalateProcessKill sends SIGKILL to the process.
func escalateProcessKill(proc *os.Process, isWindows bool) error {
	return proc.Signal(syscall.SIGKILL)
}

// getSignalInfo extracts signal information from ProcessState.
func getSignalInfo(ps *os.ProcessState) (int, string, bool) {
	if ps == nil {
		return 0, "", false
	}
	if waitStatus, ok := ps.Sys().(syscall.WaitStatus); ok && waitStatus.Signaled() {
		sig := waitStatus.Signal()
		return 128 + int(sig), sig.String(), true
	}
	return 0, "", false
}
