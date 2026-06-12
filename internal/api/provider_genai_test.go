package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Hermetic GenAI test server.
// ---------------------------------------------------------------------------

type testGenAIServer struct {
	t       *testing.T
	handler func(w http.ResponseWriter, r *http.Request)
	server  *httptest.Server
	mu      sync.Mutex
	gotURLs []string
}

// newTestGenAIServer creates a test server for genai.
func newTestGenAIServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *testGenAIServer {
	t.Helper()
	s := &testGenAIServer{t: t, handler: handler}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.gotURLs = append(s.gotURLs, r.URL.String())
		s.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *testGenAIServer) URL() string { return s.server.URL }

func (s *testGenAIServer) URLs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]string, len(s.gotURLs))
	copy(cp, s.gotURLs)
	return cp
}

func (s *testGenAIServer) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.gotURLs)
}

// newGenAIProviderForTest creates a genaiProvider that routes all traffic to the test server.
func newGenAIProviderForTest(s *testGenAIServer) *genaiProvider {
	t.Setenv("GENAI_BASE_URL", s.URL())
	t.Setenv("GENAI_API_KEY", "test-key")

	return &genaiProvider{
		client:    NewHTTPClient(30 * time.Second),
		model:     "gemini-2.5-flash",
		maxTokens: 64000,
	}
}

// Global t for use in helpers
var t *testing.T

func setT(testT *testing.T) { t = testT }

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeSSE(w http.ResponseWriter, chunks []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		panic("test server does not support Flusher")
	}
	var buf bytes.Buffer
	for _, c := range chunks {
		buf.WriteString(c)
		buf.WriteString("\n\n")
	}
	w.Write(buf.Bytes())
	flusher.Flush()
}

func readBodyJSON(r *http.Request) map[string]any {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	var v map[string]any
	json.Unmarshal(body, &v)
	return v
}

// ---------------------------------------------------------------------------
// AC1: genaiProvider implements Provider
// ---------------------------------------------------------------------------

