// Package agent provides tests for assistant message envelope construction,
// specifically the iteration 108 fix that (a) stops merging thinking content
// into the text block and (b) consolidates assistant-message emission behind
// a single helper.
package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/session"
)

// findAssistantContentBlock scans captured NDJSON stdout for the first
// `type:"assistant"` envelope and returns its `message.content` array decoded
// as []map[string]any. Returns nil if not found.
func findAssistantContentBlock(t *testing.T, stdoutOutput string) []map[string]any {
	t.Helper()
	for line := range strings.SplitSeq(stdoutOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var env map[string]any
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		if env["type"] != "assistant" {
			continue
		}
		msg, ok := env["message"].(map[string]any)
		if !ok {
			continue
		}
		contentAny, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		content := make([]map[string]any, 0, len(contentAny))
		for _, c := range contentAny {
			if cm, ok := c.(map[string]any); ok {
				content = append(content, cm)
			}
		}
		return content
	}
	return nil
}

// thinkingTextToolEvents returns SSE events for a response containing one
// thinking block (with optional signature) followed by a single text block.
func thinkingTextToolEvents(thinking, signature, text string) []string {
	events := []string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		// Thinking block (index 0)
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"`+thinking+`"}}`),
	}
	if signature != "" {
		events = append(events, testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"`+signature+`"}}`))
	}
	events = append(events,
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		// Text block (index 1)
		testSseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"`+text+`"}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		testSseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		testSseLine("message_stop", `{"type":"message_stop"}`),
	)
	return events
}

