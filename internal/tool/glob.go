package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GlobTool finds files matching a glob pattern.
type GlobTool struct{}

// NewGlobTool creates a new GlobTool.
func NewGlobTool() *GlobTool {
	return &GlobTool{}
}

// Name returns the tool name.
func (t *GlobTool) Name() string {
	return "Glob"
}

// Description returns a description of the tool.
func (t *GlobTool) Description() string {
	return "Find files matching a glob pattern. Returns paths relative to cwd, sorted newest first, max 100 results."
}

// InputSchema returns the JSON schema for tool input.
func (t *GlobTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search (default: cwd)",
			},
		},
		"required": []string{"pattern"},
	}
}

// fileMatch holds a file path with its modification time for sorting.
type fileMatch struct {
	path  string
	mtime int64
}

// matchGlob handles ** glob pattern to match across directory separators.
func matchGlob(pattern, name string) bool {
	// Handle ** matching any sequence of characters including separators
	if strings.Contains(pattern, "**") {
		// Split pattern by **
		segments := strings.Split(pattern, "**")
		if len(segments) == 1 {
			return pattern == name
		}

		// For ** at the start (e.g., **/*.txt), we need special handling
		// because ** can match the empty string (current directory)
		if segments[0] == "" {
			lastSegment := segments[len(segments)-1]

			// ** alone
			if lastSegment == "" {
				return true
			}

			// Extract just the filename pattern (e.g., "*.txt" from "/.txt" or "/foo/*.txt")
			parts := strings.Split(strings.TrimPrefix(lastSegment, "/"), "/")
			filenamePattern := parts[len(parts)-1]

			// Check if name ends with a filename matching the pattern
			// Find the last / to get the filename
			lastSlash := strings.LastIndex(name, "/")
			var filename string
			if lastSlash == -1 {
				filename = name
			} else {
				filename = name[lastSlash+1:]
			}

			// Check if filename matches the pattern
			matched, _ := filepath.Match(filenamePattern, filename)
			if matched {
				return true
			}

			// Also try matching the entire lastSegment pattern directly
			// This handles cases like **/foo/*.txt matching "bar/foo/baz.txt"
			if strings.Count(lastSegment, "/") >= 2 {
				// Multi-component suffix pattern
				trimmedPattern := strings.TrimPrefix(lastSegment, "/")
				matched, _ = filepath.Match(trimmedPattern, name)
				if matched {
					return true
				}
			}

			return false
		}

		// ** not at start: need to find the prefix segment in name
		prefix := segments[0]
		lastSegment := segments[len(segments)-1]
		idx := strings.Index(name, prefix)
		if idx != 0 {
			return false
		}

		nameAfterPrefix := name[len(prefix):]
		if lastSegment == "" {
			return true
		}

		// Now check if the remaining path could match the last segment
		// using the non-** pattern matcher
		matched, _ := filepath.Match(lastSegment, nameAfterPrefix)
		if matched {
			return true
		}

		// Also try matching with the first part of lastSegment removed
		// This handles cases like "src/**/test.txt" matching "src/foo/bar/test.txt"
		if strings.HasPrefix(lastSegment, "/") {
			restOfPattern := lastSegment[1:] // Remove leading /
			matched, _ = filepath.Match(restOfPattern, nameAfterPrefix)
			if matched {
				return true
			}
		}

		return false
	}

	// Simple pattern match using filepath.Match
	matched, _ := filepath.Match(pattern, name)
	return matched
}

// Execute finds files matching the glob pattern.
func (t *GlobTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	pattern, ok := input["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required and must be a string")
	}

	searchRoot := cwd
	if pathVal, ok := input["path"].(string); ok && pathVal != "" {
		searchRoot = pathVal

		// Resolve relative path relative to cwd
		if !filepath.IsAbs(searchRoot) {
			searchRoot = filepath.Join(cwd, searchRoot)
		}

		// Check if path is a directory
		info, err := os.Stat(searchRoot)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("path is not a directory: %s (use cwd if unsure)", pathVal)
			}
			return nil, fmt.Errorf("path is not a directory: %s (use cwd if unsure)", pathVal)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("path is not a directory: %s (use cwd if unsure)", pathVal)
		}
	}

	// Walk the directory tree and collect matching files
	var matches []fileMatch
	const maxResults = 100

	err := filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible files
		}

		// Get relative path from search root
		relPath, err := filepath.Rel(searchRoot, path)
		if err != nil {
			return nil
		}

		// Check if this path matches the pattern
		if matchGlob(pattern, relPath) {
			if !info.IsDir() {
				mtime := info.ModTime().UnixNano()
				matches = append(matches, fileMatch{path: relPath, mtime: mtime})
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking directory: %v", err)
	}

	// Empty result
	if len(matches) == 0 {
		return &ToolResult{
			Content: "No files found",
			IsError: false,
		}, nil
	}

	// Sort by modification time (newest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].mtime > matches[j].mtime
	})

	// Cap at 100 results
	truncated := len(matches) > maxResults
	if truncated {
		matches = matches[:maxResults]
	}

	// Build result content - list of relative paths
	var content strings.Builder
	for i, m := range matches {
		if i > 0 {
			content.WriteString("\n")
		}
		content.WriteString(m.path)
	}

	return &ToolResult{
		Content:   content.String(),
		IsError:   false,
		Truncated: truncated,
	}, nil
}


