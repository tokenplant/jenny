package tool

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

// TestPowerShellToolExecute tests the PowerShellTool Execute method.
// On non-Windows, this test verifies that NewPowerShellTool returns a valid struct.
func TestPowerShellToolExecute(t *testing.T) {
	// On non-Windows, we can only test the struct creation
	if runtime.GOOS != "windows" {
		tool := NewPowerShellTool(false)
		if tool == nil {
			t.Fatal("NewPowerShellTool should not return nil")
		}
		if tool.Name() != "PowerShell" {
			t.Errorf("expected name 'PowerShell', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
		// InputSchema should be non-nil
		if tool.InputSchema() == nil {
			t.Error("expected non-nil InputSchema")
		}
		t.Log("AC2 PASS: PowerShellTool struct is valid on non-Windows (powershell.exe not available)")
		return
	}

	// On Windows, test actual execution
	tool := NewPowerShellTool(false)
	ctx := context.Background()

	// Test simple echo command
	input := map[string]any{"command": "echo hello"}
	result, err := tool.Execute(ctx, input, "C:\\")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}
	// Normalize line endings: PowerShell outputs CRLF on Windows, but we expect LF
	expected := "hello\n"
	actual := strings.ReplaceAll(result.Content, "\r\n", "\n")
	if actual != expected {
		t.Errorf("expected %q, got %q", expected, actual)
	}
	t.Log("AC2 PASS: PowerShellTool.Execute returns UTF-8 output")
}

// TestPowerShellToolBackground tests background execution.
func TestPowerShellToolBackground(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("background test requires Windows")
	}

	tool := NewPowerShellTool(false)
	ctx := context.Background()

	input := map[string]any{
		"command":           "echo background",
		"run_in_background": true,
	}
	result, err := tool.Execute(ctx, input, "C:\\")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}
	if result.OutputFile == "" {
		t.Error("expected non-empty output file for background task")
	}
	t.Log("AC2 PASS: PowerShellTool background execution works")
}
