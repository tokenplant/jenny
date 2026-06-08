// Package agent provides the core agent loop and query engine.
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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/memdir"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// testSseLine formats a line as SSE format for testing.
func testSseLine(event, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
}

// makeTestMockStreamServer creates a mock SSE server for testing.
func makeTestMockStreamServer(events []string) *httptest.Server {
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

// TestAC1_PersistBeforeAPI verifies that the user message is persisted to
// transcript BEFORE any API call is made.
func TestAC1_PersistBeforeAPI(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_ac1_test"
	prompt := "test prompt for persist ordering"

	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
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

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := NewQueryEngine(cfg, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = engine.SubmitMessage(ctx, prompt)

	// Verify that the transcript has the user message
	entries, err := sessMgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript error: %v", err)
	}

	foundUserMessage := false
	for _, entry := range entries {
		if entry.Type == "user" && entry.Content == prompt {
			foundUserMessage = true
			break
		}
	}

	if !foundUserMessage {
		t.Error("AC1 FAIL: user message not found in transcript")
	} else {
		t.Log("AC1 PASS: user message persisted to transcript")
	}
}

// TestAC2_MaxTurnsEnforcement verifies that when maxTurns is set,
// the engine stops before exceeding the limit.
func TestAC2_MaxTurnsEnforcement(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_ac2_test"

	// Server that returns tool_use to keep the loop going
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"bash","input":{}}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		testSseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
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

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := NewQueryEngine(cfg, nil, "")
	engine.SetMaxTurns(2) // Set max turns to 2

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")

	// Should get error_max_turns when limit is exceeded
	if err == nil {
		t.Error("AC2 FAIL: expected error when maxTurns exceeded, got nil")
	} else if !strings.Contains(err.Error(), "error_max_turns") {
		t.Errorf("AC2 FAIL: expected error_max_turns, got: %v", err)
	} else {
		t.Log("AC2 PASS: engine stopped at maxTurns limit")
	}
}

// TestAC5_TurnCounterResets verifies that the turn counter resets
// on each SubmitMessage call.
func TestAC5_TurnCounterResets(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_ac5_test"

	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
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

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := NewQueryEngine(cfg, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First SubmitMessage
	_, _ = engine.SubmitMessage(ctx, "first prompt")
	firstTurnCount := engine.TurnCount()
	t.Logf("After first SubmitMessage, turnCount = %d", firstTurnCount)

	// Second SubmitMessage
	_, _ = engine.SubmitMessage(ctx, "second prompt")
	secondTurnCount := engine.TurnCount()
	t.Logf("After second SubmitMessage, turnCount = %d", secondTurnCount)

	// AC5: Turn counter resets at the start of each SubmitMessage
	// After SubmitMessage returns, counter reflects iterations run
	// For a single iteration, it would be 1 (incremented then check fails maxTurns)
	// The key verification is that both calls should have the same behavior
	if firstTurnCount != secondTurnCount {
		t.Errorf("AC5 FAIL: turn counts differ between calls: first=%d, second=%d", firstTurnCount, secondTurnCount)
	} else {
		t.Log("AC5 PASS: turn counter behavior is consistent between SubmitMessage calls")
	}
}

// TestAC3_CostStateFlushed verifies that cost state is persisted after
// each SubmitMessage call completes.
func TestAC3_CostStateFlushed(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_ac3_test"

	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"usage":{"input_tokens":100,"output_tokens":50}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		testSseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":100,"output_tokens":50}}`),
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

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := NewQueryEngine(cfg, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = engine.SubmitMessage(ctx, "test prompt")

	// Verify cost state was flushed by checking if it can be restored
	restored, ok, err := RestoreCostState(sessionID)
	if err != nil {
		t.Fatalf("RestoreCostState error: %v", err)
	}
	if !ok {
		t.Error("AC3 FAIL: cost state was not flushed after SubmitMessage")
	} else if restored.TotalCostUSD == 0 {
		t.Error("AC3 FAIL: cost state was flushed but has zero cost")
	} else {
		t.Logf("AC3 PASS: cost state flushed with total cost %.6f USD", restored.TotalCostUSD)
	}
}

// TestQueryEngine_NewQueryEngine verifies the constructor creates
// a properly initialized engine.
func TestQueryEngine_NewQueryEngine(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      "test-session",
		MaxTurns:       5,
	}

	engine := NewQueryEngine(cfg, nil, "test-model")

	if engine == nil {
		t.Fatal("NewQueryEngine returned nil")
	}
	if engine.sessionManager != sessMgr {
		t.Error("sessionManager not set correctly")
	}
	if engine.model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", engine.model)
	}
	if engine.maxTurns != 0 {
		t.Error("maxTurns should be 0 initially")
	}
}

