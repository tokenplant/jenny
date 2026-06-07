// Package tool provides tool implementations.
package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNotebookEditTool_AC1_NonIpynbRejected tests that non-.ipynb files are rejected.
func TestNotebookEditTool_AC1_NonIpynbRejected(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	notebookTool := NewNotebookEditTool(readCache)

	// Create a regular text file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read the file first
	readTool := NewReadTool(false, readCache)
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Try to use NotebookEdit on .txt file - should fail
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": testFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-0",
		"new_source":    "new content",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-.ipynb file")
	}
	if !strings.Contains(result.Content, "Edit tool") {
		t.Errorf("expected redirect to Edit tool, got: %s", result.Content)
	}
}

// TestNotebookEditTool_AC1_NoPriorRead tests that editing without prior Read fails.
func TestNotebookEditTool_AC1_NoPriorRead(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file but do NOT read it
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('hello')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Try to edit without reading first
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-0",
		"new_source":    "new content",
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

// TestNotebookEditTool_AC1_PartialReadRejected tests that partial read blocks edit.
func TestNotebookEditTool_AC1_PartialReadRejected(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('hello')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read with offset/limit (partial read)
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
		"offset":    float64(1),
		"limit":     float64(1),
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Try to edit - should fail due to partial read
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-0",
		"new_source":    "new content",
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

// TestNotebookEditTool_AC2_InsertRequiresCellType tests that insert requires cell_type.
func TestNotebookEditTool_AC2_InsertRequiresCellType(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('hello')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Try to insert without cell_type - should fail
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "insert",
		"new_source":    "new content",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when insert without cell_type")
	}
	if !strings.Contains(result.Content, "cell_type is required") {
		t.Errorf("expected cell_type required error, got: %s", result.Content)
	}
}

// TestNotebookEditTool_AC2_InsertValidCellType tests that insert works with valid cell_type.
func TestNotebookEditTool_AC2_InsertValidCellType(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('hello')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Insert with cell_type='code' - should succeed
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "insert",
		"cell_type":     "code",
		"new_source":    "x = 1",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("insert failed unexpectedly: %s", result.Content)
	}

	// Verify file was modified
	content, err := os.ReadFile(ipynbFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !strings.Contains(string(content), "x = 1") {
		t.Errorf("expected new cell in notebook, got: %s", string(content))
	}
}

// TestNotebookEditTool_AC3_StalenessEnforced tests that stale mtime fails before edit.
func TestNotebookEditTool_AC3_StalenessEnforced(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('hello')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Externally modify the file (change mtime without using tools)
	time.Sleep(10 * time.Millisecond)
	err = os.WriteFile(ipynbFile, []byte(`{"cells":[{"id":"cell-0","cell_type":"code","source":"modified"}]}`), 0644)
	if err != nil {
		t.Fatalf("failed to modify file externally: %v", err)
	}

	// Try to edit - should fail due to staleness
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-0",
		"new_source":    "new content",
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

// TestNotebookEditTool_AC4_ValidJSONAfterEdit tests that edit produces valid JSON.
func TestNotebookEditTool_AC4_ValidJSONAfterEdit(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('hello')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Perform replace edit
	_, err = notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-0",
		"new_source":    "print('world')",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file is valid JSON
	content, err := os.ReadFile(ipynbFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var nb map[string]any
	if err := json.Unmarshal(content, &nb); err != nil {
		t.Errorf("notebook is not valid JSON: %v", err)
	}

	// Verify structure
	cells, ok := nb["cells"].([]any)
	if !ok {
		t.Error("cells is not an array")
	}
	if len(cells) != 1 {
		t.Errorf("expected 1 cell, got %d", len(cells))
	}
}

// TestNotebookEditTool_AC5_PostEditReadReturnsNewContent tests that Read returns updated content.
func TestNotebookEditTool_AC5_PostEditReadReturnsNewContent(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('hello')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Perform replace edit
	_, err = notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-0",
		"new_source":    "print('modified')",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read again - should return new content, not cached stub
	result, err := readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file after edit: %v", err)
	}

	// The Read result should contain the modified content
	if !strings.Contains(result.Content, "print('modified')") {
		t.Errorf("expected new content in Read result, got: %s", result.Content)
	}
}

// TestNotebookEditTool_ReplaceMode tests replace mode functionality.
func TestNotebookEditTool_ReplaceMode(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file with multiple cells
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('a')"},{"id":"cell-1","cell_type":"markdown","source":"# Title"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Replace the second cell
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-1",
		"new_source":    "# New Title",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("replace failed: %s", result.Content)
	}

	// Verify file was modified
	content, err := os.ReadFile(ipynbFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !strings.Contains(string(content), "# New Title") {
		t.Errorf("expected modified content, got: %s", string(content))
	}
	// Original cell should be unchanged
	if !strings.Contains(string(content), "print('a')") {
		t.Error("expected first cell to be unchanged")
	}
}

// TestNotebookEditTool_DeleteMode tests delete mode functionality.
func TestNotebookEditTool_DeleteMode(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file with multiple cells
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('a')"},{"id":"cell-1","cell_type":"code","source":"print('b')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Delete the first cell
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "delete",
		"cell_id":       "cell-0",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("delete failed: %s", result.Content)
	}

	// Verify only one cell remains
	content, err := os.ReadFile(ipynbFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var nb map[string]any
	if err := json.Unmarshal(content, &nb); err != nil {
		t.Fatalf("notebook is not valid JSON: %v", err)
	}
	cells := nb["cells"].([]any)
	if len(cells) != 1 {
		t.Errorf("expected 1 cell after delete, got %d", len(cells))
	}
}

// TestNotebookEditTool_InsertMode tests insert mode functionality.
func TestNotebookEditTool_InsertMode(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file with one cell
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('a')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Insert a new cell after cell-0
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "insert",
		"cell_id":       "cell-0",
		"cell_type":     "markdown",
		"new_source":    "## New Section",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("insert failed: %s", result.Content)
	}

	// Verify two cells now
	content, err := os.ReadFile(ipynbFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var nb map[string]any
	if err := json.Unmarshal(content, &nb); err != nil {
		t.Fatalf("notebook is not valid JSON: %v", err)
	}
	cells := nb["cells"].([]any)
	if len(cells) != 2 {
		t.Errorf("expected 2 cells after insert, got %d", len(cells))
	}
}

// TestNotebookEditTool_CellNumericAlias tests cell-N numeric alias support.
func TestNotebookEditTool_CellNumericAlias(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file with multiple cells
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"abc123","cell_type":"code","source":"print('a')"},{"id":"def456","cell_type":"code","source":"print('b')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Use numeric alias cell-1 to reference second cell
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-1",
		"new_source":    "print('modified')",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("replace with numeric alias failed: %s", result.Content)
	}

	// Verify second cell was modified
	content, err := os.ReadFile(ipynbFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !strings.Contains(string(content), "print('modified')") {
		t.Errorf("expected modified content, got: %s", string(content))
	}
}

// TestNotebookEditTool_InvalidJSON tests that invalid JSON is rejected.
func TestNotebookEditTool_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file with invalid JSON
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	err := os.WriteFile(ipynbFile, []byte("not valid json {"), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Try to edit - should fail with JSON error
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-0",
		"new_source":    "new content",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(result.Content, "Invalid JSON") {
		t.Errorf("expected JSON error, got: %s", result.Content)
	}
}

// TestNotebookEditTool_MissingCell tests that missing cell returns error.
func TestNotebookEditTool_MissingCell(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('a')"}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Try to replace a cell that doesn't exist
	result, err := notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-99",
		"new_source":    "new content",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing cell")
	}
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected cell not found error, got: %s", result.Content)
	}
}

