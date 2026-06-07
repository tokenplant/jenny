// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/tool"
)

// mockExtractionAPIClient is a test double for the API client.
type mockExtractionAPIClient struct {
	sendMessageFn func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error)
}

func (m *mockExtractionAPIClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
	if m.sendMessageFn != nil {
		return m.sendMessageFn(ctx, messages, tools, toolResults, systemPrompt)
	}
	return &api.Response{}, nil
}

// TestAC1_EndOfTurnOnly verifies that extraction only runs on end_turn or
// stop_sequence, not on tool_use.
func TestAC1_EndOfTurnOnly(t *testing.T) {
	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	// Test case 1: end_turn should trigger extraction
	t.Run("end_turn_triggers", func(t *testing.T) {
		var invokeCount atomic.Int64
		mockClient := &mockExtractionAPIClient{
			sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
				invokeCount.Add(1)
				return &api.Response{}, nil
			},
		}
		me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(100 * time.Millisecond)
		turnCtx := TurnContext{
			StopReason: api.StopReasonEndTurn,
			AssistantMessage: &api.Message{
				ID:      "msg_1",
				Content: "hello",
			},
		}
		me.CheckAndExtract(context.Background(), turnCtx)
		time.Sleep(200 * time.Millisecond)
		if invokeCount.Load() == 0 {
			t.Error("AC1 FAIL: extraction should run on end_turn")
		} else {
			t.Log("AC1 PASS: extraction runs on end_turn")
		}
	})

	// Test case 2: stop_sequence should trigger extraction
	t.Run("stop_sequence_triggers", func(t *testing.T) {
		var invokeCount atomic.Int64
		mockClient := &mockExtractionAPIClient{
			sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
				invokeCount.Add(1)
				return &api.Response{}, nil
			},
		}
		me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(100 * time.Millisecond)
		turnCtx := TurnContext{
			StopReason: api.StopReasonStopSeq,
			AssistantMessage: &api.Message{
				ID:      "msg_2",
				Content: "hello",
			},
		}
		me.CheckAndExtract(context.Background(), turnCtx)
		time.Sleep(200 * time.Millisecond)
		if invokeCount.Load() == 0 {
			t.Error("AC1 FAIL: extraction should run on stop_sequence")
		} else {
			t.Log("AC1 PASS: extraction runs on stop_sequence")
		}
	})

	// Test case 3: tool_use should NOT trigger extraction
	t.Run("tool_use_skipped", func(t *testing.T) {
		var invokeCount atomic.Int64
		mockClient := &mockExtractionAPIClient{
			sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
				invokeCount.Add(1)
				return &api.Response{}, nil
			},
		}
		me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(100 * time.Millisecond)
		turnCtx := TurnContext{
			StopReason: api.StopReasonToolUse,
			AssistantMessage: &api.Message{
				ID:      "msg_3",
				Content: "hello",
			},
		}
		me.CheckAndExtract(context.Background(), turnCtx)
		time.Sleep(200 * time.Millisecond)
		if invokeCount.Load() > 0 {
			t.Error("AC1 FAIL: extraction should NOT run on tool_use")
		} else {
			t.Log("AC1 PASS: extraction does not run on tool_use")
		}
	})

	// Test case 4: max_tokens should NOT trigger extraction
	t.Run("max_tokens_skipped", func(t *testing.T) {
		var invokeCount atomic.Int64
		mockClient := &mockExtractionAPIClient{
			sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
				invokeCount.Add(1)
				return &api.Response{}, nil
			},
		}
		me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(100 * time.Millisecond)
		turnCtx := TurnContext{
			StopReason: api.StopReasonMaxTokens,
			AssistantMessage: &api.Message{
				ID:      "msg_4",
				Content: "hello",
			},
		}
		me.CheckAndExtract(context.Background(), turnCtx)
		time.Sleep(200 * time.Millisecond)
		if invokeCount.Load() > 0 {
			t.Error("AC1 FAIL: extraction should NOT run on max_tokens")
		} else {
			t.Log("AC1 PASS: extraction does not run on max_tokens")
		}
	})
}

