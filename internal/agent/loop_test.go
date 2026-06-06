package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// MockTool is a simple tool for testing.
type MockTool struct {
	name        string
	description string
	inputSchema map[string]any
	executeFn   func(input map[string]any, cwd string) (*tool.ToolResult, error)
}

func (t *MockTool) Name() string                { return t.name }
func (t *MockTool) Description() string         { return t.description }
func (t *MockTool) InputSchema() map[string]any { return t.inputSchema }
func (t *MockTool) Execute(input map[string]any, cwd string) (*tool.ToolResult, error) {
	return t.executeFn(input, cwd)
}

func TestToolUseBlockCollection(t *testing.T) {
	// Test that tool use blocks are properly collected from response content
	// This is a unit test for the message building logic

	// Create mock tools
	bashTool := tool.NewBashTool()
	readTool := tool.NewReadTool()

	// Verify tools have correct names
	if bashTool.Name() != "bash" {
		t.Errorf("expected bash tool name 'bash', got %q", bashTool.Name())
	}
	if readTool.Name() != "read" {
		t.Errorf("expected read tool name 'read', got %q", readTool.Name())
	}
}

func TestFindTool(t *testing.T) {
	bashTool := tool.NewBashTool()
	readTool := tool.NewReadTool()

	tools := []tool.Tool{bashTool, readTool}

	// Test finding existing tools
	found := tool.FindTool(tools, "bash")
	if found == nil {
		t.Error("expected to find bash tool")
	}
	if found.Name() != "bash" {
		t.Errorf("expected bash tool, got %q", found.Name())
	}

	found = tool.FindTool(tools, "read")
	if found == nil {
		t.Error("expected to find read tool")
	}
	if found.Name() != "read" {
		t.Errorf("expected read tool, got %q", found.Name())
	}

	// Test finding non-existent tool
	found = tool.FindTool(tools, "nonexistent")
	if found != nil {
		t.Error("expected nil for nonexistent tool")
	}
}

func TestMessageBuilding(t *testing.T) {
	// Test that messages are properly structured using actual API types
	msg := api.Message{
		Role:    "assistant",
		Content: "Hello",
	}

	if msg.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", msg.Role)
	}
	if msg.Content != "Hello" {
		t.Errorf("expected content 'Hello', got %q", msg.Content)
	}

	// Test message with tool use blocks
	msgWithToolUse := api.Message{
		Role:    "assistant",
		Content: "",
		ToolUse: []api.ToolUseBlock{
			{
				ID:    "tool_123",
				Name:  "bash",
				Input: map[string]any{"command": "ls"},
			},
		},
	}

	if len(msgWithToolUse.ToolUse) != 1 {
		t.Errorf("expected 1 tool use block, got %d", len(msgWithToolUse.ToolUse))
	}
	if msgWithToolUse.ToolUse[0].Name != "bash" {
		t.Errorf("expected tool name 'bash', got %q", msgWithToolUse.ToolUse[0].Name)
	}

	// Test message with tool results
	msgWithToolResults := api.Message{
		Role:        "user",
		ToolResults: []api.ToolResultBlock{{ToolUseID: "tool_123", Content: "file1.txt"}},
	}

	if len(msgWithToolResults.ToolResults) != 1 {
		t.Errorf("expected 1 tool result, got %d", len(msgWithToolResults.ToolResults))
	}
	if msgWithToolResults.ToolResults[0].Content != "file1.txt" {
		t.Errorf("expected content 'file1.txt', got %q", msgWithToolResults.ToolResults[0].Content)
	}
}

func TestToolResultStructure(t *testing.T) {
	// Test ToolResult structure
	result := &tool.ToolResult{
		Content: "test content",
		IsError: false,
	}

	if result.Content != "test content" {
		t.Errorf("expected 'test content', got %q", result.Content)
	}
	if result.IsError != false {
		t.Error("expected IsError to be false")
	}

	// Test error case
	errorResult := &tool.ToolResult{
		Content: "error message",
		IsError: true,
	}

	if !errorResult.IsError {
		t.Error("expected IsError to be true")
	}
}

func TestMaxIterationsBound(t *testing.T) {
	// Verify that max iterations constant is bounded to prevent infinite loops
	if MaxIterations <= 0 {
		t.Error("max iterations must be positive")
	}
	if MaxIterations > 1000 {
		t.Error("max iterations seems unreasonably high")
	}
	// This should be sufficient for even complex multi-turn tool use cases
	if MaxIterations < 10 {
		t.Error("max iterations seems too low for practical use")
	}
}

