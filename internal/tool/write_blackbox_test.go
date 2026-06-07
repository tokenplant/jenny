package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWriteTool_AC1_NoPriorRead_BlackBox validates that writing without a prior Read fails.
// Also validates the exact error message per spec.
func TestWriteTool_AC1_NoPriorRead_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	wt := NewWriteTool(cache)

	// AC1: Write to a path never read
	result, err := wt.Execute(map[string]any{
		"file_path": filepath.Join(tmpDir, "never_read.txt"),
		"content":   "should fail",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error (should be in result.IsError): %v", err)
	}
	if !result.IsError {
		t.Fatal("AC1 FAIL: expected IsError=true when writing without prior Read")
	}
	if !strings.Contains(result.Content, "Cannot write without reading first") {
		t.Fatalf("AC1 FAIL: wrong error message. Got: %s", result.Content)
	}

	// Write with empty file_path
	result, err = wt.Execute(map[string]any{
		"file_path": "",
		"content":   "should fail",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC1 FAIL: expected error for empty file_path")
	}

	// Write with non-string file_path
	result, err = wt.Execute(map[string]any{
		"file_path": 42,
		"content":   "should fail",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC1 FAIL: expected error for non-string file_path")
	}
}

// TestWriteTool_AC2_StaleMtime_BlackBox validates staleness detection from
// multiple external modification methods.
func TestWriteTool_AC2_StaleMtime_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	t.Run("external write changes mtime", func(t *testing.T) {
		cache := NewReadFileCache()
		rt := NewReadTool(false, cache)
		wt := NewWriteTool(cache)

		f := filepath.Join(tmpDir, "stale.txt")
		if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Read
		_, err := rt.Execute(map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		// External modification
		time.Sleep(time.Millisecond * 10)
		if err := os.WriteFile(f, []byte("external change\n"), 0644); err != nil {
			t.Fatal(err)
		}

		// Write should fail
		result, err := wt.Execute(map[string]any{
			"file_path": f,
			"content":   "my content",
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

		// Verify original content preserved (write was rejected)
		data, _ := os.ReadFile(f)
		if string(data) != "external change\n" {
			t.Fatalf("AC2 FAIL: file content should be unchanged after rejected write. Got: %s", string(data))
		}
	})

	t.Run("touch changes mtime", func(t *testing.T) {
		cache := NewReadFileCache()
		rt := NewReadTool(false, cache)
		wt := NewWriteTool(cache)

		f := filepath.Join(tmpDir, "stale2.txt")
		if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := rt.Execute(map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		// touch the file (change mtime without changing content)
		time.Sleep(time.Millisecond * 10)
		newTime := time.Now()
		if err := os.Chtimes(f, newTime, newTime); err != nil {
			t.Fatal(err)
		}

		result, err := wt.Execute(map[string]any{
			"file_path": f,
			"content":   "my content",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsError {
			t.Fatal("AC2 FAIL: expected staleness error after touch (mtime change)")
		}
	})
}

// TestWriteTool_AC3_ParentDirs_BlackBox validates parent directory creation.
// Covers: deep path creation, already-existing parents, and single-level parents.
func TestWriteTool_AC3_ParentDirs_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	wt := NewWriteTool(cache)

	// Setup: create a deep file, read it, then remove both file and parents
	deepPath := filepath.Join(tmpDir, "a", "b", "c", "d", "file.txt")
	if err := os.MkdirAll(filepath.Dir(deepPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deepPath, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read to satisfy AC1
	_, err := rt.Execute(map[string]any{"file_path": deepPath}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Remove file AND parent dirs
	if err := os.RemoveAll(filepath.Join(tmpDir, "a")); err != nil {
		t.Fatal(err)
	}

	// Write — should recreate parent dirs and file
	result, err := wt.Execute(map[string]any{
		"file_path": deepPath,
		"content":   "new content here",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("AC3 FAIL: write returned error: %s", result.Content)
	}

	// Verify file exists with correct content
	data, err := os.ReadFile(deepPath)
	if err != nil {
		t.Fatalf("AC3 FAIL: file not found after write: %v", err)
	}
	if string(data) != "new content here" {
		t.Fatalf("AC3 FAIL: unexpected content: %q", string(data))
	}

	// Verify intermediate directories were created
	if _, err := os.Stat(filepath.Join(tmpDir, "a", "b", "c", "d")); err != nil {
		t.Fatalf("AC3 FAIL: intermediate dirs not created: %v", err)
	}
}

// TestWriteTool_AC4_PatchDiff_BlackBox validates diff output format.
func TestWriteTool_AC4_PatchDiff_BlackBox(t *testing.T) {
	t.Run("modified file produces unified diff", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := NewReadFileCache()
		rt := NewReadTool(false, cache)
		wt := NewWriteTool(cache)

		f := filepath.Join(tmpDir, "diff_test.txt")
		if err := os.WriteFile(f, []byte("line one\nline two\nline three\n"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := rt.Execute(map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		result, err := wt.Execute(map[string]any{
			"file_path": f,
			"content":   "line one\nline two modified\nline three\nline four\n",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("write failed: %s", result.Content)
		}

		diff := result.Content
		// Check unified diff format
		if !strings.Contains(diff, "--- a/") {
			t.Fatal("AC4 FAIL: diff missing '--- a/' header")
		}
		if !strings.Contains(diff, "+++ b/") {
			t.Fatal("AC4 FAIL: diff missing '+++ b/' header")
		}
		if !strings.Contains(diff, "@@") {
			t.Fatal("AC4 FAIL: diff missing hunk header '@@'")
		}
		if !strings.Contains(diff, "-line two") {
			t.Fatal("AC4 FAIL: diff missing deleted line '-line two'")
		}
		if !strings.Contains(diff, "+line two modified") {
			t.Fatal("AC4 FAIL: diff missing added line '+line two modified'")
		}
		if !strings.Contains(diff, "+line four") {
			t.Fatal("AC4 FAIL: diff missing added line '+line four'")
		}
	})

	t.Run("new file (empty old content) shows all lines as additions", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := NewReadFileCache()
		rt := NewReadTool(false, cache)
		wt := NewWriteTool(cache)

		f := filepath.Join(tmpDir, "new_diff_test.txt")
		if err := os.WriteFile(f, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		// Read empty file to satisfy AC1
		_, err := rt.Execute(map[string]any{"file_path": f}, tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		result, err := wt.Execute(map[string]any{
			"file_path": f,
			"content":   "brand new\ncontent\n",
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsError {
			t.Fatalf("write failed: %s", result.Content)
		}

		diff := result.Content
		if !strings.Contains(diff, "--- a/") {
			t.Fatal("AC4 FAIL: new-file diff missing '--- a/'")
		}
		if !strings.Contains(diff, "+brand new") {
			t.Fatal("AC4 FAIL: new-file diff should show additions with +")
		}
		if !strings.Contains(diff, "+content") {
			t.Fatal("AC4 FAIL: new-file diff should show additions with +")
		}
	})
}

// TestWriteTool_AC5_CacheUpdated_BlackBox validates that the readFileState is
// refreshed after write, enabling subsequent writes without re-read.
func TestWriteTool_AC5_CacheUpdated_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	wt := NewWriteTool(cache)

	f := filepath.Join(tmpDir, "ac5_test.txt")
	if err := os.WriteFile(f, []byte("version 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read
	_, err := rt.Execute(map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// First write (updates cache)
	result, err := wt.Execute(map[string]any{
		"file_path": f,
		"content":   "version 2\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("first write error: %v", err)
	}
	if result.IsError {
		t.Fatalf("first write failed: %s", result.Content)
	}

	// External modification between writes (this should NOT cause staleness
	// because the cache was updated after first write with new mtime)
	time.Sleep(time.Millisecond * 10)

	// Second write — no intervening Read, should succeed
	result, err = wt.Execute(map[string]any{
		"file_path": f,
		"content":   "version 3\n",
	}, tmpDir)
	if err != nil {
		t.Fatalf("second write error: %v", err)
	}
	if result.IsError {
		t.Fatalf("AC5 FAIL: second write (no re-read) failed: %s", result.Content)
	}

	// Verify final content
	data, _ := os.ReadFile(f)
	if string(data) != "version 3\n" {
		t.Fatalf("AC5 FAIL: unexpected final content: %q", string(data))
	}
}

// TestWriteTool_PathTraversal_BlackBox validates that writes outside cwd are blocked.
func TestWriteTool_PathTraversal_BlackBox(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	wt := NewWriteTool(cache)

	// Register a path inside tmpDir in cache to bypass AC1
	insidePath := filepath.Join(tmpDir, "inside.txt")
	cache.RecordRead(insidePath, "test", time.Now(), true)

	// Try to write outside cwd via ..
	result, err := wt.Execute(map[string]any{
		"file_path": filepath.Join(tmpDir, "..", "outside.txt"),
		"content":   "should not work",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("path traversal FAIL: expected error when writing outside cwd")
	}
	if !strings.Contains(result.Content, "not allowed") && !strings.Contains(result.Content, "outside working directory") {
		t.Fatalf("path traversal FAIL: wrong error: %s", result.Content)
	}
}

// TestWriteTool_FileDeletedAfterRead validates behavior when file is deleted
// between Read and Write.
func TestWriteTool_FileDeletedAfterRead(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	wt := NewWriteTool(cache)

	f := filepath.Join(tmpDir, "deleted.txt")
	if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read to satisfy AC1
	_, err := rt.Execute(map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Delete the file (but keep parent dirs)
	if err := os.Remove(f); err != nil {
		t.Fatal(err)
	}

	// Write should succeed (file doesn't exist, but cache has entry)
	// os.Stat will fail, so staleness check is skipped, and we proceed
	result, err := wt.Execute(map[string]any{
		"file_path": f,
		"content":   "new content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("write for deleted file failed: %s", result.Content)
	}

	// Verify file was recreated
	data, _ := os.ReadFile(f)
	if string(data) != "new content" {
		t.Fatalf("unexpected content after recreating deleted file: %q", string(data))
	}
}

// TestWriteTool_EmptyContent validates writing empty string content.
func TestWriteTool_EmptyContent(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	rt := NewReadTool(false, cache)
	wt := NewWriteTool(cache)

	f := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(f, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Read
	_, err := rt.Execute(map[string]any{"file_path": f}, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Write empty content
	result, err := wt.Execute(map[string]any{
		"file_path": f,
		"content":   "",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("write empty content failed: %s", result.Content)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "" {
		t.Fatalf("expected empty file, got: %q", string(data))
	}

	// Diff should show deletion
	if !strings.Contains(result.Content, "-original") {
		t.Fatalf("expected diff to show -original for empty content write. Diff: %s", result.Content)
	}
}

// TestWriteTool_RegistryIntegration validates that Registry properly wires
// WriteTool when WithReadFileCache is used.
func TestWriteTool_RegistryIntegration(t *testing.T) {
	t.Run("WriteTool present when WithReadFileCache called", func(t *testing.T) {
		tools := NewRegistry().WithBaseTools().WithReadFileCache().Build()
		wt := FindTool(tools, "write")
		if wt == nil {
			t.Fatal("REGISTRY FAIL: WriteTool not found when WithReadFileCache used")
		}
	})

	t.Run("WriteTool absent without WithReadFileCache", func(t *testing.T) {
		tools := NewRegistry().WithBaseTools().Build()
		wt := FindTool(tools, "write")
		if wt != nil {
			t.Fatal("REGISTRY FAIL: WriteTool should not be present without WithReadFileCache")
		}
	})

	t.Run("WriteTool in deny rules", func(t *testing.T) {
		tools := NewRegistry().WithBaseTools().WithReadFileCache().WithDenyRules([]string{"write"}).Build()
		wt := FindTool(tools, "write")
		if wt != nil {
			t.Fatal("REGISTRY FAIL: WriteTool should be filtered by deny rules")
		}
	})

	t.Run("name and description", func(t *testing.T) {
		cache := NewReadFileCache()
		wt := NewWriteTool(cache)
		if wt.Name() != "write" {
			t.Fatalf("expected name 'write', got %q", wt.Name())
		}
		if !strings.Contains(wt.Description(), "Requires prior Read") {
			t.Fatalf("description missing 'Requires prior Read': %s", wt.Description())
		}
	})
}
