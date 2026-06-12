package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)

func TestDeepSeekToolResultFormat(t *testing.T) {
	mock := mockapi.NewMockServer()
	defer mock.Close()

	// Inspector that rejects array-formatted tool_results
	mock.SetRequestInspector(func(r mockapi.APIRequest) error {
		messages, ok := r.Body["messages"].([]any)
		if !ok {
			return nil
		}

		for i, msgEntry := range messages {
			msg, ok := msgEntry.(map[string]any)
			if !ok {
				continue
			}

			content, ok := msg["content"].([]any)
			if !ok {
				continue
			}

			for j, blockEntry := range content {
				block, ok := blockEntry.(map[string]any)
				if !ok {
					continue
				}

				if block["type"] == "tool_result" {
					trContent := block["content"]
					if _, isArray := trContent.([]any); isArray {
						return fmt.Errorf("messages.%d.content.%d: tool_result content must be a string, not an array", i, j)
					}
				}
			}
		}
		return nil
	})

	// Set a valid response for the cassette
	mock.SetInlineResponse("deepseek-test", `{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"Done"}],"model":"claude-3-sonnet-20240229","stop_reason":"end_turn"}`)
	mock.SetContentType("deepseek-test", "application/json")

	// Create provider pointing to mock server
	baseURL := mock.URL() + "/cassette/deepseek-test"
	t.Setenv("ANTHROPIC_BASE_URL", baseURL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	provider, err := newAnthropicProvider("claude-3-sonnet-20240229")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	provider.SetMaxTokensOverride(1000)

	// Message history with tool results
	messages := []Message{
		{
			Role:    "user",
			Content: "Hello",
		},
		{
			Role: "assistant",
			ToolUse: []ToolUseBlock{
				{
					ID:    "call_1",
					Name:  "test_tool",
					Input: map[string]any{"arg": "val"},
				},
			},
		},
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "call_1",
					Content:   "Tool output",
				},
			},
		},
	}

	// Send message
	_, err = provider.SendMessage(context.Background(), messages, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Verify IsError is preserved
	messages[2].ToolResults[0].IsError = true
	mock.SetRequestInspector(func(r mockapi.APIRequest) error {
		msgs, _ := r.Body["messages"].([]any)
		lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
		content, _ := lastMsg["content"].([]any)
		block, _ := content[0].(map[string]any)
		if block["type"] == "tool_result" {
			if block["is_error"] != true {
				return errors.New("expected is_error: true")
			}
		}
		return nil
	})
	_, err = provider.SendMessage(context.Background(), messages, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage with IsError failed: %v", err)
	}
}

func TestToolResultContentFlattenScope(t *testing.T) {
	mock := mockapi.NewMockServer()
	defer mock.Close()

	mock.SetRequestInspector(func(r mockapi.APIRequest) error {
		messages, ok := r.Body["messages"].([]any)
		if !ok {
			return nil
		}

		for _, msgEntry := range messages {
			msg, ok := msgEntry.(map[string]any)
			if !ok {
				continue
			}

			content, ok := msg["content"].([]any)
			if !ok {
				continue
			}

			for _, blockEntry := range content {
				block, ok := blockEntry.(map[string]any)
				if !ok {
					continue
				}

				// Tool use input must still be an object (not flattened)
				if block["type"] == "tool_use" {
					if _, isObject := block["input"].(map[string]any); !isObject {
						return errors.New("tool_use input must be an object")
					}
				}

				// Text content must still be a string (well, it's already a string in the SDK block)
				if block["type"] == "text" {
					if _, isString := block["text"].(string); !isString {
						return errors.New("text content must be a string")
					}
				}
			}
		}
		return nil
	})

	mock.SetInlineResponse("scope-test", `{"id":"msg_456","type":"message","role":"assistant","content":[{"type":"text","text":"Ok"}],"model":"claude-3-sonnet-20240229","stop_reason":"end_turn"}`)
	mock.SetContentType("scope-test", "application/json")

	baseURL := mock.URL() + "/cassette/scope-test"
	t.Setenv("ANTHROPIC_BASE_URL", baseURL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	provider, err := newAnthropicProvider("claude-3-sonnet-20240229")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}
	provider.SetMaxTokensOverride(1000)

	messages := []Message{
		{
			Role:    "user",
			Content: "Run tool",
		},
	}
	tools := []ToolParam{
		{
			Name: "test_tool",
			InputSchema: ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"arg": map[string]any{"type": "string"},
				},
			},
		},
	}

	_, err = provider.SendMessage(context.Background(), messages, tools, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
}
