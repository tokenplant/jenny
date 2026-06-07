package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlobTool_Execute(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create test files with different modification times
	files := []string{
		"a.txt",
		"b.txt",
		"sub/c.txt",
		"sub/d.txt",
		"sub/nested/e.txt",
	}

	for _, f := range files {
		fullPath := filepath.Join(tmpDir, f)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		// Set different modification times
		time.Sleep(time.Millisecond)
	}

	// Create a file that will be matched by glob pattern
	goFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to create go file: %v", err)
	}

	tool := NewGlobTool()

	tests := []struct {
		name    string
		input   map[string]any
		cwd     string
		wantErr bool
		errMsg  string
		checkFn func(*ToolResult) bool
		emptyOk bool // true if empty result is acceptable
	}{
		{
			name: "basic pattern matching",
			input: map[string]any{
				"pattern": "*.txt",
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
			name: "recursive pattern **/*.txt",
			input: map[string]any{
				"pattern": "**/*.txt",
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
			name: "pattern in subdirectory",
			input: map[string]any{
				"pattern": "sub/*.txt",
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, "sub/c.txt") &&
					strings.Contains(r.Content, "sub/d.txt") &&
					!strings.Contains(r.Content, "a.txt")
			},
		},
		{
			name: "pattern with path parameter",
			input: map[string]any{
				"pattern": "*.txt",
				"path":    "sub",
			},
			cwd:     tmpDir,
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError &&
					strings.Contains(r.Content, "c.txt") &&
					strings.Contains(r.Content, "d.txt")
			},
		},
		{
			name: "no matches returns empty",
			input: map[string]any{
				"pattern": "*.xyz",
			},
			cwd:     tmpDir,
			wantErr: false,
			emptyOk: true,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError && r.Content == "No files found"
			},
		},
		{
			name: "non-existent path errors",
			input: map[string]any{
				"pattern": "*.txt",
				"path":    "/non/existent/path",
			},
			cwd:     tmpDir,
			wantErr: true,
			errMsg:  "path is not a directory",
		},
		{
			name: "file as path errors",
			input: map[string]any{
				"pattern": "*.txt",
				"path":    "a.txt",
			},
			cwd:     tmpDir,
			wantErr: true,
			errMsg:  "path is not a directory",
		},
		{
			name: "missing pattern errors",
			input: map[string]any{
				"pattern": "",
			},
			cwd:     tmpDir,
			wantErr: true,
			errMsg:  "pattern is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.input, tt.cwd)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("unexpected error: %v", err)
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if result == nil || !strings.Contains(result.Content, tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, result.Content)
				}
				return
			}
			if tt.emptyOk && result != nil && result.Content == "No files found" {
				return // Expected empty result
			}
			if tt.checkFn != nil && !tt.checkFn(result) {
				t.Errorf("check failed, got content: %q, truncated: %v", result.Content, result.Truncated)
			}
		})
	}
}

func TestGlobTool_AC1_Max100Results(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 150 files
	for i := 0; i < 150; i++ {
		fullPath := filepath.Join(tmpDir, filepath.Join("dir", fmt.Sprintf("file%d.txt", i)))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	tool := NewGlobTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.txt",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count results
	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	count := len(lines)

	if count != 100 {
		t.Errorf("expected 100 results, got %d", count)
	}
	if !result.Truncated {
		t.Errorf("expected truncated to be true")
	}
}

func TestGlobTool_AC2_RelativePaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file
	subDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	tool := NewGlobTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "**/*.go",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain relative path
	if !strings.Contains(result.Content, "src/main.go") {
		t.Errorf("expected relative path 'src/main.go', got: %s", result.Content)
	}
	// Should NOT contain absolute path
	if strings.Contains(result.Content, tmpDir) {
		t.Errorf("should not contain absolute path, got: %s", result.Content)
	}
}

func TestGlobTool_AC3_EmptyResult(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewGlobTool()

	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "*.nonexistent",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Content != "No files found" {
		t.Errorf("expected 'No files found', got: %s", result.Content)
	}
	if result.IsError {
		t.Errorf("expected IsError to be false")
	}
}

func TestGlobTool_AC4_NonDirectoryPathError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file
	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	tool := NewGlobTool()

	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "file instead of directory",
			path:    "file.txt",
			wantErr: true,
			errMsg:  "path is not a directory",
		},
		{
			name:    "non-existent path",
			path:    "does/not/exist",
			wantErr: true,
			errMsg:  "path is not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), map[string]any{
				"pattern": "*.txt",
				"path":    tt.path,
			}, tmpDir)
			if tt.wantErr {
				if err == nil {
					// Error returned in result
					if result == nil || !strings.Contains(result.Content, tt.errMsg) {
						t.Errorf("expected error containing %q, got %q", tt.errMsg, result.Content)
					}
				} else {
					// Error returned as error
					if !strings.Contains(err.Error(), tt.errMsg) {
						t.Errorf("expected error containing %q, got %v", tt.errMsg, err)
					}
				}
			}
		})
	}
}

func TestGlobTool_AC5_ConcurrencySafe(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some files
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	tool := NewGlobTool()

	// Run multiple concurrent executions
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			result, err := tool.Execute(context.Background(), map[string]any{
				"pattern": "*.txt",
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
	for i := 0; i < 10; i++ {
		if !<-done {
			t.Errorf("concurrent execution %d failed", i)
		}
	}
}

func TestGlobTool_NameAndDescription(t *testing.T) {
	tool := NewGlobTool()

	if tool.Name() != "Glob" {
		t.Errorf("expected name 'Glob', got %q", tool.Name())
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

func TestGlobTool_SortedByMtime(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files with known modification times
	fileA := filepath.Join(tmpDir, "oldest.txt")
	fileB := filepath.Join(tmpDir, "newer.txt")
	fileC := filepath.Join(tmpDir, "newest.txt")

	os.WriteFile(fileA, []byte("a"), 0644)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(fileB, []byte("b"), 0644)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(fileC, []byte("c"), 0644)

	// Set file A to be older by explicitly touching it
	time.Sleep(10 * time.Millisecond)
	os.Chtimes(fileA, time.Now().Add(-1*time.Hour), time.Now().Add(-1*time.Hour))

	tool := NewGlobTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "*.txt",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 results, got %d", len(lines))
	}

	// Newest first: newest.txt, newer.txt, oldest.txt
	if !strings.Contains(lines[0], "newest") {
		t.Errorf("expected first result to be newest.txt, got: %s", lines[0])
	}
	if !strings.Contains(lines[2], "oldest") {
		t.Errorf("expected last result to be oldest.txt, got: %s", lines[2])
	}
}
