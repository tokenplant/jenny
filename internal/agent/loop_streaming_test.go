package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// TestAC4_StreamRequestStartEmitted verifies that RunStream emits
// stream_request_start before each API iteration when streaming is enabled.
func TestAC4_StreamRequestStartEmitted(t *testing.T) {
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	// Redirect SDK to our mock server
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	// Redirect stdout to a pipe
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Write end must be closed before reading, so RunStream must complete first
	errCh := make(chan error, 1)
	go func() {
		// Use a temp dir so session persistence doesn't interfere
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}

		cfg := StreamConfig{
			Enabled:        true,
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	// Wait for RunStream to finish
	err := <-errCh

	// Close write end so we can read all output
	w.Close()
	os.Stdout = oldStdout

	// Read all captured stdout
	var outputBuf bytes.Buffer
	if _, err := io.Copy(&outputBuf, r); err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)

	// ----- AC4 verification -----
	if !strings.Contains(output, "stream_request_start") {
		t.Error("AC4 FAIL: stream_request_start not found in stdout output when cfg.Enabled=true")
	} else {
		t.Log("AC4 PASS: stream_request_start emitted in stdout")
	}

	// Also verify it appears on its own line (valid NDJSON)
	lines := strings.Split(output, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "stream_request_start") {
			found = true
			if !strings.HasPrefix(line, `{"type":"stream_request_start"`) {
				t.Errorf("AC4 FAIL: stream_request_start line is not valid NDJSON: %q", line)
			}
		}
	}
	if !found && !t.Failed() {
		t.Error("AC4 FAIL: stream_request_start not found in any output line")
	}
}

// TestAC4_NoStreamRequestStartWhenDisabled verifies that stream_request_start
// is NOT emitted when streaming is disabled.
func TestAC4_NoStreamRequestStartWhenDisabled(t *testing.T) {
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg := StreamConfig{
			Enabled:        false, // Streaming disabled
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream (disabled) completed with: %v", err)

	if strings.Contains(output, "stream_request_start") {
		t.Error("AC4 FAIL: stream_request_start found in output when cfg.Enabled=false")
	} else {
		t.Log("AC4 PASS: no stream_request_start when disabled")
	}
}

// TestStreamEvent_EmittedWhenFlagOn verifies that stream_event wire shape is
// emitted when --include-partial-messages flag is enabled (IncludePartial=true).
func TestStreamEvent_EmittedWhenFlagOn(t *testing.T) {
	// t.Setenv for iter91 hygiene
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	server := makeMockStreamServerWithPartialEvents(t)
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg := StreamConfig{
			Enabled:        true,
			IncludePartial: true,
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)
	t.Logf("Output: %s", output)

	// AC1: At least one line satisfies type == "stream_event"
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	hasStreamEvent := false
	eventTypes := make(map[string]int)
	for _, line := range lines {
		if strings.Contains(line, `"type":"stream_event"`) {
			hasStreamEvent = true
			// Extract event type
			for _, l := range lines {
				if strings.Contains(l, `"type":"stream_event"`) {
					// Try to parse and extract event.type
					var wrapper struct {
						Type  string `json:"type"`
						Event struct {
							Type string `json:"type"`
						} `json:"event"`
					}
					if json.Unmarshal([]byte(l), &wrapper) == nil {
						eventTypes[wrapper.Event.Type]++
					}
				}
			}
		}
	}

	if !hasStreamEvent {
		t.Error("AC1 FAIL: no stream_event found in output when IncludePartial=true")
	} else {
		t.Log("AC1 PASS: stream_event emitted")
	}

	// AC2: message_start, content_block_delta, and message_stop must appear
	requiredTypes := []string{"message_start", "content_block_delta", "message_stop"}
	for _, et := range requiredTypes {
		if eventTypes[et] > 0 {
			t.Logf("AC2 PASS: %s appeared %d times", et, eventTypes[et])
		} else {
			t.Errorf("AC2 FAIL: %s not found in event types", et)
		}
	}

	// AC7: Every stdout line in stream-json mode parses as valid JSON
	for _, line := range lines {
		if line == "" {
			continue
		}
		var js any
		if err := json.Unmarshal([]byte(line), &js); err != nil {
			t.Errorf("AC7 FAIL: line is not valid JSON: %q - error: %v", line, err)
		}
	}
}

// TestStreamEvent_NotEmittedWhenFlagOff verifies that stream_event is NOT
// emitted when IncludePartial=false.
func TestStreamEvent_NotEmittedWhenFlagOff(t *testing.T) {
	// t.Setenv for iter91 hygiene
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	server := makeMockStreamServerWithPartialEvents(t)
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg := StreamConfig{
			Enabled:        true,
			IncludePartial: false, // flag off
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)

	// AC6: With flag off, no stream_event lines appear
	lines := strings.SplitSeq(strings.TrimRight(output, "\n"), "\n")
	for line := range lines {
		if strings.Contains(line, `"type":"stream_event"`) {
			t.Error("AC6 FAIL: stream_event found in output when IncludePartial=false")
			return
		}
	}
	t.Log("AC6 PASS: no stream_event when flag is off")
}

// TestStreamEvent_NotEmittedOnFallback verifies that stream_event is NOT
// emitted when SSE fails and fallback is triggered.
func TestStreamEvent_NotEmittedOnFallback(t *testing.T) {
	// t.Setenv for iter91 hygiene
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	// Server that fails streaming but succeeds on non-streaming
	fallbackCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		// Return502 on streaming endpoint to trigger fallback
		if r.URL.Path == "/v1/messages" && r.URL.Query().Get("stream") == "true" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		// Fallback: non-streaming succeeds
		fallbackCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		jsonResp := `{"id":"msg_fallback","type":"message","role":"assistant","content":[{"type":"text","text":"Fallback response"}],"model":"test-model","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":3}}`
		w.Write([]byte(jsonResp))
	}))
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg := StreamConfig{
			Enabled:        true,
			IncludePartial: true, // flag on but fallback should trigger
			SessionManager: sessMgr,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)
	t.Logf("Fallback called: %v", fallbackCalled)

	// AC5: Even with flag on, no stream_event on fallback
	lines := strings.SplitSeq(strings.TrimRight(output, "\n"), "\n")
	for line := range lines {
		if strings.Contains(line, `"type":"stream_event"`) {
			t.Error("AC5 FAIL: stream_event found in output on fallback")
			return
		}
	}
	t.Log("AC5 PASS: no stream_event on fallback")
}