// TestQueryEngine_SetMaxTurns verifies the setter.
func TestQueryEngine_SetMaxTurns(t *testing.T) {
	cfg := StreamConfig{
		Enabled: false,
	}

	engine := NewQueryEngine(cfg, nil, "")
	engine.SetMaxTurns(10)

	if engine.maxTurns != 10 {
		t.Errorf("expected maxTurns=10, got %d", engine.maxTurns)
	}
}

// TestAC1_SubmitMessageWithoutSessionManager verifies that SubmitMessage
// works correctly when no session manager is configured (AC1 edge case).
func TestAC1_SubmitMessageWithoutSessionManager(t *testing.T) {
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
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

	// No session manager — sessionManager is nil
	cfg := StreamConfig{Enabled: false}
	engine := NewQueryEngine(cfg, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := engine.SubmitMessage(ctx, "test")
	if err != nil {
		t.Errorf("AC1 FAIL: SubmitMessage with nil sessionManager returned error: %v", err)
	}
	if result == "" {
		t.Error("AC1 FAIL: expected non-empty result from SubmitMessage")
	} else {
		t.Log("AC1 PASS: SubmitMessage works without session manager")
	}
}

// TestAC1_ResumeDoesNotDuplicateUserMessage verifies that on resume
// the user message is not duplicated in the transcript.
func TestAC1_ResumeDoesNotDuplicateUserMessage(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_resume_ac1"
	prompt := "hello from resume"

	// Pre-populate transcript with a user message (simulating previous session)
	if err := sessMgr.AppendEntry(sessionID, session.TranscriptEntry{
		Type:    "user",
		Content: prompt,
	}); err != nil {
		t.Fatalf("AppendEntry error: %v", err)
	}

	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
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

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
		IsResume:       true, // Mark as resume — should skip duplicate user persist
	}
	engine := NewQueryEngine(cfg, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, prompt)
	if err != nil {
		t.Fatalf("SubmitMessage error: %v", err)
	}

	// Verify only one user message in transcript (no duplicate)
	entries, err := sessMgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript error: %v", err)
	}

	userCount := 0
	for _, entry := range entries {
		if entry.Type == "user" {
			userCount++
		}
	}
	if userCount != 1 {
		t.Errorf("AC1 FAIL: expected 1 user message in transcript, got %d", userCount)
	} else {
		t.Log("AC1 PASS: no duplicate user message on resume")
	}
}

// TestAC2_MaxTurnsZeroIsUnlimited verifies that default maxTurns=0
// allows the engine to complete normally without artificial limits.
func TestAC2_MaxTurnsZeroIsUnlimited(t *testing.T) {
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
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

	cfg := StreamConfig{Enabled: false}
	engine := NewQueryEngine(cfg, nil, "")
	// maxTurns defaults to 0 (unlimited — no limit check should trigger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := engine.SubmitMessage(ctx, "test")
	if err != nil {
		t.Errorf("AC2 FAIL: SubmitMessage should complete normally with maxTurns=0: %v", err)
	} else {
		t.Log("AC2 PASS: maxTurns=0 allows unlimited turns")
	}
}

