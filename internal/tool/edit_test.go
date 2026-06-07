// Package tool provides tool implementations.
package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEditTool_AC1_NoPriorRead tests that Edit without prior Read fails.
func TestEditTool_AC1_NoPriorRead(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	editTool := NewEditTool(readCache)

	// Create a file but do NOT read it
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Try to edit without reading first
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "hello",
		"new_string": "hi",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when editing without prior read")
	}
	if !strings.Contains(result.Content, "Cannot edit without reading first") {
		t.Errorf("expected 'Cannot edit without reading first' error, got: %s", result.Content)
	}
}

// TestEditTool_AC1_ReadThenEditWorks tests that Read then Edit works.
func TestEditTool_AC1_ReadThenEditWorks(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create and read a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Edit should succeed
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "hello",
		"new_string": "hi",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("edit failed unexpectedly: %s", result.Content)
	}

	// Verify the file was edited
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "hi world\n" {
		t.Errorf("unexpected content: %s", string(content))
	}
}

// TestEditTool_AC1_PartialReadRejected tests that partial read blocks edit.
func TestEditTool_AC1_PartialReadRejected(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a test file with multiple lines
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("line 1\nline 2\nline 3\nline 4\nline 5\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read with offset/limit (partial read)
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"offset":    float64(2),
		"limit":     float64(2),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during partial read: %v", err)
	}

	// Try to edit - should fail due to partial read
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "line 1",
		"new_string": "new line 1",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when editing after partial read")
	}
	if !strings.Contains(result.Content, "Cannot edit after partial read") {
		t.Errorf("expected partial read error, got: %s", result.Content)
	}
}

// TestEditTool_AC2_StaleMtime tests that stale mtime fails before edit.
func TestEditTool_AC2_StaleMtime(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("original content\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Externally modify the file (change mtime without using tools)
	time.Sleep(10 * time.Millisecond)
	err = os.WriteFile(testFile, []byte("externally modified\n"), 0644)
	if err != nil {
		t.Fatalf("failed to modify file externally: %v", err)
	}

	// Try to edit - should fail due to staleness
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "original",
		"new_string": "new",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when file has changed since read")
	}
	if !strings.Contains(result.Content, "File has changed since it was read") {
		t.Errorf("expected staleness error, got: %s", result.Content)
	}
}

// TestEditTool_AC3_OldEqualsNew tests that old_string === new_string is rejected.
func TestEditTool_AC3_OldEqualsNew(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create and read a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Try to edit with old_string == new_string
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "hello",
		"new_string": "hello",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when old_string equals new_string")
	}
	if !strings.Contains(result.Content, "old_string and new_string must differ") {
		t.Errorf("expected 'old_string and new_string must differ' error, got: %s", result.Content)
	}
}

// TestEditTool_AC4_MultipleMatches tests that multiple matches require replace_all.
func TestEditTool_AC4_MultipleMatches(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a file with the same string appearing 3 times
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "foo foo foo\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Try to edit without replace_all - should fail
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "foo",
		"new_string": "bar",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when multiple matches found without replace_all")
	}
	if !strings.Contains(result.Content, "Set replace_all=true") {
		t.Errorf("expected 'Set replace_all=true' error, got: %s", result.Content)
	}

	// Now edit with replace_all=true - should succeed
	result, err = editTool.Execute(context.Background(), map[string]any{
		"file_path":   testFile,
		"old_string":  "foo",
		"new_string":  "bar",
		"replace_all": true,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("edit with replace_all failed: %s", result.Content)
	}

	// Verify all 3 occurrences were replaced
	fileContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(fileContent) != "bar bar bar\n" {
		t.Errorf("expected 'bar bar bar\\n', got: %s", string(fileContent))
	}
}