func TestGenAIProvider_ImplementsProvider(t *testing.T) {
	setT(t)
	p := newGenAIProviderForTest(newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var _ Provider = p
	if p.Kind() != ProviderGenAI {
		t.Errorf("expected kind genai, got %q", p.Kind())
	}
	if p.GetModel() != "gemini-2.5-flash" {
		t.Errorf("expected model gemini-2.5-flash, got %q", p.GetModel())
	}
	p.SetModel("gemini-2.0-flash")
	if p.GetModel() != "gemini-2.0-flash" {
		t.Errorf("SetModel did not stick")
	}
}

// ---------------------------------------------------------------------------
// AC2: non-streaming SendMessage translates messages, tools, and system prompt
// ---------------------------------------------------------------------------

func TestGenAIProvider_SendMessage_Basic(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBodyJSON(r)

		if !strings.Contains(r.URL.Path, "generateContent") {
			t.Errorf("expected generateContent path, got %s", r.URL.Path)
		}

		// Verify system instruction
		sys, ok := body["systemInstruction"].(map[string]any)
		if !ok {
			t.Fatal("expected systemInstruction")
		}
		parts, _ := sys["parts"].([]any)
		if len(parts) == 0 || parts[0].(map[string]any)["text"] != "You are helpful." {
			t.Errorf("bad system prompt, got %v", parts)
		}

		writeJSON(w, 200, map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{"role": "model", "parts": []map[string]any{{"text": "Hi there!"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{
				"promptTokenCount":     7,
				"candidatesTokenCount": 5,
				"thoughtsTokenCount":   3,
			},
			"modelVersion": "gemini-2.5-flash",
		})
	})

	p := newGenAIProviderForTest(s)
	resp, err := p.SendMessage(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, nil, "You are helpful.", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("expected end_turn, got %q", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hi there!" {
		t.Errorf("unexpected content: %+v", resp.Content)
	}
	if resp.Usage.InputTokens != 7 {
		t.Errorf("expected 7 input tokens, got %d", resp.Usage.InputTokens)
	}
	// ThoughtsTokenCount must be folded into OutputTokens.
	if resp.Usage.OutputTokens != 8 {
		t.Errorf("expected 8 output tokens (5+3 thoughts), got %d", resp.Usage.OutputTokens)
	}
	if s.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", s.CallCount())
	}
}

// ---------------------------------------------------------------------------
// AC3: system prompt suffix is concatenated with the main system prompt
// ---------------------------------------------------------------------------

func TestGenAIProvider_SendMessage_SystemPromptSuffix(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBodyJSON(r)
		sys := body["systemInstruction"].(map[string]any)
		got := sys["parts"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(got, "primary") || !strings.Contains(got, "suffix") {
			t.Errorf("expected concatenated system prompt, got %q", got)
		}
		writeJSON(w, 200, map[string]any{
			"candidates": []map[string]any{{
				"content":      map[string]any{"role": "model", "parts": []map[string]any{{"text": "ok"}}},
				"finishReason": "STOP",
			}},
		})
	})

	p := newGenAIProviderForTest(s)
	_, err := p.SendMessage(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, nil, "primary", "suffix")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// AC4: tools are translated to genai function declarations
// ---------------------------------------------------------------------------

func TestGenAIProvider_SendMessage_Tools(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBodyJSON(r)

		tools, _ := body["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool group, got %d", len(tools))
		}
		first := tools[0].(map[string]any)
		funcs, _ := first["functionDeclarations"].([]any)
		if len(funcs) != 1 {
			t.Fatalf("expected 1 functionDeclaration, got %d", len(funcs))
		}
		fd := funcs[0].(map[string]any)
		if fd["name"] != "get_weather" {
			t.Errorf("expected get_weather, got %v", fd["name"])
		}

		writeJSON(w, 200, map[string]any{
			"candidates": []map[string]any{{
				"content": map[string]any{
					"role": "model",
					"parts": []map[string]any{{
						"functionCall": map[string]any{
							"name": "get_weather", "args": map[string]any{"location": "Tokyo"},
						},
					}},
				},
				"finishReason": "STOP",
			}},
		})
	})

	p := newGenAIProviderForTest(s)
	tools := []ToolParam{{
		Name:        "get_weather",
		Description: "weather lookup",
		InputSchema: ToolInputSchema{Type: "object", Properties: map[string]any{"location": map[string]any{"type": "string"}}, Required: []string{"location"}},
	}}
	resp, err := p.SendMessage(context.Background(), []Message{{Role: "user", Content: "weather?"}}, tools, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Errorf("expected end_turn, got %q", resp.StopReason)
	}
	found := false
	for _, b := range resp.Content {
		if b.Type == "tool_use" && b.ToolName == "get_weather" {
			found = true
			if b.ToolInput["location"] != "Tokyo" {
				t.Errorf("expected Tokyo, got %v", b.ToolInput["location"])
			}
		}
	}
	if !found {
		t.Errorf("expected tool_use block, got %+v", resp.Content)
	}
}

// ---------------------------------------------------------------------------
// AC5: tool results
// ---------------------------------------------------------------------------

