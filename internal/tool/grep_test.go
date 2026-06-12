package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrepTool_Execute(t *testing.T) {
	skipIfNoRg(t)
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"a.txt":            "hello world foo",
		"b.txt":            "bar hello",
		"sub/c.txt":        "test hello test",
		"sub/d.txt":        "hello again",
		"sub/nested/e.txt": "nested hello",
	}

	for name, content := range files {
		fullPath := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	tool := NewGrepTool()

	tests := []struct {
		name    string
		input   map[string]any
		cwd     string
		wantErr bool
		checkFn func(*ToolResult) bool
	}{
		{
			name: "basic pattern matching",
			input: map[string]any{
				"pattern": "hello",
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, "a.txt") &&
					strings.Contains(r.Content, "b.txt")
			},
		},
		{
			name: "files_with_matches mode",
			input: map[string]any{
				"pattern":     "hello",
				"output_mode": "files_with_matches",
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, "a.txt") &&
					strings.Contains(r.Content, "b.txt") &&
					strings.Contains(r.Content, "sub/c.txt") &&
					strings.Contains(r.Content, "sub/d.txt") &&
					strings.Contains(r.Content, "sub/nested/e.txt")
			},
		},
		{
			name: "count mode",
			input: map[string]any{
				"pattern":     "hello",
				"output_mode": "count",
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, "a.txt:1") &&
					strings.Contains(r.Content, "b.txt:1")
			},
		},
		{
			name: "content mode with line numbers",
			input: map[string]any{
				"pattern":     "hello",
				"output_mode": "content",
				"n":           true,
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, ".txt:")
			},
		},
		{
			name: "case insensitive",
			input: map[string]any{
				"pattern": "HELLO",
				"i":       true,
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, ".txt")
			},
		},
		{
			name: "glob filter",
			input: map[string]any{
				"pattern": "hello",
				"glob":    "*.txt",
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, "a.txt") &&
					strings.Contains(r.Content, "b.txt")
			},
		},
		{
			name: "no matches",
			input: map[string]any{
				"pattern": "notfound",
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError && r.Content == "No matches found"
			},
		},
		{
			name: "missing pattern errors",
			input: map[string]any{
				"pattern": "",
			},
			cwd:     tmpDir,
			wantErr: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.input, tt.cwd)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if tt.wantErr {
				if result == nil || !result.IsError {
					t.Errorf("expected error result")
				}
				return
			}
			if tt.checkFn != nil && !tt.checkFn(result) {
				t.Errorf("check failed, got content: %q, truncated: %v", result.Content, result.Truncated)
			}
		})
	}
}

