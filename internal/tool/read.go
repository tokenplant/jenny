// Package tool provides tool implementations.
package tool

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadTool reads files and returns their contents with line numbers.
type ReadTool struct {
	skipPermissions bool
	readCache       *ReadFileCache
}

// NewReadTool creates a new ReadTool.
func NewReadTool(skipPermissions bool, readCache *ReadFileCache) *ReadTool {
	return &ReadTool{skipPermissions: skipPermissions, readCache: readCache}
}

// Name returns the tool name.
func (t *ReadTool) Name() string {
	return "read"
}

// Description returns a description of the tool.
func (t *ReadTool) Description() string {
	return "Read the contents of a file. Use this to view files with line numbers for reference."
}

// InputSchema returns the JSON schema for tool input.
func (t *ReadTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to read",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "The line number to start reading from (1-indexed)",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "The number of lines to read",
			},
		},
		"required": []string{"file_path"},
	}
}

// Execute reads the file and returns its contents with line numbers.
func (t *ReadTool) Execute(input map[string]any, cwd string) (*ToolResult, error) {
	filePath, ok := input["file_path"].(string)
	if !ok || filePath == "" {
		return nil, fmt.Errorf("file_path is required and must be a string")
	}

	// Resolve relative paths relative to cwd
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	// Create command gate for device path validation
	gate := NewCommandGate(t.skipPermissions)

	// Check device path before access
	if err := gate.CheckDevicePath(filePath); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Access to device path blocked: %v", err),
			IsError: true,
		}, nil
	}

	// Validate path is within working directory (no path traversal)
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		absCwd = cwd
	}
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("invalid file path: %v", err)
	}

	// Clean the absolute path to resolve any traversal sequences
	absFilePathClean := filepath.Clean(absFilePath)

	// Normalize cwd for comparison
	absCwdClean := filepath.Clean(absCwd)

	// The file path must be within or equal to cwd
	// Use proper path boundary check with separator
	if !strings.HasPrefix(absFilePathClean+string(filepath.Separator), absCwdClean+string(filepath.Separator)) && absFilePathClean != absCwdClean {
		return &ToolResult{
			Content: fmt.Sprintf("Error: Access to '%s' is not allowed. File path would traverse outside working directory.", filePath),
			IsError: true,
		}, nil
	}

	// Check if file exists
	info, err := os.Stat(absFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolResult{
				Content: fmt.Sprintf("File does not exist: %s", filePath),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error accessing file: %v", err),
			IsError: true,
		}, nil
	}

	if info.IsDir() {
		return &ToolResult{
			Content: fmt.Sprintf("Error: '%s' is a directory, not a file", filePath),
			IsError: true,
		}, nil
	}

	// Open the file
	file, err := os.Open(absFilePath)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error opening file: %v", err),
			IsError: true,
		}, nil
	}
	defer file.Close()

	// Determine offset and limit
	offset := 1
	offsetExplicit := false
	if offsetVal, ok := input["offset"].(float64); ok {
		offset = int(offsetVal)
		if offset < 1 {
			offset = 1
		}
		offsetExplicit = true
	}

	limit := 0
	limitExplicit := false
	if limitVal, ok := input["limit"].(float64); ok {
		limit = int(limitVal)
		limitExplicit = true
	}

	isFullRead := !offsetExplicit && !limitExplicit

	// Read and process the file
	scanner := bufio.NewScanner(file)
	var lines []string
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		if lineNum < offset {
			continue
		}
		if limit > 0 && len(lines) >= limit {
			break
		}
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error reading file: %v", err),
			IsError: true,
		}, nil
	}

	// Format output with line numbers (matching cat -n format)
	var output strings.Builder
	totalLines := lineNum
	readLines := len(lines)

	for i, line := range lines {
		lineStr := strconv.Itoa(offset + i)
		output.WriteString(fmt.Sprintf("%6s\t%s\n", lineStr, line))
	}

	// Add summary
	output.WriteString(fmt.Sprintf("\n[%d lines, started at line %d, total lines in file: %d]",
		readLines, offset, totalLines))

	result := &ToolResult{
		Content: output.String(),
		IsError: false,
	}

	// Record the read in cache for read-before-write contract
	if t.readCache != nil {
		fullContent := ""
		if isFullRead {
			// Reuse scanner content - the scanner already read the full file
			fullContent = strings.Join(lines, "\n")
			if len(lines) > 0 {
				fullContent += "\n"
			}
		} else {
			// Partial read: need to read full file for cache
			fullContentBytes, _ := os.ReadFile(absFilePath)
			fullContent = string(fullContentBytes)
		}
		// Re-stat after read to get accurate mtime (avoids TOCTOU between stat and cache)
		finalInfo, _ := os.Stat(absFilePath)
		finalMtime := info.ModTime()
		if finalInfo != nil {
			finalMtime = finalInfo.ModTime()
		}
		t.readCache.RecordRead(absFilePath, fullContent, finalMtime, isFullRead)
	}

	return result, nil
}
