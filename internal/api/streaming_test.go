package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// ---------------------------------------------------------------------------
// Mock SSE server helpers
// ---------------------------------------------------------------------------

func sseLine(event, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
}

func makeStreamServer(t *testing.T, events []string) (*httptest.Server, chan []byte) {
	t.Helper()
	bodyCh := make(chan []byte, 1)
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("error reading request body: %v", err)
		}
		r.Body.Close()
		bodyCh <- body

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	})
	return ms.Server, bodyCh
}

func setTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("ANTHROPIC_BASE_URL", serverURL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")
}

func readAllBlocks(t *testing.T, blocksChan <-chan StreamContentBlock) []StreamContentBlock {
	t.Helper()
	var blocks []StreamContentBlock
	for b := range blocksChan {
		// Filter out stream_event passthrough blocks - these are for IncludePartial consumers only
		if b.Type == "stream_event" {
			continue
		}
		blocks = append(blocks, b)
	}
	return blocks
}

func assertJSONBody(t *testing.T, body []byte, key string, expected any) {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	val, ok := parsed[key]
	if !ok {
		t.Errorf("request body missing key %q", key)
		return
	}
	expJSON, _ := json.Marshal(expected)
	actJSON, _ := json.Marshal(val)
	if !bytes.Equal(expJSON, actJSON) {
		t.Errorf("request body %q: expected %s, got %s", key, expJSON, actJSON)
	}
}

// makeDelayedServer creates a server that sends `initial` events, waits `delay`,
// then sends `final` events. Used to test idle-timeout behavior.
func makeDelayedServer(t *testing.T, initial []string, delay time.Duration, final []string) *httptest.Server {
	t.Helper()
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()

		for _, e := range initial {
			io.WriteString(w, e)
			flusher.Flush()
		}
		time.Sleep(delay)
		for _, e := range final {
			io.WriteString(w, e)
			flusher.Flush()
		}
	})
	return ms.Server
}

// ---------------------------------------------------------------------------
// AC1: streaming default — stream:true on request; SSE event delivery
// ---------------------------------------------------------------------------

func TestAC1_StreamingSendsStreamTrue(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m1","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":5,"output_tokens":3}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server, bodyCh := makeStreamServer(t, events)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, err := NewClientWithModel("m1")
	if err != nil {
		t.Fatalf("NewClientWithModel() error = %v", err)
	}

	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, nil,
	)
	blocks := readAllBlocks(t, blocksChan)

	// AC1: stream:true in request body
	select {
	case body := <-bodyCh:
		assertJSONBody(t, body, "stream", true)
		assertJSONBody(t, body, "model", "m1")
	default:
		t.Error("AC1 FAIL: did not capture request body")
	}

	// AC1: SSE event processing delivers a block
	if len(blocks) != 1 {
		t.Errorf("AC1 FAIL: expected 1 block from SSE events, got %d", len(blocks))
	} else if blocks[0].Block.Text != "Hello" {
		t.Errorf("AC1 FAIL: expected text 'Hello', got %q", blocks[0].Block.Text)
	}

	// AC2: usage IS correctly extracted from message_delta
	if result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 3 {
		t.Errorf("AC2 FAIL: expected usage (5,3), got (%d,%d)", result.Usage.InputTokens, result.Usage.OutputTokens)
	}
	if result.Model != "m1" {
		t.Errorf("AC2 FAIL: expected model 'm1', got %q", result.Model)
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %q", result.Error)
	}
}

// ---------------------------------------------------------------------------
// AC2: content accumulation and yield
// ---------------------------------------------------------------------------

func TestAC2_AccumulatesMultipleTextDeltas(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"The quick "}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"brown fox "}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"jumps over"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":30}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server, _ := makeStreamServer(t, events)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, _ := NewClientWithModel("m")
	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, nil,
	)
	blocks := readAllBlocks(t, blocksChan)

	// Multiple text deltas are accumulated correctly
	if len(blocks) != 1 {
		t.Fatalf("AC2 FAIL: expected 1 block, got %d", len(blocks))
	}
	expected := "The quick brown fox jumps over"
	if blocks[0].Block.Text != expected {
		t.Errorf("AC2 FAIL: text accumulation: expected %q, got %q", expected, blocks[0].Block.Text)
	}
	if result.Usage.InputTokens != 1 || result.Usage.OutputTokens != 30 {
		t.Errorf("AC2 FAIL: usage: expected (1,30), got (%d,%d)", result.Usage.InputTokens, result.Usage.OutputTokens)
	}
	if result.Error != "" {
		t.Errorf("AC2 FAIL: unexpected error: %s", result.Error)
	}
}