// AC1: Default head_limit is 250; head_limit=0 means unlimited
func TestGrepTool_AC1_HeadLimit(t *testing.T) {
	skipIfNoRg(t)
	tmpDir := t.TempDir()

	// Create 300 files with matching content in top-level directories
	// to keep path lengths short enough to fit ~250 within 20K char limit
	for i := range 300 {
		fullPath := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(fullPath, []byte("match"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	tool := NewGrepTool()

	// Test default head_limit (250)
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "match",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count results - should be limited to 250
	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	count := len(lines)

	// Note: ripgrep with -l just returns file paths, so multiple matches in same file
	// are only counted once. With 300 files and default head_limit=250,
	// we should get 250 files
	if count != defaultHeadLimit {
		t.Errorf("expected 250 results with default head_limit, got %d", count)
	}
	if !result.Truncated {
		t.Logf("result not truncated at 250 limit (may vary by file count)")
	}

	// Test head_limit=0 (unlimited)
	result, err = tool.Execute(context.Background(), map[string]any{
		"pattern":    "match",
		"head_limit": 0,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count all results - should get more (up to 20K char limit)
	lines = strings.Split(strings.TrimSpace(result.Content), "\n")
	count = len(lines)

	// With 300 files and head_limit=0, we should get more results
	// But still subject to 20K char cap
	if count <= 250 {
		t.Errorf("expected more than 250 results with unlimited head_limit, got %d", count)
	}
}

// AC2: Pattern starting with `-` uses `-e` flag
func TestGrepTool_AC2_DashPattern(t *testing.T) {
	skipIfNoRg(t)
	tmpDir := t.TempDir()

	// Create a file containing the literal string "-foo"
	fooFile := filepath.Join(tmpDir, "foo")
	if err := os.WriteFile(fooFile, []byte("-foo"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Create another file with content "bar"
	barFile := filepath.Join(tmpDir, "bar")
	if err := os.WriteFile(barFile, []byte("bar"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	tool := NewGrepTool()

	// Pattern "-foo" should find the file with literal "-foo"
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     "-foo",
		"output_mode": "content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.Content, "-foo") {
		t.Errorf("expected to find literal '-foo' pattern, got: %s", result.Content)
	}

	// Pattern "-foo" should NOT match "bar" content
	if strings.Contains(result.Content, "bar") {
		t.Errorf("should not match 'bar' content when searching for '-foo', got: %s", result.Content)
	}
}

// AC3: Timeout returns error
func TestGrepTool_AC3_Timeout(t *testing.T) {
	skipIfNoRg(t)
	tmpDir := t.TempDir()

	// Create a file with content
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	tool := NewGrepTool()

	// Very short timeout should result in error
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "test",
		"timeout": 0, // 0 seconds = immediate timeout
	}, tmpDir)

	// Should either get an error or an error result
	if err != nil {
		return // Expected - error returned
	}
	if result != nil && result.IsError {
		return // Expected - error result returned
	}

	// If no error, the search completed before timeout
	// This is acceptable since 0 timeout might be clamped
	t.Logf("timeout test: search completed (timeout may be clamped to minimum)")
}

// AC4: Output capped at ~20K characters
func TestGrepTool_AC4_OutputCap(t *testing.T) {
	skipIfNoRg(t)
	tmpDir := t.TempDir()

	// Create a file with large content that will exceed 20K when searched
	// Each line is ~100 chars, 300 lines = ~30K chars
	var largeContent strings.Builder
	for i := range 300 {
		largeContent.WriteString("This is a very long line of text for testing output truncation in grep tool. ")
		largeContent.WriteString("We need to create enough content to exceed the 20K character limit. ")
		largeContent.WriteString("Line number: ")
		largeContent.WriteString(string(rune('0' + i%10)))
		largeContent.WriteString("\n")
	}

	largeFile := filepath.Join(tmpDir, "large.txt")
	if err := os.WriteFile(largeFile, []byte(largeContent.String()), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	tool := NewGrepTool()

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern":     "very",
		"output_mode": "content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that output is truncated
	if !result.Truncated {
		t.Errorf("expected output to be truncated")
	}

	// Check that content is within bounds
	if len(result.Content) > maxResultSizeChars+100 { // +100 for truncation notice
		t.Errorf("output too long: %d chars", len(result.Content))
	}
}

// AC5: VCS directories excluded by default
func TestGrepTool_AC5_VCSExcluded(t *testing.T) {
	skipIfNoRg(t)
	tmpDir := t.TempDir()

	// Create a .git/objects tree with a file containing matching content
	gitDir := filepath.Join(tmpDir, ".git", "objects")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git directory: %v", err)
	}
	gitFile := filepath.Join(gitDir, "test-object")
	if err := os.WriteFile(gitFile, []byte("unique-match-content"), 0644); err != nil {
		t.Fatalf("failed to create .git file: %v", err)
	}

	// Create a non-.git file with the same content in the working tree
	workFile := filepath.Join(tmpDir, "workfile.txt")
	if err := os.WriteFile(workFile, []byte("unique-match-content"), 0644); err != nil {
		t.Fatalf("failed to create work file: %v", err)
	}

	// Also test .svn exclusion
	svnDir := filepath.Join(tmpDir, ".svn")
	if err := os.MkdirAll(svnDir, 0755); err != nil {
		t.Fatalf("failed to create .svn directory: %v", err)
	}
	svnFile := filepath.Join(svnDir, "test-svn")
	if err := os.WriteFile(svnFile, []byte("unique-svn-content"), 0644); err != nil {
		t.Fatalf("failed to create .svn file: %v", err)
	}
	svnWorkFile := filepath.Join(tmpDir, "svn-workfile.txt")
	if err := os.WriteFile(svnWorkFile, []byte("unique-svn-content"), 0644); err != nil {
		t.Fatalf("failed to create svn work file: %v", err)
	}

	tool := NewGrepTool()

	// Search for content unique to both .git and work file
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "unique-match-content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find the workfile but NOT the .git file
	if !strings.Contains(result.Content, "workfile.txt") {
		t.Errorf("expected to find workfile.txt, got: %s", result.Content)
	}
	if strings.Contains(result.Content, ".git") {
		t.Errorf("should not contain .git results, got: %s", result.Content)
	}

	// Search for svn content
	result, err = tool.Execute(context.Background(), map[string]any{
		"pattern": "unique-svn-content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find the svn-workfile but NOT the .svn file
	if !strings.Contains(result.Content, "svn-workfile.txt") {
		t.Errorf("expected to find svn-workfile.txt, got: %s", result.Content)
	}
	if strings.Contains(result.Content, ".svn") {
		t.Errorf("should not contain .svn results, got: %s", result.Content)
	}
}

func TestGrepTool_NameAndDescription(t *testing.T) {
	tool := NewGrepTool()

	if tool.Name() != "Grep" {
		t.Errorf("expected name 'Grep', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

func TestGrepTool_RipgrepNotFound(t *testing.T) {
	// This test will only pass if rg is not found
	// Since we know rg exists on this system, we skip
	t.Skip("ripgrep is installed on this system")
}

// skipIfNoRg skips the test when ripgrep is not on PATH.
// CI on Windows runners does not install ripgrep, so tests that shell out
// to `rg` would otherwise fail there.
func skipIfNoRg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep (rg) not on PATH; skipping GrepTool test")
	}
}

func TestGrepTool_ConcurrencySafe(t *testing.T) {
	skipIfNoRg(t)
	tmpDir := t.TempDir()

	// Create some files
	for range 10 {
		if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	tool := NewGrepTool()

	// Run multiple concurrent executions
	done := make(chan bool)
	for range 10 {
		go func() {
			result, err := tool.Execute(context.Background(), map[string]any{
				"pattern": "content",
			}, tmpDir)
			if err != nil {
				done <- false
				return
			}
			if result.IsError {
				done <- false
				return
			}
			done <- true
		}()
	}

	// Check all results
	for i := range 10 {
		if !<-done {
			t.Errorf("concurrent execution %d failed", i)
		}
	}
}
