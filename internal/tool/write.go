// Package tool provides tool implementations.
package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/constants"
)

// WriteTool writes content to files with read-before-write validation.
type WriteTool struct {
	readCache    *ReadFileCache
	allowedPaths []string // If set, writes are restricted to these paths only
	activator    SkillActivator
	sessionID    string
}

// NewWriteTool creates a new WriteTool.
func NewWriteTool(readCache *ReadFileCache) *WriteTool {
	return &WriteTool{readCache: readCache}
}

// WithSessionID sets the session ID for the WriteTool.
func (t *WriteTool) WithSessionID(id string) *WriteTool {
	t.sessionID = id
	return t
}

// SetAllowedPaths restricts Write to only these paths.
// If nil or empty, no restriction is applied beyond the cwd gate.
func (t *WriteTool) SetAllowedPaths(paths []string) *WriteTool {
	t.allowedPaths = paths
	return t
}

// Name returns the tool name.
func (t *WriteTool) Name() string {
	return "write"
}

// WithReadFileCache sets the read cache for read-before-write validation.
func (t *WriteTool) WithReadFileCache(cache *ReadFileCache) *WriteTool {
	t.readCache = cache
	return t
}

// WithSkillActivator sets the skill activator for path-triggered activation.
func (t *WriteTool) WithSkillActivator(activator SkillActivator) *WriteTool {
	t.activator = activator
	return t
}

// Description returns a description of the tool.
func (t *WriteTool) Description() string {
	return "Write content to a file. Requires prior Read of the same path."
}

// InputSchema returns the JSON schema for tool input.
func (t *WriteTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to write",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		"required": []string{"file_path", "content"},
	}
}