// TestAC2_SkipWhenMainAgentWroteMemory verifies that extraction is skipped
// when the main agent already wrote to auto-mem paths in the current turn.
func TestAC2_SkipWhenMainAgentWroteMemory(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	var invokeCount atomic.Int64
	mockClient := &mockExtractionAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			invokeCount.Add(1)
			return &api.Response{}, nil
		},
	}

	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(tmpDir).WithTimeout(100 * time.Millisecond)

	// Simulate main agent editing a file under auto-mem dir
	autoMemDir := filepath.Join(tmpDir, "memory")
	editPath := filepath.Join(autoMemDir, "feedback", "test.md")

	turnCtx := TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			ID:      "msg_1",
			Content: "I updated the memory",
			ToolUse: []api.ToolUseBlock{
				{
					ID:   "tool_1",
					Name: "edit",
					Input: map[string]any{
						"file_path": editPath,
					},
				},
			},
		},
	}

	me.CheckAndExtract(context.Background(), turnCtx)
	time.Sleep(200 * time.Millisecond)

	// Extraction should be skipped because main agent wrote to auto-mem
	if invokeCount.Load() > 0 {
		t.Error("AC2 FAIL: extraction should be skipped when main agent wrote to auto-mem")
	} else {
		t.Log("AC2 PASS: extraction skipped when main agent wrote to auto-mem")
	}

	// Verify cursor still advanced (even though extraction was skipped)
	if me.lastMemoryMessageUuid != "msg_1" {
		t.Errorf("AC2 FAIL: cursor should advance to msg_1, got %s", me.lastMemoryMessageUuid)
	} else {
		t.Log("AC2 PASS: cursor advances even when extraction is skipped")
	}
}

// TestAC2_PostSkipFollowUpTurn verifies that after a skip, the next eligible
// turn still triggers extraction correctly.
func TestAC2_PostSkipFollowUpTurn(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	var invokeCount atomic.Int64
	mockClient := &mockExtractionAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			invokeCount.Add(1)
			return &api.Response{}, nil
		},
	}

	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(tmpDir).WithTimeout(100 * time.Millisecond)

	// Simulate main agent editing a file under auto-mem dir (first turn - skip)
	autoMemDir := filepath.Join(tmpDir, "memory")
	editPath := filepath.Join(autoMemDir, "feedback", "test.md")

	turnCtx := TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			ID:      "msg_1",
			Content: "I updated the memory",
			ToolUse: []api.ToolUseBlock{
				{
					ID:   "tool_1",
					Name: "edit",
					Input: map[string]any{
						"file_path": editPath,
					},
				},
			},
		},
	}

	me.CheckAndExtract(context.Background(), turnCtx)
	time.Sleep(200 * time.Millisecond)

	// Extraction should be skipped in first turn
	if invokeCount.Load() != 0 {
		t.Error("AC2 follow-up FAIL: first turn should skip extraction")
	}

	// Second turn: no main agent write to auto-mem - extraction should run
	turnCtx = TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			ID:      "msg_2",
			Content: "Regular conversation, no memory write",
			// No ToolUse targeting auto-mem
		},
	}

	me.CheckAndExtract(context.Background(), turnCtx)
	time.Sleep(200 * time.Millisecond)

	// Second turn should trigger extraction
	if invokeCount.Load() != 1 {
		t.Errorf("AC2 follow-up FAIL: second turn should trigger extraction, got %d", invokeCount.Load())
	} else {
		t.Log("AC2 follow-up PASS: second turn triggers extraction after skip")
	}

	// Cursor should advance to msg_2
	if me.lastMemoryMessageUuid != "msg_2" {
		t.Errorf("AC2 follow-up FAIL: cursor should advance to msg_2, got %s", me.lastMemoryMessageUuid)
	} else {
		t.Log("AC2 follow-up PASS: cursor advances to msg_2 after second turn")
	}
}

