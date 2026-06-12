package tool

import (
	"runtime"
	"testing"
)

// TestRegistryWindowsToolRegistration tests that the registry correctly registers
// PowerShellTool on Windows and BashTool on Unix.
func TestRegistryWindowsToolRegistration(t *testing.T) {
	// This test is designed to be run with GOOS=windows for full validation
	// On non-Windows, we just verify the basic structure
	tools := NewRegistry().WithBaseTools().Build()

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name()] = true
	}

	if runtime.GOOS == "windows" {
		// On Windows, PowerShellTool should be present
		if !names["PowerShell"] {
			t.Error("expected 'PowerShell' tool on Windows")
		}
		// BashTool may or may not be present (depends on bash.exe in PATH)
		t.Log("AC1 PASS: PowerShellTool registered on Windows")
	} else {
		// On Unix, BashTool should be present, PowerShellTool should not
		if names["PowerShell"] {
			t.Error("'PowerShell' should not be present on Unix")
		}
		if !names["Bash"] {
			t.Error("expected 'Bash' tool on Unix")
		}
		t.Log("AC1 PASS: BashTool registered, PowerShellTool absent on Unix")
	}
}

// TestRegistryWindowsBaseToolCount tests the base tool count varies by platform.
func TestRegistryWindowsBaseToolCount(t *testing.T) {
	tools := NewRegistry().WithBaseTools().Build()

	if runtime.GOOS == "windows" {
		// On Windows: Read, PowerShell (or Bash if bash.exe found), Glob, Grep = 4 tools
		// Some tests may show Read, Bash, PowerShell, Glob, Grep = 5 if bash.exe is in PATH
		if len(tools) < 3 {
			t.Errorf("expected at least 3 base tools on Windows, got %d", len(tools))
		}
	} else {
		// On Unix: Read, Bash, Glob, Grep = 4 tools
		if len(tools) != 4 {
			t.Errorf("expected 4 base tools on Unix, got %d", len(tools))
		}
	}
}