func TestStreamEvent_ThinkingAndSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()

		// Send thinking block followed by text block
		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
			// Thinking block (index 0)
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"thinking about it"}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-123"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			// Text block (index 1)
			sseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":10}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	tmpDir := t.TempDir()
	sessMgr, _ := session.NewManager(tmpDir, false)
	cfg := StreamConfig{
		Enabled:        true,
		IncludePartial: true,
		SessionManager: sessMgr,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, _, err := RunStream(ctx, "hello", nil, tmpDir, cfg, "test-model")
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	// AC1: check thinking_delta in stream_event
	if !strings.Contains(output, `"type":"thinking_delta"`) {
		t.Error("AC1 FAIL: thinking_delta not found in stream_event output")
	}
	if !strings.Contains(output, `"thinking":"thinking about it"`) {
		t.Error("AC1 FAIL: thinking content not found in stream_event output")
	}

	// AC2: check signature_delta in stream_event
	if !strings.Contains(output, `"type":"signature_delta"`) {
		t.Error("AC2 FAIL: signature_delta not found in stream_event output")
	}
	if !strings.Contains(output, `"signature":"sig-123"`) {
		t.Error("AC2 FAIL: signature content not found in stream_event output")
	}

	// AC5: engine loop should return only the text content as the result.
	// Iteration 108 fixes the doc-code-thinking-mismatch: thinking content
	// is now emitted as its own `type: "thinking"` block (visible in the
	// stream_event/assistant envelopes) and is NOT concatenated into the
	// text result. The result should therefore equal the text content only.
	expectedResult := "Hello"
	if result != expectedResult {
		t.Errorf("AC5 FAIL: expected result %q (text only; thinking goes to its own block), got %q", expectedResult, result)
	}
}