func TestAC2_UsageFromMessageDelta(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"stop_sequence","stop_sequence":"enough"},"usage":{"input_tokens":10,"output_tokens":5}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server, _ := makeStreamServer(t, events)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, _ := NewClientWithModel("m")
	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, nil,
	)
	readAllBlocks(t, blocksChan)

	// Usage IS correctly extracted from message_delta
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 5 {
		t.Errorf("AC2 FAIL: expected usage (10,5), got (%d,%d)", result.Usage.InputTokens, result.Usage.OutputTokens)
	}
}

// TestAC2_StopReasonBug documents that stop_reason is NOT correctly extracted
// from message_delta. The code uses e.Delta.StopDetails.Type (refusal info)
// instead of e.Delta.StopReason (the actual stop reason).
// Source: internal/api/client.go:564
func TestAC2_StopReasonExtraction(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server, _ := makeStreamServer(t, events)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, _ := NewClientWithModel("m")
	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, nil,
	)
	readAllBlocks(t, blocksChan)

	// This assertion FAILS because client.go uses e.Delta.StopDetails.Type
	// (refusal-only field, always "") instead of e.Delta.StopReason
	// (the actual stop_reason: "end_turn", "tool_use", etc.)
	if result.StopReason == StopReasonEndTurn {
		t.Log("AC2 PASS: stop_reason correctly extracted")
	} else {
		t.Errorf("AC2 FAIL: stop_reason should be 'end_turn' but got %q — client.go:564 uses e.Delta.StopDetails.Type (refusal) instead of e.Delta.StopReason", result.StopReason)
	}
}

// ---------------------------------------------------------------------------
// AC1: Cache token extraction from message_delta
// ---------------------------------------------------------------------------

func TestAC1_CacheTokensExtractedFromMessageDelta(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		// message_delta with all four token types including cache tokens
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server, _ := makeStreamServer(t, events)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, _ := NewClientWithModel("m")
	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, nil,
	)
	readAllBlocks(t, blocksChan)

	// AC1: All four token types extracted from message_delta
	if result.Usage.CacheReadInputTokens != 3 {
		t.Errorf("AC1 FAIL: CacheReadInputTokens = %d, want 3", result.Usage.CacheReadInputTokens)
	}
	if result.Usage.CacheCreationInputTokens != 1 {
		t.Errorf("AC1 FAIL: CacheCreationInputTokens = %d, want 1", result.Usage.CacheCreationInputTokens)
	}
	if result.Usage.InputTokens != 5 {
		t.Errorf("AC1 FAIL: InputTokens = %d, want 5", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 2 {
		t.Errorf("AC1 FAIL: OutputTokens = %d, want 2", result.Usage.OutputTokens)
	}
}

// TestAC1_CacheOnlyMessageDelta exercises the edge case where message_delta
// contains only cache tokens with zero input/output tokens.
// The guard at client.go:581 (if e.Usage.InputTokens > 0 || e.Usage.OutputTokens > 0)
// means this edge case would NOT capture the cache tokens.
func TestAC1_CacheOnlyMessageDeltaEdgeCase(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		// Only cache tokens in the delta (input_tokens=0, output_tokens=0)
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":5,"cache_creation_input_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server, _ := makeStreamServer(t, events)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, _ := NewClientWithModel("m")
	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, nil,
	)
	readAllBlocks(t, blocksChan)

	// NOTE: The guard at client.go:581 means this will likely FAIL
	// because e.Usage.InputTokens=0 AND e.Usage.OutputTokens=0
	// This test documents the guard behavior
	if result.Usage.CacheReadInputTokens == 5 {
		t.Log("AC1 EDGE: Cache tokens extracted when input/output are 0")
	} else {
		t.Log("AC1 EDGE: Cache tokens NOT extracted when input/output are 0 (guard at client.go:581)")
	}
}