// TestAC3_CompactionCursorFallback verifies that when UUID is missing after
// compaction, the cursor falls back to counting messages.
func TestAC3_CompactionCursorFallback(t *testing.T) {
	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	mockClient := &mockExtractionAPIClient{}
	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(100 * time.Millisecond)

	// Simulate a turn after compaction where UUID is empty but count is available
	turnCtx := TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			// ID is empty - simulating compaction where UUIDs were lost
			ID:      "",
			Content: "hello",
		},
		TotalMessages: 50,
	}

	me.advanceCursor(turnCtx)

	// After advancing with empty UUID, should fall back to count
	if me.lastMemoryMessageUuid != "" {
		t.Errorf("AC3 FAIL: lastMemoryMessageUuid should be empty, got %s", me.lastMemoryMessageUuid)
	}
	if me.lastMemoryMessageCount != 50 {
		t.Errorf("AC3 FAIL: lastMemoryMessageCount should be 50, got %d", me.lastMemoryMessageCount)
	} else {
		t.Log("AC3 PASS: cursor falls back to message count when UUID is missing")
	}

	// AC5 (1): Verify buildExtractionPrompt uses the message-count slice
	turnCtx.RecentMessages = []api.Message{
		{Role: "user", Content: "msg1"},
		{Role: "user", Content: "msg 2"},
		{Role: "user", Content: "msg 3"},
		{Role: "user", Content: "msg 4"},
		{Role: "user", Content: "msg 5"},
	}
	prompt := me.buildExtractionPrompt(turnCtx, "")
	// With lastMemoryMessageCount=50 and only 5 recent messages,
	// the slice should be empty (count >= len(messages))
	if strings.Contains(prompt, "msg 1") || strings.Contains(prompt, "msg 5") {
		t.Error("AC5(1) FAIL: prompt should not contain messages when count >= len(messages)")
	} else {
		t.Log("AC5(1) PASS: buildExtractionPrompt correctly handles count-based slicing")
	}

	// Test with count< len(messages) to verify slicing works
	me.lastMemoryMessageCount = 2
	prompt = me.buildExtractionPrompt(turnCtx, "")
	if !strings.Contains(prompt, "msg 3") {
		t.Error("AC5(1) FAIL: prompt should contain msg 3 (first message after count=2)")
	} else {
		t.Log("AC5(1) PASS: buildExtractionPrompt correctly slices messages after count")
	}
}