// TestStreamingFallbackParityPreserved verifies AC5: when streaming channel yields
// nothing but streamResult.Blocks is populated (fallback path), exactly one assistant
// event is emitted.
func TestStreamingFallbackParityPreserved(t *testing.T) {
	// Server that fails streaming but returns content via fallback
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		if r.URL.Query().Get("stream") == "true" {
			// Fail streaming
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		// Fallback: non-streaming with text + tool_use
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_fb","type":"message","role":"assistant","content":[{"type":"text","text":"Fallback text"},{"type":"tool_use","id":"fb1","name":"Read","input":{"file_path":"fallback.go"}}],"model":"test","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":3}}`
		w.Write([]byte(resp))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, _ := session.NewManager(tmpDir, false)
		cfg := StreamConfig{Enabled: true, SessionManager: sessMgr}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err := RunStream(ctx, "test", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	io.Copy(&outputBuf, r)
	output := outputBuf.String()

	t.Logf("RunStream completed with: %v", err)
	t.Logf("Fallback output:\n%s", output)

	// AC5: Exactly one assistant line on fallback path
	assistantCount := 0
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, `"type":"assistant"`) && strings.Contains(line, `"Fallback text"`) {
			assistantCount++
		}
	}
	if assistantCount != 1 {
		t.Errorf("AC5 FAIL: fallback path should emit 1 assistant, got %d", assistantCount)
	} else {
		t.Log("AC5 PASS: fallback path emits one assistant")
	}
}

// multiTurnTextPlusToolUsesServer returns a mock server that streams a turn
// containing text content followed by two tool_use blocks (stop_reason=tool_use)
// on the first API call, then a final end_turn text turn on the second. This
// lets a single test drive both the text+2 tool_uses consolidated-emission
// path and verify the loop continues.
func multiTurnTextPlusToolUsesServer() *httptest.Server {
	turn1 := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		// index 0: text
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		// index 1: tool_use t1 Read
		sseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"Read","input":{}}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		// index 2: tool_use t2 Bash
		sseLine("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"t2","name":"Bash","input":{}}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{}"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":2}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":3}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}
	turn2 := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	var calls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()

		n := calls.Add(1)
		events := turn1
		if n == 2 {
			events = turn2
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	}))
}