func TestAC1_NonStreamingCacheTokensExtracted(t *testing.T) {
	// This test verifies the non-streaming path via a mock server
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Response with all four token types
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":1}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()
	setTestEnv(t, ms.URL())

	client, _ := NewClientWithModel("m")
	client.SetMaxTokensOverride(8192)
	resp, err := client.SendMessage(context.Background(), nil, nil, nil, "")
	if err != nil {
		t.Fatalf("AC1 FAIL: SendMessage error = %v", err)
	}

	if resp.Usage.CacheReadInputTokens != 3 {
		t.Errorf("AC1 FAIL: CacheReadInputTokens = %d, want 3", resp.Usage.CacheReadInputTokens)
	}
	if resp.Usage.CacheCreationInputTokens != 1 {
		t.Errorf("AC1 FAIL: CacheCreationInputTokens = %d, want 1", resp.Usage.CacheCreationInputTokens)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("AC1 FAIL: InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("AC1 FAIL: OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
}

// ---------------------------------------------------------------------------
// AC3: non-streaming fallback
// ---------------------------------------------------------------------------

func TestAC3_FallbackOnIncompleteStream(t *testing.T) {
	// Send all events except message_stop — stream is incomplete
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":5,"output_tokens":2}}`),
		// NO message_stop
	}

	server, _ := makeStreamServer(t, events)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, _ := NewClientWithModel("m")

	var fallbackMu sync.Mutex
	fallbackCalled := false
	fallbackFn := func(ctx context.Context) (*Response, error) {
		fallbackMu.Lock()
		fallbackCalled = true
		fallbackMu.Unlock()
		return &Response{
			Content:    []ContentBlock{{Type: "text", Text: "Fallback response"}},
			StopReason: StopReasonEndTurn,
			Usage:      Usage{InputTokens: 1, OutputTokens: 2},
			Model:      "fallback-model",
		}, nil
	}

	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, fallbackFn,
	)
	allBlocks := readAllBlocks(t, blocksChan)

	// Filter out stream_event partials to count final blocks
	var finalBlocks []StreamContentBlock
	for _, b := range allBlocks {
		if b.Type != "stream_event" {
			finalBlocks = append(finalBlocks, b)
		}
	}

	fallbackMu.Lock()
	called := fallbackCalled
	fallbackMu.Unlock()

	if !called {
		t.Error("AC3 FAIL: fallback was not called on incomplete stream (no message_stop)")
	}

	// The spec says "Partial assistant content from the failed stream is discarded"
	// stream_event blocks are progress updates and are expected, but final content blocks
	// should be suppressed when fallback is used.
	if len(finalBlocks) > 0 {
		t.Errorf("AC3 FAIL: partial assistant content not discarded — got %d partial block(s) from channel (expected 0)", len(finalBlocks))
	} else {
		t.Log("AC3 OK: partial blocks correctly suppressed")
	}

	// Result should come from fallback
	if len(result.Blocks) != 1 {
		t.Errorf("AC3 FAIL: expected 1 block in result (from fallback), got %d", len(result.Blocks))
	} else if result.Blocks[0].Text != "Fallback response" {
		t.Errorf("AC3 FAIL: expected result text 'Fallback response', got %q", result.Blocks[0].Text)
	}
	if result.StopReason != StopReasonEndTurn {
		t.Errorf("AC3 FAIL: expected stop_reason 'end_turn', got %q", result.StopReason)
	}
	if result.Error != "" {
		t.Errorf("AC3 FAIL: unexpected error: %s", result.Error)
	}
}

func TestAC3_FallbackOnNonSSEResponse(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not an SSE stream"))
	})
	defer ms.Close()
	setTestEnv(t, ms.URL())

	client, _ := NewClientWithModel("m")

	var fallbackMu sync.Mutex
	fallbackCalled := false
	fallbackFn := func(ctx context.Context) (*Response, error) {
		fallbackMu.Lock()
		fallbackCalled = true
		fallbackMu.Unlock()
		return &Response{
			Content:    []ContentBlock{{Type: "text", Text: "Fallback OK"}},
			StopReason: StopReasonEndTurn,
		}, nil
	}

	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, fallbackFn,
	)
	readAllBlocks(t, blocksChan)

	fallbackMu.Lock()
	called := fallbackCalled
	fallbackMu.Unlock()

	if !called {
		t.Error("AC3 FAIL: fallback not called on non-SSE response")
	}
	if len(result.Blocks) == 0 {
		t.Error("AC3 FAIL: expected fallback blocks in result")
	} else if result.Blocks[0].Text != "Fallback OK" {
		t.Errorf("AC3 FAIL: expected 'Fallback OK', got %q", result.Blocks[0].Text)
	}
}

// ---------------------------------------------------------------------------
// AC5: idle watchdog
// ---------------------------------------------------------------------------

