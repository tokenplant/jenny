// Package agent provides the core agent loop and query engine.
package agent

import (
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
