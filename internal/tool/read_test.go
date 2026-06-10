package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/constants"
)

func TestReadTool_Execute(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(testFile, []byte("line 1\nline 2\nline 3\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tool := NewReadTool(false, nil)

	tests := []struct {
		name    string
		input   map[string]any
		cwd     string
		wantErr bool
		errMsg  string
		checkFn func(*ToolResult) bool
	}{
		{
			name: "basic file read",
			input: map[string]any{
				"file_path": testFile,
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, "line 1") &&
					strings.Contains(r.Content, "line 2")
			},
		},
		{
			name: "read with line numbers",
			input: map[string]any{
				"file_path": testFile,
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && strings.Contains(r.Content, "\tline 1\n")
			},
		},
		{
			name: "read with offset and limit",
			input: map[string]any{
				"file_path": testFile,
				"offset":    float64(2),
				"limit":     float64(1),
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && strings.Contains(r.Content, "line 2")
			},
		},
		{
			name: "relative path",
			input: map[string]any{
				"file_path": "test.txt",
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && strings.Contains(r.Content, "line 1")
			},
		},
		{
			name: "file does not exist",
			input: map[string]any{
				"file_path": filepath.Join(tmpDir, "nonexistent.txt"),
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError && strings.Contains(r.Content, "[Warning: file does not exist")
			},
		},
		{
			name: "directory instead of file",
			input: map[string]any{
				"file_path": tmpDir,
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && strings.Contains(r.Content, "directory")
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
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(result.Content, tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, result.Content)
				}
				return
			}
			if tt.checkFn != nil && !tt.checkFn(result) {
				t.Errorf("check failed, got content: %q", result.Content)
			}
		})
	}
}

func TestReadTool_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subdirectory
	subDir := filepath.Join(tmpDir, "sub")
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	// Create test file in subdirectory
	testFile := filepath.Join(subDir, "test.txt")
	err = os.WriteFile(testFile, []byte("secret content\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tool := NewReadTool(false, nil)

	tests := []struct {
		name    string
		input   map[string]any
		cwd     string
		allowed bool
	}{
		{
			name: "file in subdirectory is allowed",
			input: map[string]any{
				"file_path": testFile,
			},
			cwd:     tmpDir,
			allowed: true,
		},
		{
			name: "path traversal with .. should be blocked",
			input: map[string]any{
				"file_path": filepath.Join(subDir, "..", "sub", "test.txt"),
			},
			cwd:     tmpDir,
			allowed: true, // Resolved path is still within cwd
		},
		{
			name: "path traversal outside cwd should be blocked",
			input: map[string]any{
				"file_path": filepath.Join(tmpDir, "..", "..", "etc", "hostname"),
			},
			cwd:     tmpDir,
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.input, tt.cwd)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.allowed && result.IsError {
				t.Errorf("expected access to be allowed, but got error: %s", result.Content)
			}
			if !tt.allowed && !result.IsError {
				t.Errorf("expected access to be blocked, but got: %s", result.Content)
			}
		})
	}
}

