package tool

import (
	"os"
	"path/filepath"
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

	tool := NewReadTool()

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
			result, err := tool.Execute(tt.input, tt.cwd)
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

	tool := NewReadTool()

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
			result, err := tool.Execute(tt.input, tt.cwd)
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
	tool := NewReadTool()

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
