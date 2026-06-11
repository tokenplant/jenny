// Package tool provides tool implementations.
//go:build windows

package tool

import (
	"fmt"
	"os"
	"os/exec"
)

// killProcessWindows kills a process and its children using taskkill.
func killProcessWindows(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	return cmd.Run()
}

// signalProcess sends a signal to a process.
// On Windows, this uses taskkill /F /T /PID.
func signalProcess(proc *os.Process, isWindows bool) error {
	return killProcessWindows(proc.Pid)
}

// escalateProcessKill escalates a process termination.
// On Windows, this uses taskkill /F /T /PID with force.
func escalateProcessKill(proc *os.Process, isWindows bool) error {
	return killProcessWindows(proc.Pid)
}