//go:build windows

package tool

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// signalProcess sends taskkill /F /T to the process on Windows.
// Windows doesn't have a direct equivalent to SIGTERM that works reliably
// for process trees, so we use taskkill.
func signalProcess(proc *os.Process, isWindows bool) error {
	if proc == nil {
		return nil
	}
	// Use taskkill /F /T /PID <pid> to kill the process and its children
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(proc.Pid))
	return cmd.Run()
}

// escalateProcessKill is the same as signalProcess on Windows since taskkill /F is already forceful.
func escalateProcessKill(proc *os.Process, isWindows bool) error {
	return signalProcess(proc, isWindows)
}

// getSignalInfo returns false on Windows as signal concepts differ.
func getSignalInfo(ps *os.ProcessState) (int, string, bool) {
	return 0, "", false
}
