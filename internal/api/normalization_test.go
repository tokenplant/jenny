package api

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// TestNormalization_ToolResultFlattening_EdgeCases verifies the tool_result content
// flattening pass produces correct wire format for all edge cases (AC1-AC5).
// Run: go test ./internal/api/ -run "TestNormalization" -v -count=1
// Expected: 5 PASS results (all 5 AC subtests pass).
//
// Prior fixes:
// - 95f5153: flatten tool_result content for DeepSeek compatibility
// - 4e84e9c: add comprehensive edge-case tests
// - 514fb98: add t.Cleanup to clear request inspector after AC4
// - decdb7c: use LIFO indexing (reqs[len(reqs)-1]) instead of FIFO (reqs[0])
func TestNormalization_ToolResultFlattening_EdgeCases(t *testing.T) {
	mock := mockapi.NewMockServer()
	defer mock.Close()

	// Helper to create provider
	setupProvider := func(t *testing.T, cassetteID string) (*anthropicProvider, string) {
		baseURL := mock.URL() + "/cassette/" + cassetteID
		t.Setenv("ANTHROPIC_BASE_URL", baseURL)
		t.Setenv("ANTHROPIC_API_KEY", "test-key")
		provider, err := newAnthropicProvider("claude-3-sonnet-20240229")
		if err != nil {
			t.Fatalf("Failed to create provider: %v", err)
		}
		provider.SetMaxTokensOverride(1000)
		return provider, baseURL
	}

	mock.SetContentType("test", "application/json")
	mock.SetInlineResponse("test", `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"model":"claude-3-sonnet-20240229","stop_reason":"end_turn"}`)

	t.Run("AC1: Empty tool_result content serializes as empty string", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		messages := []Message{
			{
				Role: "user",
				ToolResults: []ToolResultBlock{
					{
						ToolUseID: "call_1",
						Content:   "",
					},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, "", "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		reqs := mock.Requests()
		if len(reqs) == 0 {
			t.Fatal("No requests captured")
		}

		// Verify content is a string ""
		found := false
		msgs, _ := reqs[len(reqs)-1].Body["messages"].([]any)
		for _, m := range msgs {
			msg, _ := m.(map[string]any)
			content, _ := msg["content"].([]any)
			for _, b := range content {
				block, _ := b.(map[string]any)
				if block["type"] == "tool_result" {
					found = true
					trContent, isString := block["content"].(string)
					if !isString {
						t.Errorf("tool_result content is not a string: %v", block["content"])
					}
					if trContent != "" {
						t.Errorf("expected empty string content, got %q", trContent)
					}
				}
			}
		}
		if !found {
			t.Error("tool_result block not found in request")
		}
	})

	t.Run("AC2: Multiple tool_results in one user message", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		messages := []Message{
			{
				Role: "user",
				ToolResults: []ToolResultBlock{
					{ToolUseID: "call_1", Content: "Output A"},
					{ToolUseID: "call_2", Content: "Output B"},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, "", "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		reqs := mock.Requests()
		msgs, _ := reqs[len(reqs)-1].Body["messages"].([]any)
		lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
		content, _ := lastMsg["content"].([]any)

		count := 0
		for _, b := range content {
			block, _ := b.(map[string]any)
			if block["type"] == "tool_result" {
				count++
				if _, isString := block["content"].(string); !isString {
					t.Errorf("block %d: tool_result content is not a string", count)
				}
			}
		}
		if count != 2 {
			t.Errorf("expected 2 tool_result blocks, got %d", count)
		}
	})

	t.Run("AC3: Error tool_result preserves is_error: true", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		messages := []Message{
			{
				Role: "user",
				ToolResults: []ToolResultBlock{
					{
						ToolUseID: "call_1",
						Content:   "error details",
						IsError:   true,
					},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, "", "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		reqs := mock.Requests()
		msgs, _ := reqs[len(reqs)-1].Body["messages"].([]any)
		lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
		content, _ := lastMsg["content"].([]any)
		block, _ := content[0].(map[string]any)

		if block["is_error"] != true {
			t.Error("expected is_error: true in tool_result block")
		}
		if block["content"] != "error details" {
			t.Errorf("expected content 'error details', got %v", block["content"])
		}
	})

	t.Run("AC4: Mock server rejects array content, test passes", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		// Inspector that rejects array-formatted tool_results
		mock.SetRequestInspector(func(r mockapi.APIRequest) error {
			msgs, _ := r.Body["messages"].([]any)
			for _, m := range msgs {
				msg, _ := m.(map[string]any)
				content, _ := msg["content"].([]any)
				for _, b := range content {
					block, _ := b.(map[string]any)
					if block["type"] == "tool_result" {
						if _, isArray := block["content"].([]any); isArray {
							return fmt.Errorf("REJECTED: tool_result content is an array")
						}
					}
				}
			}
			return nil
		})
		t.Cleanup(func() { mock.SetRequestInspector(nil) })

		messages := []Message{
			{
				Role: "user",
				ToolResults: []ToolResultBlock{
					{ToolUseID: "call_1", Content: "some output"},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, "", "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v (flattening might be broken)", err)
		}
		// If SendMessage succeeds, the inspector didn't return an error, meaning content was NOT an array.
	})

	t.Run("AC5: Non-tool_result blocks untouched", func(t *testing.T) {
		provider, _ := setupProvider(t, "test")

		messages := []Message{
			{
				Role:    "user",
				Content: "Hello",
				ToolUse: []ToolUseBlock{
					{
						ID:    "call_1",
						Name:  "test_tool",
						Input: map[string]any{"key": "value"},
					},
				},
				ToolResults: []ToolResultBlock{
					{ToolUseID: "call_0", Content: "prev output"},
				},
			},
		}

		_, err := provider.SendMessage(context.Background(), messages, nil, nil, "", "")
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		reqs := mock.Requests()
		msgs, _ := reqs[len(reqs)-1].Body["messages"].([]any)
		lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
		content, _ := lastMsg["content"].([]any)

		hasText := false
		hasToolUse := false
		hasToolResult := false

		for _, b := range content {
			block, _ := b.(map[string]any)
			switch block["type"] {
			case "text":
				hasText = true
				if _, isString := block["text"].(string); !isString {
					t.Error("text block 'text' field is not a string")
				}
			case "tool_use":
				hasToolUse = true
				if _, isMap := block["input"].(map[string]any); !isMap {
					t.Errorf("tool_use block 'input' field is not a map: %T", block["input"])
				}
			case "tool_result":
				hasToolResult = true
				if _, isString := block["content"].(string); !isString {
					t.Error("tool_result block 'content' field is not a string")
				}
			}
		}

		if !hasText || !hasToolUse || !hasToolResult {
			t.Errorf("missing blocks: text=%v, tool_use=%v, tool_result=%v", hasText, hasToolUse, hasToolResult)
		}
	})
}

// TestNormalization_CredentialBoundArtifactStripping verifies that redacted_thinking
// blocks are stripped from message history when a session is resumed with a different
// API key (SSNF Pass 2.D).
// Run: go test ./internal/api/ -run "TestNormalization" -v -count=1
// Expected: 5 PASS results (all 5 AC subtests pass).
func TestNormalization_CredentialBoundArtifactStripping(t *testing.T) {
	findSubstring := func(s, substr string) bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}

	containsString := func(s, substr string) bool {
		return len(s) >= len(substr) && findSubstring(s, substr)
	}

	containsRedactedThinking := func(content string) bool {
		return len(content) > 0 && (containsString(content, `<thinking type="redacted">`) || containsString(content, `"type":"redacted_thinking"`))
	}

	t.Run("AC1: Single redacted_thinking block stripped", func(t *testing.T) {
		// Set ANTHROPIC_API_KEY to "current-key" to simulate the current environment
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "current-key")

		// OriginalAPIKey is different, so stripping should occur
		caps := Capabilities{OriginalAPIKey: "original-key"}

		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_DATA_12345</thinking>`,
			},
		}

		normalized, _, logs := NormalizeMessages(messages, nil, caps)

		// Verify the redacted_thinking block was stripped
		if normalized[0].Content != "" {
			t.Errorf("redacted_thinking block should have been stripped, but content is: %s", normalized[0].Content)
		}

		// Verify NormalizationLog entry exists
		foundLog := false
		for _, log := range logs {
			if log.Pass == "StripCredentialBoundArtifacts" {
				foundLog = true
				break
			}
		}
		if !foundLog {
			t.Error("expected NormalizationLog entry for StripCredentialBoundArtifacts")
		}
	})

	t.Run("AC2: Multiple messages with redacted_thinking blocks stripped", func(t *testing.T) {
		// Set ANTHROPIC_API_KEY to "current-key" to simulate the current environment
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "current-key")

		caps := Capabilities{OriginalAPIKey: "original-key"}

		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_1</thinking>`,
				ToolUse: []ToolUseBlock{
					{ID: "call_1", Name: "tool1", Input: map[string]any{}},
				},
			},
			{
				Role:    "user",
				Content: "test",
				ToolResults: []ToolResultBlock{
					{ToolUseID: "call_1", Content: "result"},
				},
			},
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_2</thinking><thinking type="redacted">SIG_3</thinking>`,
			},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, caps)

		// First message: tool_use preserved, content stripped
		if normalized[0].Content != "" {
			t.Errorf("first message content should be empty after stripping, got: %s", normalized[0].Content)
		}
		if len(normalized[0].ToolUse) != 1 {
			t.Errorf("first message tool_use should be preserved, got %d", len(normalized[0].ToolUse))
		}

		// Second message (user): content preserved as-is
		if normalized[1].Content != "test" {
			t.Errorf("second message content should be preserved, got: %s", normalized[1].Content)
		}

		// Third message: all redacted blocks stripped, content empty
		if normalized[2].Content != "" {
			t.Errorf("third message content should be empty after stripping, got: %s", normalized[2].Content)
		}
	})

	t.Run("AC3: Non-redacted thinking preserved", func(t *testing.T) {
		// Set ANTHROPIC_API_KEY to "current-key" to simulate the current environment
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "current-key")

		caps := Capabilities{OriginalAPIKey: "original-key"}

		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking>valid chain of thought</thinking><thinking type="redacted">SIG</thinking>`,
			},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, caps)

		// Non-redacted thinking should be preserved
		if !containsString(normalized[0].Content, `<thinking>valid chain of thought</thinking>`) {
			t.Errorf("non-redacted thinking should be preserved, but content is: %s", normalized[0].Content)
		}

		// Redacted thinking should be stripped
		if containsRedactedThinking(normalized[0].Content) {
			t.Errorf("redacted_thinking should be stripped, but found in: %s", normalized[0].Content)
		}
	})

	t.Run("AC4: Stripping inactive when key matches", func(t *testing.T) {
		// Set ANTHROPIC_API_KEY to "test-key" to simulate the current environment
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "test-key")

		// When OriginalAPIKey matches current ANTHROPIC_API_KEY, stripping should be skipped
		caps := Capabilities{OriginalAPIKey: "test-key"}

		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_DATA</thinking>`,
			},
		}

		normalized, _, _ := NormalizeMessages(messages, nil, caps)

		// When keys match, redacted_thinking should be preserved
		if !containsRedactedThinking(normalized[0].Content) {
			t.Errorf("redacted_thinking should be preserved when keys match, but content is: %s", normalized[0].Content)
		}
	})

	t.Run("AC5: NormalizationLog entry on strip", func(t *testing.T) {
		// Test that NormalizationLog is properly populated
		messages := []Message{
			{
				Role:    "assistant",
				Content: `<thinking type="redacted">SIG_DATA</thinking><thinking type="redacted">SIG_DATA_2</thinking>`,
			},
		}

		// Save and restore original env
		origKey := os.Getenv("ANTHROPIC_API_KEY")
		defer os.Setenv("ANTHROPIC_API_KEY", origKey)
		os.Setenv("ANTHROPIC_API_KEY", "current-key")

		caps := Capabilities{OriginalAPIKey: "original-key"}
		_, _, logs := NormalizeMessages(messages, nil, caps)

		// Verify log entry
		foundLog := false
		for _, log := range logs {
			if log.Pass == "StripCredentialBoundArtifacts" {
				foundLog = true
				// Should mention 2 blocks stripped
				if !containsString(log.Message, "2") {
					t.Errorf("log message should mention 2 blocks stripped: %s", log.Message)
				}
				break
			}
		}
		if !foundLog {
			t.Error("expected NormalizationLog entry for StripCredentialBoundArtifacts")
		}
	})
}
