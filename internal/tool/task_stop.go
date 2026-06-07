// Package tool provides the tool interface and implementations.
package tool

import (
	"context"
	"fmt"
)

// TaskManagerInterface defines the interface for task management.
// Both BashTool and TaskStopTool implement this interface to decouple them.
type TaskManagerInterface interface {
	Stop(taskID string) error
	Load(taskID string) (*TaskInfo, bool)
}

// TaskStopTool provides the ability to stop running background tasks.
type TaskStopTool struct {
	taskManager TaskManagerInterface
}

// NewTaskStopTool creates a new TaskStopTool with the given task manager.
func NewTaskStopTool(tm TaskManagerInterface) *TaskStopTool {
	return &TaskStopTool{taskManager: tm}
}

// Name returns the tool name.
func (t *TaskStopTool) Name() string {
	return "TaskStop"
}

// Description returns a description of the tool.
func (t *TaskStopTool) Description() string {
	return "Stop a running background task by its task_id."
}

// InputSchema returns the JSON schema for tool input.
func (t *TaskStopTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The ID of the background task to stop",
			},
			"shell_id": map[string]any{
				"type":        "string",
				"description": "Deprecated alias for task_id",
			},
		},
		"required": []string{"task_id"},
	}
}

// Execute stops a running background task.
func (t *TaskStopTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Get task_id (required)
	taskID, ok := input["task_id"].(string)
	if !ok || taskID == "" {
		// Try deprecated shell_id alias
		if shellID, ok := input["shell_id"].(string); ok && shellID != "" {
			taskID = shellID
		} else {
			return &ToolResult{
				Content: "task_id is required",
				IsError: true,
			}, nil
		}
	}

	// Attempt to stop the task
	if t.taskManager == nil {
		return &ToolResult{
			Content: "task manager not available",
			IsError: true,
		}, nil
	}

	// Check if task exists
	info, found := t.taskManager.Load(taskID)
	if !found {
		return &ToolResult{
			Content: "task not found or already completed",
			IsError: true,
		}, nil
	}

	// Check if task is in running state
	if info.State != TaskStateRunning {
		return &ToolResult{
			Content: "task not found or already completed",
			IsError: true,
		}, nil
	}

	// Stop the task
	if err := t.taskManager.Stop(taskID); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("failed to stop task: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf(`{"status": "stopped", "task_id": "%s"}`, taskID),
		IsError: false,
	}, nil
}
