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

// ---------------------------------------------------------------------------
// TestOpenAIProvider_SystemPrompt tests system prompt handling
// ---------------------------------------------------------------------------

func TestOpenAIProvider_SystemPrompt(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		json.Unmarshal(body, &req)

		// Check for system message
		messages, ok := req["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatal("expected messages")
		}

		firstMsg := messages[0].(map[string]any)
		if firstMsg["role"] != "system" {
			t.Errorf("expected first message to be system, got %v", firstMsg["role"])
		}
		if firstMsg["content"] != "You are a helpful assistant." {
			t.Errorf("expected system content, got %v", firstMsg["content"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"chatcmpl-123","object":"chat.completion","created":1677652288,"model":"gpt-5.4-nano","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-5.4-nano")

	client, _ := NewClientWithModel("")

	_, err := client.SendMessage(
		context.Background(),
		[]Message{{Role: "user", Content: "Hi"}},
		nil, nil,
		"You are a helpful assistant.",
		"",
	)
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_ChatStreamWithToolCalls tests streaming tool calls
// ---------------------------------------------------------------------------

func TestOpenAIProvider_ChatStreamWithToolCalls(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Send SSE chunks with tool call
		chunks := []string{
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-5.4-nano","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-5.4-nano","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\":"}}]},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-5.4-nano","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Boston\"}"}}]},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-5.4-nano","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n\n"))
			w.(http.Flusher).Flush()
		}
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-5.4-nano")

	client, _ := NewClientWithModel("")

	tools := []ToolParam{
		{
			Name:        "get_weather",
			Description: "Get weather",
			InputSchema: ToolInputSchema{Type: "object", Properties: map[string]any{"location": map[string]any{"type": "string"}}, Required: []string{"location"}},
		},
	}

	blocksChan, result := client.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "Weather?"}},
		tools, nil, "", "",
		30*time.Second,
		30*time.Second,
		nil,
	)

	var blocks []StreamContentBlock
	for block := range blocksChan {
		blocks = append(blocks, block)
	}

	if result.StopReason != StopReasonToolUse {
		t.Errorf("expected stop reason 'tool_use', got %q", result.StopReason)
	}

	// Check for tool_use block in result
	foundToolUse := false
	for _, block := range result.Blocks {
		if block.Type == "tool_use" && block.ToolName == "get_weather" {
			foundToolUse = true
			if block.ToolInput["location"] != "Boston" {
				t.Errorf("expected location 'Boston', got %v", block.ToolInput["location"])
			}
		}
	}
	if !foundToolUse {
		t.Error("expected tool_use block in result")
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_ReasoningContent tests non-streaming thinking block
// ---------------------------------------------------------------------------

func TestOpenAIProvider_ReasoningContent(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chatcmpl-123",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "o3-mini",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"reasoning_content": "Let me think about this carefully.",
					"content": "The answer is 42."
				},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 20}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")

	client, _ := NewClientWithModel("")
	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "What is the answer?"}}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "thinking" {
		t.Errorf("expected first block type 'thinking', got %q", resp.Content[0].Type)
	}
	if resp.Content[0].Thinking != "Let me think about this carefully." {
		t.Errorf("expected thinking text, got %q", resp.Content[0].Thinking)
	}
	if resp.Content[1].Type != "text" {
		t.Errorf("expected second block type 'text', got %q", resp.Content[1].Type)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_CachedTokensNonStreaming tests non-streaming cache
// ---------------------------------------------------------------------------

func TestOpenAIProvider_CachedTokensNonStreaming(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chatcmpl-123",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "gpt-5.4-nano",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello"},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 200,
				"completion_tokens": 10,
				"prompt_tokens_details": {"cached_tokens": 150}
			}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-5.4-nano")

	client, _ := NewClientWithModel("")
	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	if resp.Usage.CacheReadInputTokens != 150 {
		t.Errorf("expected CacheReadInputTokens 150, got %d", resp.Usage.CacheReadInputTokens)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_StreamingReasoningContent tests streaming thinking
// ---------------------------------------------------------------------------

func TestOpenAIProvider_StreamingReasoningContent(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"o3-mini","choices":[{"index":0,"delta":{"reasoning_content":"Let me "},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"o3-mini","choices":[{"index":0,"delta":{"reasoning_content":"think."},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"o3-mini","choices":[{"index":0,"delta":{"content":"The answer."},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"o3-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n\n"))
			w.(http.Flusher).Flush()
		}
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")

	client, _ := NewClientWithModel("")

	blocksChan, result := client.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "What is the answer?"}},
		nil, nil, "", "",
		30*time.Second,
		30*time.Second,
		nil,
	)

	// Collect channel blocks - must see incremental thinking events
	var channelBlocks []StreamContentBlock
	for block := range blocksChan {
		channelBlocks = append(channelBlocks, block)
	}

	// Verify at least one thinking block was emitted incrementally during streaming
	var thinkingEmitted bool
	for _, b := range channelBlocks {
		if b.Block.Type == "thinking" {
			thinkingEmitted = true
		}
	}
	if !thinkingEmitted {
		t.Error("expected thinking block to be emitted during streaming")
	}

	// Final blocks should include thinking then text
	var thinkingBlock, textBlock *ContentBlock
	for i := range result.Blocks {
		b := &result.Blocks[i]
		if b.Type == "thinking" {
			thinkingBlock = b
		} else if b.Type == "text" {
			textBlock = b
		}
	}

	if thinkingBlock == nil {
		t.Fatal("expected thinking block in result")
	}
	if thinkingBlock.Thinking != "Let me think." {
		t.Errorf("expected accumulated thinking 'Let me think.', got %q", thinkingBlock.Thinking)
	}
	if textBlock == nil {
		t.Fatal("expected text block in result")
	}

	// Verify thinking comes before text in result.Blocks
	thinkingIdx, textIdx := -1, -1
	for i, b := range result.Blocks {
		if b.Type == "thinking" {
			thinkingIdx = i
		} else if b.Type == "text" {
			textIdx = i
		}
	}
	if thinkingIdx >= textIdx {
		t.Error("expected thinking block before text block")
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_StreamingCachedTokens tests streaming cache tokens
// ---------------------------------------------------------------------------

func TestOpenAIProvider_StreamingCachedTokens(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-5.4-nano","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-5.4-nano","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":50}}}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n\n"))
			w.(http.Flusher).Flush()
		}
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "gpt-5.4-nano")

	client, _ := NewClientWithModel("")

	blocksChan, result := client.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "Hi"}},
		nil, nil, "", "",
		30*time.Second,
		30*time.Second,
		nil,
	)

	for range blocksChan {
	}

	if result.Usage.CacheReadInputTokens != 50 {
		t.Errorf("expected CacheReadInputTokens 50, got %d", result.Usage.CacheReadInputTokens)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIProvider_ReasoningContentRoundTrip tests AC4: reasoning_content
// round-trip for multi-turn OpenAI Chat conversations
// ---------------------------------------------------------------------------

func TestOpenAIProvider_ReasoningContentRoundTrip(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify reasoning_content is present on assistant message
		messages, ok := req["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatal("expected messages in request")
		}

		// Find assistant message with tool_calls
		var foundReasoningContent bool
		for _, msg := range messages {
			m := msg.(map[string]any)
			if m["role"] == "assistant" {
				if rc, ok := m["reasoning_content"].(string); ok && rc == "Previous thinking process..." {
					foundReasoningContent = true
				}
				// Also verify tool_calls are present alongside reasoning_content
				if _, ok := m["tool_calls"].([]any); ok {
					if _, ok := m["reasoning_content"]; !ok {
						t.Error("expected reasoning_content on assistant message with tool_calls")
					}
				}
			}
		}

		if !foundReasoningContent {
			t.Error("expected reasoning_content 'Previous thinking process...' in assistant message")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chatcmpl-126",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "o3-mini",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Based on my reasoning, the answer is clear."
				},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 20, "completion_tokens": 10}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")

	client, _ := NewClientWithModel("")

	// Simulate a resumed conversation with thinking from previous turn
	messages := []Message{
		{Role: "user", Content: "What's the answer?"},
		{Role: "assistant", Content: "I need to analyze this",
			Thinking: "Previous thinking process...",
			ToolUse:  []ToolUseBlock{{ID: "call_prev", Name: "analyze", Input: map[string]any{"data": "test"}}}},
	}
	toolResults := []ToolResult{
		{ToolUseID: "call_prev", Content: "Analysis complete"},
	}

	resp, err := client.SendMessage(context.Background(), messages, nil, toolResults, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("expected stop reason 'end_turn', got %q", resp.StopReason)
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIResponsesProvider_Basic tests basic Responses API response
// AC1: Responses API selection via OPENAI_WIRE_API=responses
// ---------------------------------------------------------------------------

func TestOpenAIResponsesProvider_Basic(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		if req["model"] != "o3-mini" {
			t.Errorf("expected model 'o3-mini', got %v", req["model"])
		}

		if req["input"] == nil {
			t.Error("expected input in request")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "o3-mini",
			"output": [
				{
					"id": "msg_1",
					"type": "message",
					"role": "assistant",
					"content": [
						{"type": "output_text", "text": "The answer is 42."}
					]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "What is the answer?"}}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("expected stop reason 'end_turn', got %q", resp.StopReason)
	}

	if len(resp.Content) != 1 || resp.Content[0].Type != "text" {
		t.Fatalf("expected 1 text block, got %v", resp.Content)
	}

	if resp.Content[0].Text != "The answer is 42." {
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
// TestOpenAIResponsesProvider_ReasoningEffort tests reasoning effort config
// AC2: --effort flag maps to reasoning_config.effort
// ---------------------------------------------------------------------------

func TestOpenAIResponsesProvider_ReasoningEffort(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify reasoning_config.effort is present
		reasoningConfig, ok := req["reasoning_config"].(map[string]any)
		if !ok {
			t.Fatal("expected reasoning_config in request")
		}
		if reasoningConfig["effort"] != "high" {
			t.Errorf("expected reasoning_config.effort 'high', got %v", reasoningConfig["effort"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_123",
			"model": "o3-mini",
			"output": [
				{
					"id": "reasoning_1",
					"type": "reasoning",
					"summary": {"type": "summary", "text": "I need to think about this..."}
				},
				{
					"id": "msg_1",
					"type": "message",
					"role": "assistant",
					"content": [{"type": "output_text", "text": "The answer is 42."}]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Set thinking config with effort
	if setter, ok := client.provider.(interface{ SetThinkingConfig(ThinkingConfig) }); ok {
		setter.SetThinkingConfig(ThinkingConfig{Effort: "high"})
	}

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "What is the answer?"}}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	// Verify thinking block is extracted
	var foundThinking bool
	for _, block := range resp.Content {
		if block.Type == "thinking" && block.Thinking == "I need to think about this..." {
			foundThinking = true
		}
	}
	if !foundThinking {
		t.Error("expected thinking block with reasoning summary")
	}
}

// ---------------------------------------------------------------------------
// TestOpenAIResponsesProvider_ToolCalls tests Responses API with tools
// AC1: Responses API handles tool calls correctly
// ---------------------------------------------------------------------------

func TestOpenAIResponsesProvider_ToolCalls(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		json.Unmarshal(body, &req)

		// Verify tools are present
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "resp_124",
			"model": "o3-mini",
			"output": [
				{
					"id": "msg_1",
					"type": "message",
					"role": "assistant",
					"content": [
						{
							"type": "function_call",
							"id": "call_abc123",
							"name": "get_weather",
							"arguments": "{\"location\":\"San Francisco\"}"
						}
					]
				}
			],
			"usage": {"input_tokens": 10, "output_tokens": 20}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")
	t.Setenv("OPENAI_WIRE_API", "responses")

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
// TestOpenAIProvider_ReasoningEffortChat tests reasoning_effort in Chat API
// BLK1: Effort flag wired to Chat API provider
// ---------------------------------------------------------------------------

func TestOpenAIProvider_ReasoningEffortChat(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to parse request: %v", err)
		}

		// Verify reasoning_effort is present
		if req["reasoning_effort"] != "high" {
			t.Errorf("expected reasoning_effort 'high', got %v", req["reasoning_effort"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"id": "chatcmpl-125",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "o3-mini",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "The answer is 42."
				},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 15}
		}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_DEFAULT_MODEL", "o3-mini")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}

	// Set thinking config with effort on Chat API provider
	if setter, ok := client.provider.(interface{ SetThinkingConfig(ThinkingConfig) }); ok {
		setter.SetThinkingConfig(ThinkingConfig{Effort: "high"})
	}

	resp, err := client.SendMessage(context.Background(), []Message{{Role: "user", Content: "What is the answer?"}}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("expected stop reason 'end_turn', got %q", resp.StopReason)
	}
}
