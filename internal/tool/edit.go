// Package tool provides tool implementations.
package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// EditTool performs exact string replacement in files.
type EditTool struct {
	readCache    *ReadFileCache
	allowedPaths []string // If set, edits are restricted to these paths only
	activator    SkillActivator
}

// NewEditTool creates a new EditTool.
func NewEditTool(readCache *ReadFileCache) *EditTool {
	return &EditTool{readCache: readCache}
}

// SetAllowedPaths restricts Edit to only these paths.
// If nil or empty, no restriction is applied.
func (t *EditTool) SetAllowedPaths(paths []string) *EditTool {
	t.allowedPaths = paths
	return t
}

// Name returns the tool name.
func (t *EditTool) Name() string {
	return "edit"
}

// WithReadFileCache sets the read cache for read-before-write validation.
func (t *EditTool) WithReadFileCache(cache *ReadFileCache) *EditTool {
	t.readCache = cache
	return t
}

// WithSkillActivator sets the skill activator for path-triggered activation.
func (t *EditTool) WithSkillActivator(activator SkillActivator) *EditTool {
	t.activator = activator
	return t
}

// Description returns a description of the tool.
func (t *EditTool) Description() string {
	return "Replace exact string in a file. Requires prior Read of the same path."
}

// InputSchema returns the JSON schema for tool input.
func (t *EditTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to edit",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The exact string to find and replace",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The replacement string",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences (required when multiple matches)",
			},
		},
		"required": []string{"file_path", "old_string", "new_string"},
	}
}