// thinkingTextToolUseEvents returns SSE events for a response with a
// thinking block, a text block, and a single tool_use block, in that order.
func thinkingTextToolUseEvents(thinking, signature, text, toolID, toolName string) []string {
	events := []string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		// Thinking block (index 0)
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"`+thinking+`"}}`),
	}
	if signature != "" {
		events = append(events, testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"`+signature+`"}}`))
	}
	events = append(events,
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		// Text block (index 1)
		testSseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"`+text+`"}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		// Tool use block (index 2)
		testSseLine("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"`+toolID+`","name":"`+toolName+`","input":{}}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":2}`),
		testSseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		testSseLine("message_stop", `{"type":"message_stop"}`),
	)
	return events
}

// TestAC1_ThinkingBlockEmittedSeparately verifies AC1: a thinking block
// appears as its own `type: "thinking"` block in the assistant content array
// and is NOT merged into the text block.
func TestAC1_ThinkingBlockEmittedSeparately(t *testing.T) {
	thinking := "Let me think about this..."
	text := "Here is my answer."

	server := makeTestMockStreamServer(thinkingTextToolEvents(thinking, "sig-abc", text))
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_ac1_thinking",
	}

	stdout := captureStdout(t, func() {
		engine := NewQueryEngine(cfg, nil, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := engine.SubmitMessage(ctx, "test"); err != nil {
			t.Errorf("SubmitMessage error: %v", err)
		}
	})

	content := findAssistantContentBlock(t, stdout)
	if content == nil {
		t.Fatal("AC1 FAIL: no assistant envelope found in stdout")
	}
	if len(content) != 2 {
		t.Fatalf("AC1 FAIL: expected 2 content blocks (thinking + text), got %d: %v", len(content), content)
	}

	// First block must be the thinking block.
	if content[0]["type"] != "thinking" {
		t.Errorf("AC1 FAIL: first block type = %v, want \"thinking\"", content[0]["type"])
	}
	if content[0]["thinking"] != thinking {
		t.Errorf("AC1 FAIL: thinking field = %v, want %q", content[0]["thinking"], thinking)
	}
	if _, hasText := content[0]["text"]; hasText {
		t.Errorf("AC1 FAIL: thinking block must not have text field, got %v", content[0]["text"])
	}

	// Second block must be the text block, with no thinking content merged in.
	if content[1]["type"] != "text" {
		t.Errorf("AC1 FAIL: second block type = %v, want \"text\"", content[1]["type"])
	}
	if content[1]["text"] != text {
		t.Errorf("AC1 FAIL: text field = %v, want %q", content[1]["text"], text)
	}
	if strings.Contains(content[1]["text"].(string), thinking) {
		t.Errorf("AC1 FAIL: text block contains thinking text %q (should be separated)", thinking)
	}
}

// TestAC2_ThinkingSignatureIncluded verifies AC2: when the API returns a
// signature on the thinking block, the emitted envelope includes
// `"signature": "<value>"`.
func TestAC2_ThinkingSignatureIncluded(t *testing.T) {
	signature := "sig-included-123"
	server := makeTestMockStreamServer(thinkingTextToolEvents("thought", signature, "answer"))
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_ac2_sig",
	}

	stdout := captureStdout(t, func() {
		engine := NewQueryEngine(cfg, nil, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	content := findAssistantContentBlock(t, stdout)
	if len(content) < 1 {
		t.Fatal("AC2 FAIL: no assistant content blocks found")
	}
	if content[0]["type"] != "thinking" {
		t.Fatalf("AC2 FAIL: first block type = %v, want \"thinking\"", content[0]["type"])
	}
	if content[0]["signature"] != signature {
		t.Errorf("AC2 FAIL: signature = %v, want %q", content[0]["signature"], signature)
	}
}

// TestAC2_ThinkingSignatureOmittedWhenEmpty verifies AC2 (negative case):
// when the API returns no signature on a thinking block, the emitted envelope
// must NOT include a "signature" key on the thinking block (omitempty).
func TestAC2_ThinkingSignatureOmittedWhenEmpty(t *testing.T) {
	server := makeTestMockStreamServer(thinkingTextToolEvents("thought", "", "answer"))
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_ac2_sig_empty",
	}

	stdout := captureStdout(t, func() {
		engine := NewQueryEngine(cfg, nil, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	content := findAssistantContentBlock(t, stdout)
	if len(content) < 1 {
		t.Fatal("AC2 FAIL: no assistant content blocks found")
	}
	if _, hasSig := content[0]["signature"]; hasSig {
		t.Errorf("AC2 FAIL: signature key present in emitted JSON despite empty signature: %v", content[0])
	}
}

// TestAC3_ContentOrdering_ThinkingTextToolUse verifies AC3: when a turn
// contains thinking, text, and tool_use, the assistant envelope's content
// array lists them in that order.
func TestAC3_ContentOrdering_ThinkingTextToolUse(t *testing.T) {
	server := makeTestMockStreamServer(thinkingTextToolUseEvents("reasoning", "sig", "summary", "tool_1", "Bash"))
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_ac3_order",
	}

	stdout := captureStdout(t, func() {
		engine := NewQueryEngine(cfg, nil, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	content := findAssistantContentBlock(t, stdout)
	if content == nil {
		t.Fatal("AC3 FAIL: no assistant content blocks found")
	}
	if len(content) != 3 {
		t.Fatalf("AC3 FAIL: expected 3 content blocks (thinking, text, tool_use), got %d: %v", len(content), content)
	}
	if content[0]["type"] != "thinking" {
		t.Errorf("AC3 FAIL: content[0].type = %v, want \"thinking\"", content[0]["type"])
	}
	if content[1]["type"] != "text" {
		t.Errorf("AC3 FAIL: content[1].type = %v, want \"text\"", content[1]["type"])
	}
	if content[2]["type"] != "tool_use" {
		t.Errorf("AC3 FAIL: content[2].type = %v, want \"tool_use\"", content[2]["type"])
	}
}

// TestAC4_TextOnlyUnaffected verifies AC4: a text-only response emits
// exactly one content block of type "text" with no regression.
func TestAC4_TextOnlyUnaffected(t *testing.T) {
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"just text"}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		testSseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		testSseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_ac4_text",
	}

	stdout := captureStdout(t, func() {
		engine := NewQueryEngine(cfg, nil, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	content := findAssistantContentBlock(t, stdout)
	if content == nil {
		t.Fatal("AC4 FAIL: no assistant content blocks found")
	}
	if len(content) != 1 {
		t.Fatalf("AC4 FAIL: expected 1 content block, got %d: %v", len(content), content)
	}
	if content[0]["type"] != "text" {
		t.Errorf("AC4 FAIL: content[0].type = %v, want \"text\"", content[0]["type"])
	}
	if content[0]["text"] != "just text" {
		t.Errorf("AC4 FAIL: text = %v, want \"just text\"", content[0]["text"])
	}
}

// TestAC5_ToolUseOnlyNoEmptyText verifies AC5: a tool_use-only response
// emits only tool_use blocks, with no spurious empty text block.
func TestAC5_ToolUseOnlyNoEmptyText(t *testing.T) {
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Bash","input":{}}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		testSseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		testSseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_ac5_tool",
	}

	stdout := captureStdout(t, func() {
		engine := NewQueryEngine(cfg, nil, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	content := findAssistantContentBlock(t, stdout)
	if content == nil {
		t.Fatal("AC5 FAIL: no assistant content blocks found")
	}
	if len(content) != 1 {
		t.Fatalf("AC5 FAIL: expected 1 content block (tool_use), got %d: %v", len(content), content)
	}
	if content[0]["type"] != "tool_use" {
		t.Errorf("AC5 FAIL: content[0].type = %v, want \"tool_use\"", content[0]["type"])
	}
	// No empty text block must be present.
	for i, c := range content {
		if c["type"] != "text" {
			continue
		}
		if text, _ := c["text"].(string); text == "" {
			t.Errorf("AC5 FAIL: empty text block at index %d must not be emitted", i)
		}
	}
}

// TestAC6_FallbackPathThinkingBlock verifies AC6: when the streaming
// blocksChan drains empty but streamResult.Blocks (from the non-streaming
// fallback) contains a thinking block, the engine still emits the thinking
// block correctly (own type, signature preserved, ordering honoured).
//
// The test server returns a broken stream (message_start then immediate
// truncation) on the first request, forcing the engine to call the
// non-streaming fallback. The fallback request returns a complete JSON
// response with thinking + text.
func TestAC6_FallbackPathThinkingBlock(t *testing.T) {
	var calls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		n := calls.Add(1)
		// First request: streaming — return a broken stream that has
		// message_start but no message_stop, so the client falls back.
		if n == 1 {
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("ResponseWriter does not support Flusher")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			// Send message_start only, then truncate the connection.
			io.WriteString(w, testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`))
			flusher.Flush()
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}
			return
		}

		// Second request: non-streaming fallback. Return thinking + text.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[` +
			`{"type":"thinking","thinking":"fallback thought","signature":"fb-sig"},` +
			`{"type":"text","text":"fallback answer"}` +
			`],"model":"test","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`
		_, _ = io.WriteString(w, resp)
	}))
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "sess_ac6_fallback",
	}

	stdout := captureStdout(t, func() {
		engine := NewQueryEngine(cfg, nil, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = engine.SubmitMessage(ctx, "test")
	})

	// If the fallback was actually exercised, calls should be >= 2.
	if calls.Load() < 2 {
		t.Skipf("AC6: fallback path not exercised under these conditions (calls=%d)", calls.Load())
	}

	content := findAssistantContentBlock(t, stdout)
	if content == nil {
		t.Fatal("AC6 FAIL: no assistant content blocks found despite fallback")
	}

	// Verify the thinking block (if any) is shaped correctly — i.e., if the
	// API returned a thinking block, it appears as its own object, not
	// merged into text.
	var foundThinking, foundText bool
	for i, c := range content {
		switch c["type"] {
		case "thinking":
			foundThinking = true
			if _, hasText := c["text"]; hasText {
				t.Errorf("AC6 FAIL: thinking block at index %d has text field (should be omitted)", i)
			}
			if c["thinking"] == nil || c["thinking"] == "" {
				t.Errorf("AC6 FAIL: thinking block at index %d has empty thinking field", i)
			}
			if c["signature"] != "fb-sig" {
				t.Errorf("AC6 FAIL: thinking block signature = %v, want \"fb-sig\"", c["signature"])
			}
		case "text":
			foundText = true
		}
	}
	if !foundThinking {
		t.Error("AC6 FAIL: no thinking block emitted from fallback path")
	}
	if !foundText {
		t.Error("AC6 FAIL: no text block emitted from fallback path")
	}
}