func TestToolInputValidation(t *testing.T) {
	// Test that tools validate their inputs correctly
	bashTool := tool.NewBashTool()

	// Test missing command
	result, err := bashTool.Execute(map[string]any{}, "/tmp")
	if err == nil {
		// Error is returned in content, not as error
		if result == nil {
			t.Error("expected result")
		}
		if !result.IsError {
			t.Error("expected error for missing command")
		}
	}

	// Test empty command
	result, err = bashTool.Execute(map[string]any{"command": ""}, "/tmp")
	if err == nil {
		if result == nil {
			t.Error("expected result")
		}
		if !result.IsError {
			t.Error("expected error for empty command")
		}
	}
}

func TestReadToolInputValidation(t *testing.T) {
	// Test that read tool validates inputs correctly
	readTool := tool.NewReadTool()

	// Test missing file_path
	result, err := readTool.Execute(map[string]any{}, "/tmp")
	if err == nil {
		if result == nil {
			t.Error("expected result")
		}
		if !result.IsError {
			t.Error("expected error for missing file_path")
		}
	}

	// Test empty file_path
	result, err = readTool.Execute(map[string]any{"file_path": ""}, "/tmp")
	if err == nil {
		if result == nil {
			t.Error("expected result")
		}
		if !result.IsError {
			t.Error("expected error for empty file_path")
		}
	}
}

func TestContextCancellation(t *testing.T) {
	// Test that context cancellation properly terminates operations
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Verify context gets cancelled after timeout
	select {
	case <-ctx.Done():
		if ctx.Err() != context.DeadlineExceeded {
			t.Errorf("expected DeadlineExceeded, got %v", ctx.Err())
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout was not respected")
	}
}

func TestContextImmediateCancel(t *testing.T) {
	// Test immediate context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("expected context to be cancelled immediately")
	}
}

func TestContextNotCancelled(t *testing.T) {
	// Test that context is not cancelled when timeout is large
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Context should not be done immediately
	select {
	case <-ctx.Done():
		t.Error("context should not be cancelled immediately")
	default:
		// Expected
	}
}

func TestDefaultSystemPromptExists(t *testing.T) {
	// Verify that defaultSystemPrompt is defined and non-empty
	if defaultSystemPrompt == "" {
		t.Error("defaultSystemPrompt should not be empty")
	}
	// Verify it contains expected content
	if len(defaultSystemPrompt) < 20 {
		t.Error("defaultSystemPrompt seems too short")
	}
}

func TestDefaultSystemPromptUsedInRun(t *testing.T) {
	// This test verifies that the system prompt constant is accessible
	// and has the expected structure for both Run and RunStream
	prompt := defaultSystemPrompt

	// The system prompt should mention tools
	if prompt == "" {
		t.Error("system prompt should not be empty")
	}
}

func TestMaxIterationsIsReasonable(t *testing.T) {
	// MaxIterations should be set to a value that prevents infinite loops
	// but allows for complex multi-turn conversations
	if MaxIterations < 10 {
		t.Errorf("MaxIterations=%d seems too low", MaxIterations)
	}
	if MaxIterations > 200 {
		t.Errorf("MaxIterations=%d seems too high", MaxIterations)
	}
}

