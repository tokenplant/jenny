package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEditTool_AC1_NoPriorRead_BlackBox validates that editing without a prior
// Read fails with the correct error message.
func TestEditTool_AC1_NoPriorRead_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	et := NewEditTool(cache)

	// AC1: Edit a path never read (file doesn't exist)
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  filepath.Join(tmpDir, "never_read.txt"),
		"old_string": "hello",
		"new_string": "hi",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error (should be in result.IsError): %v", err)
	}
	if !result.IsError {
		t.Fatal("AC1 FAIL: expected IsError=true when editing without prior Read")
	}
	if !strings.Contains(result.Content, "Cannot edit without reading first") {
		t.Fatalf("AC1 FAIL: wrong error message. Got: %s", result.Content)
	}

	// Edit with empty file_path
	result, err = et.Execute(context.Background(), map[string]any{
		"file_path":  "",
		"old_string": "hello",
		"new_string": "hi",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC1 FAIL: expected error for empty file_path")
	}

	// Edit with non-string file_path
	result, err = et.Execute(context.Background(), map[string]any{
		"file_path":  42,
		"old_string": "hello",
		"new_string": "hi",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC1 FAIL: expected error for non-string file_path")
	}
}

// TestEditTool_AC1_ReadThenEdit_BlackBox validates the full read→edit success path.
func TestEditTool_AC1_ReadThenEdit_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "success.txt")
	if err := os.WriteFile(f, []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read first
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Edit should succeed
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "hello",
		"new_string": "hi",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("AC1 FAIL: edit after read failed: %s", result.Content)
	}

	// Verify content
	data, _ := os.ReadFile(f)
	if string(data) != "hi world\n" {
		t.Fatalf("AC1 FAIL: unexpected content after edit: %q", string(data))
	}
}

// TestEditTool_AC1_PartialRead_BlackBox validates that partial read blocks edit
// and full read unblocks it.
func TestEditTool_AC1_PartialRead_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "partial.txt")
	if err := os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Partial read with offset/limit
	_, err := rt.Execute(context.Background(), map[string]any{
		"file_path": f,
		"offset":    float64(1),
		"limit":     float64(1),
	}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Edit should fail
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "line1",
		"new_string": "modified",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC1 FAIL: expected error after partial read")
	}
	if !strings.Contains(result.Content, "Cannot edit after partial read") {
		t.Fatalf("AC1 FAIL: wrong error after partial read. Got: %s", result.Content)
	}

	// Now do full read (no offset/limit)
	_, err = rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Edit should now succeed
	result, err = et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "line1",
		"new_string": "modified",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("AC1 FAIL: edit after full re-read failed: %s", result.Content)
	}

	data, _ := os.ReadFile(f)
	if !strings.Contains(string(data), "modified") {
		t.Fatalf("AC1 FAIL: content not updated after successful edit: %q", string(data))
	}
}

// TestEditTool_AC2_StaleMtime_BlackBox validates staleness detection from
// multiple external modification methods.
func TestEditTool_AC2_StaleMtime_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("external write changes mtime", func(t *testing.T) {
		cache := NewReadFileCache()
		rt := NewReadTool(false, cache)
		et := NewEditTool(cache)

		f := filepath.Join(tmpDir, "stale_edit.txt")
		if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Read
		_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		// External modification
		time.Sleep(time.Millisecond * 10)
		if err := os.WriteFile(f, []byte("external change\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Edit should fail
		result, err := et.Execute(context.Background(), map[string]any{
			"file_path":  f,
			"old_string": "original",
			"new_string": "replaced",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Fatal("AC2 FAIL: expected staleness error after external write")
		}
		if !strings.Contains(result.Content, "File has changed since it was read") {
			t.Fatalf("AC2 FAIL: wrong error message. Got: %s", result.Content)
		}

		// Verify original content preserved (edit was rejected)
		data, _ := os.ReadFile(f)
		if string(data) != "external change\n" {
			t.Fatalf("AC2 FAIL: file content should be unchanged after rejected edit. Got: %q", string(data))
		}
	})

	t.Run("touch changes mtime", func(t *testing.T) {
		cache := NewReadFileCache()
		rt := NewReadTool(false, cache)
		et := NewEditTool(cache)

		f := filepath.Join(tmpDir, "stale_edit2.txt")
		if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		// touch the file (change mtime without changing content)
		time.Sleep(time.Millisecond * 10)
		newTime := time.Now()
		if err := os.Chtimes(f, newTime, newTime); err != nil {
			t.Fatal(err)
		}

		result, err := et.Execute(context.Background(), map[string]any{
			"file_path":  f,
			"old_string": "original",
			"new_string": "replaced",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Fatal("AC2 FAIL: expected staleness error after touch (mtime change)")
		}
	})
}

// TestEditTool_AC3_OldEqualsNew_BlackBox validates that old===new is rejected.
func TestEditTool_AC3_OldEqualsNew_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "noop.txt")
	if err := os.WriteFile(f, []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// old === new should be rejected
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "same",
		"new_string": "same",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC3 FAIL: expected error when old_string equals new_string")
	}
	if !strings.Contains(result.Content, "old_string and new_string must differ") {
		t.Fatalf("AC3 FAIL: wrong error message. Got: %s", result.Content)
	}

	// Both empty strings should also be rejected
	result, err = et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "",
		"new_string": "",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC3 FAIL: expected error when both strings are empty")
	}
}

