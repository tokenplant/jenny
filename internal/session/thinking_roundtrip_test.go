package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/internal/api"
)

// TestThinkingPersistenceRoundTrip_OpenAIChat tests AC4: OpenAI Chat round-trip with reasoning_content
func TestThinkingPersistenceRoundTrip_OpenAIChat(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "jenny-thinking-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_openai_chat_thinking"

	// Simulate an assistant entry with thinking from OpenAI Chat API
	entry := TranscriptEntry{
		Type:      "assistant",
		Content:   "The answer is 42.",
		Thinking:  "Let me think about this: 2 * 21 = 42",
		ToolUse:   []ToolUse{{ID: "call_123", Name: "calculate", Input: map[string]any{"expr": "2*21"}}},
	}

	if err := m.AppendEntry(sessionID, entry); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Load and verify thinking is persisted
	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded))
	}

	if loaded[0].Thinking != entry.Thinking {
		t.Errorf("Thinking = %q, want %q", loaded[0].Thinking, entry.Thinking)
	}

	if loaded[0].Content != entry.Content {
		t.Errorf("Content = %q, want %q", loaded[0].Content, entry.Content)
	}

	if len(loaded[0].ToolUse) != 1 {
		t.Errorf("expected 1 tool_use, got %d", len(loaded[0].ToolUse))
	}
}

// TestThinkingPersistenceRoundTrip_Anthropic tests AC4: Anthropic thinking block with signature
func TestThinkingPersistenceRoundTrip_Anthropic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-thinking-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_anthropic_thinking"

	// Simulate an assistant entry with thinking from Anthropic API
	entry := TranscriptEntry{
		Type:      "assistant",
		Content:   "I'll help you with that.",
		Thinking:  "The user wants assistance. Let me analyze the best approach...",
		Signature: "sig_anthropic_abc123",
		ToolUse:   []ToolUse{{ID: "toolu_xyz", Name: "web_search", Input: map[string]any{"query": "help"}}},
	}

	if err := m.AppendEntry(sessionID, entry); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded))
	}

	if loaded[0].Thinking != entry.Thinking {
		t.Errorf("Thinking = %q, want %q", loaded[0].Thinking, entry.Thinking)
	}

	if loaded[0].Signature != entry.Signature {
		t.Errorf("Signature = %q, want %q", loaded[0].Signature, entry.Signature)
	}
}

// TestThinkingPersistenceRoundTrip_ResponsesAPI tests AC4: Responses API thinking block
func TestThinkingPersistenceRoundTrip_ResponsesAPI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-thinking-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_responses_thinking"

	// Simulate an assistant entry with thinking from Responses API
	entry := TranscriptEntry{
		Type:      "assistant",
		Content:   "Based on my analysis, the solution is clear.",
		Thinking:  "I analyzed the problem step by step and arrived at the conclusion.",
		ToolUse:   []ToolUse{{ID: "call_resp_123", Name: "analyze_data", Input: map[string]any{"data": "sample"}}},
	}

	if err := m.AppendEntry(sessionID, entry); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded))
	}

	if loaded[0].Thinking != entry.Thinking {
		t.Errorf("Thinking = %q, want %q", loaded[0].Thinking, entry.Thinking)
	}

	if loaded[0].Content != entry.Content {
		t.Errorf("Content = %q, want %q", loaded[0].Content, entry.Content)
	}
}