// TestNotebookEditTool_CodeCellResetsOutputs tests that replace resets outputs for code cells.
func TestNotebookEditTool_CodeCellResetsOutputs(t *testing.T) {
	tmpDir := t.TempDir()
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)
	notebookTool := NewNotebookEditTool(readCache)

	// Create an .ipynb file with a code cell that has outputs
	ipynbFile := filepath.Join(tmpDir, "test.ipynb")
	nbContent := `{"cells":[{"id":"cell-0","cell_type":"code","source":"print('a')","execution_count":1,"outputs":[{"output_type":"stream","text":["hello\n"]}]}]}`
	err := os.WriteFile(ipynbFile, []byte(nbContent), 0644)
	if err != nil {
		t.Fatalf("failed to create notebook file: %v", err)
	}

	// Read the file first
	_, err = readTool.Execute(context.Background(), map[string]any{
		"file_path": ipynbFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Replace the code cell
	_, err = notebookTool.Execute(context.Background(), map[string]any{
		"notebook_path": ipynbFile,
		"edit_mode":     "replace",
		"cell_id":       "cell-0",
		"new_source":    "print('b')",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify outputs were reset
	content, err := os.ReadFile(ipynbFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var nb map[string]any
	if err := json.Unmarshal(content, &nb); err != nil {
		t.Fatalf("notebook is not valid JSON: %v", err)
	}
	cells := nb["cells"].([]any)
	cell := cells[0].(map[string]any)
	if cell["outputs"] != nil && len(cell["outputs"].([]any)) != 0 {
		t.Error("expected outputs to be reset to empty array")
	}
	if cell["execution_count"] != nil {
		t.Error("expected execution_count to be reset to nil")
	}
}
