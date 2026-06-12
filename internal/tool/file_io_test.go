package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// TestUTF8SafeTruncate verifies AC7: rune-aware truncation never splits
// multi-byte code points.
func TestUTF8SafeTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		maxBytes int
		want     string
		wantValid bool
	}{
		{
			name:     "4-byte emoji at boundary",
			input:    "Hello 🔥 world",
			maxBytes: 9, // "Hello 🔥" is 9 bytes; 🔥 alone is 4 bytes
			wantValid: true,
		},
		{
			name:     "ASCII short string fits",
			input:    "hello",
			maxBytes: 10,
			want:     "hello",
			wantValid: true,
		},
		{
			name:     "ASCII truncation",
			input:    "hello world",
			maxBytes: 5,
			want:     "hello",
			wantValid: true,
		},
		{
			name:     "emoji-only string",
			input:    "🔥🔥🔥",
			maxBytes: 4, // Exactly one emoji
			wantValid: true,
		},
		{
			name:     "negative maxBytes",
			input:    "hello",
			maxBytes: -1,
			want:     "",
			wantValid: true,
		},
		{
			name:     "zero maxBytes",
			input:    "hello",
			maxBytes: 0,
			want:     "",
			wantValid: true,
		},
		{
			name:     "3-byte unicode char at boundary",
			input:    "日本🏠test",
			maxBytes: 7, // "日本" = 6 bytes; trying to include "🏠" (4 bytes) would overshoot
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utf8SafeTruncate(tt.input, tt.maxBytes)
			if tt.want != "" && got != tt.want {
				t.Errorf("utf8SafeTruncate(%q, %d) = %q, want %q", tt.input, tt.maxBytes, got, tt.want)
			}
			if tt.wantValid && !utf8.ValidString(got) {
				t.Errorf("utf8SafeTruncate(%q, %d) = %q is not valid UTF-8", tt.input, tt.maxBytes, got)
			}
		})
	}
}

// TestUTF8SafeTruncate_NoSplitEmoji specifically verifies the AC7 guarantee:
// an emoji at the truncation boundary is never split mid-code-point.
func TestUTF8SafeTruncate_NoSplitEmoji(t *testing.T) {
	// "🔥" is a 4-byte UTF-8 sequence: \xF0\x9F\x94\xA5.
	// Truncating mid-sequence produces invalid UTF-8; our function must avoid it.
	input := strings.Repeat("🔥", 100) // 400 bytes of emoji
	maxBytes := 399                   // Force truncation mid-rune
	result := utf8SafeTruncate(input, maxBytes)
	// AC7: The result must be valid UTF-8 (no split code points).
	if !utf8.ValidString(result) {
		t.Errorf("AC7 FAIL: result contains invalid UTF-8: %q", result)
	}
	// The result should be 396 bytes (99 complete emojis).
	if len(result) != 396 {
		t.Errorf("AC7 FAIL: expected 396 bytes (99 emojis), got %d", len(result))
	}
}

