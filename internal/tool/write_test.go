package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWriteTool_AC1_NoPriorRead tests that Write without prior Read fails.
func TestWriteTool_AC1_NoPriorRead(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	writeTool := NewWriteTool(readCache)

	// Try to write without reading first
	newFile := filepath.Join(tmpDir, "newfile.txt")
	result, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": newFile,
		"content":   "hello world",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when writing without prior read")
	}
	if !strings.Contains(result.Content, "Cannot write without reading first") {
		t.Errorf("expected 'Cannot write without reading first' error, got: %s", result.Content)
	}
}

// TestWriteTool_AC1_NoPriorReadThenReadWorks tests that Read then Write works.
func TestWriteTool_AC1_ReadThenWriteWorks(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	writeTool := NewWriteTool(readCache)

	// Create a new file first
	newFile := filepath.Join(tmpDir, "newfile.txt")
	err := os.WriteFile(newFile, []byte("initial content\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	readResult, err := readTool.Execute(context.Background(), map[string]any{
		"file_path": newFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}
	if readResult.IsError {
		t.Fatalf("read failed: %s", readResult.Content)
	}

	// Now write should succeed
	writeResult, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": newFile,
		"content":   "new content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writeResult.IsError {
		t.Errorf("write failed unexpectedly: %s", writeResult.Content)
	}
}

// TestWriteTool_AC2_StaleMtime tests that stale mtime fails before write.
func TestWriteTool_AC2_StaleMtime(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	writeTool := NewWriteTool(readCache)

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

	// Try to write - should fail due to staleness
	result, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"content":   "new content",
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

// TestWriteTool_AC3_ParentDirs verifies parent directory creation works.
func TestWriteTool_AC3_ParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	writeTool := NewWriteTool(readCache)

	// Define a deep path where NO parent directories exist
	deepDir := filepath.Join(tmpDir, "newdir", "subdir", "deep")
	testFile := filepath.Join(deepDir, "newfile.txt")

	// Verify parent dirs don't exist (this is the test precondition)
	if _, err := os.Stat(deepDir); err == nil || !os.IsNotExist(err) {
		t.Fatalf("parent directory should not exist for AC3 test: %v", err)
	}

	// Create and read a file at the deep path (this creates the parents)
	err := os.MkdirAll(deepDir, 0755)
	if err != nil {
		t.Fatalf("failed to create parent directories: %v", err)
	}
	err = os.WriteFile(testFile, []byte("initial content\n"), 0644)
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

	// Now delete the file AND parent dirs to simulate truly new path
	err = os.RemoveAll(deepDir)
	if err != nil {
		t.Fatalf("failed to remove file and parent dirs: %v", err)
	}

	// Write to the deep path - WriteTool should create parent dirs automatically
	writeResult, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"content":   "new content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writeResult.IsError {
		t.Fatalf("write failed: %s", writeResult.Content)
	}

	// Verify file was created with correct content
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Errorf("file was not created: %v", err)
	}
	if string(content) != "new content" {
		t.Errorf("unexpected content: %s", string(content))
	}
}

// TestWriteTool_AC4_PatchDiff tests that the result includes patch diff.
func TestWriteTool_AC4_PatchDiff(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	writeTool := NewWriteTool(readCache)

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

	// Write new content
	result, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"content":   "line 1\nline 2 modified\nline 3\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("write failed: %s", result.Content)
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

// TestWriteTool_AC5_CacheUpdated tests that second write succeeds without re-read.
func TestWriteTool_AC5_CacheUpdated(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	writeTool := NewWriteTool(readCache)

	// Create and read a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("original\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during first read: %v", err)
	}

	// First write
	result1, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"content":   "first write\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during first write: %v", err)
	}
	if result1.IsError {
		t.Fatalf("first write failed: %s", result1.Content)
	}

	// Second write (no intervening read) - should succeed because cache was updated
	result2, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"content":   "second write\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during second write: %v", err)
	}
	if result2.IsError {
		t.Errorf("second write failed unexpectedly: %s", result2.Content)
	}

	// Verify final content
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read final content: %v", err)
	}
	if string(content) != "second write\n" {
		t.Errorf("unexpected final content: %s", string(content))
	}
}

// TestReadWriteTool_ReadBeforeWrite is an integration test.
func TestReadWriteTool_ReadBeforeWrite(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	writeTool := NewWriteTool(readCache)

	// Create initial file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file
	readResult, err := readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if readResult.IsError {
		t.Fatalf("read returned error: %s", readResult.Content)
	}
	if !strings.Contains(readResult.Content, "hello world") {
		t.Errorf("read content missing expected text: %s", readResult.Content)
	}

	// Write modified content
	writeResult, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"content":   "hello universe\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if writeResult.IsError {
		t.Fatalf("write returned error: %s", writeResult.Content)
	}

	// Verify diff is present
	if !strings.Contains(writeResult.Content, "---") || !strings.Contains(writeResult.Content, "+++") {
		t.Errorf("write result missing diff format: %s", writeResult.Content)
	}

	// Verify file content was updated
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file after write: %v", err)
	}
	if string(content) != "hello universe\n" {
		t.Errorf("unexpected file content: %s", string(content))
	}
}

// TestWriteTool_PartialReadFails tests that write after partial read fails.
func TestWriteTool_PartialReadFails(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	writeTool := NewWriteTool(readCache)

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

	// Try to write - should fail due to partial read
	result, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"content":   "new content\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when writing after partial read")
	}
	if !strings.Contains(result.Content, "Cannot write after partial read") {
		t.Errorf("expected partial read error, got: %s", result.Content)
	}
}

// TestWriteTool_UnchangedContent tests that writing unchanged content produces minimal diff.
func TestWriteTool_UnchangedContent(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	writeTool := NewWriteTool(readCache)

	// Create and read a file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "same content\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Write same content
	result, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"content":   content,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("write failed: %s", result.Content)
	}

	// Diff should still be present but might be minimal
	if !strings.Contains(result.Content, "---") || !strings.Contains(result.Content, "+++") {
		t.Errorf("expected diff format, got: %s", result.Content)
	}
}