func TestGenAIProvider_SendMessage_ToolResults(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBodyJSON(r)
		contents, _ := body["contents"].([]any)

		// Expect: model turn (tool_call) + user turn (function_response)
		if len(contents) != 2 {
			t.Fatalf("expected 2 turns, got %d", len(contents))
		}
		userTurn := contents[1].(map[string]any)
		parts, _ := userTurn["parts"].([]any)
		if len(parts) != 1 {
			t.Fatalf("expected 1 part in user turn, got %d", len(parts))
		}
		part := parts[0].(map[string]any)
		if _, ok := part["functionResponse"].(map[string]any); !ok {
			t.Fatalf("expected functionResponse, got %+v", part)
		}
		writeJSON(w, 200, map[string]any{
			"candidates": []map[string]any{{
				"content":      map[string]any{"role": "model", "parts": []map[string]any{{"text": "done"}}},
				"finishReason": "STOP",
			}},
		})
	})

	p := newGenAIProviderForTest(s)
	messages := []Message{
		{Role: "assistant", ToolUse: []ToolUseBlock{{ID: "call-1", Name: "get_weather", Input: map[string]any{"location": "Tokyo"}}}},
		{Role: "user", ToolResults: []ToolResultBlock{{ToolUseID: "call-1", Content: "Sunny"}}},
	}
	_, err := p.SendMessage(context.Background(), messages, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// AC6: max_tokens
// ---------------------------------------------------------------------------

func TestGenAIProvider_SendMessage_MaxTokens(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"candidates": []map[string]any{{
				"content":      map[string]any{"role": "model", "parts": []map[string]any{{"text": "..."}}},
				"finishReason": "MAX_TOKENS",
			}},
		})
	})

	p := newGenAIProviderForTest(s)
	resp, err := p.SendMessage(context.Background(), []Message{{Role: "user", Content: "go"}}, nil, nil, "", "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}
	if resp.StopReason != StopReasonMaxTokens {
		t.Errorf("expected max_tokens, got %q", resp.StopReason)
	}
}

// ---------------------------------------------------------------------------
// AC7: API errors are wrapped as *RetryableHTTPError
// ---------------------------------------------------------------------------

func TestGenAIProvider_SendMessage_ErrorMapping(t *testing.T) {
	setT(t)
	cases := []struct {
		name       string
		status     int
		body       string
		isPerm     bool
	}{
		{"429", 429, `{"error":{"code":429,"message":"rate limit","status":"RESOURCE_EXHAUSTED"}}`, false},
		{"503", 503, `{"error":{"code":503,"message":"unavailable","status":"UNAVAILABLE"}}`, false},
		{"400", 400, `{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`, true},
		{"401", 401, `{"error":{"code":401,"message":"unauthorized","status":"UNAUTHENTICATED"}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			p := newGenAIProviderForTest(s)
			_, err := p.SendMessage(context.Background(), []Message{{Role: "user", Content: "x"}}, nil, nil, "", "")
			if err == nil {
				t.Fatalf("expected error")
			}
			r, ok := err.(*RetryableHTTPError)
			if !ok {
				t.Fatalf("expected *RetryableHTTPError, got %T: %v", err, err)
			}
			if r.StatusCode != tc.status {
				t.Errorf("expected status %d, got %d", tc.status, r.StatusCode)
			}
			if r.IsPermanent != tc.isPerm {
				t.Errorf("expected IsPermanent=%v, got %v", tc.isPerm, r.IsPermanent)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AC8: streaming yields incremental text blocks
// ---------------------------------------------------------------------------

func TestGenAIProvider_Stream_Basic(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "streamGenerateContent") {
			t.Errorf("expected streamGenerateContent, got %s", r.URL.Path)
		}
		writeSSE(w, []string{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello "}]}}]}`,
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"World"}]}}]}`,
			`data: {"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`,
		})
	})

	p := newGenAIProviderForTest(s)
	blocksChan, result := p.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "say hi"}},
		nil, nil, "", "",
		2*time.Second,
	)
	var got []string
	for b := range blocksChan {
		if b.Block.Type == "text" {
			got = append(got, b.Block.Text)
		}
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if result.StopReason != StopReasonEndTurn {
		t.Errorf("expected end_turn, got %q", result.StopReason)
	}
	if len(got) == 0 {
		t.Error("expected at least one text block on the channel")
	}
	if len(result.Blocks) == 0 {
		t.Error("expected result.Blocks to be populated")
	}
}

// ---------------------------------------------------------------------------
// AC9: streaming function call
// ---------------------------------------------------------------------------

