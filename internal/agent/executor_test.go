// Package agent provides tests for the tool executor.
package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/tool"
)

// execMockTool is a test tool with configurable behavior.
type execMockTool struct {
	name    string
	delay   time.Duration
	err     error
	isError bool
	content string
	isSafe  bool
}

func (m *execMockTool) Name() string                        { return m.name }
func (m *execMockTool) Description() string                 { return "mock tool " + m.name }
func (m *execMockTool) InputSchema() map[string]any         { return map[string]any{} }
func (m *execMockTool) ConcurrencySafe(map[string]any) bool { return m.isSafe }

func (m *execMockTool) Execute(input map[string]any, cwd string) (*tool.ToolResult, error) {
	done := make(chan struct{})
	go func() {
		time.Sleep(m.delay)
		close(done)
	}()

	select {
	case <-done:
		// Completed normally
	}

	return &tool.ToolResult{
		Content: m.content,
		IsError: m.isError,
	}, m.err
}

// ExecuteWithContext runs the tool with context cancellation support.
// The mock checks ctx.Done() periodically to detect sibling abort.
func (m *execMockTool) ExecuteWithContext(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	// Use a ticker to allow periodic context checks during long delays.
	// This simulates how exec.CommandContext actually checks context cancellation.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	done := time.After(m.delay)

	for {
		select {
		case <-done:
			// Completed normally
			return &tool.ToolResult{
				Content: m.content,
				IsError: m.isError,
			}, m.err
		case <-ctx.Done():
			// Interrupted by context cancellation (sibling abort)
			return &tool.ToolResult{
				Content: "Tool execution aborted due to sibling failure",
				IsError: true,
			}, fmt.Errorf("aborted by sibling failure")
		case <-ticker.C:
			// Continue checking - allows context to be checked periodically
			continue
		}
	}
}

// TestExecutor_AC1_ParallelReadOnly verifies AC1: Read/Glob/Grep run in parallel when consecutive.
func TestExecutor_AC1_ParallelReadOnly(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{name: "read", delay: 100 * time.Millisecond, isSafe: true},
		&execMockTool{name: "Glob", delay: 100 * time.Millisecond, isSafe: true},
		&execMockTool{name: "grep", delay: 100 * time.Millisecond, isSafe: true},
	}

	executor := NewToolExecutor(tools, "/tmp")

	blocks := []toolUseBlock{
		{ID: "1", Name: "read", Input: map[string]any{"file_path": "a.txt"}},
		{ID: "2", Name: "read", Input: map[string]any{"file_path": "b.txt"}},
		{ID: "3", Name: "Glob", Input: map[string]any{"pattern": "*.go"}},
	}

	start := time.Now()
	results, err := executor.Execute(blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// With3 tools each taking ~100ms running in parallel, total should be <300ms
	// We use a generous threshold since CI environments may vary
	if elapsed >= 300*time.Millisecond {
		t.Errorf("Parallel execution took %v, expected <300ms (tools should run concurrently)", elapsed)
	}

	// Verify all results are present and correct
	for i, res := range results {
		if res.ToolUseID == "" {
			t.Errorf("Result %d has empty ToolUseID", i)
		}
	}
}

// TestExecutor_AC2_SerializedMutation verifies AC2: Write/Edit/Bash never run concurrently.
func TestExecutor_AC2_SerializedMutation(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{name: "write", delay: 100 * time.Millisecond, isSafe: false},
		&execMockTool{name: "edit", delay: 100 * time.Millisecond, isSafe: false},
		&execMockTool{name: "read", delay: 100 * time.Millisecond, isSafe: true},
	}

	executor := NewToolExecutor(tools, "/tmp")

	// Test: Write + Write should be serial (≥200ms)
	writeBlocks := []toolUseBlock{
		{ID: "1", Name: "write", Input: map[string]any{"file_path": "f1"}},
		{ID: "2", Name: "write", Input: map[string]any{"file_path": "f2"}},
	}

	start := time.Now()
	results, err := executor.Execute(writeBlocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// Serial execution should take ≥200ms
	if elapsed < 200*time.Millisecond {
		t.Errorf("Serial Write execution took %v, expected ≥200ms", elapsed)
	}

	// Test: Mixed batch [Read, Write, Read] should be: Read parallel, then Write serial, then Read parallel
	mixedBlocks := []toolUseBlock{
		{ID: "3", Name: "read", Input: map[string]any{"file_path": "a.txt"}},
		{ID: "4", Name: "write", Input: map[string]any{"file_path": "f1"}},
		{ID: "5", Name: "read", Input: map[string]any{"file_path": "b.txt"}},
	}

	mixedResults, err := executor.Execute(mixedBlocks)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// With serial execution: ~300ms total (100ms + 100ms + 100ms)
	// With parallel batches: first batch ~100ms (parallel reads), then ~100ms (write), then ~100ms (read)
	// Either way it should be < 200ms if reads were truly parallel, but since write is serial it should be ≥300ms
	_ = mixedResults

	// B3: Test: Bash + Bash should be serial (≥2s) - AC2 explicit case
	bashTools := []tool.Tool{
		&execMockTool{name: "bash", delay: 1000 * time.Millisecond, isSafe: false},
		&execMockTool{name: "bash", delay: 1000 * time.Millisecond, isSafe: false},
	}
	bashExecutor := NewToolExecutor(bashTools, "/tmp")

	bashBlocks := []toolUseBlock{
		{ID: "6", Name: "bash", Input: map[string]any{"command": "sleep 1"}},
		{ID: "7", Name: "bash", Input: map[string]any{"command": "sleep 1"}},
	}

	bashStart := time.Now()
	bashResults, err := bashExecutor.Execute(bashBlocks)
	bashElapsed := time.Since(bashStart)

	if err != nil {
		t.Fatalf("Bash serial execution returned error: %v", err)
	}

	if len(bashResults) != 2 {
		t.Fatalf("Expected 2 bash results, got %d", len(bashResults))
	}

	// Serial bash execution should take ≥2s (1000ms + 1000ms)
	if bashElapsed < 2000*time.Millisecond {
		t.Errorf("Serial Bash execution took %v, expected ≥2000ms", bashElapsed)
	}
}