// TestThinkingPersistenceRoundTrip_MultiTurn tests a full multi-turn conversation with thinking
func TestThinkingPersistenceRoundTrip_MultiTurn(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-multiturn-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_multiturn_thinking"

	// Turn 1: User asks question
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: "What's the weather?"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Turn 1: Assistant responds with thinking and tool call
	if err := m.AppendEntry(sessionID, TranscriptEntry{
		Type:     "assistant",
		Content:  "I'll check the weather for you.",
		Thinking: "The user wants weather info. I should call the weather tool.",
		ToolUse:  []ToolUse{{ID: "call_w1", Name: "get_weather", Input: map[string]any{"location": "NYC"}}},
	}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Turn 2: Tool result
	if err := m.AppendEntry(sessionID, TranscriptEntry{
		Type:    "tool_result",
		ToolID:  "call_w1",
		Content: "Sunny, 72°F",
	}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Turn 2: Assistant responds with thinking
	if err := m.AppendEntry(sessionID, TranscriptEntry{
		Type:     "assistant",
		Content:  "It's sunny and 72°F in NYC today.",
		Thinking: "The weather API returned sunny conditions. Let me provide a friendly response.",
	}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Load full transcript
	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(loaded))
	}

	// Verify thinking in first assistant message
	if loaded[1].Thinking != "The user wants weather info. I should call the weather tool." {
		t.Errorf("Turn 1 thinking = %q, want %q", loaded[1].Thinking, "The user wants weather info. I should call the weather tool.")
	}

	// Verify thinking in second assistant message
	if loaded[3].Thinking != "The weather API returned sunny conditions. Let me provide a friendly response." {
		t.Errorf("Turn 2 thinking = %q, want %q", loaded[3].Thinking, "The weather API returned sunny conditions. Let me provide a friendly response.")
	}

	// Verify tool_use is preserved
	if len(loaded[1].ToolUse) != 1 || loaded[1].ToolUse[0].Name != "get_weather" {
		t.Errorf("ToolUse not preserved correctly")
	}
}

// TestThinkingPersistenceRoundTrip_BackwardCompat tests AC5: loading old transcripts without thinking fields
func TestThinkingPersistenceRoundTrip_BackwardCompat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-backcompat-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_backcompat"

	// Create a transcript manually with old format (no thinking/signature fields)
	path := m.transcriptPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	oldEntries := []TranscriptEntry{
		{Type: "user", Content: "Hello"},
		{Type: "assistant", Content: "Hi there"},
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for _, e := range oldEntries {
		data, _ := json.Marshal(e)
		f.Write(append(data, '\n'))
	}
	f.Close()

	// Load should not error
	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 2 {
		t.Errorf("expected 2 entries, got %d", len(loaded))
	}

	// Thinking and Signature should be empty (not error)
	for _, e := range loaded {
		if e.Thinking != "" {
			t.Errorf("Thinking should be empty for old format, got %q", e.Thinking)
		}
		if e.Signature != "" {
			t.Errorf("Signature should be empty for old format, got %q", e.Signature)
		}
	}
}

// TestThinkingPersistenceRoundTrip_TranscriptToAPIMessages tests that thinking can be
// extracted from transcript entries and passed to RebuildMessages
func TestThinkingPersistenceRoundTrip_TranscriptToAPIMessages(t *testing.T) {
	// This test verifies the data contract between transcript loading and API message building
	tmpDir, err := os.MkdirTemp("", "jenny-api-contract-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_api_contract"

	// Append entry with full thinking context
	entry := TranscriptEntry{
		Type:      "assistant",
		Content:   "Here's the analysis.",
		Thinking:  "Step 1: Identify the key variables...",
		Signature: "sig_789xyz",
		ToolUse: []ToolUse{
			{ID: "tool_1", Name: "analyze", Input: map[string]any{"data": "test"}},
		},
	}
	if err := m.AppendEntry(sessionID, entry); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	// Simulate RebuildMessages behavior: extract thinking from transcript entry
	if len(loaded) > 0 {
		e := loaded[0]
		msg := api.Message{
			Role:      e.Type,
			Content:   e.Content,
			Thinking:  e.Thinking,
			Signature: e.Signature,
		}

		// Verify the message can carry thinking/signature
		if msg.Thinking != entry.Thinking {
			t.Errorf("msg.Thinking = %q, want %q", msg.Thinking, entry.Thinking)
		}
		if msg.Signature != entry.Signature {
			t.Errorf("msg.Signature = %q, want %q", msg.Signature, entry.Signature)
		}
	}
}