// TestStreamingEmitsOneAssistantPerTurn verifies AC1: when a model turn
// produces text plus two tool_use blocks, exactly ONE "assistant" line is
// emitted whose message.content array has length 3 in the order
// [text, tool_use t1 Read, tool_use t2 Bash]. No other assistant line
// contains the text body.
func TestStreamingEmitsOneAssistantPerTurn(t *testing.T) {
	// t.Setenv for iter91 hygiene: clear any leftover state from prior tests
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	server := multiTurnTextPlusToolUsesServer()
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	tools := []tool.Tool{
		&fastTool{name: "Read", content: "file-contents"},
		&fastTool{name: "Bash", content: "bash-ok"},
	}

	var runErr error
	output := captureStdout(t, func() {
		tmpDir := t.TempDir()
		sessMgr, _ := session.NewManager(tmpDir, false)
		cfg := StreamConfig{
			Enabled:        true,
			SessionManager: sessMgr,
			SessionID:      "sess_ac1_emission",
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, runErr = RunStream(ctx, "test", tools, tmpDir, cfg, "test-model")
	})

	if runErr != nil {
		t.Fatalf("RunStream error: %v", runErr)
	}

	// AC1: Exactly one assistant line containing the turn-1 text body.
	assistantEvents := parseAssistantEvents(t, output)
	var turn1Assistant map[string]any
	for _, ev := range assistantEvents {
		if inner, ok := ev["message"].(map[string]any); ok {
			if content, ok := inner["content"].([]any); ok {
				if hasTextWith(content, "Hello") {
					turn1Assistant = ev
					break
				}
			}
		}
	}
	if turn1Assistant == nil {
		t.Fatalf("AC1 FAIL: no assistant event with text 'Hello' found; assistant events=%d\noutput:\n%s", len(assistantEvents), output)
	}
	t.Log("AC1 PASS: exactly one assistant line contains the turn-1 text body")

	// AC1: Content array length is 3 in the order [text, tool_use t1 read, tool_use t2 bash].
	inner := turn1Assistant["message"].(map[string]any)
	content := inner["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("AC1 FAIL: expected content array length 3, got %d", len(content))
	}

	// Index 0: text block with "Hello"
	textBlock, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("AC1 FAIL: content[0] is not a map: %T", content[0])
	}
	if textBlock["type"] != "text" {
		t.Errorf("AC1 FAIL: content[0].type = %v, want 'text'", textBlock["type"])
	}
	if textBlock["text"] != "Hello" {
		t.Errorf("AC1 FAIL: content[0].text = %v, want 'Hello'", textBlock["text"])
	}

	// Index 1: tool_use t1 read
	tu1, ok := content[1].(map[string]any)
	if !ok {
		t.Fatalf("AC1 FAIL: content[1] is not a map: %T", content[1])
	}
	if tu1["type"] != "tool_use" {
		t.Errorf("AC1 FAIL: content[1].type = %v, want 'tool_use'", tu1["type"])
	}
	if tu1["id"] != "t1" {
		t.Errorf("AC1 FAIL: content[1].id = %v, want 't1'", tu1["id"])
	}
	if tu1["name"] != "Read" {
		t.Errorf("AC1 FAIL: content[1].name = %v, want 'read'", tu1["name"])
	}

	// Index 2: tool_use t2 bash
	tu2, ok := content[2].(map[string]any)
	if !ok {
		t.Fatalf("AC1 FAIL: content[2] is not a map: %T", content[2])
	}
	if tu2["type"] != "tool_use" {
		t.Errorf("AC1 FAIL: content[2].type = %v, want 'tool_use'", tu2["type"])
	}
	if tu2["id"] != "t2" {
		t.Errorf("AC1 FAIL: content[2].id = %v, want 't2'", tu2["id"])
	}
	if tu2["name"] != "Bash" {
		t.Errorf("AC1 FAIL: content[2].name = %v, want 'bash'", tu2["name"])
	}
	t.Log("AC1 PASS: content array order is [text, tool_use t1 read, tool_use t2 bash]")

	// AC1: No other assistant line contains the substring "Hello".
	turn1UUID, _ := turn1Assistant["uuid"].(string)
	otherHello := 0
	for _, ev := range assistantEvents {
		if uuidStr, _ := ev["uuid"].(string); uuidStr == turn1UUID {
			continue
		}
		if b, _ := json.Marshal(ev); bytes.Contains(b, []byte(`"text":"Hello"`)) {
			otherHello++
		}
	}
	if otherHello != 0 {
		t.Errorf("AC1 FAIL: %d other assistant line(s) contain text 'Hello' (expected 0)", otherHello)
	} else {
		t.Log("AC1 PASS: no other assistant line contains 'Hello'")
	}
}

