package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/testutil"
)

// captureStdout delegates to testutil.CaptureStdout for stdout capture.
var captureStdout = testutil.CaptureStdout

func makeMockStreamServerWithPartialEvents(t *testing.T) *httptest.Server {
	t.Helper()
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

		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	}))
}

func makeMockStreamServerWithCacheTokens(t *testing.T) *httptest.Server {
	t.Helper()
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

		// SSE events with all four token types in message_delta usage
		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from stream"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			// message_delta with all four token types including cache tokens (AC1, AC4)
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":1}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		for _, e := range events {
			fmt.Fprint(w, e)
			flusher.Flush()
		}
	}))
}

// makeMockStreamServerWithEvents creates a mock SSE server with explicit event slice.
func makeMockStreamServerWithEvents(t *testing.T, events []string) *httptest.Server {
	t.Helper()
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

		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	}))
}

// TestHelpers verifies that the SSE mock server helpers produce valid servers.
func TestHelpers(t *testing.T) {
	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message"}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}
	server := makeMockStreamServerWithEvents(t, events)
	defer server.Close()

	// Verify server is reachable and returns SSE headers
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type: text/event-stream, got: %s", resp.Header.Get("Content-Type"))
	}
}

// parseAssistantEvents returns all parsed StreamMessage envelopes of type
// "assistant" found in the NDJSON output, preserving original order.
func parseAssistantEvents(t *testing.T, ndjson string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(ndjson, "\n") {
		if !strings.Contains(line, `"type":"assistant"`) {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("unmarshal assistant line: %v\nline: %s", err, line)
		}
		out = append(out, msg)
	}
	return out
}

// parseNDJSONLines parses output into a slice of map[string]any for each line.
func parseNDJSONLines(t *testing.T, output string) []map[string]any {
	var result []map[string]any
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Logf("Warning: failed to parse JSON line: %q, error: %v", line, err)
			continue
		}
		result = append(result, m)
	}
	return result
}