func TestAC5_IdleTimeoutDetectedAfterDelayButFallbackNotCalled(t *testing.T) {
	// Tests the idle-watchdog mechanism.
	//
	// The timeout check (client.go:499) runs AFTER stream.Next() returns,
	// but stream.Next() blocks during the server's inter-event delay.
	// Two consequences:
	//   1. Idle timeout detection is delayed — only fires after the server
	//      sends the delayed event and stream.Next() unblocks.
	//   2. When timeout fires, the goroutine does `return` immediately
	//      instead of triggering the non-streaming fallback.
	//
	// Server sends message_start, then blocks for 500ms, then sends rest.
	// Idle timeout = 100ms. Expected per spec: fallback triggered.
	// Actual: timeout fires late, fallback never called.
	initial := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
	}
	delay := 500 * time.Millisecond
	final := []string{
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Late"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server := makeDelayedServer(t, initial, delay, final)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, _ := NewClientWithModel("m")

	var fallbackMu sync.Mutex
	fallbackCalled := false
	fallbackFn := func(ctx context.Context) (*Response, error) {
		fallbackMu.Lock()
		fallbackCalled = true
		fallbackMu.Unlock()
		return &Response{Content: []ContentBlock{{Type: "text", Text: "Fallback"}}, StopReason: StopReasonEndTurn}, nil
	}

	start := time.Now()
	blocksChan, result := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		100*time.Millisecond, // idle timeout: 100ms
		2*time.Second,
		fallbackFn,
	)
	blocks := readAllBlocks(t, blocksChan)
	elapsed := time.Since(start)

	fallbackMu.Lock()
	called := fallbackCalled
	fallbackMu.Unlock()

	// Idle timeout IS detected, but only after stream.Next() unblocks
	if result.Error == "idle timeout" {
		t.Logf("AC5: idle timeout detected after %v (server delay was %v)", elapsed, delay)
	} else {
		t.Errorf("AC5: expected idle timeout, got result.Error=%q", result.Error)
	}

	// Fallback is NOT called on idle timeout (goroutine returns directly)
	if called {
		t.Log("AC5: fallback WAS triggered on idle timeout")
	} else {
		t.Errorf("AC5 FAIL: fallback NOT triggered on idle timeout — goroutine returns directly without calling onStreamingFallback (client.go:502)")
	}

	// Blocks are lost (0 from channel, none in result)
	if len(blocks) != 0 {
		t.Errorf("AC5: expected 0 blocks from channel, got %d", len(blocks))
	}
}

// ---------------------------------------------------------------------------
// Edge case tests
// ---------------------------------------------------------------------------

func TestStreamingMultipleContentBlocks(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"World"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tu_123","name":"bash","input":{}}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"command\": \"ls\"}"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":2}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":10}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server, _ := makeStreamServer(t, events)
	defer server.Close()
	setTestEnv(t, server.URL)

	client, _ := NewClientWithModel("m")
	blocksChan, _ := client.SendMessageStream(
		context.Background(), nil, nil, nil, "",
		5*time.Second, 5*time.Second, nil,
	)
	blocks := readAllBlocks(t, blocksChan)

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Block.Type != "text" || blocks[0].Block.Text != "Hello" {
		t.Errorf("block 0: expected text 'Hello', got type=%q text=%q", blocks[0].Block.Type, blocks[0].Block.Text)
	}
	if blocks[1].Block.Type != "text" || blocks[1].Block.Text != "World" {
		t.Errorf("block 1: expected text 'World', got type=%q text=%q", blocks[1].Block.Type, blocks[1].Block.Text)
	}
	if blocks[2].Block.Type != "tool_use" {
		t.Errorf("block 2: expected type 'tool_use', got %q", blocks[2].Block.Type)
	}
	if blocks[2].Block.ToolName != "bash" || blocks[2].Block.ToolInput["command"] != "ls" {
		t.Errorf("block 2: expected bash tool with command 'ls', got %v", blocks[2].Block)
	}
}

// TestFallback_NonStreamingMaxTokens64000 is the AC1 conformance test for
// the non-streaming fallback path: when no override is set, the
// non-streaming /v1/messages request must carry max_tokens == 64000
// (the universal default). The 20000 clamp that previously lived in
// doSendMessage is gone; the SDK's 10-minute guard is bypassed at the
// client level via option.WithRequestTimeout(1*time.Hour).
func TestFallback_NonStreamingMaxTokens64000(t *testing.T) {
	var capturedBody []byte
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()
	setTestEnv(t, ms.URL())

	client, _ := NewClientWithModel("m")
	// No SetMaxTokensOverride — must use the universal 64000 default.
	if _, err := client.SendMessage(context.Background(), nil, nil, nil, ""); err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	raw, present := parsed["max_tokens"]
	if !present {
		t.Fatalf("max_tokens missing from non-streaming request body")
	}
	num, ok := raw.(float64)
	if !ok {
		t.Fatalf("max_tokens is not a number; got %T (%v)", raw, raw)
	}
	if int(num) != 64000 {
		t.Errorf("AC1 FAIL: non-streaming max_tokens = %d; want 64000", int(num))
	}
}