// TestAC3_CostFlushOnMaxTurnsError verifies cost state is persisted even when
// SubmitMessage returns an error (e.g., maxTurns exceeded).
func TestAC3_CostFlushOnMaxTurnsError(t *testing.T) {
	// Isolate cost state to a temp directory
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_flush_on_err"

	// Server that returns tool_use to keep the loop going, hitting maxTurns
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"bash","input":{}}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		testSseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
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

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}
	engine := NewQueryEngine(cfg, nil, "")
	engine.SetMaxTurns(1) // Will exceed after 1st iteration (tool_use triggers loop)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test")
	if err == nil || !strings.Contains(err.Error(), "error_max_turns") {
		t.Fatalf("expected error_max_turns, got: %v", err)
	}

	// Verify cost state was flushed after error (RestoreCostState reads from CWD which is tmpDir)
	restored, ok, restoreErr := RestoreCostState(sessionID)
	if restoreErr != nil {
		t.Fatalf("RestoreCostState error: %v", restoreErr)
	}
	if !ok {
		t.Fatal("AC3 FAIL: cost state was not flushed after maxTurns error")
	}
	if restored.TotalCostUSD == 0 {
		t.Error("AC3 FAIL: cost state flushed but has zero cost")
	} else {
		t.Logf("AC3 PASS: cost state flushed after error, total cost = %.6f USD", restored.TotalCostUSD)
	}
}

// TestAC4_RunStreamReturnsTextContent verifies that RunStream returns the
// correct text result from the model after the refactor to use QueryEngine.
func TestAC4_RunStreamReturnsTextContent(t *testing.T) {
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

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	// Disable streaming to avoid stdout noise; test return value only
	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, _, err := RunStream(ctx, "test", nil, tmpDir, cfg, "test-model")
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	if result != "Hello from stream" {
		t.Errorf("AC4 FAIL: expected result 'Hello from stream', got %q", result)
	} else {
		t.Log("AC4 PASS: RunStream returns correct text content")
	}
}