// Execute replaces exact string in a file after validating the read-before-write contract.
func (t *EditTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	filePath, ok := input["file_path"].(string)
	if !ok || filePath == "" {
		return &ToolResult{
			Content: "file_path is required and must be a string",
			IsError: true,
		}, nil
	}

	oldString, ok := input["old_string"].(string)
	if !ok {
		oldString = ""
	}

	newString, ok := input["new_string"].(string)
	if !ok {
		newString = ""
	}

	replaceAll, _ := input["replace_all"].(bool)

	// Resolve relative paths relative to cwd (but preserve tilde for now)
	resolvedPath := filePath
	if !filepath.IsAbs(filePath) {
		resolvedPath = filepath.Join(cwd, filePath)
	}
	resolvedPath = filepath.Clean(resolvedPath)

	// Check allowedPaths restriction first - paths in allowedPaths bypass cwd gate
	// Use prefix matching to allow subdirectories under allowed paths
	if len(t.allowedPaths) > 0 {
		allowed := false
		for _, allowedPath := range t.allowedPaths {
			if resolvedPath == allowedPath || strings.HasPrefix(resolvedPath, allowedPath+string(filepath.Separator)) {
				allowed = true
				break
			}
		}
		if !allowed {
			// Path not in allowlist - apply cwd gate
			var pathErr error
			filePath, pathErr = PathInWorkingDir(resolvedPath, cwd)
			if pathErr != nil {
				return &ToolResult{
					Content: pathErr.Error(),
					IsError: true,
				}, nil
			}
		} else {
			// Path is in allowlist - skip cwd gate and use resolved path
			filePath = resolvedPath
		}
	} else {
		// No allowedPaths restriction - apply cwd gate
		var pathErr error
		filePath, pathErr = PathInWorkingDir(resolvedPath, cwd)
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

	// AC5: Check .ipynb extension
	if strings.HasSuffix(filePath, ".ipynb") {
		return &ToolResult{
			Content: "Editing .ipynb files requires NotebookEdit tool. Use .py source file instead.",
			IsError: true,
		}, nil
	}

	// AC1: Check readFileState cache for the path
	entry, exists := t.readCache.GetRead(filePath)
	if !exists {
		return &ToolResult{
			Content: "Cannot edit without reading first. Use Read tool on this path before Edit.",
			IsError: true,
		}, nil
	}

	// AC2: Check staleness
	info, err := os.Stat(filePath)
	if err == nil {
		// File exists, check mtime
		if info.ModTime().After(entry.Mtime) {
			return &ToolResult{
				Content: "File has changed since it was read. Re-read the file before editing.",
				IsError: true,
			}, nil
		}
	}

	// AC1 continued: Check if entry was a partial read
	if !entry.IsFullRead {
		return &ToolResult{
			Content: "Cannot edit after partial read. Use Read tool without offset/limit to get the full file first.",
			IsError: true,
		}, nil
	}

	// AC3: Check old === new
	if oldString == newString {
		return &ToolResult{
			Content: "old_string and new_string must differ",
			IsError: true,
		}, nil
	}

	// Read current file content from disk
	currentContent, err := os.ReadFile(filePath)
	if err != nil {
		// File doesn't exist - check if readFileState has entry
		if os.IsNotExist(err) {
			// AC1 edge case: file doesn't exist but has read cache entry
			// Create the file with new_string content if oldString is empty
			if oldString == "" {
				// Create parent directories if needed
				parentDir := filepath.Dir(filePath)
				if parentDir != "" && parentDir != "." {
					if mkErr := os.MkdirAll(parentDir, 0755); mkErr != nil {
						return &ToolResult{
							Content: fmt.Sprintf("Failed to create parent directory: %v", mkErr),
							IsError: true,
						}, nil
					}
				}
				if writeErr := os.WriteFile(filePath, []byte(newString), 0644); writeErr != nil {
					return &ToolResult{
						Content: fmt.Sprintf("Failed to create file: %v", writeErr),
						IsError: true,
					}, nil
				}
				// Get new mtime
				newInfo, _ := os.Stat(filePath)
				newMtime := entry.Mtime
				if newInfo != nil {
					newMtime = newInfo.ModTime()
				}
				// Update cache
				t.readCache.RecordRead(filePath, newString, newMtime, true, 0, 0)
				return &ToolResult{
					Content: fmt.Sprintf("Created file with content: %s", newString),
					IsError: false,
				}, nil
			}
			return &ToolResult{
				Content: "Cannot edit without reading first. Use Read tool on this path before Edit.",
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Failed to read file: %v", err),
			IsError: true,
		}, nil
	}

	// Normalize line endings: CRLF -> LF for matching
	content := string(currentContent)
	content = normalizeLineEndings(content)

	// Count occurrences of old_string in current content
	count := strings.Count(content, oldString)

	// AC4: Multiple matches require replace_all
	if count > 1 && !replaceAll {
		return &ToolResult{
			Content: fmt.Sprintf("String found %d times. Set replace_all=true to replace all occurrences.", count),
			IsError: true,
		}, nil
	}

	// Zero matches: return specific error with snippet
	if count == 0 {
		// Provide context about where the string was expected
		previewLen := min(len(content), 100)
		preview := content[:previewLen]
		if previewLen < len(content) {
			preview += "..."
		}
		preview = strings.ReplaceAll(preview, "\n", "\\n")
		return &ToolResult{
			Content: fmt.Sprintf("String not found in file. First 100 chars: %s", preview),
			IsError: true,
		}, nil
	}

	// Apply replacement
	var newContent string
	if replaceAll {
		newContent = strings.Replace(content, oldString, newString, -1)
	} else {
		newContent = strings.Replace(content, oldString, newString, 1)
	}

	// Check for binary content (null bytes indicate binary)
	if isBinary(content) {
		return &ToolResult{
			Content: "Cannot edit binary files",
			IsError: true,
		}, nil
	}

	// Max file size check: 1 GiB
	if info != nil && info.Size() > 1<<30 {
		return &ToolResult{
			Content: "File exceeds maximum size of 1 GiB",
			IsError: true,
		}, nil
	}

	// Write new content
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to write file: %v", err),
			IsError: true,
		}, nil
	}

	// Get new mtime after write
	newInfo, _ := os.Stat(filePath)
	newMtime := entry.Mtime
	if newInfo != nil {
		newMtime = newInfo.ModTime()
	}

	// Generate patch diff using old content from cache vs new content
	oldContentFromCache := entry.Content
	diff := GenerateUnifiedDiff(oldContentFromCache, newContent, filePath)

	// Update readFileCache after successful edit
	t.readCache.RecordRead(filePath, newContent, newMtime, true, 0, 0)

	return &ToolResult{
		Content: diff,
		IsError: false,
	}, nil
}

// normalizeLineEndings converts CRLF to LF for consistent matching.
func normalizeLineEndings(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return content
}

// isBinary checks if content appears to be binary.
func isBinary(content string) bool {
	return slices.Contains([]byte(content), 0)
}