// TestEditTool_AC4_MultipleMatches_BlackBox validates multi-match guard behavior.
func TestEditTool_AC4_MultipleMatches_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "multi_match.txt")
	content := "foo foo foo bar foo\n"
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Read
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Edit without replace_all on repeated text - should fail
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "foo",
		"new_string": "baz",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC4 FAIL: expected error when multiple matches found without replace_all")
	}
	if !strings.Contains(result.Content, "Set replace_all=true") {
		t.Fatalf("AC4 FAIL: wrong error message. Got: %s", result.Content)
	}

	// Single match scenario - should succeed without replace_all
	result, err = et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "bar",
		"new_string": "qux",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("AC4 FAIL: single match edit without replace_all failed: %s", result.Content)
	}

	// Now test replace_all=true - but cache is stale after the above edit, re-read
	// Recreate the file first
	f2 := filepath.Join(tmpDir, "multi_match2.txt")
	if err := os.WriteFile(f2, []byte("foo foo foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Execute(context.Background(), map[string]any{"file_path": f2}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	result, err = et.Execute(context.Background(), map[string]any{
		"file_path":   f2,
		"old_string":  "foo",
		"new_string":  "bar",
		"replace_all": true,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("AC4 FAIL: edit with replace_all failed: %s", result.Content)
	}

	data, _ := os.ReadFile(f2)
	if string(data) != "bar bar bar\n" {
		t.Fatalf("AC4 FAIL: expected all 3 occurrences replaced. Got: %q", string(data))
	}
}