func TestReadTool_NameAndDescription(t *testing.T) {
	tool := NewReadTool(false, nil)

	if tool.Name() != "Read" {
		t.Errorf("expected name 'Read', got %q", tool.Name())
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

func TestReadTool_SizeLimits(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewReadTool(false, nil)

	// Create a small file (under all limits)
	smallFile := filepath.Join(tmpDir, "small.txt")
	err := os.WriteFile(smallFile, []byte("line 1\nline 2\nline 3\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create small file: %v", err)
	}

	// Create a medium file (between 128KB and 256KB)
	// Use lines with newlines so bufio.Scanner can handle it
	// Need to be >128KB to test maxSizeBytes rejection with128KB limit
	// Also need to test maxTokens with custom max_tokens parameter
	mediumFile := filepath.Join(tmpDir, "medium.txt")
	var mediumLines []string
	for range 1000 {
		mediumLines = append(mediumLines, strings.Repeat("x", 150))
	}
	mediumContent := []byte(strings.Join(mediumLines, "\n") + "\n")
	err = os.WriteFile(mediumFile, mediumContent, 0644)
	if err != nil {
		t.Fatalf("failed to create medium file: %v", err)
	}

	// Create a large file (>256KB)
	largeFile := filepath.Join(tmpDir, "large.txt")
	var largeLines []string
	for range 3000 {
		largeLines = append(largeLines, strings.Repeat("y", 100))
	}
	largeContent := []byte(strings.Join(largeLines, "\n") + "\n")
	err = os.WriteFile(largeFile, largeContent, 0644)
	if err != nil {
		t.Fatalf("failed to create large file: %v", err)
	}

	// Create a file with content that exceeds maxTokens (>100KB of text)
	hugeFile := filepath.Join(tmpDir, "huge.txt")
	var hugeLines []string
	for range 1500 {
		hugeLines = append(hugeLines, strings.Repeat("z", 100))
	}
	hugeContent := []byte(strings.Join(hugeLines, "\n") + "\n")
	err = os.WriteFile(hugeFile, hugeContent, 0644)
	if err != nil {
		t.Fatalf("failed to create huge file: %v", err)
	}

	tests := []struct {
		name        string
		input       map[string]any
		cwd         string
		wantErr     bool
		errContains string
	}{
		{
			name: "small file under default maxSizeBytes succeeds",
			input: map[string]any{
				"file_path": smallFile,
			},
			cwd:     tmpDir,
			wantErr: false,
		},
		{
			name: "large file exceeds default maxSizeBytes rejected pre-read",
			input: map[string]any{
				"file_path": largeFile,
			},
			cwd:         tmpDir,
			wantErr:     true,
			errContains: "maxSizeBytes",
		},
		{
			name: "medium file with max_size 128KB rejected",
			input: map[string]any{
				"file_path": mediumFile,
				"max_size":  float64(128 * 1024),
			},
			cwd:         tmpDir,
			wantErr:     true,
			errContains: "maxSizeBytes",
		},
		{
			name: "medium file with max_size 256KB and high max_tokens succeeds",
			input: map[string]any{
				"file_path":  mediumFile,
				"max_size":   float64(256 * 1024),
				"max_tokens": float64(50000),
			},
			cwd:     tmpDir,
			wantErr: false,
		},
		{
			name: "partial read of large file succeeds (skips size check)",
			input: map[string]any{
				"file_path": largeFile,
				"offset":    float64(1),
				"limit":     float64(10),
			},
			cwd:     tmpDir,
			wantErr: false,
		},
		{
			name: "huge file content exceeds default maxTokens rejected post-read",
			input: map[string]any{
				"file_path": hugeFile,
			},
			cwd:         tmpDir,
			wantErr:     true,
			errContains: "maxTokens",
		},
		{
			name: "huge file with high max_tokens succeeds",
			input: map[string]any{
				"file_path":  hugeFile,
				"max_tokens": float64(50000),
			},
			cwd:     tmpDir,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.input, tt.cwd)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr {
				if !result.IsError {
					t.Errorf("expected error containing %q, got success: %q", tt.errContains, result.Content)
				} else if tt.errContains != "" && !strings.Contains(result.Content, tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, result.Content)
				}
			} else {
				if result.IsError {
					t.Errorf("expected success, got error: %q", result.Content)
				}
			}
		})
	}
}

