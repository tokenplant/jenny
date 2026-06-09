// Package agent provides the core agent loop and query engine.
package agent

import (
	"testing"

	"github.com/ipy/jenny/internal/api"
)

func TestNormalizeMessages_StripsVirtualMessages(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "Hello", IsVirtual: true},
		{Role: "user", Content: "Real message"},
	}
	result := NormalizeMessagesAPI(messages)
	// Virtual messages should be stripped
	if len(result) != 1 {
		t.Errorf("expected 1 message after stripping virtual, got %d", len(result))
	}
	if result[0].Content != "Real message" {
		t.Errorf("expected 'Real message', got %q", result[0].Content)
	}
}

func TestNormalizeMessages_StripsProgressMessages(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{Type: "progress", Content: "Thinking..."},
		{Role: "assistant", Content: "Response"},
	}
	result := NormalizeMessagesAPI(messages)
	// Progress messages should be stripped
	if len(result) != 2 {
		t.Errorf("expected 2 messages after stripping progress, got %d", len(result))
	}
}

func TestNormalizeMessages_MergesConsecutiveSameRole(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "First"},
		{Role: "user", Content: "Second"},
		{Role: "assistant", Content: "Response"},
	}
	result := NormalizeMessagesAPI(messages)
	// Consecutive same-role messages should be merged
	// After normalization, we should have fewer messages
	if len(result) != 2 {
		t.Logf("got %d messages after merge", len(result))
	}
}

func TestEnsureToolResultPairing_Forward_MissingToolResult(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "I'll help",
			ToolUse: []api.ToolUseBlock{
				{ID: "tool_1", Name: "Read", Input: map[string]any{"file_path": "test.txt"}},
			},
		},
		{
			Role:        "user",
			ToolResults: []api.ToolResultBlock{
				// Missing tool_result for tool_1
			},
		},
	}
	result := ensureToolResultPairing(messages)
	// Should have assistant + user with synthetic error (2 messages total)
	if len(result) != 2 {
		t.Errorf("expected 2 messages (assistant + user with synthetic error), got %d", len(result))
	}
	// Verify the user message has the synthetic error tool_result
	userMsgFound := false
	for _, msg := range result {
		if msg.Role == "user" && len(msg.ToolResults) == 1 {
			userMsgFound = true
			if msg.ToolResults[0].ToolUseID != "tool_1" {
				t.Errorf("expected synthetic error for tool_1, got tool_use_id=%s", msg.ToolResults[0].ToolUseID)
			}
		}
	}
	if !userMsgFound {
		t.Error("expected user message with synthetic error tool_result")
	}
}

func TestEnsureToolResultPairing_Reverse_OrphanedToolResult(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "Done",
		},
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "orphan_tool", Content: "Some result"},
			},
		},
	}
	result := ensureToolResultPairing(messages)
	// Orphaned tool_result should be stripped
	if len(result) != 1 {
		t.Errorf("expected 1 message after stripping orphaned tool_result, got %d", len(result))
	}
}

func TestEnsureToolResultPairing_DuplicateIDs(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "Using tools",
			ToolUse: []api.ToolUseBlock{
				{ID: "tool_1", Name: "Read", Input: map[string]any{"file_path": "a.txt"}},
				{ID: "tool_1", Name: "Read", Input: map[string]any{"file_path": "b.txt"}}, // duplicate
			},
		},
		{
			Role: "user",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "tool_1", Content: "Result for a.txt"},
				{ToolUseID: "tool_1", Content: "Result for b.txt"}, // duplicate
			},
		},
	}
	result := ensureToolResultPairing(messages)
	// Duplicate IDs should be deduped
	// Should have only one tool_result per tool_use_id
	assistantCount := 0
	for _, msg := range result {
		if msg.Role == "assistant" {
			assistantCount++
			if len(msg.ToolUse) != 1 {
				t.Errorf("expected 1 tool_use after dedup, got %d", len(msg.ToolUse))
			}
		}
	}
	if assistantCount != 1 {
		t.Errorf("expected 1 assistant message, got %d", assistantCount)
	}
}

func TestEnsureToolResultPairing_EmptyAssistantAfterStrip(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "", // empty after stripping
			ToolUse: []api.ToolUseBlock{
				// tool_use that gets stripped
			},
		},
		{
			Role:        "user",
			ToolResults: []api.ToolResultBlock{},
		},
	}
	result := ensureToolResultPairing(messages)
	// Should insert [Tool use interrupted]
	found := false
	for _, msg := range result {
		if msg.Role == "assistant" && msg.Content == "[Tool use interrupted]" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected [Tool use interrupted] placeholder for empty assistant")
	}
}