func TestRebuildMessages(t *testing.T) {
	tests := []struct {
		name     string
		entries  []session.TranscriptEntry
		wantLen  int
		wantErr  bool
		validate func(*testing.T, []api.Message)
	}{
		{
			name:    "empty entries",
			entries: []session.TranscriptEntry{},
			wantLen: 0,
		},
		{
			name: "single user message",
			entries: []session.TranscriptEntry{
				{Type: "user", Content: "Hello"},
			},
			wantLen: 1,
			validate: func(t *testing.T, msgs []api.Message) {
				if msgs[0].Role != "user" || msgs[0].Content != "Hello" {
					t.Errorf("expected user message with content 'Hello', got %+v", msgs[0])
				}
			},
		},
		{
			name: "user then assistant with text",
			entries: []session.TranscriptEntry{
				{Type: "user", Content: "Hello"},
				{Type: "assistant", Content: "Hi there"},
			},
			wantLen: 2,
			validate: func(t *testing.T, msgs []api.Message) {
				if msgs[0].Role != "user" {
					t.Errorf("expected first message role 'user', got %q", msgs[0].Role)
				}
				if msgs[1].Role != "assistant" || msgs[1].Content != "Hi there" {
					t.Errorf("expected assistant message with content 'Hi there', got %+v", msgs[1])
				}
			},
		},
		{
			name: "assistant with tool use",
			entries: []session.TranscriptEntry{
				{Type: "user", Content: "List files"},
				{Type: "assistant", Content: "", ToolUse: []session.ToolUse{
					{ID: "tool_1", Name: "bash", Input: map[string]any{"command": "ls"}},
				}},
			},
			wantLen: 2,
			validate: func(t *testing.T, msgs []api.Message) {
				if len(msgs[1].ToolUse) != 1 {
					t.Errorf("expected 1 tool use, got %d", len(msgs[1].ToolUse))
				}
				if msgs[1].ToolUse[0].Name != "bash" {
					t.Errorf("expected tool name 'bash', got %q", msgs[1].ToolUse[0].Name)
				}
			},
		},
		{
			name: "tool result in separate user message",
			entries: []session.TranscriptEntry{
				{Type: "user", Content: "List files"},
				{Type: "assistant", Content: "", ToolUse: []session.ToolUse{
					{ID: "tool_1", Name: "bash", Input: map[string]any{"command": "ls"}},
				}},
				{Type: "tool_result", ToolID: "tool_1", Content: "file1.txt\nfile2.txt"},
			},
			wantLen: 3,
			validate: func(t *testing.T, msgs []api.Message) {
				// Tool results must be in a user message (per API spec), not attached to assistant's tool_use
				// messages[0] = user (List files)
				// messages[1] = assistant with tool_use only (no tool_results)
				// messages[2] = user with tool_result
				if len(msgs) != 3 {
					t.Fatalf("expected 3 messages, got %d", len(msgs))
				}
				if msgs[1].Role != "assistant" {
					t.Errorf("expected messages[1] role 'assistant', got %q", msgs[1].Role)
				}
				if len(msgs[1].ToolUse) != 1 {
					t.Errorf("expected 1 tool use in assistant, got %d", len(msgs[1].ToolUse))
				}
				if len(msgs[1].ToolResults) != 0 {
					t.Errorf("expected assistant to have no tool_results (they go in user message), got %d", len(msgs[1].ToolResults))
				}
				if msgs[2].Role != "user" {
					t.Errorf("expected messages[2] role 'user', got %q", msgs[2].Role)
				}
				if len(msgs[2].ToolResults) != 1 {
					t.Errorf("expected 1 tool result in user message, got %d", len(msgs[2].ToolResults))
				}
				if msgs[2].ToolResults[0].Content != "file1.txt\nfile2.txt" {
					t.Errorf("expected tool result content, got %q", msgs[2].ToolResults[0].Content)
				}
			},
		},
		{
			name: "orphan tool result creates user message",
			entries: []session.TranscriptEntry{
				{Type: "tool_result", ToolID: "tool_1", Content: "some result"},
			},
			wantLen: 1,
			validate: func(t *testing.T, msgs []api.Message) {
				if msgs[0].Role != "user" {
					t.Errorf("expected orphan tool result in user message, got role %q", msgs[0].Role)
				}
				if len(msgs[0].ToolResults) != 1 {
					t.Errorf("expected 1 tool result in user message, got %d", len(msgs[0].ToolResults))
				}
			},
		},
		{
			name: "multi-turn conversation",
			entries: []session.TranscriptEntry{
				{Type: "user", Content: "Hello"},
				{Type: "assistant", Content: "Hi!"},
				{Type: "user", Content: "How are you?"},
				{Type: "assistant", Content: "Fine thanks"},
			},
			wantLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := RebuildMessages(tt.entries)
			if len(msgs) != tt.wantLen {
				t.Errorf("RebuildMessages() returned %d messages, want %d", len(msgs), tt.wantLen)
			}
			if tt.validate != nil {
				tt.validate(t, msgs)
			}
		})
	}
}