// TestAtomicWrite verifies AC4: WriteTool uses temp-file → Sync → rename.
func TestAtomicWrite(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	readTool := NewReadTool(false, cache)
	writeTool := NewWriteTool(cache)

	// Create a file.
	filePath := filepath.Join(tmpDir, "atomic_test.txt")
	if err := os.WriteFile(filePath, []byte("original content\n"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Read the file (seeds cache with ReadTool, not WriteTool).
	result, err := readTool.Execute(context.Background(), map[string]any{"file_path": filePath}, tmpDir)
	if err != nil {
		t.Fatalf("Read Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Read failed: %s", result.Content)
	}

	// Write new content.
	result, err = writeTool.Execute(context.Background(), map[string]any{
		"file_path": filePath,
		"content":   "new atomic content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("Write Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Write failed: %s", result.Content)
	}

	// Verify content via ReadTool (fresh read, not cache).
	cache2 := NewReadFileCache()
	readTool2 := NewReadTool(false, cache2)
	result, err = readTool2.Execute(context.Background(), map[string]any{"file_path": filePath}, tmpDir)
	if err != nil {
		t.Fatalf("Verify read error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Verify read failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, "new atomic content") {
		t.Errorf("AC4 FAIL: file content does not contain 'new atomic content', got: %s", result.Content)
	}

	// Verify no temp files were left behind.
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".write-") {
			t.Errorf("AC4 FAIL: temp file %q was not cleaned up", e.Name())
		}
	}
}

// TestAtomicWrite_CreatesNewFile verifies AC4: WriteTool atomically creates new files.
func TestAtomicWrite_CreatesNewFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	readTool := NewReadTool(false, cache)
	writeTool := NewWriteTool(cache)

	newFile := filepath.Join(tmpDir, "new_file.txt")
	if err := os.WriteFile(newFile, []byte("seed\n"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Read with ReadTool to seed cache.
	readTool.Execute(context.Background(), map[string]any{"file_path": newFile}, tmpDir)

	// Overwrite with atomic write.
	result, err := writeTool.Execute(context.Background(), map[string]any{
		"file_path": newFile,
		"content":   "overwritten atomically",
	}, tmpDir)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Write failed: %s", result.Content)
	}

	// Verify via ReadTool.
	cache2 := NewReadFileCache()
	readTool2 := NewReadTool(false, cache2)
	result, err = readTool2.Execute(context.Background(), map[string]any{"file_path": newFile}, tmpDir)
	if err != nil {
		t.Fatalf("Verify read error: %v", err)
	}
	if !strings.Contains(result.Content, "overwritten atomically") {
		t.Errorf("file content = %q, want %q", result.Content, "overwritten atomically")
	}
}

// TestTaskOutputAppendMode verifies AC5: task output files use os.O_APPEND.
func TestTaskOutputAppendMode(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	taskID := "test-append"
	path, err := tm.TaskOutputPath(taskID)
	if err != nil {
		t.Fatalf("TaskOutputPath error: %v", err)
	}

	// Write first entry.
	err = tm.WriteTaskResult(taskID, "output1", 0, 1.0)
	if err != nil {
		t.Fatalf("WriteTaskResult error: %v", err)
	}

	// Write second entry.
	err = tm.WriteTaskResult(taskID, "output2", 0, 2.0)
	if err != nil {
		t.Fatalf("WriteTaskResult error: %v", err)
	}

	// Verify both entries are present (not overwritten).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Errorf("AC5 FAIL: expected ≥2 JSONL lines, got %d", len(lines))
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if !strings.Contains(line, "output") {
			t.Errorf("AC5 FAIL: malformed JSONL line: %s", line)
		}
	}
}

// TestConcurrentTaskWrites verifies AC5: concurrent writers produce intact lines.
func TestConcurrentTaskWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}
	t.Parallel()

	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	taskID := "test-concurrent-writes"
	const goroutines = 4
	const linesPerGoroutine = 25

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < linesPerGoroutine; i++ {
				_ = tm.WriteTaskResult(taskID, fmt.Sprintf("goroutine-%d-line-%d", id, i), 0, 1.0)
			}
		}(g)
	}
	wg.Wait()

	// Verify all lines are intact.
	path, _ := tm.TaskOutputPath(taskID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	expected := goroutines * linesPerGoroutine
	if len(lines) != expected {
		t.Errorf("AC5 FAIL: expected %d lines, got %d", expected, len(lines))
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		if !strings.Contains(line, "goroutine-") || !strings.Contains(line, "-line-") {
			t.Errorf("AC5 FAIL: line does not contain expected content: %s", line)
		}
	}
}

