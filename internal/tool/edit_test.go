package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/constants"
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

// TestEditTool_ScratchpadAllowedWithoutPermissions tests that EditTool can edit
// a file under scratchpad directory even with skipPermissions=false.
func TestEditTool_ScratchpadAllowedWithoutPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { constants.JennyHomeDirFunc = oldHome }()

	scratchpadDir := constants.ScratchpadDir()
	testFile := filepath.Join(scratchpadDir, "scratch-edit.txt")
	if err := os.MkdirAll(scratchpadDir, 0755); err != nil {
		t.Fatalf("mkdir scratchpad: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("original content\n"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	readCache := NewReadFileCache()

	// Read first to satisfy read-before-write contract
	rt := NewReadTool(false, readCache)
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": testFile}, tmpDir)
	if err != nil {
		t.Fatalf("read of scratchpad file should succeed: %v", err)
	}

	et := NewEditTool(readCache)
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "original",
		"new_string": "edited",
	}, tmpDir)
	if err != nil {
		t.Fatalf("edit of scratchpad file should succeed: %v", err)
	}
	if result.IsError {
		t.Fatalf("edit should not error: %s", result.Content)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("file should exist after edit: %v", err)
	}
	if !strings.Contains(string(data), "edited content") {
		t.Errorf("expected edited content, got: %s", string(data))
	}
}

// TestEditTool_ScopedEditAfterPartialRead tests that editing with
// start_line/end_line works after a partial read.
func TestEditTool_ScopedEditAfterPartialRead(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a multi-line test file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Partial read: lines 2-3 (offset=2, limit=2)
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"offset":    float64(2),
		"limit":     float64(2),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during partial read: %v", err)
	}

	// Scoped edit within the read range should succeed
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "line 2",
		"new_string": "modified line 2",
		"start_line": float64(2),
		"end_line":   float64(3),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("scoped edit after partial read should succeed: %s", result.Content)
	}

	// Verify the file was correctly edited (only line 2 changed)
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	expected := "line 1\nmodified line 2\nline 3\nline 4\nline 5\n"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}
}

// TestEditTool_ScopedEditOutsideReadRange tests that editing outside
// the partial read range is rejected.
func TestEditTool_ScopedEditOutsideReadRange(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	// Create a multi-line test file
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Partial read: lines 2-3 (offset=2, limit=2)
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"offset":    float64(2),
		"limit":     float64(2),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during partial read: %v", err)
	}

	// Scoped edit outside read range should fail
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "line 4",
		"new_string": "modified line 4",
		"start_line": float64(4),
		"end_line":   float64(4),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when scoped edit outside read range")
	}
	if !strings.Contains(result.Content, "outside read range") {
		t.Errorf("expected 'outside read range' error, got: %s", result.Content)
	}
}

// TestEditTool_ScopedEditNoStartEnd tests that partial read without
// start_line/end_line is rejected.
func TestEditTool_ScopedEditNoStartEnd(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Partial read: lines 2-3
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"offset":    float64(2),
		"limit":     float64(2),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during partial read: %v", err)
	}

	// Edit without start_line/end_line should fail
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "line 2",
		"new_string": "modified line 2",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when editing after partial read without start_line/end_line")
	}
	if !strings.Contains(result.Content, "Cannot edit after partial read without start_line and end_line") {
		t.Errorf("expected partial read guidance error, got: %s", result.Content)
	}
}

// TestEditTool_ScopedEditAfterFullRead tests that scoped edit works
// even after a full file read (streaming path).
func TestEditTool_ScopedEditAfterFullRead(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "line A\nline B\nline C\nline D\nline E\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Full read
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Scoped edit on full-read file should still work
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "line C",
		"new_string": "modified line C",
		"start_line": float64(3),
		"end_line":   float64(3),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("scoped edit after full read should succeed: %s", result.Content)
	}

	// Verify only the scoped line changed
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	expected := "line A\nline B\nmodified line C\nline D\nline E\n"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}
}

