// Package tool provides tool implementations.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// NotebookEditTool modifies Jupyter .ipynb files.
type NotebookEditTool struct {
	readCache *ReadFileCache
}

// NewNotebookEditTool creates a new NotebookEditTool.
func NewNotebookEditTool(readCache *ReadFileCache) *NotebookEditTool {
	return &NotebookEditTool{readCache: readCache}
}

// Name returns the tool name.
func (t *NotebookEditTool) Name() string {
	return "notebook_edit"
}

// Description returns a description of the tool.
func (t *NotebookEditTool) Description() string {
	return "Edit Jupyter notebook cells in .ipynb files. Supports replace, insert, and delete operations."
}

// InputSchema returns the JSON schema for tool input.
func (t *NotebookEditTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"notebook_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the .ipynb file to edit",
			},
			"edit_mode": map[string]any{
				"type":        "string",
				"description": "Edit mode: replace (default), insert, or delete",
			},
			"cell_id": map[string]any{
				"type":        "string",
				"description": "Cell ID (required for replace/delete, optional for insert)",
			},
			"cell_type": map[string]any{
				"type":        "string",
				"description": "Cell type: code or markdown (required for insert, ignored for replace/delete)",
			},
			"new_source": map[string]any{
				"type":        "string",
				"description": "New cell source content (required for replace/insert)",
			},
		},
		"required": []string{"notebook_path"},
	}
}

// Execute performs notebook edit operations.
func (t *NotebookEditTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	notebookPath, ok := input["notebook_path"].(string)
	if !ok || notebookPath == "" {
		return &ToolResult{
			Content: "notebook_path is required and must be a string",
			IsError: true,
		}, nil
	}

	editMode, _ := input["edit_mode"].(string)
	if editMode == "" {
		editMode = "replace"
	}

	cellID, _ := input["cell_id"].(string)
	cellType, _ := input["cell_type"].(string)
	newSource, _ := input["new_source"].(string)

	// Resolve relative paths relative to cwd
	if !filepath.IsAbs(notebookPath) {
		notebookPath = filepath.Join(cwd, notebookPath)
	}

	// Clean the path
	notebookPath = filepath.Clean(notebookPath)

	// Gate: ensure path is within working directory
	var pathErr error
	notebookPath, pathErr = PathInWorkingDir(notebookPath, cwd)
	if pathErr != nil {
		return &ToolResult{
			Content: pathErr.Error(),
			IsError: true,
		}, nil
	}

	// AC1: Check extension is .ipynb
	if !strings.HasSuffix(notebookPath, ".ipynb") {
		return &ToolResult{
			Content: "Editing non-.ipynb files requires the Edit tool. Use Edit for regular files.",
			IsError: true,
		}, nil
	}

	// AC3: Check readFileState cache for the path
	entry, exists := t.readCache.GetRead(notebookPath)
	if !exists {
		return &ToolResult{
			Content: "Cannot edit without reading first. Use Read tool on this path before NotebookEdit.",
			IsError: true,
		}, nil
	}

	// AC3: Check staleness
	info, err := os.Stat(notebookPath)
	if err == nil {
		if info.ModTime().After(entry.Mtime) {
			return &ToolResult{
				Content: "File has changed since it was read. Re-read the file before editing.",
				IsError: true,
			}, nil
		}
	}

	// AC3: Check if entry was a partial read
	if !entry.IsFullRead {
		return &ToolResult{
			Content: "Cannot edit after partial read. Use Read tool without offset/limit to get the full file first.",
			IsError: true,
		}, nil
	}

	// AC2: Validate parameters based on edit mode
	switch editMode {
	case "replace":
		// cell_id required for replace
		if cellID == "" {
			return &ToolResult{
				Content: "cell_id is required for replace mode",
				IsError: true,
			}, nil
		}
		// new_source required for replace
		if newSource == "" {
			return &ToolResult{
				Content: "new_source is required for replace mode",
				IsError: true,
			}, nil
		}
	case "insert":
		// AC2: cell_type required for insert
		if cellType == "" {
			return &ToolResult{
				Content: "cell_type is required for insert mode (use 'code' or 'markdown')",
				IsError: true,
			}, nil
		}
		if cellType != "code" && cellType != "markdown" {
			return &ToolResult{
				Content: "cell_type must be 'code' or 'markdown'",
				IsError: true,
			}, nil
		}
		// new_source required for insert
		if newSource == "" {
			return &ToolResult{
				Content: "new_source is required for insert mode",
				IsError: true,
			}, nil
		}
	case "delete":
		// cell_id required for delete
		if cellID == "" {
			return &ToolResult{
				Content: "cell_id is required for delete mode",
				IsError: true,
			}, nil
		}
	default:
		return &ToolResult{
			Content: "edit_mode must be 'replace', 'insert', or 'delete'",
			IsError: true,
		}, nil
	}

	// Parse the notebook JSON
	var nb map[string]any
	if err := json.Unmarshal([]byte(entry.Content), &nb); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Invalid JSON in notebook file: %v", err),
			IsError: true,
		}, nil
	}

	// Get cells array
	cellsRaw, ok := nb["cells"]
	if !ok {
		return &ToolResult{
			Content: "Notebook has no cells array",
			IsError: true,
		}, nil
	}
	cells, ok := cellsRaw.([]any)
	if !ok {
		return &ToolResult{
			Content: "Notebook cells is not an array",
			IsError: true,
		}, nil
	}

	var oldContent string
	var newContent string
	var cellIdx int
	var found bool

	switch editMode {
	case "replace":
		cellIdx, found = findCellByID(cells, cellID)
		if !found {
			return &ToolResult{
				Content: fmt.Sprintf("Cell '%s' not found in notebook. Use cell index (cell-0, cell-1, etc.) or cell ID.", cellID),
				IsError: true,
			}, nil
		}
		cell := cells[cellIdx].(map[string]any)
		oldContent = getCellSource(cell)
		cell["source"] = newSource
		// Reset execution_count and outputs for code cells
		if getCellType(cell) == "code" {
			cell["execution_count"] = nil
			cell["outputs"] = []any{}
		}
		newContent = newSource

	case "insert":
		targetIdx := 0
		if cellID != "" {
			idx, found := findCellByID(cells, cellID)
			if found {
				targetIdx = idx + 1 // insert after target
			}
		}
		// Generate cell ID: hex timestamp for nbformat >= 4.5
		newCellID := fmt.Sprintf("%x", time.Now().UnixNano())
		newCell := map[string]any{
			"id":        newCellID,
			"cell_type": cellType,
			"metadata":  map[string]any{},
			"source":    newSource,
		}
		if cellType == "code" {
			newCell["execution_count"] = nil
			newCell["outputs"] = []any{}
		} else {
			newCell["attachments"] = map[string]any{}
		}
		// Splice in the new cell
		cells = spliceCell(cells, targetIdx, newCell)
		nb["cells"] = cells
		oldContent = ""
		newContent = newSource

	case "delete":
		cellIdx, found = findCellByID(cells, cellID)
		if !found {
			return &ToolResult{
				Content: fmt.Sprintf("Cell '%s' not found in notebook. Use cell index (cell-0, cell-1, etc.) or cell ID.", cellID),
				IsError: true,
			}, nil
		}
		cell := cells[cellIdx].(map[string]any)
		oldContent = getCellSource(cell)
		cells = spliceCell(cells, cellIdx, nil) // nil means delete
		nb["cells"] = cells
		newContent = ""
	}

	// Marshal back to JSON with one-space indent
	newJSON, err := json.MarshalIndent(nb, "", " ")
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to serialize notebook: %v", err),
			IsError: true,
		}, nil
	}

	// Write the file
	if err := os.WriteFile(notebookPath, newJSON, 0644); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Failed to write notebook file: %v", err),
			IsError: true,
		}, nil
	}

	// Get new mtime after write
	newInfo, _ := os.Stat(notebookPath)
	newMtime := entry.Mtime
	if newInfo != nil {
		newMtime = newInfo.ModTime()
	}

	// Generate diff output
	var diff string
	if oldContent != "" || newContent != "" {
		diff = GenerateUnifiedDiff(oldContent, newContent, notebookPath)
	} else {
		diff = fmt.Sprintf("Inserted cell at position in %s", notebookPath)
	}

	// AC5: Update readFileCache with new content, offset undefined to break Read dedup
	t.readCache.RecordRead(notebookPath, string(newJSON), newMtime, true)

	return &ToolResult{
		Content: diff,
		IsError: false,
	}, nil
}

