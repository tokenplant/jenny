// Package tool provides tool implementations.
package tool

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ipy/jenny/internal/constants"
)

// EditTool performs exact string replacement in files.
type EditTool struct {
	readCache    *ReadFileCache
	allowedPaths []string // If set, edits are restricted to these paths only
	activator    SkillActivator
	sessionID    string
}

// NewEditTool creates a new EditTool.
func NewEditTool(readCache *ReadFileCache) *EditTool {
	return &EditTool{readCache: readCache}
}

// WithSessionID sets the session ID for the EditTool.
func (t *EditTool) WithSessionID(id string) *EditTool {
	t.sessionID = id
	return t
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
	return "Replace exact string in a file. Requires prior Read of the same path. Supports scoped edits with start_line/end_line for partial reads."
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
			"start_line": map[string]any{
				"type":        "number",
				"description": "First line (1-indexed) of scoped replacement range. Required when editing after partial read.",
			},
			"end_line": map[string]any{
				"type":        "number",
				"description": "Last line (1-indexed, inclusive) of scoped replacement range. Required when start_line is provided.",
			},
			"num_expected": map[string]any{
				"type":        "number",
				"description": "Expected number of replacements. If actual count differs, the operation is aborted.",
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

	// Parse optional scoped editing parameters
	startLine := 0
	if startVal, ok := input["start_line"].(float64); ok {
		startLine = int(startVal)
	}

	endLine := 0
	if endVal, ok := input["end_line"].(float64); ok {
		endLine = int(endVal)
	}

	numExpected := 0
	if numVal, ok := input["num_expected"].(float64); ok {
		numExpected = int(numVal)
	}

	// Validate that end_line >= start_line when both provided
	if startLine > 0 && endLine > 0 && endLine < startLine {
		return &ToolResult{
			Content: fmt.Sprintf("end_line (%d) must be >= start_line (%d)", endLine, startLine),
			IsError: true,
		}, nil
	}

	isScoped := startLine > 0 && endLine > 0

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
			// Path not in allowlist - apply cwd gate with scratchpad exception
			var pathErr error
			filePath, pathErr = PathInWorkingDir(resolvedPath, cwd, constants.ScratchpadDir(t.sessionID))
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
		// No allowedPaths restriction - apply cwd gate with scratchpad exception
		var pathErr error
		filePath, pathErr = PathInWorkingDir(resolvedPath, cwd, constants.ScratchpadDir(t.sessionID))
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

	// AC1 continued: For partial reads, require scoped range contained within read range
	if !entry.IsFullRead {
		if !isScoped {
			return &ToolResult{
				Content: "Cannot edit after partial read without start_line and end_line. " +
					"Read the full file first, or provide start_line/end_line within the read range.",
				IsError: true,
			}, nil
		}
		// Validate that the scoped range is contained within the read range
		// Read range is [offset, offset+limit-1] (1-indexed, inclusive)
		readStart := entry.Offset
		readEnd := entry.Offset + entry.Limit - 1
		if startLine < readStart || endLine > readEnd {
			return &ToolResult{
				Content: fmt.Sprintf("Scoped range [%d, %d] is outside read range [%d, %d]. "+
					"Read the full file first, or adjust start_line/end_line.", startLine, endLine, readStart, readEnd),
				IsError: true,
			}, nil
		}
	}

	// AC3: Check old === new
	if oldString == newString {
		return &ToolResult{
			Content: "old_string and new_string must differ",
			IsError: true,
		}, nil
	}

	// Dispatch: scoped streaming edit vs. global in-memory edit
	if isScoped {
		return t.executeScoped(filePath, oldString, newString, replaceAll, startLine, endLine, numExpected, entry)
	}
	return t.executeGlobal(filePath, oldString, newString, replaceAll, numExpected, entry, info)
}

// executeGlobal performs the original in-memory replacement (full file read).
func (t *EditTool) executeGlobal(filePath, oldString, newString string, replaceAll bool, numExpected int, entry *ReadFileEntry, info os.FileInfo) (*ToolResult, error) {
	// AC4: 1 GiB size guard must run before os.ReadFile. The `info` parameter
	// is non-nil for existing files (see call site at line 254). Without
	// this guard, a >1GiB file would be loaded into memory before the size
	// check rejected it, causing an OOM.
	if info != nil && info.Size() > 1<<30 {
		return &ToolResult{
			Content: "File exceeds maximum size of 1 GiB",
			IsError: true,
		}, nil
	}

	// Read current file content from disk
	currentContent, err := os.ReadFile(filePath)
	if err != nil {
		// File doesn't exist - check if readFileState has entry
		if os.IsNotExist(err) {
			return t.handleMissingFile(filePath, oldString, newString, entry)
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

	// Check num_expected first (safety guard)
	if numExpected > 0 && count != numExpected {
		return &ToolResult{
			Content: fmt.Sprintf("Expected %d replacement(s) but found %d. Operation aborted.", numExpected, count),
			IsError: true,
		}, nil
	}

	// Zero matches: return specific error with snippet
	if count == 0 {
		return zeroMatchError(content), nil
	}

	// AC4: Multiple matches require replace_all
	if count > 1 && !replaceAll {
		return &ToolResult{
			Content: fmt.Sprintf("String found %d times. Set replace_all=true to replace all occurrences.", count),
			IsError: true,
		}, nil
	}

	// Check for binary content (null bytes indicate binary)
	if isBinary(content) {
		return &ToolResult{
			Content: "Cannot edit binary files",
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

	// Write new content
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to write file: %v", err),
			IsError: true,
		}, nil
	}

	return t.finalizeEdit(filePath, newContent, entry)
}

// executeScoped performs a line-by-line streaming edit restricted to a line range.
// Only the scoped line range is buffered in memory; before and after sections
// stream directly through to a temp file for O(1) memory usage.
func (t *EditTool) executeScoped(filePath, oldString, newString string, replaceAll bool, startLine, endLine, numExpected int, entry *ReadFileEntry) (*ToolResult, error) {
	// Open the file for streaming
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return t.handleMissingFile(filePath, oldString, newString, entry)
		}
		return &ToolResult{
			Content: fmt.Sprintf("Failed to open file: %v", err),
			IsError: true,
		}, nil
	}
	defer file.Close()

	// Create a temporary file in the same directory for atomic write
	tmpFile, err := os.CreateTemp(filepath.Dir(filePath), ".edit-*")
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to create temp file: %v", err),
			IsError: true,
		}, nil
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up on any error path

	// Normalize path separators on Windows so the rename call uses consistent paths.
	// filepath.Dir returns backslashes; os.CreateTemp on some Go versions may return
	// forward slashes. Mixing separators can cause "Access is denied" errors on Windows.
	if runtime.GOOS == "windows" {
		tmpPath = filepath.FromSlash(tmpPath)
	}

	// Use bufio.Reader with ReadBytes for line-level granularity
	// that preserves delimiter info for streaming before/after sections.
	reader := bufio.NewReader(file)

	// Phase 1: Stream before-range lines directly to tmpFile (no buffering)
	for lineNum := 1; lineNum < startLine; lineNum++ {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) == 0 {
			// EOF before startLine — the range is past end of file
			tmpFile.Close()
			return &ToolResult{
				Content: fmt.Sprintf("File has only %d lines, but scoped range starts at line %d", lineNum-1, startLine),
				IsError: true,
			}, nil
		}
		if _, writeErr := tmpFile.Write(normalizeLineEndingsBytes(lineBytes)); writeErr != nil {
			tmpFile.Close()
			return &ToolResult{
				Content: fmt.Sprintf("Failed to write to temp file: %v", writeErr),
				IsError: true,
			}, nil
		}
		if readErr != nil {
			break // EOF after last line read
		}
	}

	// Phase 2: Buffer scoped-range lines (bounded by [startLine, endLine])
	var scopedLines []string       // Lines within [startLine, endLine], trailing \n stripped
	scopedEndsWithNewline := false // Whether the last scoped line had a \n delimiter

	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) == 0 {
			break // EOF before endLine — use whatever was read
		}
		// Store normalized line (CRLF→LF) with trailing \n stripped
		normalized := normalizeLineEndingsBytes(lineBytes)
		scopedEndsWithNewline = len(normalized) > 0 && normalized[len(normalized)-1] == '\n'
		scopedLines = append(scopedLines, trimNewline(string(normalized)))
		if readErr != nil {
			break
		}
	}

	// Build scoped content for matching (joined by \n, no trailing \n)
	scopedContent := strings.Join(scopedLines, "\n")

	// Count matches
	count := strings.Count(scopedContent, oldString)

	// Check num_expected first (safety guard)
	if numExpected > 0 && count != numExpected {
		tmpFile.Close()
		return &ToolResult{
			Content: fmt.Sprintf("Expected %d replacement(s) but found %d in scoped range [%d,%d]. Operation aborted.",
				numExpected, count, startLine, endLine),
			IsError: true,
		}, nil
	}

	// Zero matches: return specific error with snippet
	if count == 0 {
		tmpFile.Close()
		return zeroMatchError(scopedContent), nil
	}

	// AC4: Multiple matches require replace_all
	if count > 1 && !replaceAll {
		tmpFile.Close()
		return &ToolResult{
			Content: fmt.Sprintf("String found %d times in scoped range [%d,%d]. Set replace_all=true to replace all occurrences.",
				count, startLine, endLine),
			IsError: true,
		}, nil
	}

	// Check for binary content in scoped lines
	if isBinary(scopedContent) {
		tmpFile.Close()
		return &ToolResult{
			Content: "Cannot edit binary files",
			IsError: true,
		}, nil
	}

	// Apply replacement
	var modifiedScoped string
	if replaceAll {
		modifiedScoped = strings.Replace(scopedContent, oldString, newString, -1)
	} else {
		modifiedScoped = strings.Replace(scopedContent, oldString, newString, 1)
	}

	// Write modified scoped content
	if _, writeErr := tmpFile.WriteString(modifiedScoped); writeErr != nil {
		tmpFile.Close()
		return &ToolResult{
			Content: fmt.Sprintf("Failed to write modified content: %v", writeErr),
			IsError: true,
		}, nil
	}

	// Add newline separator between scoped content and after-lines if the
	// original file had a newline after the last scoped line.
	if scopedEndsWithNewline && !strings.HasSuffix(modifiedScoped, "\n") {
		if _, writeErr := tmpFile.WriteString("\n"); writeErr != nil {
			tmpFile.Close()
			return &ToolResult{
				Content: fmt.Sprintf("Failed to write newline separator: %v", writeErr),
				IsError: true,
			}, nil
		}
	}

	// Phase 3: Stream after-range lines directly to tmpFile (no buffering).
	// Normalize line endings to maintain consistency (before-lines and scoped
	// content are already LF-normalized).
	for {
		lineBytes, readErr := reader.ReadBytes('\n')
		if len(lineBytes) > 0 {
			normalized := normalizeLineEndingsBytes(lineBytes)
			if _, writeErr := tmpFile.Write(normalized); writeErr != nil {
				tmpFile.Close()
				return &ToolResult{
					Content: fmt.Sprintf("Failed to write remaining file content: %v", writeErr),
					IsError: true,
				}, nil
			}
		}
		if readErr != nil {
			break
		}
	}

	// AC5: Atomic edit with fsync and cross-device fallback.
	//
	// 1. Sync the temp file's contents to disk before closing so the rename
	//    is durable across power loss / crash. Without Sync, a crash between
	//    Close and Rename could leave the file with stale or missing
	//    contents.
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return &ToolResult{
			Content: fmt.Sprintf("Failed to sync temp file: %v", err),
			IsError: true,
		}, nil
	}
	tmpFile.Close()

	// 2. Atomic rename. On Windows, os.Rename can fail with "Access is denied"
	// (transient AV scanner handle on the temp file) even when the paths are
	// on the same device. Fall back to copy+replace for any non-EXDEV error,
	// so the edit succeeds even under AV interference.
	if err := os.Rename(tmpPath, filePath); err != nil {
		if isCrossDeviceErr(err) {
			if fbErr := copyAndReplace(tmpPath, filePath); fbErr != nil {
				return &ToolResult{
					Content: fmt.Sprintf("Failed to rename temp file (and EXDEV fallback failed): %v / %v", err, fbErr),
					IsError: true,
				}, nil
			}
		} else {
			// On Windows, retry once after a brief sleep — transient AV handle often releases quickly.
			if runtime.GOOS == "windows" {
				time.Sleep(10 * time.Millisecond)
				if retryErr := os.Rename(tmpPath, filePath); retryErr == nil {
					// Success on retry — fall through to finalize
				} else {
					// Retry failed too — fall back to copy+replace for any non-EXDEV error.
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
	}

	// Build new content for cache: need full file for diff generation
	newFullContent, readErr := os.ReadFile(filePath)
	var newContentStr string
	if readErr == nil {
		newContentStr = string(newFullContent)
	}

	return t.finalizeEdit(filePath, newContentStr, entry)
}

// normalizeLineEndingsBytes converts CRLF to LF in a byte slice.
func normalizeLineEndingsBytes(data []byte) []byte {
	return []byte(normalizeLineEndings(string(data)))
}

// trimNewline removes a single trailing \n if present.
func trimNewline(s string) string {
	return strings.TrimSuffix(s, "\n")
}

// handleMissingFile implements the "old_string == ” on missing file" create-semantic.
func (t *EditTool) handleMissingFile(filePath, oldString, newString string, entry *ReadFileEntry) (*ToolResult, error) {
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
		return t.finalizeEdit(filePath, newString, entry)
	}
	return &ToolResult{
		Content: "Cannot edit without reading first. Use Read tool on this path before Edit.",
		IsError: true,
	}, nil
}

// finalizeEdit updates the cache and returns the diff result.
func (t *EditTool) finalizeEdit(filePath, newContent string, entry *ReadFileEntry) (*ToolResult, error) {
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

// zeroMatchError creates a helpful error when old_string is not found.
func zeroMatchError(content string) *ToolResult {
	previewLen := min(len(content), 100)
	preview := content[:previewLen]
	if previewLen < len(content) {
		preview += "..."
	}
	preview = strings.ReplaceAll(preview, "\n", "\\n")
	return &ToolResult{
		Content: fmt.Sprintf("String not found in file. First 100 chars: %s", preview),
		IsError: true,
	}
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

// isCrossDeviceErr reports whether err is a *os.LinkError whose underlying
// errno is EXDEV (cross-device link). Used to trigger the EXDEV fallback
// path in executeScoped.
func isCrossDeviceErr(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return false
}

// copyAndReplace is the fallback for executeScoped. It copies the temp
// file's contents to filePath, deletes the temp file, and returns any
// error encountered. On Windows, O_TRUNC overwrites the existing file
// in-place without needing to delete it first (avoids "file in use" errors
// when an AV scanner or the Read tool has the file open).
func copyAndReplace(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening src: %w", err)
	}
	defer src.Close()

	// On Windows, retry once if dst is briefly held by an AV scanner.
	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		if runtime.GOOS == "windows" {
			time.Sleep(10 * time.Millisecond)
			dst, err = os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		}
		if err != nil {
			return fmt.Errorf("opening dst: %w", err)
		}
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copying contents: %w", err)
	}

	// Best-effort cleanup of the temp file. A failure here does not
	// affect the edit's correctness.
	_ = os.Remove(srcPath)
	return nil
}