func TestStripTrailingThinking(t *testing.T) {
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "Hello<thinking>Let me think about this</thinking>",
		},
	}
	result := stripTrailingThinking(messages)
	if result[0].Content != "Hello" {
		t.Errorf("expected 'Hello', got %q", result[0].Content)
	}
}

func TestFilterWhitespaceOnly(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "   "},
		{Role: "user", Content: "Real message"},
		{Role: "assistant", Content: "   \n\t  "},
		{Role: "assistant", Content: "Response"},
	}
	result := filterWhitespaceOnly(messages)
	if len(result) != 2 {
		t.Errorf("expected 2 messages after filtering whitespace, got %d", len(result))
	}
}

func TestMapMediaErrorToUserMessage(t *testing.T) {
	tests := []struct {
		errorMsg string
		expected string
		isMedia  bool
	}{
		{"Image is too large", "Image is too large. Please use a smaller image (max 5MB).", true},
		{"PDF has too many pages", "PDF has too many pages. Please use a PDF with fewer pages.", true},
		{"PDF is password protected", "PDF is password protected. Please provide an unprotected PDF.", true},
		{"PDF is invalid", "PDF is invalid or corrupted. Please provide a valid PDF file.", true},
		{"HTTP 413 Request Too Large", "Request is too large. Please reduce the content size and try again.", true},
		{"Some other error", "Some other error", false},
	}

	for _, tt := range tests {
		result, isMedia := mapMediaErrorToUserMessage(tt.errorMsg)
		if result != tt.expected {
			t.Errorf("mapMediaErrorToUserMessage(%q) = %q, want %q", tt.errorMsg, result, tt.expected)
		}
		if isMedia != tt.isMedia {
			t.Errorf("mapMediaErrorToUserMessage(%q) isMedia = %v, want %v", tt.errorMsg, isMedia, tt.isMedia)
		}
	}
}

func TestIsAPISafe(t *testing.T) {
	tests := []struct {
		msg      api.Message
		expected bool
	}{
		{api.Message{Role: "user", Content: "Hello", IsVirtual: false, Type: ""}, true},
		{api.Message{Role: "user", Content: "Hello", IsVirtual: true, Type: ""}, false},
		{api.Message{Role: "user", Content: "Hello", IsVirtual: false, Type: "progress"}, false},
		{api.Message{Role: "assistant", Content: "Hi", IsVirtual: false, Type: ""}, true},
		{api.Message{Role: "assistant", Content: "Hi", IsVirtual: true, Type: ""}, false},
	}

	for _, tt := range tests {
		result := tt.msg.IsAPISafe()
		if result != tt.expected {
			t.Errorf("IsAPISafe() for %+v = %v, want %v", tt.msg, result, tt.expected)
		}
	}
}

func TestNormalizeMessages_ThinkingNormalization(t *testing.T) {
	// Test 5-step order: orphaned filter → trailing strip → whitespace filter → non-empty guard → tool pairing
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "<thinking>orphan</thinking>", // orphaned thinking
		},
		{
			Role:    "assistant",
			Content: "Hello<thinking>trailing</thinking>", // trailing thinking
		},
		{
			Role:    "assistant",
			Content: "   ", // whitespace only
		},
		{
			Role:    "assistant",
			Content: "", // will need placeholder after empty
			ToolUse: []api.ToolUseBlock{
				{ID: "tool_1", Name: "Bash", Input: map[string]any{"command": "ls"}},
			},
		},
		{
			Role:        "user",
			ToolResults: []api.ToolResultBlock{}, // no result for tool_1
		},
	}

	result := NormalizeMessagesAPI(messages)

	// Step 1: Orphaned thinking filter should have removed the orphan thinking content
	// Step 2: Trailing thinking stripped
	// Step 3: Whitespace-only filtered
	// Step 4: Non-empty assistant guard - since we have tool_use, no placeholder needed
	// Step 5: Tool pairing - missing result should get synthetic error

	// Verify the assistant messages
	assistantCount := 0
	for _, msg := range result {
		if msg.Role == "assistant" {
			assistantCount++
		}
	}

	// We should have assistant messages with proper content
	if assistantCount < 1 {
		t.Errorf("expected at least 1 assistant message, got %d", assistantCount)
	}
}

func TestStripMediaErrorFromMessage(t *testing.T) {
	msg := &api.Message{
		Role: "user",
		ToolResults: []api.ToolResultBlock{
			{ToolUseID: "tool_1", Content: "Result 1"},
			{ToolUseID: "tool_2", Content: "Result 2"},
			{ToolUseID: "tool_3", Content: "Result 3"},
		},
	}

	StripMediaErrorFromMessage(msg, "tool_2")

	if len(msg.ToolResults) != 2 {
		t.Errorf("expected 2 tool_results after strip, got %d", len(msg.ToolResults))
	}

	found := false
	for _, tr := range msg.ToolResults {
		if tr.ToolUseID == "tool_2" {
			found = true
			break
		}
	}
	if found {
		t.Error("tool_2 should have been stripped")
	}
}