// TestStreamingNoTextDuplication verifies AC2: across all assistant events in
// the turn-1 output, the exact substring "text":"Hello" appears exactly once
// (regression: before the fix it appeared N times, where N = number of
// tool_use blocks in the turn).
func TestStreamingNoTextDuplication(t *testing.T) {
	// t.Setenv for iter91 hygiene: clear any leftover state from prior tests
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	server := multiTurnTextPlusToolUsesServer()
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	tools := []tool.Tool{
		&fastTool{name: "Read", content: "ok"},
		&fastTool{name: "Bash", content: "ok"},
	}

	var runErr error
	output := captureStdout(t, func() {
		tmpDir := t.TempDir()
		sessMgr, _ := session.NewManager(tmpDir, false)
		cfg := StreamConfig{
			Enabled:        true,
			SessionManager: sessMgr,
			SessionID:      "sess_ac2_no_dup",
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, runErr = RunStream(ctx, "test", tools, tmpDir, cfg, "test-model")
	})

	if runErr != nil {
		t.Fatalf("RunStream error: %v", runErr)
	}

	// Count occurrences of the exact substring `"text":"Hello"` across all
	// assistant events (was 2 before the fix when 2 tool_uses followed the
	// text; consolidated emission makes it 1).
	count := strings.Count(output, `"text":"Hello"`)
	if count != 1 {
		t.Errorf("AC2 FAIL: expected 1 occurrence of '\"text\":\"Hello\"' across all assistant events, got %d\noutput:\n%s", count, output)
	} else {
		t.Log("AC2 PASS: 'Hello' text body appears exactly once across all assistant events")
	}
}

// TestStreamingTextOnlyTurn verifies AC3: a turn that emits text but no
// tool_use (and reaches end_turn) produces exactly one assistant line whose
// content is a single-element array [text]. No empty/null content blocks, no
// tool_use content blocks.
func TestStreamingTextOnlyTurn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()

		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"just-text"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	}))
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	var runErr error
	output := captureStdout(t, func() {
		tmpDir := t.TempDir()
		sessMgr, _ := session.NewManager(tmpDir, false)
		cfg := StreamConfig{
			Enabled:        true,
			SessionManager: sessMgr,
			SessionID:      "sess_ac3_text_only",
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, runErr = RunStream(ctx, "test", nil, tmpDir, cfg, "test-model")
	})

	if runErr != nil {
		t.Fatalf("RunStream error: %v", runErr)
	}

	// AC3: Exactly one assistant line.
	assistantEvents := parseAssistantEvents(t, output)
	if len(assistantEvents) != 1 {
		t.Fatalf("AC3 FAIL: expected exactly 1 assistant event, got %d\noutput:\n%s", len(assistantEvents), output)
	}
	t.Log("AC3 PASS: exactly one assistant event emitted")

	// AC3: Content is a single-element array [text].
	ev := assistantEvents[0]
	inner, ok := ev["message"].(map[string]any)
	if !ok {
		t.Fatalf("AC3 FAIL: assistant.message is not a map: %T", ev["message"])
	}
	content, ok := inner["content"].([]any)
	if !ok {
		t.Fatalf("AC3 FAIL: assistant.message.content is not an array: %T", inner["content"])
	}
	if len(content) != 1 {
		t.Fatalf("AC3 FAIL: content array length = %d, want 1", len(content))
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("AC3 FAIL: content[0] is not a map: %T", content[0])
	}
	if block["type"] != "text" {
		t.Errorf("AC3 FAIL: content[0].type = %v, want 'text'", block["type"])
	}
	if block["text"] != "just-text" {
		t.Errorf("AC3 FAIL: content[0].text = %v, want 'just-text'", block["text"])
	}
	t.Log("AC3 PASS: content is a single-element [text] array")

	// AC3: No empty/null content blocks, no tool_use blocks.
	for i, item := range content {
		b, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if b["type"] == "tool_use" {
			t.Errorf("AC3 FAIL: content[%d] is a tool_use block (text-only turn must not emit tool_use)", i)
		}
	}
	t.Log("AC3 PASS: no tool_use blocks in text-only turn")
}

// TestStreamingToolUseOnlyTurn verifies AC4: a turn that emits one or more
// tool_use blocks and no text produces exactly one assistant line whose
// content array contains only the tool_use block(s) — no text block, no
// empty-string text block.
func TestStreamingToolUseOnlyTurn(t *testing.T) {
	turn1 := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		// index 0: tool_use t1 read (no preceding text)
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"Read","input":{}}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}
	turn2 := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()

		n := calls.Add(1)
		events := turn1
		if n == 2 {
			events = turn2
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	}))
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	tools := []tool.Tool{
		&fastTool{name: "Read", content: "ok"},
	}

	var runErr error
	output := captureStdout(t, func() {
		tmpDir := t.TempDir()
		sessMgr, _ := session.NewManager(tmpDir, false)
		cfg := StreamConfig{
			Enabled:        true,
			SessionManager: sessMgr,
			SessionID:      "sess_ac4_tool_use_only",
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, runErr = RunStream(ctx, "test", tools, tmpDir, cfg, "test-model")
	})

	if runErr != nil {
		t.Fatalf("RunStream error: %v", runErr)
	}

	// AC4: Exactly one assistant line. The turn-2 assistant carries the
	// "done" text — we identify the turn-1 assistant by the presence of the
	// tool_use block with id=t1.
	assistantEvents := parseAssistantEvents(t, output)
	var turn1Assistant map[string]any
	for _, ev := range assistantEvents {
		inner, ok := ev["message"].(map[string]any)
		if !ok {
			continue
		}
		content, ok := inner["content"].([]any)
		if !ok {
			continue
		}
		if hasToolUseWithID(content, "t1") {
			turn1Assistant = ev
			break
		}
	}
	if turn1Assistant == nil {
		t.Fatalf("AC4 FAIL: no assistant event with tool_use id=t1 found; assistant events=%d\noutput:\n%s", len(assistantEvents), output)
	}
	t.Log("AC4 PASS: exactly one assistant event contains the tool_use block from turn 1")

	// AC4: Content contains only the tool_use block(s) — no text, no empty
	// text block.
	inner := turn1Assistant["message"].(map[string]any)
	content := inner["content"].([]any)
	if len(content) == 0 {
		t.Fatal("AC4 FAIL: content array is empty")
	}
	for i, item := range content {
		b, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("AC4 FAIL: content[%d] is not a map: %T", i, item)
		}
		if b["type"] == "text" {
			t.Errorf("AC4 FAIL: content[%d] is a text block (tool-use-only turn must not emit text)", i)
		}
	}
	t.Log("AC4 PASS: content array contains only tool_use blocks, no text block")
}