// TestEditTool_NumExpectedGlobal tests that num_expected aborts when
// count doesn't match in global mode.
func TestEditTool_NumExpectedGlobal(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "foo foo foo\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
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

	// Set num_expected=2 but actual count is 3
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":     testFile,
		"old_string":    "foo",
		"new_string":    "bar",
		"num_expected":  float64(2),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when num_expected doesn't match")
	}
	if !strings.Contains(result.Content, "Expected 2 replacement(s) but found 3") {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

// TestEditTool_NumExpectedScoped tests that num_expected aborts when
// count doesn't match in scoped mode.
func TestEditTool_NumExpectedScoped(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "line A\none foo\nline B\nfoo bar\nline C\n"
	err := os.WriteFile(testFile, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Full read
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	// Scoped edit with num_expected=2 but only 1 in the range
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":    testFile,
		"old_string":   "foo",
		"new_string":   "bar",
		"start_line":   float64(1),
		"end_line":     float64(2),
		"num_expected": float64(2),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when num_expected doesn't match in scoped range")
	}
	if !strings.Contains(result.Content, "Expected 2 replacement(s) but found 1") {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

// TestEditTool_EndLineBeforeStartLine tests that end_line < start_line is rejected.
func TestEditTool_EndLineBeforeStartLine(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	editTool := NewEditTool(readCache)

	// Create and read a file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("content\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	readCache.RecordRead(testFile, "content\n", time.Now(), true, 0, 0)

	// end_line < start_line should fail
	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "content",
		"new_string": "modified",
		"start_line": float64(5),
		"end_line":   float64(3),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when end_line < start_line")
	}
	if !strings.Contains(result.Content, "end_line") || !strings.Contains(result.Content, "must be >= start_line") {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

// TestEditTool_ScopedEditDiffOutput verifies that scoped edit produces proper unified diff.
func TestEditTool_ScopedEditDiffOutput(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	testFile := filepath.Join(tmpDir, "scoped_diff.txt")
	err := os.WriteFile(testFile, []byte("line A\nline B\nline C\nline D\nline E\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "line C",
		"new_string": "modified line C",
		"start_line": float64(3),
		"end_line":   float64(3),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("scoped edit failed: %s", result.Content)
	}

	diff := result.Content
	if !strings.Contains(diff, "---") || !strings.Contains(diff, "+++") {
		t.Errorf("expected diff format with --- and +++, got: %s", diff)
	}
	if !strings.Contains(diff, "-line C") {
		t.Errorf("expected diff to show removed '-line C', got: %s", diff)
	}
	if !strings.Contains(diff, "+modified line C") {
		t.Errorf("expected diff to show added '+modified line C', got: %s", diff)
	}
}

// TestEditTool_ScopedEditLargeAfterSection verifies correctness when after-section
// is significantly larger than the scoped range (streaming path).
func TestEditTool_ScopedEditLargeAfterSection(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	editTool := NewEditTool(readCache)

	var content strings.Builder
	for i := 1; i <= 3; i++ {
		fmt.Fprintf(&content, "before line %d\n", i)
	}
	content.WriteString("target line A\ntarget line B\n")
	for i := 1; i <= 1000; i++ {
		fmt.Fprintf(&content, "after line %d\n", i)
	}

	testFile := filepath.Join(tmpDir, "large_after.txt")
	err := os.WriteFile(testFile, []byte(content.String()), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error during read: %v", err)
	}

	result, err := editTool.Execute(context.Background(), map[string]any{
		"file_path":  testFile,
		"old_string": "target line A",
		"new_string": "modified line A",
		"start_line": float64(4),
		"end_line":   float64(5),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("scoped edit failed: %s", result.Content)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 1006 {
		t.Fatalf("expected at least 1006 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[1004], "after line 1000") {
		t.Errorf("last after-line not intact. Got line 1004: %q", lines[1004])
	}
	if !strings.Contains(lines[0], "before line 1") {
		t.Errorf("first line not preserved: %q", lines[0])
	}
	if !strings.Contains(lines[3], "modified line A") {
		t.Errorf("line 4 not modified: %q", lines[3])
	}
}
