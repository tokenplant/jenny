package tool

import (
	"testing"
)

// TestWindowsCommandGate_CheckPath tests path validation for Windows.
func TestWindowsCommandGate_CheckPath(t *testing.T) {
	gate := NewWindowsCommandGate(false)

	// Test blocked paths
	blockedPaths := []string{
		"C:\\Windows\\System32",
		"C:\\WINDOWS\\system32",
		"C:\\Windows\\System32\\cmd.exe",
		"C:\\Users\\TestUser\\AppData\\Roaming",
		"C:\\Users\\TestUser\\AppData\\Local",
		"C:\\Users\\TestUser\\AppData\\LocalLow",
		"C:\\$Recycle.Bin",
		"\\\\.\\pipe\\somepipe",
		"\\\\.\\PhysicalDrive0",
		"\\\\.\\C:",
	}

	for _, path := range blockedPaths {
		err := gate.CheckPath(path)
		if err == nil {
			t.Errorf("expected error for blocked path %q", path)
		}
	}

	// Test allowed paths
	allowedPaths := []string{
		"C:\\Projects\\myapp",
		"C:\\Users\\TestUser\\Documents",
		"C:\\temp",
		"D:\\data",
	}

	for _, path := range allowedPaths {
		err := gate.CheckPath(path)
		if err != nil {
			t.Errorf("unexpected error for allowed path %q: %v", path, err)
		}
	}

	t.Log("AC3 PASS: WindowsCommandGate blocks restricted paths")
}

// TestWindowsCommandGate_CheckCommand tests command validation for Windows.
func TestWindowsCommandGate_CheckCommand(t *testing.T) {
	gate := NewWindowsCommandGate(false)

	// Test blocked commands
	blockedCommands := []string{
		"Set-ExecutionPolicy RemoteSigned",
		"set-executionpolicy -scope currentuser unrestricted",
		"reg.exe add",
		"reg.exe query",
		"reg query HKLM\\Software",
		"sc.exe create",
		"sc.exe delete",
		"sc query",
	}

	for _, cmd := range blockedCommands {
		err := gate.CheckCommand(cmd)
		if err == nil {
			t.Errorf("expected error for blocked command %q", cmd)
		}
	}

	// Test allowed commands
	allowedCommands := []string{
		"Get-ChildItem",
		"echo hello",
		"dir",
		"cd C:\\Projects",
	}

	for _, cmd := range allowedCommands {
		err := gate.CheckCommand(cmd)
		if err != nil {
			t.Errorf("unexpected error for allowed command %q: %v", cmd, err)
		}
	}

	t.Log("AC3 PASS: WindowsCommandGate blocks restricted commands")
}

// TestWindowsCommandGate_SkipPermissions tests that skipPermissions bypasses checks.
func TestWindowsCommandGate_SkipPermissions(t *testing.T) {
	gate := NewWindowsCommandGate(true) // skipPermissions = true

	// All paths and commands should be allowed
	err := gate.CheckPath("C:\\Windows\\System32")
	if err != nil {
		t.Errorf("expected no error when skipPermissions is true, got: %v", err)
	}

	err = gate.CheckCommand("Set-ExecutionPolicy RemoteSigned")
	if err != nil {
		t.Errorf("expected no error when skipPermissions is true, got: %v", err)
	}
}