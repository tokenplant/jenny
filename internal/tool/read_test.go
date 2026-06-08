package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
					contains(r.Content, "line 1") &&
					contains(r.Content, "line 2")
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
				return r != nil && contains(r.Content, "\tline 1\n")
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
				return r != nil && contains(r.Content, "line 2")
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
				return r != nil && contains(r.Content, "line 1")
			},
		},
		{
			name: "file does not exist",
			input: map[string]any{
				"file_path": filepath.Join(tmpDir, "nonexistent.txt"),
			},
			cwd:     tmpDir,
			wantErr: false, // Returns error in content
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError && contains(r.Content, "does not exist")
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
				return r != nil && r.IsError && contains(r.Content, "directory")
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
				if !contains(result.Content, tt.errMsg) {
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
				"file_path": filepath.Join(tmpDir, "..", "..", "etc", "passwd"),
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

	if tool.Name() != "read" {
		t.Errorf("expected name 'read', got %q", tool.Name())
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
				} else if tt.errContains != "" && !contains(result.Content, tt.errContains) {
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