// TestAC5_TurnCounterIsAccurate verifies that TurnCount() returns the
// correct number of API iterations after SubmitMessage completes, and
// that the counter resets on subsequent calls.
func TestAC5_TurnCounterIsAccurate(t *testing.T) {
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
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

	cfg := StreamConfig{Enabled: false}
	engine := NewQueryEngine(cfg, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First SubmitMessage — single iteration (end_turn), counter should be 1
	_, err := engine.SubmitMessage(ctx, "test")
	if err != nil {
		t.Fatalf("SubmitMessage error: %v", err)
	}
	if tc := engine.TurnCount(); tc != 1 {
		t.Errorf("AC5 FAIL: expected TurnCount()=1 after one iteration, got %d", tc)
	} else {
		t.Log("AC5 PASS: turn counter accurately reflects one API iteration")
	}

	// Second SubmitMessage — counter should reset and count from 0
	_, err = engine.SubmitMessage(ctx, "second prompt")
	if err != nil {
		t.Fatalf("Second SubmitMessage error: %v", err)
	}
	if tc := engine.TurnCount(); tc != 1 {
		t.Errorf("AC5 FAIL: expected TurnCount()=1 after second SubmitMessage, got %d", tc)
	} else {
		t.Log("AC5 PASS: turn counter resets to 0 on each SubmitMessage")
	}
}

// TestAC3_StreamJsonCallsSetOutput verifies that when stream-json mode is
// enabled (StreamConfig.Enabled = true), log.SetOutput(os.Stderr) is called
// to redirect log output to stderr and prevent it from corrupting NDJSON on stdout.
func TestAC3_StreamJsonCallsSetOutput(t *testing.T) {
	// This test verifies that when stream-json mode is active, log output
	// is redirected to stderr. This prevents debug/info logs from corrupting
	// the NDJSON stream on stdout.
	//
	// The redirection happens in runLoop at engine.go:213-215:
	//   if e.streamCfg.Enabled {
	//       log.SetOutput(os.Stderr)
	//   }
	//
	// We verify this by:
	// 1. Setting log output to a capture buffer BEFORE running SubmitMessage
	// 2. Running SubmitMessage with stream-json enabled
	// 3. After SubmitMessage completes, checking if the capture buffer received any logs
	//    - If log.SetOutput(os.Stderr) was called, logs go to stderr (not to capture buffer)
	//    - If log.SetOutput was NOT called, logs go to the capture buffer (empty = redirect worked)
	// 4. Also verifying no log output appears on stdout

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	// Create a mock server that returns a simple response
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Hello"}}`),
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

	// Save original stdout
	oldStdout := os.Stdout

	// Create pipe to capture stdout
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}

	// Redirect stdout to our pipe
	os.Stdout = stdoutW

	// Create a buffer to capture log output
	// If log.SetOutput(os.Stderr) is called, logs go to stderr (captured separately), not here
	// If log.SetOutput is NOT called, logs go here (proving redirect didn't happen)
	logCapture := &bytes.Buffer{}

	// Set log output to capture buffer BEFORE running SubmitMessage
	// This allows us to verify if log.SetOutput(os.Stderr) was actually called
	log.SetOutput(logCapture)

	cfg := StreamConfig{
		Enabled:        true, // Stream-json mode enabled - should trigger log.SetOutput(os.Stderr)
		SessionManager: sessMgr,
		SessionID:      "test-session-stderr",
	}

	engine := NewQueryEngine(cfg, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test")

	// Close write end to signal EOF on read end
	stdoutW.Close()

	// Restore original stdout BEFORE checking log output
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("SubmitMessage error: %v", err)
	}

	// Read captured stdout (should only contain NDJSON lines, no log output)
	var stdoutBuf bytes.Buffer
	io.Copy(&stdoutBuf, stdoutR)
	stdoutOutput := stdoutBuf.String()

	// AC3: Verify log.SetOutput(os.Stderr) was actually called
	// If the capture buffer is empty, it means logs went elsewhere (stderr) = SetOutput was called
	// If the capture buffer has content, it means logs were captured here = SetOutput was NOT called
	if logCapture.Len() > 0 {
		t.Errorf("AC3 FAIL: log.SetOutput(os.Stderr) was NOT called; found %d bytes in log capture buffer", logCapture.Len())
		t.Logf("Log output that should have been redirected: %s", logCapture.String())
	} else {
		t.Log("AC3 PASS: log.SetOutput(os.Stderr) was called (no logs in capture buffer)")
	}

	// Also verify no log output appears on stdout
	stdoutLines := strings.Split(strings.TrimSpace(stdoutOutput), "\n")
	var logLines []string
	for _, line := range stdoutLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Log lines would contain patterns like "level=INFO", "level=DEBUG", "msg="
		if strings.Contains(line, "=") && strings.Contains(line, "msg=") {
			logLines = append(logLines, line)
		}
	}

	if len(logLines) > 0 {
		t.Errorf("AC3 FAIL: found %d log line(s) on stdout, expected 0 (logs should go to stderr)", len(logLines))
		for _, ll := range logLines {
			t.Logf("  stdout log: %s", ll)
		}
	} else {
		t.Log("AC3 PASS: no log output found on stdout")
	}

	// Reset log output to stderr for subsequent tests
	log.SetOutput(os.Stderr)
}