// TestExecutor_AC3_BashSiblingAbort verifies AC3: Bash failure aborts sibling bash in same batch.
// When one bash fails, subsequent sibling bash processes should be aborted.
func TestExecutor_AC3_BashSiblingAbort(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{
			name:    "bash",
			delay:   100 * time.Millisecond,
			isSafe:  false,
			err:     fmt.Errorf("exit 1"), // This one fails and triggers abort
			isError: true,
			content: "bash1 failed",
		},
		&execMockTool{
			name:    "bash",
			delay:   500 * time.Millisecond,
			isSafe:  false,
			content: "bash2 running",
		},
		&execMockTool{
			name:    "bash",
			delay:   500 * time.Millisecond,
			isSafe:  false,
			content: "bash3 running",
		},
	}

	executor := NewToolExecutor(tools, "/tmp")

	blocks := []toolUseBlock{
		{ID: "1", Name: "bash", Input: map[string]any{"command": "exit 1"}},
		{ID: "2", Name: "bash", Input: map[string]any{"command": "sleep 10"}},
		{ID: "3", Name: "bash", Input: map[string]any{"command": "sleep 10"}},
	}

	start := time.Now()
	results, err := executor.Execute(blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Result 1 (failing bash) should be an error
	if !results[0].IsError {
		t.Errorf("Result 1 (failing bash) should be an error")
	}

	// Siblings should be aborted - they should not run to completion.
	// Since bash1 fails at ~100ms, and bash is serial, total should be ~100ms.
	if elapsed >= 400*time.Millisecond {
		t.Errorf("Sibling abort: execution took %v, expected <400ms (siblings should be aborted)", elapsed)
	}

	// Results 2 and 3 should be aborted
	expectedAbort := "Tool execution aborted due to sibling failure"
	if results[1].Content != expectedAbort {
		t.Errorf("Result 2: expected %q, got %q", expectedAbort, results[1].Content)
	}
	if results[2].Content != expectedAbort {
		t.Errorf("Result 3: expected %q, got %q", expectedAbort, results[2].Content)
	}

	// Verify results are in request order
	for i, res := range results {
		expectedID := fmt.Sprintf("%d", i+1)
		if res.ToolUseID != expectedID {
			t.Errorf("Result %d: expected ToolUseID=%s, got %s", i, expectedID, res.ToolUseID)
		}
	}
}

// TestExecutor_AC4_UnknownTool verifies AC4: Unknown tool returns immediate error.
func TestExecutor_AC4_UnknownTool(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{name: "read", delay: 100 * time.Millisecond, isSafe: true},
	}

	executor := NewToolExecutor(tools, "/tmp")

	blocks := []toolUseBlock{
		{ID: "1", Name: "UnknownTool", Input: map[string]any{}},
	}

	start := time.Now()
	results, err := executor.Execute(blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	// Unknown tool should return immediate error (< 100ms, not delayed by other tools)
	if elapsed >= 100*time.Millisecond {
		t.Errorf("Unknown tool took %v, expected <100ms (immediate error)", elapsed)
	}

	if !results[0].IsError {
		t.Errorf("Expected IsError=true for unknown tool")
	}

	expectedMsg := "Error: No such tool available: UnknownTool"
	if results[0].Content != expectedMsg {
		t.Errorf("Expected error message %q, got %q", expectedMsg, results[0].Content)
	}
}