func TestGenAIProvider_Stream_FunctionCall(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, []string{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"location":"Paris"}}}]}}]}`,
			`data: {"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`,
		})
	})

	p := newGenAIProviderForTest(s)
	blocksChan, result := p.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "weather"}},
		[]ToolParam{{Name: "get_weather", InputSchema: ToolInputSchema{Type: "object"}}},
		nil, "", "", 2*time.Second,
	)
	for range blocksChan {
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	found := false
	for _, b := range result.Blocks {
		if b.Type == "tool_use" && b.ToolName == "get_weather" {
			found = true
			if b.ToolInput["location"] != "Paris" {
				t.Errorf("expected Paris, got %v", b.ToolInput["location"])
			}
		}
	}
	if !found {
		t.Errorf("expected tool_use block, got %+v", result.Blocks)
	}
}

// ---------------------------------------------------------------------------
// AC10: thinking parts
// ---------------------------------------------------------------------------

func TestGenAIProvider_Stream_Thinking(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, []string{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"thought":true,"text":"Let me think"}]}}]}`,
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"answer"}]},"finishReason":"STOP"}]}`,
		})
	})
	p := newGenAIProviderForTest(s)
	blocksChan, result := p.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "?"}},
		nil, nil, "", "", 2*time.Second,
	)
	for range blocksChan {
	}
	var thinking, text string
	for _, b := range result.Blocks {
		if b.Type == "thinking" {
			thinking = b.Thinking
		}
		if b.Type == "text" {
			text = b.Text
		}
	}
	if thinking != "Let me think" {
		t.Errorf("expected thinking 'Let me think', got %q", thinking)
	}
	if text != "answer" {
		t.Errorf("expected text 'answer', got %q", text)
	}
}

// ---------------------------------------------------------------------------
// AC11: stream usage tokens
// ---------------------------------------------------------------------------

func TestGenAIProvider_Stream_Usage(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, []string{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"cachedContentTokenCount":7}}`,
			`data: {"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`,
		})
	})

	p := newGenAIProviderForTest(s)
	blocksChan, result := p.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		nil, nil, "", "", 2*time.Second,
	)
	for range blocksChan {
	}
	if result.Usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 3 {
		t.Errorf("expected 3 output tokens, got %d", result.Usage.OutputTokens)
	}
	if result.Usage.CacheReadInputTokens != 7 {
		t.Errorf("expected 7 cached tokens, got %d", result.Usage.CacheReadInputTokens)
	}
}

// ---------------------------------------------------------------------------
// AC12: streaming error
// ---------------------------------------------------------------------------

func TestGenAIProvider_Stream_Error(t *testing.T) {
	setT(t)
	s := newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`))
	})

	p := newGenAIProviderForTest(s)
	blocksChan, result := p.SendMessageStream(
		context.Background(),
		[]Message{{Role: "user", Content: "x"}},
		nil, nil, "", "", 2*time.Second,
	)
	for range blocksChan {
	}
	if result.Error == "" {
		t.Error("expected error to be set")
	}
	if !result.IsPermanent {
		t.Error("expected IsPermanent for 400")
	}
}

// ---------------------------------------------------------------------------
// AC13: ProviderWithRetryConfig
// ---------------------------------------------------------------------------

func TestGenAIProvider_RetryConfig(t *testing.T) {
	setT(t)
	p := newGenAIProviderForTest(newTestGenAIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	cfg := RetryConfig{MaxRetries: 5, Max529Retries: 2}
	p.SetRetryConfig(cfg)
	if p.retryConfig.MaxRetries != 5 {
		t.Errorf("expected MaxRetries 5, got %d", p.retryConfig.MaxRetries)
	}
}

// ---------------------------------------------------------------------------
// AC14: ProviderKind constant
// ---------------------------------------------------------------------------

func TestGenAIProvider_KindConstant(t *testing.T) {
	if ProviderGenAI != "genai" {
		t.Errorf("expected ProviderGenAI = genai, got %q", ProviderGenAI)
	}
}