// findCellByID finds a cell index by ID or numeric alias (cell-0, cell-1, etc.)
func findCellByID(cells []any, id string) (int, bool) {
	// Handle numeric alias (cell-0, cell-1, etc.)
	if after, ok := strings.CutPrefix(id, "cell-"); ok {
		idxStr := after
		idx := 0
		for i, c := range cells {
			if cell, ok := c.(map[string]any); ok {
				// Check if this cell's id matches
				if cellID, ok := cell["id"].(string); ok && cellID == id {
					return i, true
				}
			}
		}
		// Try parsing as numeric index
		if n, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && n == 1 {
			if idx >= 0 && idx < len(cells) {
				return idx, true
			}
		}
		return 0, false
	}

	// Direct ID lookup
	for i, c := range cells {
		if cell, ok := c.(map[string]any); ok {
			if cellID, ok := cell["id"].(string); ok && cellID == id {
				return i, true
			}
		}
	}
	return 0, false
}

// getCellType returns the cell type or "unknown" if not set.
func getCellType(cell map[string]any) string {
	if ct, ok := cell["cell_type"].(string); ok {
		return ct
	}
	return "unknown"
}

// getCellSource returns the cell source content as a string.
func getCellSource(cell map[string]any) string {
	src, ok := cell["source"]
	if !ok {
		return ""
	}
	switch s := src.(type) {
	case string:
		return s
	case []any:
		var lines []string
		for _, l := range s {
			if line, ok := l.(string); ok {
				lines = append(lines, line)
			}
		}
		return strings.Join(lines, "")
	}
	return ""
}

// spliceCell inserts or deletes a cell at the given index.
func spliceCell(cells []any, idx int, newCell map[string]any) []any {
	if newCell == nil {
		// Delete mode: remove cell at idx
		if idx >= 0 && idx < len(cells) {
			return slices.Delete[[]any](cells, idx, idx+1)
		}
		return cells
	}
	// Insert mode: add new cell at idx
	result := make([]any, len(cells)+1)
	at := 0
	for i, c := range cells {
		if i == idx {
			result[at] = newCell
			at++
		}
		result[at] = c
		at++
	}
	// If inserting at end
	if idx >= len(cells) {
		result[at] = newCell
	}
	return result
}