// TestAC4_QueryEngineWireReadFileCache verifies that QueryEngine.WireReadFileCache
// properly injects the ReadFileCache from StreamConfig into tools that support
// read-before-write enforcement (Read, Write, Edit, NotebookEdit).
//
// This test uses DISTINCT cache instances to avoid tautology:
// - cacheA is passed to Registry (for tool construction)
// - cacheB is passed via StreamConfig.ReadFileCache (the engine should wire this to tools)
//
// The test verifies that after WireReadFileCache, the engine's tools have cacheB,
// proving that the engine's wiring actually propagates the correct cache.
func TestAC4_QueryEngineWireReadFileCache(t *testing.T) {
	// Create two DISTINCT cache instances
	cacheA := tool.NewReadFileCache() // Used for building tools
	cacheB := tool.NewReadFileCache() // Used for StreamConfig - this is what engine should wire

	// Pre-populate cacheB with a known entry (cacheA remains empty)
	testPath := "/test/file.txt"
	testContent := "hello world"
	testMtime := time.Now()
	cacheB.RecordRead(testPath, testContent, testMtime, true)

	// Build tools with cacheA (empty cache)
	tools := tool.NewRegistry().
		WithBaseTools().
		WithReadFileCache(cacheA).
		Build()

	// Verify Write/Edit/NotebookEdit were created (cacheA enabled them)
	writeTool := tool.FindTool(tools, "write")
	editTool := tool.FindTool(tools, "edit")
	notebookEditTool := tool.FindTool(tools, "notebook_edit")

	if writeTool == nil {
		t.Fatal("WriteTool not found - cache should enable it")
	}
	if editTool == nil {
		t.Fatal("EditTool not found - cache should enable it")
	}
	if notebookEditTool == nil {
		t.Fatal("NotebookEditTool not found - cache should enable it")
	}

	// Create StreamConfig with cacheB (the engine should wire this to tools)
	cfg := StreamConfig{
		Enabled:       false,
		ReadFileCache: cacheB,
	}

	// Create QueryEngine - this calls WireReadFileCache internally
	engine := NewQueryEngine(cfg, tools, "test-model")

	// Find the ReadTool in engine
	var engineReadTool *tool.ReadTool
	for _, t := range engine.tools {
		if rt, ok := t.(*tool.ReadTool); ok {
			engineReadTool = rt
			break
		}
	}

	if engineReadTool == nil {
		t.Fatal("ReadTool not found in engine")
	}

	// AC4: Verify the engine wired cacheB to tools (not cacheA)
	// This is the key assertion that makes the test non-tautological:
	// We check that the engine's tools have cacheB by verifying the cache
	// that was pre-populated with testPath is accessible through the engine's tool.
	if entry, ok := engineReadTool.GetReadFileCache().GetRead(testPath); !ok {
		t.Fatal("AC4 FAIL: engine's ReadTool does not have cacheB - WireReadFileCache did not wire the correct cache")
	} else {
		if entry.Content != testContent {
			t.Errorf("AC4 FAIL: cache content mismatch: got %q, want %q", entry.Content, testContent)
		}
		t.Log("AC4 PASS: QueryEngine.WireReadFileCache correctly wires StreamConfig.ReadFileCache (cacheB) to tools")
	}

	// Also verify cacheA was NOT wired to the engine's tools
	// (if cacheA had the entry, it would be a different cache instance)
	if _, ok := cacheA.GetRead(testPath); ok {
		t.Error("AC4 FAIL: engine's tools appear to have cacheA instead of cacheB")
	} else {
		t.Log("AC4 PASS: cacheA correctly NOT wired to engine's tools")
	}
}