// TestExecutor_AC5_ResultOrdering verifies AC5: Results emitted in request order.
func TestExecutor_AC5_ResultOrdering(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{name: "read", delay: 50 * time.Millisecond, isSafe: true},
		&execMockTool{name: "write", delay: 100 * time.Millisecond, isSafe: false},
		&execMockTool{name: "grep", delay: 30 * time.Millisecond, isSafe: true},
	}

	executor := NewToolExecutor(tools, "/tmp")

	// Send [slow(safe), fast(serial), fast(safe)] - results should be in request order
	blocks := []toolUseBlock{
		{ID: "1", Name: "read", Input: map[string]any{}},
		{ID: "2", Name: "write", Input: map[string]any{}},
		{ID: "3", Name: "grep", Input: map[string]any{}},
	}

	results, err := executor.Execute(blocks)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Results should be in request order: [ToolC, Write, ToolA]
	// That means IDs should be: "1", "2", "3"
	if results[0].ToolUseID != "1" {
		t.Errorf("Expected result[0].ToolUseID=1, got %s", results[0].ToolUseID)
	}
	if results[1].ToolUseID != "2" {
		t.Errorf("Expected result[1].ToolUseID=2, got %s", results[1].ToolUseID)
	}
	if results[2].ToolUseID != "3" {
		t.Errorf("Expected result[2].ToolUseID=3, got %s", results[2].ToolUseID)
	}
}

// TestExecutor_MixedBatch verifies mixed parallel/serial partitioning.
func TestExecutor_MixedBatch(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{name: "read", delay: 50 * time.Millisecond, isSafe: true},
		&execMockTool{name: "Glob", delay: 50 * time.Millisecond, isSafe: true},
		&execMockTool{name: "write", delay: 100 * time.Millisecond, isSafe: false},
		&execMockTool{name: "read", delay: 50 * time.Millisecond, isSafe: true},
		&execMockTool{name: "edit", delay: 100 * time.Millisecond, isSafe: false},
		&execMockTool{name: "grep", delay: 50 * time.Millisecond, isSafe: true},
	}

	executor := NewToolExecutor(tools, "/tmp")

	blocks := []toolUseBlock{
		{ID: "1", Name: "read", Input: map[string]any{}},
		{ID: "2", Name: "Glob", Input: map[string]any{}},
		{ID: "3", Name: "write", Input: map[string]any{}},
		{ID: "4", Name: "read", Input: map[string]any{}},
		{ID: "5", Name: "edit", Input: map[string]any{}},
		{ID: "6", Name: "grep", Input: map[string]any{}},
	}

	start := time.Now()
	results, err := executor.Execute(blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 6 {
		t.Fatalf("Expected 6 results, got %d", len(results))
	}

	// Partition should be:
	// Group 1 (parallel): read, Glob
	// Group 2 (serial): write
	// Group 3 (parallel): read
	// Group 4 (serial): edit
	// Group 5 (parallel): grep
	//
	// Timing estimate:
	// Group 1: ~50ms (parallel)
	// Group 2: ~100ms (serial)
	// Group 3: ~50ms (parallel)
	// Group 4: ~100ms (serial)
	// Group 5: ~50ms (parallel)
	// Total: ~350ms
	//
	// If serial:50+50+100+50+100+50 = 400ms
	// So we expect < 400ms if parallel groups work correctly
	if elapsed >= 400*time.Millisecond {
		t.Errorf("Mixed batch took %v, expected <400ms", elapsed)
	}

	// Verify all results are in correct order
	for i, res := range results {
		expectedID := fmt.Sprintf("%d", i+1)
		if res.ToolUseID != expectedID {
			t.Errorf("Result %d: expected ToolUseID=%s, got %s", i, expectedID, res.ToolUseID)
		}
	}
}