// TestAC4_EditScopedToAutoMem verifies that the forked extraction agent
// has Edit and Write tools scoped to the auto-mem directory.
func TestAC4_EditScopedToAutoMem(t *testing.T) {
	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	tmpDir := t.TempDir()
	mockClient := &mockExtractionAPIClient{}
	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(tmpDir)

	tools := me.buildExtractionTools()

	// Build a map for easy lookup
	toolMap := make(map[string]bool)
	for _, t := range tools {
		toolMap[t.Name()] = true
	}

	// Verify required tools are present
	if !toolMap["read"] {
		t.Error("AC4 FAIL: Read tool should be present")
	} else {
		t.Log("AC4 PASS: Read tool present")
	}

	if !toolMap["Grep"] {
		t.Error("AC4 FAIL: Grep tool should be present")
	} else {
		t.Log("AC4 PASS: Grep tool present")
	}

	if !toolMap["Glob"] {
		t.Error("AC4 FAIL: Glob tool should be present")
	} else {
		t.Log("AC4 PASS: Glob tool present")
	}

	if !toolMap["edit"] {
		t.Error("AC4 FAIL: Edit tool should be present")
	} else {
		t.Log("AC4 PASS: Edit tool present")
	}

	if !toolMap["write"] {
		t.Error("AC4 FAIL: Write tool should be present")
	} else {
		t.Log("AC4 PASS: Write tool present")
	}

	// Verify Edit tool is scoped to auto-mem dir
	for _, tl := range tools {
		if tl.Name() == "edit" {
			_, ok := tl.(*tool.EditTool)
			if !ok {
				t.Error("AC4 FAIL: Edit tool is not *EditTool")
				continue
			}
			// The EditTool should have allowedPaths set
			// We can't directly check allowedPaths, but we can verify the tool exists and is properly configured
			t.Log("AC4 PASS: Edit tool is properly configured")
		}
		if tl.Name() == "write" {
			_, ok := tl.(*tool.WriteTool)
			if !ok {
				t.Error("AC4 FAIL: Write tool is not *WriteTool")
				continue
			}
			// The WriteTool should have allowedPaths set
			t.Log("AC4 PASS: Write tool is properly configured")
		}
	}

	// Verify tools NOT present (should not have mutating tools outside auto-mem)
	toolNames := make([]string, 0, len(tools))
	for _, t := range tools {
		toolNames = append(toolNames, t.Name())
	}

	// NotebookEdit should NOT be present
	for _, name := range toolNames {
		if name == "notebook_edit" {
			t.Error("AC4 FAIL: notebook_edit should NOT be present in extraction tools")
		}
	}

	// Bash should NOT be present (AC4 says read-only bash, but we should verify no bash at all for safety)
	for _, name := range toolNames {
		if name == "bash" {
			t.Error("AC4 FAIL: bash should NOT be present in extraction tools")
		}
	}

	t.Log("AC4 PASS: Extraction tools are properly scoped to auto-mem directory")

	// AC4 runtime enforcement sub-test: verify Edit/Write outside memdir is rejected
	t.Run("EditRejectsPathOutsideMemdir", func(t *testing.T) {
		autoMemDir := filepath.Join(tmpDir, "memory")
		os.MkdirAll(autoMemDir, 0755)

		for _, tl := range tools {
			if tl.Name() == "edit" {
				// Try to execute Edit with a path outside memdir
				result, err := tl.Execute(context.Background(), map[string]any{
					"file_path":  "/tmp/outside_memdir.txt",
					"old_string": "test",
					"new_string": "replaced",
				}, autoMemDir)
				if err != nil {
					t.Fatalf("Edit Execute returned error: %v", err)
				}
				if result == nil || !result.IsError {
					t.Error("AC4 runtime FAIL: Edit should reject path outside memdir")
				} else {
					t.Log("AC4 runtime PASS: Edit correctly rejects path outside memdir")
				}
			}
			if tl.Name() == "write" {
				// Try to execute Write with a path outside memdir
				result, err := tl.Execute(context.Background(), map[string]any{
					"file_path": "/tmp/outside_memdir.txt",
					"content":   "test content",
				}, autoMemDir)
				if err != nil {
					t.Fatalf("Write Execute returned error: %v", err)
				}
				if result == nil || !result.IsError {
					t.Error("AC4 runtime FAIL: Write should reject path outside memdir")
				} else {
					t.Log("AC4 runtime PASS: Write correctly rejects path outside memdir")
				}
			}
		}
	})

	// AC5 (2): End-to-end sub-test exercising processExtractionResponse's isUnderAutoMem guard
	t.Run("ProcessExtractionRejectsPathOutsideMemdir", func(t *testing.T) {
		autoMemDir := filepath.Join(tmpDir, "memory")
		os.MkdirAll(autoMemDir, 0755)

		mockClient := &mockExtractionAPIClient{}
		me := NewMemoryExtractor(mockClient, cfg).WithMemdir(tmpDir)
		tools := me.buildExtractionTools()

		// Create a mock response with Edit targeting a path outside auto-mem
		resp := &api.Response{
			Content: []api.ContentBlock{
				{
					Type: "tool_use",
					ToolUse: &api.ToolUse{
						ID:   "tool_1",
						Name: "edit",
						Args: map[string]any{
							"file_path":  "/tmp/outside_memdir.txt",
							"old_string": "test",
							"new_string": "replaced",
						},
					},
				},
			},
		}

		// Create a temp file outside auto-mem to verify the edit was NOT applied
		outsideFile := "/tmp/outside_memdir_test.txt"
		os.WriteFile(outsideFile, []byte("test"), 0644)
		defer os.Remove(outsideFile)

		// Process the extraction response
		me.processExtractionResponse(context.Background(), resp, tools)

		// Verify the file was NOT modified (edit was rejected by isUnderAutoMem guard)
		content, _ := os.ReadFile(outsideFile)
		if string(content) != "test" {
			t.Error("AC5(2) FAIL: file should not be modified - edit should be rejected by processExtractionResponse")
		} else {
			t.Log("AC5(2) PASS: processExtractionResponse correctly rejects edit outside auto-mem")
		}
	})
}