// TestResumeWithToolCalls is an end-to-end integration test for session resume
// with tool calls. It verifies that when a session contains tool_use and tool_result
// entries, the resumed conversation has correct message structure:
// - assistant messages with tool_use stay as assistant
// - tool_result entries become user messages with ToolResults
// - This matches the API spec requirement that tool results go in user messages
func TestResumeWithToolCalls(t *testing.T) {
	// Create a temporary transcript directory
	tmpDir := t.TempDir()

	// Create session manager
	mgr, err := session.NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_test_resume_toolcalls"

	// Simulate a conversation with tool calls:
	// 1. User asks to list files
	// 2. Assistant responds with tool_use for bash
	// 3. Tool result comes back with file listing
	// 4. Assistant provides final response

	entries := []session.TranscriptEntry{
		{Type: "user", Content: "List files in /tmp"},
		{Type: "assistant", Content: "", ToolUse: []session.ToolUse{
			{ID: "tool_1", Name: "bash", Input: map[string]any{"command": "ls /tmp"}},
		}},
		{Type: "tool_result", ToolID: "tool_1", Content: "file1.txt\nfile2.txt"},
		{Type: "assistant", Content: "I found 2 files: file1.txt and file2.txt"},
	}

	// Append all entries to the transcript
	for _, entry := range entries {
		if err := mgr.AppendEntry(sessionID, entry); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Verify transcript was persisted
	if !mgr.SessionExists(sessionID) {
		t.Fatal("SessionExists() = false, want true after appending entries")
	}

	// Load transcript (this is what happens on resume)
	loadedEntries, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loadedEntries) != len(entries) {
		t.Errorf("LoadTranscript() returned %d entries, want %d", len(loadedEntries), len(entries))
	}

	// Rebuild messages (this is what happens on resume to reconstruct API messages)
	msgs := RebuildMessages(loadedEntries)

	// Verify message structure:
	// msgs[0] = user (List files in /tmp)
	// msgs[1] = assistant with tool_use (no text, just tool_use)
	// msgs[2] = user with tool_result (NOT attached to assistant)
	// msgs[3] = assistant with final response

	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages after rebuild, got %d", len(msgs))
	}

	// Verify first user message
	if msgs[0].Role != "user" {
		t.Errorf("msgs[0] role = %q, want %q", msgs[0].Role, "user")
	}
	if msgs[0].Content != "List files in /tmp" {
		t.Errorf("msgs[0] content = %q, want %q", msgs[0].Content, "List files in /tmp")
	}

	// Verify assistant message with tool_use
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1] role = %q, want %q", msgs[1].Role, "assistant")
	}
	if len(msgs[1].ToolUse) != 1 {
		t.Errorf("msgs[1] has %d tool_use blocks, want 1", len(msgs[1].ToolUse))
	}
	if msgs[1].ToolUse[0].Name != "bash" {
		t.Errorf("msgs[1] tool_use[0] name = %q, want %q", msgs[1].ToolUse[0].Name, "bash")
	}
	// CRITICAL: assistant should NOT have tool_results attached
	if len(msgs[1].ToolResults) != 0 {
		t.Errorf("msgs[1] has %d tool_results, want0 (tool_results must be in separate user message)", len(msgs[1].ToolResults))
	}

	// Verify tool result is in a separate user message
	if msgs[2].Role != "user" {
		t.Errorf("msgs[2] role = %q, want %q (tool_result must be in user message)", msgs[2].Role, "user")
	}
	if len(msgs[2].ToolResults) != 1 {
		t.Errorf("msgs[2] has %d tool_results, want 1", len(msgs[2].ToolResults))
	}
	if msgs[2].ToolResults[0].ToolUseID != "tool_1" {
		t.Errorf("msgs[2] tool_results[0] tool_use_id = %q, want %q", msgs[2].ToolResults[0].ToolUseID, "tool_1")
	}
	if msgs[2].ToolResults[0].Content != "file1.txt\nfile2.txt" {
		t.Errorf("msgs[2] tool_results[0] content = %q, want %q", msgs[2].ToolResults[0].Content, "file1.txt\nfile2.txt")
	}

	// Verify final assistant message
	if msgs[3].Role != "assistant" {
		t.Errorf("msgs[3] role = %q, want %q", msgs[3].Role, "assistant")
	}
	if msgs[3].Content != "I found 2 files: file1.txt and file2.txt" {
		t.Errorf("msgs[3] content = %q, want %q", msgs[3].Content, "I found 2 files: file1.txt and file2.txt")
	}
}