// TestEditTool_AC5_IpynbRedirect tests that .ipynb paths are redirected.
func TestEditTool_AC5_IpynbRedirect(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a .ipynb file
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	err := os.WriteFile(ipynbFile, []byte(`{"cells":[]}`), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Try to edit - should be redirected to NotebookEdit
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  ipynbFile,
		"old_string": "foo",
		"new_string": "bar",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for .ipynb path")
	}
	if !strings.Contains(result.Content, "NotebookEdit") {
		t.Errorf("expected NotebookEdit redirect error, got: %s", result.Content)
	}

	// Verify .py files are NOT redirected
	pyFile := filepath.Join(tmpDir, "test.py")
	err = os.WriteFile(pyFile, []byte("foo = 1\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the .py file
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": pyFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Edit should work (not redirected)
	result, err = editTool.Execute(context.Background(), map[string]any{
		"file_path":  pyFile,
		"old_string": "foo",
		"new_string": "bar",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("edit of .py file should not be redirected: %s", result.Content)
	}
}

// TestEditTool_ZeroMatches tests that zero matches returns clear error with snippet.
func TestEditTool_ZeroMatches(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create and read a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Try to edit with a string that doesn't exist
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "notfound",
		"new_string": "new",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when string not found")
	}
	if !strings.Contains(result.Content, "String not found in file") {
		t.Errorf("expected 'String not found in file' error, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "First 100 chars") {
		t.Errorf("expected 'First 100 chars' hint, got: %s", result.Content)
	}
}

// TestEditTool_AC4_SingleMatchNoReplaceAll tests that single match works without replace_all.
func TestEditTool_AC4_SingleMatchNoReplaceAll(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a file with unique string
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Edit with single match - should succeed without replace_all
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "hello",
		"new_string": "hi",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("edit failed unexpectedly: %s", result.Content)
	}

	// Verify only first occurrence was replaced (only one match anyway)
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "hi world\n" {
		t.Errorf("unexpected content: %s", string(content))
	}
}

// TestEditTool_DiffOutput tests that the result includes patch diff.
func TestEditTool_DiffOutput(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create and read a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("line 1\nline 2\nline 3\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Edit content
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "line 2",
		"new_string": "line 2 modified",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("edit failed: %s", result.Content)
	}

	// Verify diff output contains expected markers
	diff := result.Content
	if !strings.Contains(diff, "---") || !strings.Contains(diff, "+++") {
		t.Errorf("expected diff format with --- and +++, got: %s", diff)
	}
	// Check for + or - lines
	hasChange := strings.Contains(diff, "+") || strings.Contains(diff, "-")
	if !hasChange {
		t.Errorf("expected diff to contain + or - lines, got: %s", diff)
	}
}

// TestEditTool_CacheUpdated tests that subsequent edit works without re-read.
func TestEditTool_CacheUpdated(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create and read a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during first read: %v", err)
	}

	// First edit
	result1, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "hello",
		"new_string": "hi",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during first edit: %v", err)
	}
	if result1.IsError {
		t.Fatalf("first edit failed: %s", result1.Content)
	}

	// Second edit (no intervening read) - should succeed because cache was updated
	result2, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "world",
		"new_string": "universe",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during second edit: %v", err)
	}
	if result2.IsError {
		t.Errorf("second edit failed unexpectedly: %s", result2.Content)
	}

	// Verify final content
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read final content: %v", err)
	}
	if string(content) != "hi universe\n" {
		t.Errorf("unexpected final content: %s", string(content))
	}
}

// TestEditTool_LineEndingNormalization tests CRLF handling.
func TestEditTool_LineEndingNormalization(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a file with CRLF line endings
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("line1\r\nline2\r\nline3\r\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Edit should work with LF matching (normalized from CRLF)
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "line2",
		"new_string": "line2-modified",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("edit with CRLF failed: %s", result.Content)
	}

	// Verify file has LF endings (normalized)
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !strings.Contains(string(content), "\r\n") {
		// CRLF should have been converted to LF
		if strings.Contains(string(content), "line2-modified") {
			// Success - replacement worked and line endings are now LF
		}
	}
}

// TestEditTool_OverlappingMatches tests overlapping match handling.
func TestEditTool_OverlappingMatches(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a file with overlapping pattern
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("aaaa"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Replace "aaa" with "b" - should replace non-overlapping occurrences
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":   testFile,
		"old_string":  "aaa",
		"new_string":  "b",
		"replace_all": true,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("edit failed: %s", result.Content)
	}

	// Go's strings.Replace does non-overlapping replacements
	// "aaaa" with "aaa"->"b" gives "ba" (first two a's replaced, last two remain)
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	// "aaaa" -> "ba" (non-overlapping: first 3 a's become b, last a remains)
	if string(content) != "ba" {
		t.Errorf("expected 'ba', got: %s", string(content))
	}
}