func TestReadTool_Dedup(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewReadFileCache()
	tool := NewReadTool(false, cache)

	// Create test file
	testFile := filepath.Join(tmpDir, "dedup.txt")
	err := os.WriteFile(testFile, []byte("line 1\nline 2\nline 3\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// AC1: First read should return content
	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("first read should not error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "line 1") || !strings.Contains(result.Content, "line 2") {
		t.Fatalf("first read should return content, got: %s", result.Content)
	}

	// AC1: Second read with same path/offset/limit should return stub
	result, err = tool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("dedup read should not error: %s", result.Content)
	}
	if result.Content != "file unchanged" {
		t.Fatalf("second read should return 'file unchanged' stub, got: %s", result.Content)
	}

	// AC2: Modify file (change mtime), read again should return new content
	err = os.Chtimes(testFile, time.Now(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("failed to change mtime: %v", err)
	}
	newContent := []byte("line 1\nline 2 modified\nline 3\n")
	err = os.WriteFile(testFile, newContent, 0644)
	if err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	result, err = tool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("read after modification should not error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "line 2 modified") {
		t.Fatalf("read after modification should return new content, got: %s", result.Content)
	}

	// AC3: Different offset should bypass dedup and return full content
	result, err = tool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
		"offset":    float64(2),
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("read with different offset should not error: %s", result.Content)
	}
	if result.Content == "file unchanged" {
		t.Fatalf("different offset should bypass dedup, got stub: %s", result.Content)
	}
	if !strings.Contains(result.Content, "line 2") {
		t.Fatalf("different offset should return content, got: %s", result.Content)
	}
}

func TestReadTool_BlockDeviceGuard(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewReadTool(false, nil)

	// AC4: Test deny list for /dev paths
	devPaths := []string{
		"/dev/null",
		"/dev/zero",
		"/dev/urandom",
		"/proc/self/fd/0",
	}

	for _, devPath := range devPaths {
		result, err := tool.Execute(context.Background(), map[string]any{
			"file_path": devPath,
		}, tmpDir)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", devPath, err)
		}
		if !result.IsError {
			t.Fatalf("block device %s should be rejected, got: %s", devPath, result.Content)
		}
		if !strings.Contains(result.Content, "device") {
			t.Fatalf("error should mention device, got: %s", result.Content)
		}
	}

	// AC5: Regular file at a custom path should work normally
	// Create a file with a name that looks suspicious but isn't a device
	regularFile := filepath.Join(tmpDir, "regular_file.txt")
	err := os.WriteFile(regularFile, []byte("regular content\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create regular file: %v", err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": regularFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("regular file should not be rejected: %s", result.Content)
	}
	if !strings.Contains(result.Content, "regular content") {
		t.Fatalf("regular file should return content, got: %s", result.Content)
	}
}

// TestReadTool_SkipPermissions tests AC1: cwd bypass with skipPermissions flag
func TestReadTool_SkipPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewReadTool(false, nil)

	// Test that /etc/passwd is blocked without skipPermissions
	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": "/etc/passwd",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected traversal error without skipPermissions")
	}

	// Test that access is allowed WITH skipPermissions
	toolWithSkip := NewReadTool(true, nil)
	result, err = toolWithSkip.Execute(context.Background(), map[string]any{
		"file_path": "/etc/passwd",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error with skipPermissions: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success with skipPermissions, got error: %s", result.Content)
	}
}

// TestReadTool_ScratchpadAccess tests AC4: scratchpad is always accessible
func TestReadTool_ScratchpadAccess(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewReadTool(false, nil)

	// Override JennyHomeDir to use tmpDir
	originalFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { constants.JennyHomeDirFunc = originalFunc }()

	// Create scratchpad directory
	scratchpadDir := constants.ScratchpadDir()
	if err := os.MkdirAll(scratchpadDir, 0755); err != nil {
		t.Fatalf("failed to create scratchpad dir: %v", err)
	}

	// Create a test file in scratchpad
	testFile := filepath.Join(scratchpadDir, "test.txt")
	err := os.WriteFile(testFile, []byte("scratchpad content\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test that scratchpad file is accessible WITHOUT skipPermissions
	result, err := tool.Execute(context.Background(), map[string]any{
		"file_path": testFile,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error reading scratchpad: %v", err)
	}
	if result.IsError {
		t.Errorf("scratchpad file should be accessible, got error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "scratchpad content") {
		t.Errorf("expected scratchpad content, got: %s", result.Content)
	}

	// Test that /etc/passwd still fails without skipPermissions
	result, err = tool.Execute(context.Background(), map[string]any{
		"file_path": "/etc/passwd",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected /etc/passwd to be blocked without skipPermissions")
	}
}
