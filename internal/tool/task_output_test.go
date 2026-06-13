package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/constants"
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
		OutputFile: filepath.Join(tmpDir, constants.ProjectDirName, "tasks", "test-task-2.output"),
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
		OutputFile: filepath.Join(tmpDir, constants.ProjectDirName, "tasks", "test-task-timeout.output"),
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
	outputPath := filepath.Join(tmpDir, constants.ProjectDirName, "tasks", "test-task-file.output")
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
		OutputFile: filepath.Join(tmpDir, constants.ProjectDirName, "tasks", "test-task-never-completes.output"),
		StartTime:  time.Now(),
		Command:    "sleep 300",
	})

	tool := NewTaskOutputTool(tm)
	ctx := context.Background()

	// Use a short timeout (0.1s) to confirm timeout behavior without waiting long
	// The 600s cap is verified by TestParseTimeoutSeconds (700 -> 600)
	start := time.Now()
	result, err := tool.Execute(ctx, map[string]any{
		"task_id": "test-task-never-completes",
		"timeout": 0.1, // 100ms timeout
	}, "/tmp")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() should return error on timeout")
	}
	if result.Content != "timeout waiting for task test-task-never-completes" {
		t.Errorf("Execute() content = %v, want timeout message", result.Content)
	}
	// Verify elapsed is close to the short timeout (100ms)
	// Allow range: >= 50ms (some time passed) and < 2s (reasonable bound)
	if elapsed < 50*time.Millisecond || elapsed >= 2*time.Second {
		t.Errorf("Execute() elapsed=%v, expected ~100ms (50ms-2s range)", elapsed)
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

// TestTaskOutputAppend verifies that WriteTaskResult uses append mode
// (AC6: Task output file uses append mode). Two consecutive writes must
// produce two JSONL lines in the file.
func TestTaskOutputAppend(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	// First write: should create the file
	if err := tm.WriteTaskResult("task-append-1", "first output", 0, 1.0); err != nil {
		t.Fatalf("first WriteTaskResult error = %v", err)
	}

	// Second write: should append, not truncate
	if err := tm.WriteTaskResult("task-append-1", "second output", 0, 2.0); err != nil {
		t.Fatalf("second WriteTaskResult error = %v", err)
	}

	path, err := tm.TaskOutputPath("task-append-1")
	if err != nil {
		t.Fatalf("TaskOutputPath error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	// File should have two JSONL lines, both preserved
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines after two WriteTaskResult calls, got %d: %q", len(lines), string(data))
	}

	var first, second TaskResultEntry
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("Unmarshal first line: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("Unmarshal second line: %v", err)
	}

	if first.Output != "first output" {
		t.Errorf("first.Output = %q, want %q", first.Output, "first output")
	}
	if first.DurationSeconds != 1.0 {
		t.Errorf("first.DurationSeconds = %v, want 1.0", first.DurationSeconds)
	}
	if second.Output != "second output" {
		t.Errorf("second.Output = %q, want %q", second.Output, "second output")
	}
	if second.DurationSeconds != 2.0 {
		t.Errorf("second.DurationSeconds = %v, want 2.0", second.DurationSeconds)
	}
}

// TestTaskOutputFlushThenWrite verifies that FlushPartialOutput followed by
// WriteTaskResult preserves both entries (AC6).
func TestTaskOutputFlushThenWrite(t *testing.T) {
	tmpDir := t.TempDir()
	tm := NewTaskManager().WithProjectRoot(tmpDir)

	if err := tm.FlushPartialOutput("task-flush-1", "partial output", 0.5); err != nil {
		t.Fatalf("FlushPartialOutput error = %v", err)
	}
	if err := tm.WriteTaskResult("task-flush-1", "final output", 0, 1.5); err != nil {
		t.Fatalf("WriteTaskResult error = %v", err)
	}

	path, err := tm.TaskOutputPath("task-flush-1")
	if err != nil {
		t.Fatalf("TaskOutputPath error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d: %q", len(lines), string(data))
	}

	var first, second TaskResultEntry
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("Unmarshal first line: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("Unmarshal second line: %v", err)
	}

	if first.Output != "partial output" {
		t.Errorf("first.Output = %q, want %q", first.Output, "partial output")
	}
	if first.ExitCode != -1 {
		t.Errorf("first.ExitCode = %d, want -1 (partial)", first.ExitCode)
	}
	if second.Output != "final output" {
		t.Errorf("second.Output = %q, want %q", second.Output, "final output")
	}
	if second.ExitCode != 0 {
		t.Errorf("second.ExitCode = %d, want 0", second.ExitCode)
	}
}