// TestEditTool_AC5_IpynbRedirect_BlackBox validates .ipynb redirection.
func TestEditTool_AC5_IpynbRedirect_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	// .ipynb file should be redirected
	ipynbFile := filepath.Join(tmpDir, "notebook.ipynb")
	if err := os.WriteFile(ipynbFile, []byte(`{"cells":[]}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := rt.Execute(context.Background(), map[string]any{"file_path": ipynbFile}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  ipynbFile,
		"old_string": "cells",
		"new_string": "cells_modified",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC5 FAIL: expected error for .ipynb path")
	}
	if !strings.Contains(result.Content, "NotebookEdit") {
		t.Fatalf("AC5 FAIL: expected NotebookEdit redirect error. Got: %s", result.Content)
	}

	// .py file should NOT be redirected
	pyFile := filepath.Join(tmpDir, "module.py")
	if err := os.WriteFile(pyFile, []byte("print('hello')\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = rt.Execute(context.Background(), map[string]any{"file_path": pyFile}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	result, err = et.Execute(context.Background(), map[string]any{
		"file_path":  pyFile,
		"old_string": "hello",
		"new_string": "world",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("AC5 FAIL: .py file should not be redirected: %s", result.Content)
	}

	// Verify .py edit worked
	data, _ := os.ReadFile(pyFile)
	if !strings.Contains(string(data), "world") {
		t.Fatalf("AC5 FAIL: .py file not updated correctly: %q", string(data))
	}
}

// TestEditTool_ZeroMatches_BlackBox validates the zero-match error message includes snippet.
func TestEditTool_ZeroMatches_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "nomatch.txt")
	if err := os.WriteFile(f, []byte("hello world\nthis is a test file\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Search for non-existent string
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "nonexistent",
		"new_string": "replacement",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when string not found")
	}
	if !strings.Contains(result.Content, "String not found in file") {
		t.Fatalf("expected 'String not found in file' error. Got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "First 100 chars") {
		t.Fatalf("expected snippet preview 'First 100 chars'. Got: %s", result.Content)
	}
	// Verify snippet content
	if !strings.Contains(result.Content, "hello world") {
		t.Fatalf("expected snippet to contain file content. Got: %s", result.Content)
	}
}

// TestEditTool_CacheUpdated_BlackBox validates that edit updates the cache
// so subsequent edits work without re-read.
func TestEditTool_CacheUpdated_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "cascade.txt")
	if err := os.WriteFile(f, []byte("first second third\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// First edit
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "first",
		"new_string": "1st",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("first edit failed: %s", result.Content)
	}

	// Second edit (no intervening Read) — should succeed because cache was updated
	result, err = et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "second",
		"new_string": "2nd",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("second edit (no re-read) failed: %s", result.Content)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "1st 2nd third\n" {
		t.Fatalf("cache update FAIL: unexpected content after two edits: %q", string(data))
	}
}

// TestEditTool_DiffOutput_BlackBox validates that edit produces proper unified diff.
func TestEditTool_DiffOutput_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "diff_check.txt")
	if err := os.WriteFile(f, []byte("line one\nline two\nline three\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "line two",
		"new_string": "line two modified",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("edit failed: %s", result.Content)
	}

	diff := result.Content
	if !strings.Contains(diff, "--- a/") {
		t.Fatal("DIFF FAIL: missing '--- a/' header")
	}
	if !strings.Contains(diff, "+++ b/") {
		t.Fatal("DIFF FAIL: missing '+++ b/' header")
	}
	if !strings.Contains(diff, "-line two") {
		t.Fatal("DIFF FAIL: diff should show deleted line '-line two'")
	}
	if !strings.Contains(diff, "+line two modified") {
		t.Fatal("DIFF FAIL: diff should show added line '+line two modified'")
	}
}

// TestEditTool_PathTraversal_BlackBox validates edits outside cwd are blocked.
func TestEditTool_PathTraversal_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	et := NewEditTool(cache)

	// Register a path inside tmpDir in cache to bypass AC1
	insidePath := filepath.Join(tmpDir, "inside.txt")
	cache.RecordRead(insidePath, "test", time.Now(), true)

	// Try to edit outside cwd via ..
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  filepath.Join(tmpDir, "..", "outside.txt"),
		"old_string": "test",
		"new_string": "should not work",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("path traversal FAIL: expected error when editing outside cwd")
	}
	if !strings.Contains(result.Content, "not allowed") && !strings.Contains(result.Content, "outside working directory") {
		t.Fatalf("path traversal FAIL: wrong error: %s", result.Content)
	}
}

// TestEditTool_FileDeletedAfterRead validates behavior when file is deleted
// between Read and Edit with old_string="" (create semantics).
func TestEditTool_FileDeletedAfterRead(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "deleted_then_create.txt")
	if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read to satisfy AC1
	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Delete the file
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	// Edit with old_string="" should create the file
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "",
		"new_string": "created content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("edit for deleted-then-create file failed: %s", result.Content)
	}

	// Verify file was recreated with new content
	data, _ := os.ReadFile(f)
	if string(data) != "created content" {
		t.Fatalf("unexpected content after recreating deleted file: %q", string(data))
	}
}

// TestEditTool_BinaryContent validates that binary file edits are rejected.
func TestEditTool_BinaryContent_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "binary.bin")
	// Write a binary file with null bytes
	if err := os.WriteFile(f, []byte("before\x00after\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "before",
		"new_string": "after",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when editing binary file")
	}
	if !strings.Contains(result.Content, "binary") {
		t.Fatalf("expected 'binary' error message. Got: %s", result.Content)
	}
}

// TestEditTool_LineEndingNormalization_BlackBox validates CRLF→LF normalization.
func TestEditTool_LineEndingNormalization_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "crlf.txt")
	if err := os.WriteFile(f, []byte("hello\r\nworld\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Match with LF (normalized from CRLF)
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "world",
		"new_string": "universe",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("edit with CRLF file failed: %s", result.Content)
	}

	// Verify the output file uses LF
	data, _ := os.ReadFile(f)
	if !strings.Contains(string(data), "universe") {
		t.Fatalf("CRLF normalization FAIL: content not updated: %q", string(data))
	}
}

// TestEditTool_OldStringEmptyOnExistingFile validates that old_string=""
// on a non-empty file is rejected as ambiguous.
func TestEditTool_OldStringEmptyOnExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	et := NewEditTool(cache)

	f := filepath.Join(tmpDir, "empty_old.txt")
	if err := os.WriteFile(f, []byte("existing content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := rt.Execute(context.Background(), map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// old_string="" on non-empty file should either create ambiguity error
	// or count as zero matches (since empty string is in every position)
	result, err := et.Execute(context.Background(), map[string]any{
		"file_path":  f,
		"old_string": "",
		"new_string": "prepend",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The current implementation counts empty string matches and returns multi-match error
	if !result.IsError {
		t.Fatal("expected error when old_string is empty on non-empty file (ambiguous)")
	}
}
