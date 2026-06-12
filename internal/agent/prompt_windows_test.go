package agent

import (
	"runtime"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/tool"
)

// TestWindowsSystemPromptHint tests that the system prompt includes Windows-specific
// hints when running on Windows.
func TestWindowsSystemPromptHint(t *testing.T) {
	cfg := StreamConfig{}
	tools := []tool.Tool{}
	cwd := "/tmp" // Outside git repo for simplicity

	prompt := buildSystemPrompt(cfg, tools, cwd)

	if runtime.GOOS == "windows" {
		// On Windows, the prompt should contain Windows-specific hints
		expectedHint := "You are running on Windows. Use the PowerShell tool for system commands. Be aware of Windows file path conventions"
		if !strings.Contains(prompt, expectedHint) {
			t.Errorf("expected Windows hint in prompt, got:\n%s", prompt)
		}
		t.Log("AC7 PASS: System prompt contains Windows-specific hints")
	} else {
		// On Unix, Windows hints should be absent
		windowsHint := "Use the PowerShell tool"
		if strings.Contains(prompt, windowsHint) {
			t.Error("Windows hint should not be present on Unix")
		}
		t.Log("AC7 PASS: System prompt does not contain Windows hints on Unix")
	}
}

// TestPlatformSection_WindowsHint tests the platformSection function directly.
func TestPlatformSection_WindowsHint(t *testing.T) {
	section, ok := platformSection("/test/path")
	if !ok {
		t.Fatal("expected platform section to be included")
	}

	if !strings.Contains(section, "Platform:") {
		t.Error("should contain Platform:")
	}
	if !strings.Contains(section, "/test/path") {
		t.Error("should contain the cwd path")
	}

	if runtime.GOOS == "windows" {
		expectedHint := "You are running on Windows"
		if !strings.Contains(section, expectedHint) {
			t.Errorf("expected Windows hint in platform section, got: %s", section)
		}
	}
}