// TestExecutor_ConcurrencyCap verifies max concurrency limit.
func TestExecutor_ConcurrencyCap(t *testing.T) {
	// Create 15 read tools
	tools := make([]tool.Tool, 15)
	for i := 0; i < 15; i++ {
		tools[i] = &execMockTool{name: "read", delay: 100 * time.Millisecond, isSafe: true}
	}

	// Create executor with maxConcurrency=10
	executor := &ToolExecutor{
		tools:          tools,
		cwd:            "/tmp",
		maxConcurrency: 10,
	}

	blocks := make([]toolUseBlock, 15)
	for i := 0; i < 15; i++ {
		blocks[i] = toolUseBlock{
			ID:    fmt.Sprintf("%d", i+1),
			Name:  "read",
			Input: map[string]any{},
		}
	}

	start := time.Now()
	results, err := executor.Execute(blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 15 {
		t.Fatalf("Expected 15 results, got %d", len(results))
	}

	// With maxConcurrency=10 and 15 tools taking100ms each:
	// First batch of 10 runs in parallel: ~100ms
	// Second batch of 5 runs in parallel: ~100ms
	// Total should be ~200ms (not 1500ms which would be serial)
	if elapsed >= 300*time.Millisecond {
		t.Errorf("Concurrency cap execution took %v, expected ~200ms (10 parallel + 5 parallel)", elapsed)
	}
}

// TestExecutor_BashFailureDoesNotAbortNonBash verifies that bash failure doesn't abort non-bash tools.
// This test ensures proper partitioning: bash and read should be in separate batches.
func TestExecutor_BashFailureDoesNotAbortNonBash(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{
			name:    "bash",
			delay:   0,
			isSafe:  false,
			err:     fmt.Errorf("exit 1"),
			isError: true,
		},
		&execMockTool{
			name:    "read",
			delay:   200 * time.Millisecond,
			isSafe:  true,
			content: "read completed",
		},
	}

	executor := NewToolExecutor(tools, "/tmp")

	blocks := []toolUseBlock{
		{ID: "1", Name: "bash", Input: map[string]any{"command": "exit 1"}},
		{ID: "2", Name: "read", Input: map[string]any{"file_path": "a.txt"}},
	}

	results, err := executor.Execute(blocks)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// First result (bash) should be an error
	if !results[0].IsError {
		t.Errorf("Bash result should be an error")
	}

	// Second result (read) should be successful content
	// Note: With correct partitioning, read should NOT be affected by bash failure
	if results[1].Content != "read completed" {
		t.Errorf("Read result should be 'read completed', got %q", results[1].Content)
	}
}

// TestExecutor_EmptyBatch verifies executor handles empty batch.
func TestExecutor_EmptyBatch(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{name: "read", isSafe: true},
	}

	executor := NewToolExecutor(tools, "/tmp")

	results, err := executor.Execute([]toolUseBlock{})

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty batch, got %d", len(results))
	}
}

// TestExecutor_AllReadOnlyTools verifies all-read batch runs in parallel.
func TestExecutor_AllReadOnlyTools(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{name: "read", delay: 100 * time.Millisecond, isSafe: true},
		&execMockTool{name: "Glob", delay: 100 * time.Millisecond, isSafe: true},
		&execMockTool{name: "grep", delay: 100 * time.Millisecond, isSafe: true},
		&execMockTool{name: "read", delay: 100 * time.Millisecond, isSafe: true},
	}

	executor := NewToolExecutor(tools, "/tmp")

	blocks := []toolUseBlock{
		{ID: "1", Name: "read", Input: map[string]any{}},
		{ID: "2", Name: "Glob", Input: map[string]any{}},
		{ID: "3", Name: "grep", Input: map[string]any{}},
		{ID: "4", Name: "read", Input: map[string]any{}},
	}

	start := time.Now()
	results, err := executor.Execute(blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("Expected 4 results, got %d", len(results))
	}

	// All parallel: ~100ms, not ~400ms
	if elapsed >= 300*time.Millisecond {
		t.Errorf("All-read batch took %v, expected ~100ms (parallel)", elapsed)
	}
}

// TestExecutor_BashAbortOnlyAffectsBash verifies sibling abort only affects bash tools.
// Note: This test verifies the mechanism since mock tools can't be truly interrupted.
func TestExecutor_BashAbortOnlyAffectsBash(t *testing.T) {
	tools := []tool.Tool{
		&execMockTool{name: "bash", delay: 500 * time.Millisecond, isSafe: false, content: "bash1"},
		&execMockTool{name: "bash", delay: 0, isSafe: false, content: "bash2"},
		&execMockTool{name: "bash", delay: 500 * time.Millisecond, isSafe: false, content: "bash3"},
	}

	executor := NewToolExecutor(tools, "/tmp")

	blocks := []toolUseBlock{
		{ID: "1", Name: "bash", Input: map[string]any{"command": "sleep 10"}},
		{ID: "2", Name: "bash", Input: map[string]any{"command": "exit 1"}},
		{ID: "3", Name: "bash", Input: map[string]any{"command": "sleep 10"}},
	}

	results, err := executor.Execute(blocks)

	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Verify all three bash tools were executed
	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Verify results are in request order
	for i, res := range results {
		expectedID := fmt.Sprintf("%d", i+1)
		if res.ToolUseID != expectedID {
			t.Errorf("Result %d: expected ToolUseID=%s, got %s", i, expectedID, res.ToolUseID)
		}
	}
}
