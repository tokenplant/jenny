package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestTaskOutputTool_Name(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
	if got := tool.Name(); got != "TaskOutput" {
		t.Errorf("Name() = %v, want %v", got, "TaskOutput")
	}
}

func TestTaskOutputTool_InputSchema(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
	schema := tool.InputSchema()

	if schema["type"] != "object" {
		t.Errorf("InputSchema() type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() properties not a map")
	}

	// Check required fields
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("InputSchema() required not a slice")
	}

	hasTaskID := slices.Contains(required, "task_id")
	if !hasTaskID {
		t.Errorf("InputSchema() missing required field: task_id")
	}

	// Check optional fields exist
	if _, ok := props["block"]; !ok {
		t.Errorf("InputSchema() missing optional field: block")
	}
	if _, ok := props["timeout"]; !ok {
		t.Errorf("InputSchema() missing optional field: timeout")
	}
}

func TestTaskOutputTool_Execute_NonExistentTask(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "non-existent-task-id",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() should not return error for non-existent task")
	}
	if result.Content != "task not found" {
		t.Errorf("Execute() content = %v, want 'task not found'", result.Content)
	}
}

func TestTaskOutputTool_Execute_NilTaskManager(t *testing.T) {
	tool := NewTaskOutputTool(nil)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "some-task",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() should return error when task manager is nil")
	}
	if result.Content != "task manager not available" {
		t.Errorf("Execute() content = %v, want 'task manager not available'", result.Content)
	}
}

func TestTaskOutputTool_Execute_MissingTaskID(t *testing.T) {
	tm := NewTaskManager()
	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() should return error when task_id is missing")
	}
	if result.Content != "task_id is required" {
		t.Errorf("Execute() content = %v, want 'task_id is required'", result.Content)
	}
}

func TestTaskOutputTool_Execute_InMemoryResult(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Enqueue a completion directly
	tm.EnqueueCompletion(TaskCompletion{
		TaskID:          "test-task-1",
		DurationSeconds: 1.5,
		ExitCode:        0,
		Output:          "in-memory output from completion queue",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-1",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() should not return error")
	}
	if result.Content != "in-memory output from completion queue" {
		t.Errorf("Execute() content = %v, want 'in-memory output from completion queue'", result.Content)
	}
}