// Execute writes content to a file after validating the read-before-write contract.
func (t *WriteTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	filePath, ok := input["file_path"].(string)
	if !ok || filePath == "" {
		return &ToolResult{
			Content: "file_path is required and must be a string",
			IsError: true,
		}, nil
	}

	content, ok := input["content"].(string)
	if !ok {
		content = ""
	}

	// Resolve relative paths relative to cwd
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	// Clean the path
	filePath = filepath.Clean(filePath)

	// Check allowedPaths restriction first - paths in allowedPaths bypass cwd gate
	// Use prefix matching to allow subdirectories under allowed paths
	if len(t.allowedPaths) > 0 {
		allowed := false
		for _, allowedPath := range t.allowedPaths {
			if filePath == allowedPath || strings.HasPrefix(filePath, allowedPath+string(filepath.Separator)) {
				allowed = true
				break
			}
		}
		if !allowed {
			// Path not in allowlist - apply cwd gate with scratchpad exception
			var pathErr error
			filePath, pathErr = PathInWorkingDir(filePath, cwd, constants.ScratchpadDir(t.sessionID))
			if pathErr != nil {
				return &ToolResult{
					Content: pathErr.Error(),
					IsError: true,
				}, nil
			}
		}
	} else {
		// No allowedPaths restriction - apply cwd gate with scratchpad exception
		var pathErr error
		filePath, pathErr = PathInWorkingDir(filePath, cwd, constants.ScratchpadDir(t.sessionID))
		if pathErr != nil {
			return &ToolResult{
				Content: pathErr.Error(),
				IsError: true,
			}, nil
		}
	}

	// Trigger skill activation based on path access (after path validation, before file I/O)
	if t.activator != nil {
		t.activator.ActivateForPath(filePath)
	}

	// AC1: Check readFileState cache for the path
	entry, exists := t.readCache.GetRead(filePath)
	if !exists {
		return &ToolResult{
			Content: "Cannot write without reading first. Use Read tool on this path before Write.",
			IsError: true,
		}, nil
	}

	// AC2: Check staleness
	info, err := os.Stat(filePath)
	if err == nil {
		// File exists, check mtime
		if info.ModTime().After(entry.Mtime) {
			return &ToolResult{
				Content: "File has changed since it was read. Re-read the file before writing.",
				IsError: true,
			}, nil
		}
	}

	// AC1 continued: Check if entry was a partial read
	if !entry.IsFullRead {
		return &ToolResult{
			Content: "Cannot write after partial read. Use Read tool without offset/limit to get the full file first.",
			IsError: true,
		}, nil
	}

	// Create parent directories (AC3)
	parentDir := filepath.Dir(filePath)
	if parentDir != "" && parentDir != "." {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Failed to create parent directory: %v", err),
				IsError: true,
			}, nil
		}
	}

	// AC4: Atomic write — write to temp file in same directory, sync, rename.
	// This guarantees that a crash never leaves a partially-written target.
	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), ".write-*")
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to create temp file: %v", err),
			IsError: true,
		}, nil
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up on any error path

	// Normalize path separators on Windows for consistent rename.
	if runtime.GOOS == "windows" {
		tmpPath = filepath.FromSlash(tmpPath)
	}

	if _, err := tmpFile.Write([]byte(content)); err != nil {
		tmpFile.Close()
		return &ToolResult{
			Content: fmt.Sprintf("Failed to write temp file: %v", err),
			IsError: true,
		}, nil
	}

	// Sync to durable storage before rename.
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return &ToolResult{
			Content: fmt.Sprintf("Failed to sync temp file: %v", err),
			IsError: true,
		}, nil
	}
	tmpFile.Close()

	// Atomic rename with cross-device fallback.
	if err := os.Rename(tmpPath, filePath); err != nil {
		if isCrossDeviceErr(err) {
			if fbErr := copyAndReplace(tmpPath, filePath); fbErr != nil {
				return &ToolResult{
					Content: fmt.Sprintf("Failed to rename temp file (EXDEV fallback failed): %v / %v", err, fbErr),
					IsError: true,
				}, nil
			}
		} else if runtime.GOOS == "windows" {
			time.Sleep(10 * time.Millisecond)
			if retryErr := os.Rename(tmpPath, filePath); retryErr != nil {
				if fbErr := copyAndReplace(tmpPath, filePath); fbErr != nil {
					return &ToolResult{
						Content: fmt.Sprintf("Failed to rename temp file: %v (retry failed: %v, fallback also failed: %v)", err, retryErr, fbErr),
						IsError: true,
					}, nil
				}
			}
		} else {
			return &ToolResult{
				Content: fmt.Sprintf("Failed to rename temp file: %v", err),
				IsError: true,
			}, nil
		}
	}

	// Get new mtime after write
	newInfo, _ := os.Stat(filePath)
	var newMtime = entry.Mtime
	if newInfo != nil {
		newMtime = newInfo.ModTime()
	}

	// Generate patch diff (AC4)
	oldContent := entry.Content
	if oldContent == "" && exists {
		oldContent = ""
	}
	diff := GenerateUnifiedDiff(oldContent, content, filePath)

	// Update readFileCache after successful write (AC5)
	t.readCache.UpdateAfterWrite(filePath, content, newMtime)

	return &ToolResult{
		Content: diff,
		IsError: false,
	}, nil
}

// PathInWorkingDir checks if a path is within the working directory or scratchpadDir.
func PathInWorkingDir(filePath, cwd string, scratchpadDirs ...string) (string, error) {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		absCwd = cwd
	}
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("invalid file path: %w", err)
	}

	absFilePath = filepath.Clean(absFilePath)
	absCwd = filepath.Clean(absCwd)
	sep := string(filepath.Separator)

	// Check if path is within scratchpad directories
	for _, scratchpadDir := range scratchpadDirs {
		if scratchpadDir != "" && strings.HasPrefix(absFilePath+sep, filepath.Clean(scratchpadDir)+sep) {
			return absFilePath, nil
		}
	}

	if !strings.HasPrefix(absFilePath+sep, absCwd+sep) && absFilePath != absCwd {
		return "", fmt.Errorf("access to '%s' is not allowed: path would traverse outside working directory", filePath)
	}

	return absFilePath, nil
}


