package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/session"
)

func sseLine(event, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
}

func makeMockStreamServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume and discard request body
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

		// Send a complete streaming response (text block, end_turn)
		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from stream"}}`),
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

// TestAC4_StreamRequestStartEmitted verifies that RunStream emits
// stream_request_start before each API iteration when streaming is enabled.
func TestAC4_StreamRequestStartEmitted(t *testing.T) {
	server := makeMockStreamServer(t)
	defer server.Close()

	// Redirect SDK to our mock server
	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key-00000")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

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
	server := makeMockStreamServer(t)
	defer server.Close()

	origBaseURL := os.Getenv("ANTHROPIC_BASE_URL")
	origAPIKey := os.Getenv("ANTHROPIC_API_KEY")
	os.Setenv("ANTHROPIC_BASE_URL", server.URL)
	os.Setenv("ANTHROPIC_API_KEY", "test-key-00000")
	defer func() {
		os.Setenv("ANTHROPIC_BASE_URL", origBaseURL)
		os.Setenv("ANTHROPIC_API_KEY", origAPIKey)
	}()

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