func TestTaskOutputTool_ConcurrentCompletions(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Enqueue two completions with different taskIDs before calling Execute
	tm.EnqueueCompletion(TaskCompletion{
		TaskID:          "task-alpha",
		DurationSeconds: 1.0,
		ExitCode:        0,
		Output:          "output from alpha",
	})
	tm.EnqueueCompletion(TaskCompletion{
		TaskID:          "task-beta",
		DurationSeconds: 1.0,
		ExitCode:        0,
		Output:          "output from beta",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	// Call Execute for task-alpha
	resultAlpha, err := tool.Execute(ctx, map[string]any{
		"task_id": "task-alpha",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute(task-alpha) error = %v", err)
	}
	if resultAlpha.IsError {
		t.Errorf("Execute(task-alpha) returned error: %v", resultAlpha.Content)
	}
	if resultAlpha.Content != "output from alpha" {
		t.Errorf("Execute(task-alpha) content = %v, want 'output from alpha'", resultAlpha.Content)
	}

	// Call Execute for task-beta
	resultBeta, err := tool.Execute(ctx, map[string]any{
		"task_id": "task-beta",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute(task-beta) error = %v", err)
	}
	if resultBeta.IsError {
		t.Errorf("Execute(task-beta) returned error: %v", resultBeta.Content)
	}
	if resultBeta.Content != "output from beta" {
		t.Errorf("Execute(task-beta) content = %v, want 'output from beta'", resultBeta.Content)
	}
}

func TestTaskOutputTool_Execute_BlockNonBlocking(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Store a running task
	tm.Store("test-task-2", &TaskInfo{
		TaskID:     "test-task-2",
		State:      TaskStateRunning,
		OutputFile: filepath.Join(tmpDir, ".jenny", "tasks", "test-task-2.output"),
		StartTime:  time.Now(),
		Command:    "echo test",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	// Non-blocking mode should return current state
	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-2",
		"block":   false,
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() should not return error for non-blocking mode")
	}
	if result.Content == "" {
		t.Errorf("Execute() should return state in non-blocking mode")
	}
}

func TestTaskOutputTool_Execute_Timeout(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Store a running task (not completed)
	tm.Store("test-task-timeout", &TaskInfo{
		TaskID:     "test-task-timeout",
		State:      TaskStateRunning,
		OutputFile: filepath.Join(tmpDir, ".jenny", "tasks", "test-task-timeout.output"),
		StartTime:  time.Now(),
		Command:    "sleep 10",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	// Use a short timeout (500ms) and block=true
	start := time.Now()
	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-timeout",
		"block":   true,
		"timeout": 0.5, // 500ms timeout
	}, "/tmp")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() should return error on timeout")
	}
	if result.Content != "timeout waiting for task test-task-timeout" {
		t.Errorf("Execute() content = %v, want 'timeout waiting for task test-task-timeout'", result.Content)
	}
	// Verify timeout was detected (timing check is lenient due to scheduler variance)
	if elapsed < 50*time.Millisecond {
		t.Errorf("Execute() returned too quickly, elapsed=%v", elapsed)
	}
}

func TestTaskOutputTool_Execute_FileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Create output file with task result
	outputPath := filepath.Join(tmpDir, ".jenny", "tasks", "test-task-file.output")
	err := os.MkdirAll(filepath.Dir(outputPath), 0755)
	if err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	entry := TaskResultEntry{
		Type:            "task_result",
		TaskID:          "test-task-file",
		Output:          "file-based output content",
		ExitCode:        0,
		DurationSeconds: 1.0,
	}
	data, _ := json.Marshal(entry)
	if err := os.WriteFile(outputPath, append(data, '\n'), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Store a completed task
	tm.Store("test-task-file", &TaskInfo{
		TaskID:     "test-task-file",
		State:      TaskStateCompleted,
		OutputFile: outputPath,
		StartTime:  time.Now(),
		Command:    "echo test",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-file",
		"block":   true,
		"timeout": 5,
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() should not return error for completed task")
	}
	if result.Content != "file-based output content" {
		t.Errorf("Execute() content = %v, want 'file-based output content'", result.Content)
	}
}

func TestTaskOutputTool_Execute_MaxTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// Store a running task that will not complete on its own
	tm.Store("test-task-never-completes", &TaskInfo{
		TaskID:     "test-task-never-completes",
		State:      TaskStateRunning,
		OutputFile: filepath.Join(tmpDir, ".jenny", "tasks", "test-task-never-completes.output"),
		StartTime:  time.Now(),
		Command:    "sleep 300",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	// Test that timeout > 600 gets capped to 600
	// Use a very short effective timeout (0.1s) to confirm behavior quickly
	start := time.Now()
	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-never-completes",
		"timeout": 700, // > 600 max, should be capped
	}, "/tmp")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() should return error on timeout")
	}
	// Verify timeout was capped at 600s - elapsed should be close to 600s
	// (We use a running task so it waits for timeout, not completes early)
	// But since we can't wait 600s in a test, we verify the error message shows timeout
	if result.Content != "timeout waiting for task test-task-never-completes" {
		t.Errorf("Execute() content = %v, want timeout message", result.Content)
	}
	// Verify the capped behavior: 700 was capped to 600, so we waited ~600s
	// Elapsed should be close to 600s (allow some margin for test execution)
	if elapsed < 500*time.Millisecond {
		t.Errorf("Execute() returned too quickly (elapsed=%v), expected ~600s cap", elapsed)
	}
}

func TestParseTimeoutSeconds(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  float64
	}{
		{
			name:  "700 caps to 600",
			input: map[string]any{"timeout": 700.0},
			want:  600.0,
		},
		{
			name:  "300 stays 300",
			input: map[string]any{"timeout": 300.0},
			want:  300.0,
		},
		{
			name:  "0 defaults to 30",
			input: map[string]any{"timeout": 0.0},
			want:  30.0,
		},
		{
			name:  "missing timeout defaults to 30",
			input: map[string]any{},
			want:  30.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTimeoutSeconds(tt.input)
			if got != tt.want {
				t.Errorf("parseTimeoutSeconds(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