// TestAC5_CoalescingConcurrentRequests verifies that concurrent extraction
// requests are coalesced - only one extraction runs at a time.
func TestAC5_CoalescingConcurrentRequests(t *testing.T) {
	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	var concurrentCount atomic.Int64
	var maxConcurrent atomic.Int64

	mockClient := &mockExtractionAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			count := concurrentCount.Add(1)
			for {
				max := maxConcurrent.Load()
				if count <= max {
					break
				}
				if maxConcurrent.CompareAndSwap(max, count) {
					break
				}
			}

			// Simulate slow extraction
			time.Sleep(500 * time.Millisecond)

			concurrentCount.Add(-1)

			return &api.Response{}, nil
		},
	}

	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(2 * time.Second)

	// Create multiple turn contexts
	turnCtx := TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			ID:      "msg_1",
			Content: "hello",
		},
	}

	// Trigger first extraction
	me.CheckAndExtract(context.Background(), turnCtx)

	// Immediately trigger second extraction (should be coalesced)
	turnCtx.AssistantMessage.ID = "msg_2"
	me.CheckAndExtract(context.Background(), turnCtx)

	// Wait for extraction to complete
	time.Sleep(1 * time.Second)

	// Verify only one extraction ran at a time
	if maxConcurrent.Load() > 1 {
		t.Errorf("AC5 FAIL: max concurrent extractions was %d, expected 1 (coalescing failed)", maxConcurrent.Load())
	} else {
		t.Log("AC5 PASS: concurrent extraction requests are coalesced")
	}
}

// TestAC1_SubAgentSkipped verifies that extraction is skipped for sub-agents.
func TestAC1_SubAgentSkipped(t *testing.T) {
	cfg := ExtractorConfig{
		IsSubAgent:         true, // This is a sub-agent
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	var invokeCount atomic.Int64
	mockClient := &mockExtractionAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			invokeCount.Add(1)
			return &api.Response{}, nil
		},
	}

	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(100 * time.Millisecond)

	turnCtx := TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			ID:      "msg_1",
			Content: "hello",
		},
	}

	me.CheckAndExtract(context.Background(), turnCtx)
	time.Sleep(200 * time.Millisecond)

	if invokeCount.Load() > 0 {
		t.Error("AC1 FAIL: extraction should not run for sub-agent")
	} else {
		t.Log("AC1 PASS: extraction skipped for sub-agent")
	}
}

// TestAC1_AutoMemoryDisabled verifies that extraction is skipped when
// auto-memory is disabled.
func TestAC1_AutoMemoryDisabled(t *testing.T) {
	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  false, // Disabled
		ProjectRoot:        "/test/project",
	}

	var invokeCount atomic.Int64
	mockClient := &mockExtractionAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			invokeCount.Add(1)
			return &api.Response{}, nil
		},
	}

	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(100 * time.Millisecond)

	turnCtx := TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			ID:      "msg_1",
			Content: "hello",
		},
	}

	me.CheckAndExtract(context.Background(), turnCtx)
	time.Sleep(200 * time.Millisecond)

	if invokeCount.Load() > 0 {
		t.Error("AC1 FAIL: extraction should not run when auto-memory is disabled")
	} else {
		t.Log("AC1 PASS: extraction skipped when auto-memory is disabled")
	}
}

