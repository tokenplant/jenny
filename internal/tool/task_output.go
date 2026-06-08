// Package tool provides the tool interface and implementations.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"
)

// parseTimeoutSeconds extracts and validates the timeout parameter from input.
// Returns default of 30s if not provided or if timeout <= 0, caps at 600s maximum.
func parseTimeoutSeconds(input map[string]any) float64 {
	timeoutSecondsFloat := 30.0
	if t, ok := input["timeout"].(float64); ok {
		if t > 0 {
			timeoutSecondsFloat = math.Min(t, 600.0)
		}
	}
	return timeoutSecondsFloat
}

// TaskOutputTool retrieves output from background tasks.
type TaskOutputTool struct {
	taskManager *TaskManager
}

// NewTaskOutputTool creates a new TaskOutputTool with the given task manager.
func NewTaskOutputTool(tm *TaskManager) *TaskOutputTool {
	return &TaskOutputTool{taskManager: tm}
}

// Name returns the tool name.
func (t *TaskOutputTool) Name() string {
	return "TaskOutput"
}

// Description returns a description of the tool.
func (t *TaskOutputTool) Description() string {
	return "Retrieve output from a background task by its task_id. Supports blocking and non-blocking modes."
}

// InputSchema returns the JSON schema for tool input.
func (t *TaskOutputTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The ID of the background task",
			},
			"block": map[string]any{
				"type":        "boolean",
				"description": "If true, wait for task completion (default: true)",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Maximum seconds to wait for completion (default: 30, max: 600)",
			},
		},
		"required": []string{"task_id"},
	}
}

// Execute retrieves output from a background task.
func (t *TaskOutputTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Get task_id (required)
	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		return &ToolResult{
			Content: "task_id is required",
			IsError: true,
		}, nil
	}

	// Get block parameter (default: true)
	block := true
	if b, ok := input["block"].(bool); ok {
		block = b
	}

	// Get timeout parameter (default: 30s, max: 600s)
	timeoutSecondsFloat := parseTimeoutSeconds(input)

	if t.taskManager == nil {
		return &ToolResult{
			Content: "task manager not available",
			IsError: true,
		}, nil
	}

	// Try to get output from completion queue first (in-memory result)
	// Drain all completions, extract matching, re-enqueue non-matching
	all := t.taskManager.DrainCompletions()
	var match *TaskCompletion
	var nonMatching []TaskCompletion
	for _, c := range all {
		if c.TaskID == taskID {
			match = &c
		} else {
			nonMatching = append(nonMatching, c)
		}
	}
	// Re-enqueue non-matching completions
	for _, c := range nonMatching {
		t.taskManager.EnqueueCompletion(c)
	}
	if match != nil {
		return &ToolResult{
			Content: match.Output,
			IsError: false,
		}, nil
	}

	// Check if task exists
	info, found := t.taskManager.Load(taskID)
	if !found {
		return &ToolResult{
			Content: "task not found",
			IsError: false,
		}, nil
	}

	// If not blocking, return current state
	if !block {
		return &ToolResult{
			Content: fmt.Sprintf(`{"task_id": "%s", "state": "%s"}`, taskID, info.State),
			IsError: false,
		}, nil
	}

	// Blocking: wait for completion or timeout
	deadline := time.Now().Add(time.Duration(timeoutSecondsFloat * float64(time.Second)))
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return &ToolResult{
				Content: "task output retrieval cancelled",
				IsError: true,
			}, nil
		case <-ticker.C:
			// Check completion queue first (early exit)
			// Drain all completions, extract matching, re-enqueue non-matching
			all := t.taskManager.DrainCompletions()
			var match *TaskCompletion
			var nonMatching []TaskCompletion
			for _, c := range all {
				if c.TaskID == taskID {
					match = &c
				} else {
					nonMatching = append(nonMatching, c)
				}
			}
			// Re-enqueue non-matching completions
			for _, c := range nonMatching {
				t.taskManager.EnqueueCompletion(c)
			}
			if match != nil {
				return &ToolResult{
					Content: match.Output,
					IsError: false,
				}, nil
			}

			// Check task state
			info, found := t.taskManager.Load(taskID)
			if !found {
				return &ToolResult{
					Content: "task not found",
					IsError: false,
				}, nil
			}

			if info.State == TaskStateCompleted || info.State == TaskStateStopped {
				// Read output from file
				output, err := t.readTaskOutput(info)
				if err != nil {
					return &ToolResult{
						Content: fmt.Sprintf("task ended but failed to read output: %v", err),
						IsError: true,
					}, nil
				}
				return &ToolResult{
					Content: output,
					IsError: false,
				}, nil
			}

			// Check timeout
			if time.Now().After(deadline) {
				return &ToolResult{
					Content: fmt.Sprintf("timeout waiting for task %s", taskID),
					IsError: true,
				}, nil
			}
		}
	}
}

// readTaskOutput reads and parses the task output file.
func (t *TaskOutputTool) readTaskOutput(info *TaskInfo) (string, error) {
	data, err := os.ReadFile(info.OutputFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("output file not found")
		}
		return "", fmt.Errorf("reading output file: %w", err)
	}

	// Parse JSONL entry (last line contains the final result)
	lines := splitJSONLLines(data)
	if len(lines) == 0 {
		return "", fmt.Errorf("empty output file")
	}

	// Parse the last entry (final result)
	var entry TaskResultEntry
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		return "", fmt.Errorf("parsing output: %w", err)
	}

	return entry.Output, nil
}

// splitJSONLLines splits JSONL data into lines, handling the last line without newline.
func splitJSONLLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if line := string(data[start:i]); line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	// Add remaining content as last line
	if remaining := string(data[start:]); remaining != "" {
		lines = append(lines, remaining)
	}
	return lines
}