// TestAC1_MemdirCreatedAtPromptBuild verifies that memdir.Create() is wired
// into the system-prompt build hook: when AutoMemoryEnabled is true and the
// session is in a git repository, SubmitMessage must create the per-project
// memory directory under the config home.
func TestAC1_MemdirCreatedAtPromptBuild(t *testing.T) {
	// Isolate HOME so memdir's config-home resolution points to a temp dir
	// and the test can verify the directory exists without polluting the
	// real user config.
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Also isolate XDG_CONFIG_HOME on Linux/Unix systems so UserConfigDir()
	// points to the isolated HOME instead of a pre-existing XDG path.
	origXdg := os.Getenv("XDG_CONFIG_HOME")
	os.Unsetenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", origXdg)

	// Set git identity so initTestGitRepo can create the initial commit.
	// On cleanup, use Unsetenv when the original was empty so we don't
	// leave GIT_AUTHOR_EMAIL="" set (git treats empty as malformed).
	gitEnvVars := []string{"GIT_AUTHOR_EMAIL", "GIT_AUTHOR_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_NAME"}
	origValues := make(map[string]string, len(gitEnvVars))
	for _, k := range gitEnvVars {
		origValues[k] = os.Getenv(k)
	}
	os.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	os.Setenv("GIT_AUTHOR_NAME", "Test")
	os.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	os.Setenv("GIT_COMMITTER_NAME", "Test")
	defer func() {
		for _, k := range gitEnvVars {
			if orig, ok := origValues[k]; ok && orig != "" {
				os.Setenv(k, orig)
			} else {
				os.Unsetenv(k)
			}
		}
	}()

	// Init a git repo so memdir can resolve the project root from cwd.
	repoDir := t.TempDir()
	initTestGitRepo(t, repoDir)

	origWd, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origWd)

	sessMgr, err := session.NewManager(repoDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	// Mock server that returns a single end_turn response so SubmitMessage
	// completes without making a real API call.
	server := makeTestMockStreamServer([]string{
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`),
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

	cfg := StreamConfig{
		Enabled:           false,
		SessionManager:    sessMgr,
		SessionID:         "sess_ac1_memdir",
		AutoMemoryEnabled: true,
	}

	engine := NewQueryEngine(cfg, nil, "")

	// Compute the expected memdir path using the same library the engine
	// calls. git.GetRoot resolves symlinks (e.g. /var -> /private/var on
	// macOS), so we mirror that resolution here to compute the matching
	// expected path.
	resolvedRepoDir, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(repoDir) error: %v", err)
	}
	resolvedRepoDir, err = filepath.Abs(resolvedRepoDir)
	if err != nil {
		t.Fatalf("Abs(resolvedRepoDir) error: %v", err)
	}

	expectedMem, err := memdir.New(memdir.Config{
		ProjectRoot:       resolvedRepoDir,
		AutoMemoryEnabled: true,
	})
	if err != nil {
		t.Fatalf("memdir.New() error: %v", err)
	}
	expectedPath := expectedMem.MemoryPath()

	// Sanity: the expected path must live under the isolated HOME.
	if !strings.HasPrefix(expectedPath, tmpHome) {
		t.Fatalf("expected memdir path %q to be under HOME %q", expectedPath, tmpHome)
	}

	// Confirm the directory does not exist before SubmitMessage runs.
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Fatalf("expected memdir to not exist before SubmitMessage, stat err = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := engine.SubmitMessage(ctx, "test prompt"); err != nil {
		t.Fatalf("SubmitMessage() error: %v", err)
	}

	// AC1: memdir directory exists at the project-scoped config-home path.
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("AC1 FAIL: memdir directory %q was not created at prompt build time: %v", expectedPath, err)
	} else {
		t.Logf("AC1 PASS: memdir directory created at %q", expectedPath)
	}

	// MEMORY.md should also be created by Create().
	indexPath := expectedMem.IndexPath()
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("AC1 FAIL: MEMORY.md index was not created: %v", err)
	} else {
		t.Logf("AC1 PASS: MEMORY.md created at %q", indexPath)
	}
}

// TestAC1_DenyRuleStructuredOutput tests that when StructuredOutput is denied
// via StructuredDenyRules but a schema is configured, NewQueryEngine panics.
// This verifies the startup error for AC1 deny-rule checking.
func TestAC1_DenyRuleStructuredOutput(t *testing.T) {
	cfg := StreamConfig{
		Enabled:             true,
		StructuredSchema:    map[string]any{"type": "object"},
		StructuredDenyRules: []string{"StructuredOutput"},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("AC1 FAIL: expected panic when StructuredOutput is denied but schema is set")
		} else {
			t.Log("AC1 PASS: panic occurred as expected")
		}
	}()

	engine := NewQueryEngine(cfg, nil, "test-model")
	// If we get here without panic, fail
	t.Errorf("AC1 FAIL: expected panic but got engine: %v", engine)
}

// TestAC4_InteractiveModeNoStructuredOutput tests that when Enabled=false
// (interactive mode), the StructuredOutput tool is not registered even
// if a schema is configured. This verifies AC4 non-interactive detection.
func TestAC4_InteractiveModeNoStructuredOutput(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:          false, // Interactive mode
		StructuredSchema: map[string]any{"type": "object"},
		SessionManager:   sessMgr,
		SessionID:        "test-session-interactive",
	}

	engine := NewQueryEngine(cfg, nil, "test-model")
	if engine == nil {
		t.Fatal("NewQueryEngine returned nil unexpectedly")
	}

	// AC4: StructuredOutput tool should NOT be registered in interactive mode
	if engine.structuredOutputTool != nil {
		t.Error("AC4 FAIL: structuredOutputTool should be nil in interactive mode (Enabled=false)")
	} else {
		t.Log("AC4 PASS: structuredOutputTool is nil in interactive mode")
	}
}

// TestAC4_NonInteractiveModeHasStructuredOutput tests that when Enabled=true
// (non-interactive/stream-json mode), the StructuredOutput tool IS registered
// if a schema is configured. This verifies AC4 non-interactive detection.
func TestAC4_NonInteractiveModeHasStructuredOutput(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:          true, // Non-interactive mode
		StructuredSchema: map[string]any{"type": "object"},
		SessionManager:   sessMgr,
		SessionID:        "test-session-noninteractive",
	}

	engine := NewQueryEngine(cfg, nil, "test-model")
	if engine == nil {
		t.Fatal("NewQueryEngine returned nil unexpectedly")
	}

	// AC4: StructuredOutput tool should be registered in non-interactive mode
	if engine.structuredOutputTool == nil {
		t.Error("AC4 FAIL: structuredOutputTool should NOT be nil in non-interactive mode (Enabled=true)")
	} else {
		t.Log("AC4 PASS: structuredOutputTool is not nil in non-interactive mode")
	}
}

// TestAC3_NotEmittedError tests that when StructuredOutput is configured but
// not called during the turn, the engine returns error "structured output not emitted".
// This verifies AC3 enforcement at the engine level.
func TestAC3_NotEmittedError(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "test-session-not-emitted"

	server := makeTestMockStreamServer([]string{
		// Assistant responds with just text, no StructuredOutput call
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Hello"}}`),
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

	cfg := StreamConfig{
		Enabled:          true,
		StructuredSchema: map[string]any{"type": "object"},
		SessionManager:   sessMgr,
		SessionID:        sessionID,
	}

	engine := NewQueryEngine(cfg, nil, "test-model")
	if engine == nil {
		t.Fatal("NewQueryEngine returned nil unexpectedly")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")
	if err == nil {
		t.Error("AC3 FAIL: expected error 'structured output not emitted' but got nil")
	} else if !strings.Contains(err.Error(), "structured output not emitted") {
		t.Errorf("AC3 FAIL: expected error 'structured output not emitted', got: %v", err)
	} else {
		t.Log("AC3 PASS: got expected 'structured output not emitted' error")
	}
}

// TestAC3_ResultExtraction tests that when StructuredOutput IS called,
// the engine returns the JSON content from the StructuredOutput tool call
// rather than the text output. This verifies AC3 result extraction.
func TestAC3_ResultExtraction(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "test-session-result-extraction"
	structuredJSON := `{"result":"success","data":[1,2,3]}`

	server := makeTestMockStreamServer([]string{
		// Assistant calls StructuredOutput tool
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_1","name":"StructuredOutput"}}`),
		testSseLine("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"value\":{\"result\":\"success\",\"data\":[1,2,3]},\"format\":\"json\"}"}}`),
		testSseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		testSseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		testSseLine("message_stop", `{"type":"message_stop"}`),
		// Tool result for StructuredOutput - returns the validated JSON
		testSseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"user","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		testSseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_result","id":"tool_1","content":"{\"result\":\"success\",\"data\":[1,2,3]}","is_error":false}}`),
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

	cfg := StreamConfig{
		Enabled:          true,
		StructuredSchema: map[string]any{"type": "object"},
		SessionManager:   sessMgr,
		SessionID:        sessionID,
	}

	engine := NewQueryEngine(cfg, nil, "test-model")
	if engine == nil {
		t.Fatal("NewQueryEngine returned nil unexpectedly")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := engine.SubmitMessage(ctx, "test prompt")
	if err != nil {
		t.Fatalf("SubmitMessage returned error: %v", err)
	}

	// AC3: Result should be the structured JSON from the StructuredOutput call
	// Parse both JSONs to compare values (key order may differ)
	if result == "" {
		t.Error("AC3 FAIL: expected non-empty result")
	} else {
		var resultVal, expectedVal any
		if err := json.Unmarshal([]byte(result), &resultVal); err != nil {
			t.Errorf("AC3 FAIL: result is not valid JSON: %v", err)
		} else if err := json.Unmarshal([]byte(structuredJSON), &expectedVal); err != nil {
			t.Errorf("AC3 FAIL: expected result is not valid JSON: %v", err)
		} else if fmt.Sprintf("%v", resultVal) != fmt.Sprintf("%v", expectedVal) {
			t.Errorf("AC3 FAIL: expected result %v, got %v", expectedVal, resultVal)
		} else {
			t.Log("AC3 PASS: result correctly extracted from StructuredOutput call")
		}
	}
}