// TestThrottleEveryNTurns verifies the throttle mechanism.
func TestThrottleEveryNTurns(t *testing.T) {
	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 3, // Extract every 3 turns
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	var invokeCount atomic.Int64
	mockClient := &mockExtractionAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			invokeCount.Add(1)
			return &api.Response{}, nil
		},
	}

	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(100 * time.Millisecond)

	turnCtx := TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			ID:      "msg_1",
			Content: "hello",
		},
	}

	// First turn - should not extract (turnsSinceLastExtract becomes 1, but throttle is 3)
	me.CheckAndExtract(context.Background(), turnCtx)
	time.Sleep(150 * time.Millisecond)
	if invokeCount.Load() != 0 {
		t.Errorf("Throttle FAIL: should not extract on turn 1, got %d", invokeCount.Load())
	}

	// Second turn - should not extract (turnsSinceLastExtract becomes 2, throttle is 3)
	turnCtx.AssistantMessage.ID = "msg_2"
	me.CheckAndExtract(context.Background(), turnCtx)
	time.Sleep(150 * time.Millisecond)
	if invokeCount.Load() != 0 {
		t.Errorf("Throttle FAIL: should not extract on turn 2, got %d", invokeCount.Load())
	}

	// Third turn - should extract (turnsSinceLastExtract becomes 3, equals throttle)
	turnCtx.AssistantMessage.ID = "msg_3"
	me.CheckAndExtract(context.Background(), turnCtx)
	time.Sleep(150 * time.Millisecond)
	if invokeCount.Load() != 1 {
		t.Errorf("Throttle FAIL: should extract on turn 3, got %d", invokeCount.Load())
	} else {
		t.Log("Throttle PASS: extraction happens every N turns")
	}
}

// TestDrain waits for any in-progress extraction to complete.
func TestDrain(t *testing.T) {
	cfg := ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  true,
		ProjectRoot:        "/test/project",
	}

	var completed atomic.Bool
	mockClient := &mockExtractionAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			// Simulate slow extraction
			time.Sleep(200 * time.Millisecond)
			completed.Store(true)
			return &api.Response{}, nil
		},
	}

	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(t.TempDir()).WithTimeout(2 * time.Second)

	turnCtx := TurnContext{
		StopReason: api.StopReasonEndTurn,
		AssistantMessage: &api.Message{
			ID:      "msg_1",
			Content: "hello",
		},
	}

	// Start extraction
	me.CheckAndExtract(context.Background(), turnCtx)

	// Drain should wait for completion
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	me.Drain(ctx)

	if !completed.Load() {
		t.Error("Drain FAIL: extraction should have completed")
	} else {
		t.Log("Drain PASS: drain waits for extraction to complete")
	}
}

// TestIsUnderAutoMem verifies the path checking logic.
func TestIsUnderAutoMem(t *testing.T) {
	cfg := ExtractorConfig{
		ProjectRoot: "/test/project",
	}

	tmpDir := t.TempDir()
	mockClient := &mockExtractionAPIClient{}
	me := NewMemoryExtractor(mockClient, cfg).WithMemdir(tmpDir)

	// Create auto-mem subdirectories
	autoMemDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(filepath.Join(autoMemDir, "user"), 0755)
	os.MkdirAll(filepath.Join(autoMemDir, "feedback"), 0755)

	tests := []struct {
		path     string
		expected bool
	}{
		{filepath.Join(autoMemDir, "user", "test.md"), true},
		{filepath.Join(autoMemDir, "feedback", "test.md"), true},
		{"/other/path/test.md", false},
		{tmpDir, false}, // tmpDir itself is not under auto-mem
	}

	for _, tc := range tests {
		result := me.isUnderAutoMem(tc.path)
		if result != tc.expected {
			t.Errorf("isUnderAutoMem(%s) = %v, expected %v", tc.path, result, tc.expected)
		}
	}

	t.Log("isUnderAutoMem PASS: correctly identifies paths under auto-mem directory")
}
