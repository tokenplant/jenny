package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// TestNormalization_ToolResultFlattening_EdgeCases verifies the tool_result content
// flattening pass produces correct wire format for all edge cases (AC1-AC5).
// Run: go test ./internal/api/ -run "TestNormalization" -v -count=1
// Expected: 5 PASS results (all5 AC subtests pass).
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