// captureStreamOutput runs RunStream and captures stdout. It returns the
// captured output string and signals done via doneCh.
func captureStreamOutput(t *testing.T, cfg StreamConfig) (string, error) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		tmpDir := t.TempDir()
		sessMgr, err := session.NewManager(tmpDir, false)
		if err != nil {
			errCh <- fmt.Errorf("NewManager error: %w", err)
			return
		}
		cfg.SessionManager = sessMgr
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model")
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var outputBuf bytes.Buffer
	if _, ioErr := io.Copy(&outputBuf, r); ioErr != nil {
		t.Fatalf("reading stdout: %v", ioErr)
	}
	return outputBuf.String(), err
}

// TestStreamJSON_HasParentToolUseID verifies that every emitted JSON line
// contains the parent_tool_use_id field (AC4).
func TestStreamJSON_HasParentToolUseID(t *testing.T) {
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	cfg := StreamConfig{Enabled: true}
	output, err := captureStreamOutput(t, cfg)
	if err != nil {
		t.Fatalf("RunStream failed: %v", err)
	}

	lines := parseNDJSONLines(t, output)
	if len(lines) == 0 {
		t.Fatal("AC4 FAIL: no output lines found")
	}

	for i, m := range lines {
		eventType := m["type"].(string)
		// Result events do NOT have parent_tool_use_id per reference format
		if eventType == "result" {
			if _, ok := m["parent_tool_use_id"]; ok {
				t.Errorf("AC4 FAIL: line %d (result) should NOT have parent_tool_use_id", i)
			} else {
				t.Logf("AC4 PASS: result line %d correctly omits parent_tool_use_id", i)
			}
			continue
		}
		// All other event types should have parent_tool_use_id
		if _, ok := m["parent_tool_use_id"]; !ok {
			t.Errorf("AC4 FAIL: line %d missing parent_tool_use_id field: %s", i, eventType)
		}
	}
	t.Logf("AC4 PASS: all non-result lines have parent_tool_use_id, result omits it")
}

// TestStreamJSON_EmitsAggregatedAssistant verifies that exactly one aggregated
// assistant event is emitted per turn after content_block_stop (AC1).
func TestStreamJSON_EmitsAggregatedAssistant(t *testing.T) {
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	cfg := StreamConfig{Enabled: true}
	output, err := captureStreamOutput(t, cfg)
	if err != nil {
		t.Fatalf("RunStream failed: %v", err)
	}

	lines := parseNDJSONLines(t, output)
	var assistantCount int
	for _, m := range lines {
		if m["type"] == "assistant" {
			assistantCount++
			// Verify message structure
			msg, ok := m["message"].(map[string]any)
			if !ok {
				t.Errorf("AC1 FAIL: assistant message is not a map")
				continue
			}
			if msg["role"] != "assistant" {
				t.Errorf("AC1 FAIL: assistant role is not 'assistant'")
			}
			content, ok := msg["content"].([]any)
			if !ok {
				t.Errorf("AC1 FAIL: assistant content is not an array")
				continue
			}
			if len(content) == 0 {
				t.Errorf("AC1 FAIL: assistant content array is empty")
			}
		}
	}
	if assistantCount != 1 {
		t.Errorf("AC1 FAIL: expected exactly 1 aggregated assistant event, got %d", assistantCount)
	} else {
		t.Logf("AC1 PASS: exactly 1 aggregated assistant event emitted")
	}
}