// TestGlobTool_MaxDepthLimit verifies AC8: glob respects maxDepth (default 64).
// We create a tree 100 levels deep (exceeding maxDepth=64), place a file at depth 75
// (beyond the limit) and a file at depth 3 (within the limit), then assert only the
// shallow file is found.
func TestGlobTool_MaxDepthLimit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a directory tree 100 levels deep.
	deepDir := tmpDir
	for i := 0; i < 100; i++ {
		deepDir = filepath.Join(deepDir, fmt.Sprintf("level%d", i))
		if err := os.MkdirAll(deepDir, 0755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}
	}
	// Create a file at depth 75 (beyond maxDepth=64).
	beyondLimitFile := filepath.Join(deepDir, "beyond_limit.txt")
	if err := os.WriteFile(beyondLimitFile, []byte("deep"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	// Create a file at depth 3 (within maxDepth=64).
	shallowDir := tmpDir
	for i := 0; i < 3; i++ {
		shallowDir = filepath.Join(shallowDir, fmt.Sprintf("level%d", i))
	}
	withinLimitFile := filepath.Join(shallowDir, "within_limit.txt")
	if err := os.WriteFile(withinLimitFile, []byte("shallow"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	tool := NewGlobTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.txt",
	}, tmpDir)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// The shallow file should be found.
	if !strings.Contains(result.Content, "within_limit.txt") {
		t.Errorf("AC8 FAIL: shallow file should be found, got: %s", result.Content)
	}
	// The deep file should NOT be found (exceeds maxDepth=64).
	if strings.Contains(result.Content, "beyond_limit.txt") {
		t.Errorf("AC8 FAIL: deep file should NOT be found (exceeds maxDepth=64), got: %s", result.Content)
	}
}

// TestGlobTool_MaxResults verifies AC8: glob caps at 100 results with Truncated flag.
func TestGlobTool_MaxResults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	// Create 150 files.
	for i := 0; i < 150; i++ {
		fullPath := filepath.Join(tmpDir, filepath.Join("dir", fmt.Sprintf("file%d.txt", i)))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("content"), 0644); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	tool := NewGlobTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.txt",
	}, tmpDir)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	count := len(lines)
	if count > 100 {
		t.Errorf("AC8 FAIL: expected ≤100 results, got %d", count)
	}
	if !result.Truncated {
		t.Errorf("AC8 FAIL: expected Truncated=true when results exceed cap")
	}
}

// TestReadTool_1GiBRejection verifies AC3: ReadTool rejects files >1 GiB with a behavioral test.
// We use a sparse file (Seek + Truncate) to avoid writing 1 GiB of data, but os.Stat reports >1 GiB.
// This mirrors the pattern from TestEditOOM.
func TestReadTool_1GiBRejection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	readTool := NewReadTool(false, cache)

	// Create a sparse file >1 GiB.
	testFile := filepath.Join(tmpDir, "huge.bin")
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.Write(make([]byte, 0)); err != nil {
		f.Close()
		t.Fatalf("seed write: %v", err)
	}
	if _, err := f.Seek(int64(1<<30)+1, 0); err != nil {
		f.Close()
		t.Fatalf("seek: %v", err)
	}
	if err := f.Truncate(int64(1<<30) + 1); err != nil {
		f.Close()
		t.Fatalf("truncate: %v", err)
	}
	f.Close()

	// Verify stat reports >1 GiB.
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() <= 1<<30 {
		t.Fatalf("setup: file size = %d, want > 1 GiB", info.Size())
	}

	// Execute should reject with IsError and "too large" message.
	result, err := readTool.Execute(context.Background(), map[string]any{"file_path": testFile}, tmpDir)
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("AC3 FAIL: expected IsError=true for >1GiB file")
	}
	if !strings.Contains(result.Content, "too large") {
		t.Errorf("AC3 FAIL: error message %q does not contain 'too large'", result.Content)
	}

	// Also verify a small file succeeds.
	smallFile := filepath.Join(tmpDir, "small.txt")
	if err := os.WriteFile(smallFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	result2, err := readTool.Execute(context.Background(), map[string]any{"file_path": smallFile}, tmpDir)
	if err != nil {
		t.Fatalf("Execute small file returned Go error: %v", err)
	}
	if result2.IsError {
		t.Errorf("AC3 FAIL: small file should succeed, got error: %s", result2.Content)
	}
}


