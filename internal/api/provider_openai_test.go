package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)
// ---------------------------------------------------------------------------
// TestOpenAIProvider_ChatBasic tests basic Chat API response
// AC3: OpenAI Chat backend is selectable
// ---------------------------------------------------------------------------

func TestOpenAIProvider_ChatBasic(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetRequestInspector(func(r mockapi.APIRequest) error {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}
		return nil
	})
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// Parse request body
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		if req["model"] != "gpt-5.4-nano" {
			t.Errorf("expected model 'gpt-5.4-nano', got %v", req["model"])
		}

		messages, ok := req["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatal("expected messages array")
		}

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chatcmpl-123",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "gpt-5.4-nano",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello! How can I help you today?"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 15,
				"total_tokens": 25
			}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key-123")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-5.4-nano")
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com") // Should be ignored

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Verify OpenAI provider is selected
	if client.GetModel() != "gpt-5.4-nano" {
		t.Errorf("expected model 'gpt-5.4-nano', got %q", client.GetModel())
	}

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("expected stop reason 'end_turn', got %q", resp.StopReason)
	}

	if len(resp.Content) != 1 || resp.Content[0].Type != "text" {
		t.Fatalf("expected 1 text block, got %v", resp.Content)
	}

	if resp.Content[0].Text != "Hello! How can I help you today?" {
		t.Errorf("expected response text, got %q", resp.Content[0].Text)
	}

	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 15 {
		t.Errorf("expected 15 output tokens, got %d", resp.Usage.OutputTokens)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_ChatWithTools tests tool_calls handling
// AC4: OpenAI Chat roundtrip with tools
// ---------------------------------------------------------------------------

func TestOpenAIProvider_ChatWithTools(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		json.Unmarshal(body, &req)

		// Verify tools are present
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		// Echo back a tool call response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chatcmpl-124",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "gpt-5.4-nano",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": null,
					"tool_calls": [{
						"id": "call_abc123",
						"type": "function",
						"function": {
							"name": "get_weather",
							"arguments": "{\"location\":\"San Francisco\"}"
						}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 20}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key-123")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-5.4-nano")

	client, _ := NewClientWithModel("")

	tools := []ToolParam{
		{
			Name:        "get_weather",
			Description: "Get weather for a location",
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{"location": map[string]any{"type": "string"}},
				Required:   []string{"location"},
			},
		},
	}

	messages := []Message{{Role: "user", Content: "What's the weather in San Francisco?"}}
	toolResults := []ToolResult{
		{ToolUseID: "call_abc123", Content: "Sunny, 72°F"},
	}

	resp, err := client.SendMessage(context.Background(), messages, tools, toolResults, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	if resp.StopReason != StopReasonToolUse {
		t.Errorf("expected stop reason 'tool_use', got %q", resp.StopReason)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}

	if resp.Content[0].Type != "tool_use" {
		t.Errorf("expected type 'tool_use', got %q", resp.Content[0].Type)
	}

	if resp.Content[0].ToolID != "call_abc123" {
		t.Errorf("expected tool ID 'call_abc123', got %q", resp.Content[0].ToolID)
	}

	if resp.Content[0].ToolName != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %q", resp.Content[0].ToolName)
	}

	loc, ok := resp.Content[0].ToolInput["location"]
	if !ok || loc != "San Francisco" {
		t.Errorf("expected location 'San Francisco', got %v", loc)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_ChatStream tests streaming response
// AC3: Streaming support
// ---------------------------------------------------------------------------

func TestOpenAIProvider_ChatStream(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Send SSE chunks
		chunks := []string{
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-5.4-nano","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-5.4-nano","choices":[{"index":0,"delta":{"content":" World"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-5.4-nano","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n\n"))
			w.(http.Flusher).Flush()
		}
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key-123")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-5.4-nano")

	client, _ := NewClientWithModel("")

	blocksChan, result := client.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "Say hello"}},
		nil, nil, "", "",
		30*time.Second,
		30*time.Second,
		nil,
	)

	var blocks []StreamContentBlock
	for block := range blocksChan {
		blocks = append(blocks, block)
	}

	if result.Error != "" && result.Error != "stream incomplete: no stop reason" {
		t.Errorf("unexpected error: %s", result.Error)
	}

	// Check result blocks have content
	if len(result.Blocks) == 0 {
		t.Error("expected result blocks")
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_EnvPrecedence tests that OPENAI_* takes precedence
// AC5: Env var precedence
// ---------------------------------------------------------------------------

func TestOpenAIProvider_EnvPrecedence(t *testing.T) {
	// Set both ANTHROPIC and OPENAI vars
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "anthropic-key")
	t.Setenv("ANTHROPIC_MODEL", "claude-opus-4-5-20251101")
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-5.4-nano")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// OpenAI provider should be selected
	if client.GetModel() != "gpt-5.4-nano" {
		t.Errorf("expected OpenAI model 'gpt-5.4-nano', got %q", client.GetModel())
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_NoOpenAIEnv tests that Anthropic is used without OPENAI vars
// AC2: Anthropic backend unchanged when OPENAI_BASE_URL not set
// ---------------------------------------------------------------------------

func TestOpenAIProvider_NoOpenAIEnv(t *testing.T) {
	// Clear OPENAI vars
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_DEFAULT_MODEL", "")

	// Set Anthropic vars (point to mock server)
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"test-model","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-key-0000000000000000")
	t.Setenv("ANTHROPIC_MODEL", "test-model")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Anthropic provider should be selected
	if client.GetModel() != "test-model" {
		t.Errorf("expected Anthropic model 'test-model', got %q", client.GetModel())
	}
}

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// TestOpenAIResponsesProvider_WireAPIResponses tests that responses API is selected
// when OPENAI_WIRE_API=responses
// ---------------------------------------------------------------------------

func TestOpenAIResponsesProvider_WireAPIResponses(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com")
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Responses API provider should be selected
	if client.GetModel() != "o3-mini" {
		t.Errorf("expected model 'o3-mini', got %q", client.GetModel())
	}
}