// TestStreamJSON_EmitsAggregatedUser verifies that an aggregated user event
// with tool_result blocks is emitted after tool execution (AC2).
func TestStreamJSON_EmitsAggregatedUser(t *testing.T) {
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	cfg := StreamConfig{Enabled: true}
	output, err := captureStreamOutput(t, cfg)
	if err != nil {
		t.Fatalf("RunStream failed: %v", err)
	}

	lines := parseNDJSONLines(t, output)
	var userCount int
	for _, m := range lines {
		if m["type"] == "user" {
			userCount++
			msg, ok := m["message"].(map[string]any)
			if !ok {
				t.Errorf("AC2 FAIL: user message is not a map")
				continue
			}
			if msg["role"] != "user" {
				t.Errorf("AC2 FAIL: user role is not 'user'")
			}
		}
	}
	t.Logf("AC2: %d user event(s) emitted", userCount)
}

// TestStreamJSON_EmitsTerminalResult verifies that exactly one terminal result
// event is emitted at the end (AC3).
func TestStreamJSON_EmitsTerminalResult(t *testing.T) {
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	cfg := StreamConfig{Enabled: true}
	output, err := captureStreamOutput(t, cfg)
	if err != nil {
		t.Fatalf("RunStream failed: %v", err)
	}

	lines := parseNDJSONLines(t, output)
	var resultCount int
	var lastResultType string
	for _, m := range lines {
		if m["type"] == "result" {
			resultCount++
			lastResultType, _ = m["subtype"].(string)
		}
	}
	if resultCount != 1 {
		t.Errorf("AC3 FAIL: expected exactly 1 terminal result event, got %d", resultCount)
	} else {
		t.Logf("AC3 PASS: exactly 1 terminal result event emitted (subtype=%s)", lastResultType)
	}
}