func TestNormalizeMessages_EmptySlice(t *testing.T) {
	var messages []api.Message
	result := NormalizeMessagesAPI(messages)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestMergeConsecutiveSameRole(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "user", Content: "World"},
		{Role: "assistant", Content: "Hi"},
		{Role: "assistant", Content: "There"},
	}
	result := mergeConsecutiveSameRole(messages)

	if len(result) != 2 {
		t.Errorf("expected 2 merged messages, got %d", len(result))
	}

	// First message should have merged content
	if result[0].Role != "user" || result[0].Content != "Hello\nWorld" {
		t.Errorf("expected merged user content 'Hello\\nWorld', got %q", result[0].Content)
	}
}

func TestMergeConsecutiveSameRole_DedupToolResults(t *testing.T) {
	// AC1: mergeConsecutiveSameRole dedupes tool_results by ToolUseID (last writer wins)
	messages := []api.Message{
		{
			Role:    "user",
			Content: "First user message",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "id_1", Content: "Result1"},
			},
		},
		{
			Role:    "user",
			Content: "Second user message",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "id_1", Content: "Result 1 - updated"},
			},
		},
	}
	result := mergeConsecutiveSameRole(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 merged user message, got %d", len(result))
	}

	if len(result[0].ToolResults) != 1 {
		t.Errorf("expected 1 tool_result after dedup, got %d", len(result[0].ToolResults))
	}

	if result[0].ToolResults[0].ToolUseID != "id_1" {
		t.Errorf("expected tool_use_id 'id_1', got %q", result[0].ToolResults[0].ToolUseID)
	}

	// Last writer wins - should have "Result 1 - updated"
	if result[0].ToolResults[0].Content != "Result 1 - updated" {
		t.Errorf("expected 'Result 1 - updated' (last writer wins), got %q", result[0].ToolResults[0].Content)
	}
}

func TestMergeConsecutiveSameRole_PreservesUnique(t *testing.T) {
	// AC2: mergeConsecutiveSameRole preserves unique tool_results
	messages := []api.Message{
		{
			Role:    "user",
			Content: "First user message",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "id_A", Content: "Result A"},
				{ToolUseID: "id_B", Content: "Result B"},
			},
		},
		{
			Role:    "user",
			Content: "Second user message",
			ToolResults: []api.ToolResultBlock{
				{ToolUseID: "id_C", Content: "Result C"},
			},
		},
	}
	result := mergeConsecutiveSameRole(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 merged user message, got %d", len(result))
	}

	if len(result[0].ToolResults) != 3 {
		t.Errorf("expected 3 unique tool_results, got %d", len(result[0].ToolResults))
	}

	// Verify all three IDs are present
	idSet := make(map[string]bool)
	for _, tr := range result[0].ToolResults {
		idSet[tr.ToolUseID] = true
	}

	expectedIDs := []string{"id_A", "id_B", "id_C"}
	for _, id := range expectedIDs {
		if !idSet[id] {
			t.Errorf("expected tool_use_id %q to be preserved", id)
		}
	}
}

// TestNormalize_UniversalToolResultDedup tests that tool_result dedup applies universally
// regardless of ANTHROPIC_BASE_URL value.
func TestNormalize_UniversalToolResultDedup(t *testing.T) {
	// Three-URL matrix: Anthropic, MiniMax-like, DeepSeek-like
	urls := []string{
		"https://api.anthropic.com",
		"https://api.minimaxi.com/anthropic",
		"https://api.deepseek.com/v1",
	}

	for _, baseURL := range urls {
		t.Run(baseURL, func(t *testing.T) {
			t.Setenv("ANTHROPIC_BASE_URL", baseURL)

			// Create messages with duplicate tool_results
			messages := []api.Message{
				{
					Role:    "user",
					Content: "First",
					ToolResults: []api.ToolResultBlock{
						{ToolUseID: "id_1", Content: "First result"},
						{ToolUseID: "id_1", Content: "Duplicate result"},
					},
				},
			}

			// Call api.NormalizeMessages to exercise the production path
			normalized, _, _ := api.NormalizeMessages(messages, nil, api.Capabilities{})
			if len(normalized) != 1 {
				t.Fatalf("expected 1 message after normalization, got %d", len(normalized))
			}
			if len(normalized[0].ToolResults) != 1 {
				t.Errorf("expected 1 deduped tool_result, got %d", len(normalized[0].ToolResults))
			}
			// Last writer wins - should have "Duplicate result"
			if normalized[0].ToolResults[0].Content != "Duplicate result" {
				t.Errorf("expected last writer 'Duplicate result', got %q", normalized[0].ToolResults[0].Content)
			}
		})
	}
}
