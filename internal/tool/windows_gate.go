package tool

import (
	"fmt"
	"strings"
)

// WindowsCommandGate provides security validation for PowerShell and read commands on Windows.
type WindowsCommandGate struct {
	skipPermissions bool
}

// NewWindowsCommandGate creates a new WindowsCommandGate.
func NewWindowsCommandGate(skipPermissions bool) *WindowsCommandGate {
	return &WindowsCommandGate{skipPermissions: skipPermissions}
}

// CheckCommand validates a command against blocked patterns on Windows.
func (g *WindowsCommandGate) CheckCommand(command string) error {
	if g.skipPermissions {
		return nil
	}

	blockedCommands := []string{
		"set-executionpolicy",
		"reg.exe",
		"sc.exe",
		"reg",
		"sc",
	}

	// Tokenize to find the command name
	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return nil
	}

	cmdName := strings.ToLower(tokens[0])
	for _, blocked := range blockedCommands {
		if cmdName == blocked {
			return fmt.Errorf("command '%s' is restricted on Windows for security reasons", tokens[0])
		}
	}

	// Also check for substrings for cases like "reg.exe" being part of a larger string
	// or powershell calling it directly.
	lowerCmd := strings.ToLower(command)
	for _, blocked := range blockedCommands {
		// Only block if it's a whole word or starts with it
		if strings.HasPrefix(lowerCmd, blocked+" ") || lowerCmd == blocked {
			return fmt.Errorf("command '%s' is restricted on Windows for security reasons", blocked)
		}
	}

	return nil
}

// CheckPath validates that a path is not a restricted Windows path.
// This implementation is platform-agnostic to support testing on Unix.
func (g *WindowsCommandGate) CheckPath(path string) error {
	if g.skipPermissions {
		return nil
	}

	// Normalize slashes for comparison
	normPath := strings.ReplaceAll(strings.ToLower(path), "/", "\\")

	blockedPaths := []string{
		`c:\windows\system32`,
		`c:\$recycle.bin`,
		`\\.\pipe\`,
	}

	for _, blocked := range blockedPaths {
		if strings.HasPrefix(normPath, strings.ToLower(blocked)) {
			return fmt.Errorf("access to path %s is blocked on Windows", path)
		}
	}

	// Block AppData
	if strings.Contains(normPath, `\appdata\`) || strings.HasSuffix(normPath, `\appdata`) {
		return fmt.Errorf("access to AppData is blocked on Windows")
	}

	// Block raw physical drives
	if strings.HasPrefix(normPath, `\\.\physicaldrive`) || strings.HasPrefix(normPath, `\\.\`) {
		return fmt.Errorf("access to physical drives or low-level devices is blocked on Windows")
	}

	return nil
}