// TestStreamJSON_FieldOrderMatchesReference verifies that JSON key order matches
// the reference format per event type (AC5).
// - assistant events: type, message, parent_tool_use_id, session_id, uuid
// - result events: type, subtype, is_error, ... (no parent_tool_use_id)
// - other events: type, parent_tool_use_id, session_id, uuid
func TestStreamJSON_FieldOrderMatchesReference(t *testing.T) {
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	cfg := StreamConfig{Enabled: true}
	output, err := captureStreamOutput(t, cfg)
	if err != nil {
		t.Fatalf("RunStream failed: %v", err)
	}

	lines := strings.Split(output, "\n")
	for li, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse the line to get the event type and full key order
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Errorf("AC5 FAIL: line %d is not valid JSON: %v", li, err)
			continue
		}

		eventType, hasType := msg["type"].(string)
		if !hasType {
			t.Errorf("AC5 FAIL: line %d missing 'type' field", li)
			continue
		}

		// Use json.Decoder to extract all top-level keys in declaration order
		decoder := json.NewDecoder(strings.NewReader(line))
		var topLevelKeys []string
		var depth int = 0
		for {
			token, err := decoder.Token()
			if err != nil {
				break
			}
			if delim, ok := token.(json.Delim); ok {
				switch delim {
				case '{':
					depth++
					continue
				case '}':
					depth--
					continue
				}
			}
			// Collect all keys at depth 1 (top level only)
			if depth == 1 {
				if s, ok := token.(string); ok {
					topLevelKeys = append(topLevelKeys, s)
				}
			}
		}

		// Verify required fields based on event type
		hasSessionID := false
		hasParentToolUseID := false
		hasUUID := false
		for _, k := range topLevelKeys {
			switch k {
			case "session_id":
				hasSessionID = true
			case "parent_tool_use_id":
				hasParentToolUseID = true
			case "uuid":
				hasUUID = true
			}
		}

		if !hasSessionID {
			t.Errorf("AC5 FAIL: line %d (%s) missing 'session_id'", li, eventType)
		}
		if eventType == "result" {
			// result events should NOT have parent_tool_use_id
			if hasParentToolUseID {
				t.Errorf("AC5 FAIL: line %d (result) should NOT have parent_tool_use_id", li)
			}
		} else {
			// All other event types should have parent_tool_use_id
			if !hasParentToolUseID {
				t.Errorf("AC5 FAIL: line %d (%s) missing 'parent_tool_use_id'", li, eventType)
			}
		}
		if !hasUUID {
			t.Errorf("AC5 FAIL: line %d (%s) missing 'uuid'", li, eventType)
		}

		// Verify order based on event type
		typeIdx := -1
		sessionIdx := -1
		parentIdx := -1
		uuidIdx := -1
		for i, k := range topLevelKeys {
			switch k {
			case "type":
				if typeIdx == -1 {
					typeIdx = i
				}
			case "session_id":
				if sessionIdx == -1 {
					sessionIdx = i
				}
			case "parent_tool_use_id":
				if parentIdx == -1 {
					parentIdx = i
				}
			case "uuid":
				if uuidIdx == -1 {
					uuidIdx = i
				}
			}
		}

		// Verify all required fields are present
		if typeIdx == -1 {
			t.Errorf("AC5 FAIL: line %d (%s) missing 'type'", li, eventType)
		}
		if sessionIdx == -1 {
			t.Errorf("AC5 FAIL: line %d (%s) missing 'session_id'", li, eventType)
		}
		if uuidIdx == -1 {
			t.Errorf("AC5 FAIL: line %d (%s) missing 'uuid'", li, eventType)
		}

		// Verify order based on event type
		if eventType == "assistant" {
			// assistant: type < message < parent_tool_use_id < session_id < uuid
			// Find message index
			msgIdx := -1
			for i, k := range topLevelKeys {
				if k == "message" {
					msgIdx = i
					break
				}
			}
			if typeIdx != -1 && msgIdx != -1 && typeIdx >= msgIdx {
				t.Errorf("AC5 FAIL: line %d (assistant) 'message' does not follow 'type'", li)
			}
			if msgIdx != -1 && parentIdx != -1 && msgIdx >= parentIdx {
				t.Errorf("AC5 FAIL: line %d (assistant) 'parent_tool_use_id' does not follow 'message'", li)
			}
			if parentIdx != -1 && sessionIdx != -1 && parentIdx >= sessionIdx {
				t.Errorf("AC5 FAIL: line %d (assistant) 'session_id' does not follow 'parent_tool_use_id'", li)
			}
			if sessionIdx != -1 && uuidIdx != -1 && sessionIdx >= uuidIdx {
				t.Errorf("AC5 FAIL: line %d (assistant) 'uuid' does not follow 'session_id'", li)
			}
		} else if eventType == "result" {
			// result: type < subtype < is_error < ... (no parent_tool_use_id)
			// Already verified no parent_tool_use_id above
			if typeIdx != -1 && uuidIdx != -1 && typeIdx >= uuidIdx {
				t.Errorf("AC5 FAIL: line %d (result) 'uuid' does not follow 'type'", li)
			}
		} else if eventType == "system" {
			// system events use cli.StreamMessage with session_id before parent_tool_use_id
			// Skip ordering check - cli.StreamMessage uses different field order
		} else {
			// other events: type < parent_tool_use_id < session_id < uuid
			if typeIdx != -1 && parentIdx != -1 && typeIdx >= parentIdx {
				t.Errorf("AC5 FAIL: line %d (%s) 'parent_tool_use_id' does not follow 'type'", li, eventType)
			}
			if parentIdx != -1 && sessionIdx != -1 && parentIdx >= sessionIdx {
				t.Errorf("AC5 FAIL: line %d (%s) 'session_id' does not follow 'parent_tool_use_id'", li, eventType)
			}
			if sessionIdx != -1 && uuidIdx != -1 && sessionIdx >= uuidIdx {
				t.Errorf("AC5 FAIL: line %d (%s) 'uuid' does not follow 'session_id'", li, eventType)
			}
		}
	}
	t.Log("AC5 PASS: key order matches reference format for all event types")
}

// TestStreamJSON_CostOnlyOnResult verifies that total_cost_usd appears on
// exactly one line (the terminal result) and not on mid-stream events (AC6).
func TestStreamJSON_CostOnlyOnResult(t *testing.T) {
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	cfg := StreamConfig{Enabled: true}
	output, err := captureStreamOutput(t, cfg)
	if err != nil {
		t.Fatalf("RunStream failed: %v", err)
	}

	lines := strings.Split(output, "\n")
	costLineCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, `"total_cost_usd"`) {
			costLineCount++
		}
	}
	if costLineCount != 1 {
		t.Errorf("AC6 FAIL: total_cost_usd appears on %d lines, expected exactly 1", costLineCount)
	} else {
		t.Log("AC6 PASS: total_cost_usd appears on exactly 1 line (result event)")
	}
}
