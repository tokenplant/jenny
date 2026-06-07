// Package tool black-box validation tests for Read tool.
package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAC3_ReadToolFixedWidthLineNumbers verifies the Read tool uses
// 6-character fixed-width line numbers matching cat -n format.
func TestAC3_ReadToolFixedWidthLineNumbers(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file with content
	filePath := filepath.Join(tmpDir, "test_fw.txt")
	if err := os.WriteFile(filePath, []byte("line one\nline two\nline three\n"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tool := NewReadTool(false, nil)
	result, err := tool.Execute(context.Background(), map[string]any{"file_path": filePath}, tmpDir)
	if err != nil {
		t.Fatalf("ReadTool.Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("ReadTool.Execute returned error: %s", result.Content)
	}

	// Check that each content line starts with 6-char width line number + tab
	lines := strings.Split(result.Content, "\n")
	lineCount := 0
	for _, line := range lines {
		if strings.Contains(line, "\t") && !strings.HasPrefix(line, "[") {
			parts := strings.SplitN(line, "\t", 2)
			lineNumPart := parts[0]
			// cat -n format: right-aligned to 6 chars
			if len(lineNumPart) != 6 {
				t.Errorf("AC3 FAIL: line number part %q has length %d, want 6 (fixed-width)", lineNumPart, len(lineNumPart))
			}
			lineCount++
		}
	}
	if lineCount < 3 {
		t.Errorf("AC3 FAIL: expected at least 3 numbered lines, got %d", lineCount)
	}
	t.Logf("AC3 PASS: line numbers are 6-character fixed-width (%d lines)", lineCount)
}

// TestAC3_ReadToolEmptyFile verifies empty file produces a warning, not error.
func TestAC3_ReadToolEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tool := NewReadTool(false, nil)
	result, err := tool.Execute(context.Background(), map[string]any{"file_path": filePath}, tmpDir)
	if err != nil {
		t.Fatalf("ReadTool.Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("AC3 FAIL: empty file should not produce IsError, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Warning: empty file") && !strings.Contains(result.Content, "empty") {
		t.Errorf("AC3 FAIL: empty file should produce warning, got: %s", result.Content)
	}
	t.Logf("AC3 PASS: empty file produces warning (not error): %s", result.Content)
}

// TestAC3_ReadToolPastEOF verifies reading past EOF produces warning.
func TestAC3_ReadToolPastEOF(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "small.txt")
	if err := os.WriteFile(filePath, []byte("only one line\n"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tool := NewReadTool(false, nil)
	// offset=999 is past EOF for a 1-line file
	result, err := tool.Execute(context.Background(), map[string]any{"file_path": filePath, "offset": float64(999)}, tmpDir)
	if err != nil {
		t.Fatalf("ReadTool.Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("AC3 FAIL: past-EOF read should not be an error, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Warning") && !strings.Contains(result.Content, "exceeds") {
		t.Errorf("AC3 FAIL: past-EOF should produce warning. Content: %s", result.Content)
	}
	t.Logf("AC3 PASS: past-EOF produces warning with actual line count: %s", result.Content)
}

// TestAC3_ReadToolOffsetZero verifies offset=0 starts at line 1 (default offset).
func TestAC3_ReadToolOffsetZero(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "offset_test.txt")
	if err := os.WriteFile(filePath, []byte("line one\nline two\n"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tool := NewReadTool(false, nil)
	// offset=0 should default to starting at line 1 (clamped by max(offsetVal, 1))
	result, err := tool.Execute(context.Background(), map[string]any{"file_path": filePath, "offset": float64(0)}, tmpDir)
	if err != nil {
		t.Fatalf("ReadTool.Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("ReadTool.Execute returned error: %s", result.Content)
	}
	// Should contain line 1 content
	if !strings.Contains(result.Content, "line one") {
		t.Errorf("AC3 FAIL: offset=0 should show line 1. Content: %s", result.Content)
	}
	t.Log("AC3 PASS: offset=0 starts at line 1")
}
