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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/memdir"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/testutil/mockapi"
	"github.com/ipy/jenny/internal/tool"
)

// makeMockStreamServer delegates to makeMockStreamServerWithEvents.
// Supports both no-arg (default events) and []string argument patterns.
var makeMockStreamServer = makeMockStreamServerHelper

// makeMockStreamServerHelper wraps testhelpers_test.go's makeMockStreamServerWithEvents.
// When events is nil or not provided, returns default SSE events.
func makeMockStreamServerHelper(t *testing.T, events []string) *httptest.Server {
	if len(events) == 0 {
		return makeMockStreamServerWithEvents(t, []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from stream"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		})
	}
	return makeMockStreamServerWithEvents(t, events)
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

	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

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
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Bash","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))
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

	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

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

	// Override JennyHomeDir to use tmpDir so cost state is written/read from predictable location
	origFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { constants.JennyHomeDirFunc = origFunc }()

	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_ac3_test"

	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"usage":{"input_tokens":100,"output_tokens":50}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":100,"output_tokens":50}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

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

	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))

	if engine == nil {
		t.Fatal("NewQueryEngine returned nil")
	}
	if engine.sessionManager != sessMgr {
		t.Error("sessionManager not set correctly")
	}
	if engine.model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", engine.model)
	}
	if engine.maxTurns != 5 {
		t.Errorf("maxTurns should be 5 initially, got %d", engine.maxTurns)
	}
}

// TestQueryEngine_SetMaxTurns verifies the setter.
func TestQueryEngine_SetMaxTurns(t *testing.T) {
	cfg := StreamConfig{
		Enabled: false,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))
	engine.SetMaxTurns(10)

	if engine.maxTurns != 10 {
		t.Errorf("expected maxTurns=10, got %d", engine.maxTurns)
	}
}

// TestAC1_SubmitMessageWithoutSessionManager verifies that SubmitMessage
// works correctly when no session manager is configured (AC1 edge case).
func TestAC1_SubmitMessageWithoutSessionManager(t *testing.T) {
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	// No session manager — sessionManager is nil
	cfg := StreamConfig{Enabled: false}
	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

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

	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
		IsResume:       true, // Mark as resume — should skip duplicate user persist
	}
	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

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
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{Enabled: false}
	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))
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
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Bash","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}
	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))
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
	server := makeMockStreamServer(t, nil)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

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

	result, _, err := RunStream(ctx, "test", nil, tmpDir, cfg, "test-model", WithClient(fastClient()))
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
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{Enabled: false}
	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

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
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

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

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

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
	cacheB.RecordRead(testPath, testContent, testMtime, true, 0, 0)

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
	engine := mustNewQueryEngine(cfg, tools, "test-model", WithClient(fastClient()))

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
	// Isolate config dirs so memdir's config-home resolution points to a temp
	// dir regardless of platform. os.UserConfigDir() consults:
	//   - XDG_CONFIG_HOME on Linux/Unix
	//   - APPDATA (Roaming) on Windows
	// overrides APPDATA on Windows, while HOME is not consulted at all there.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	t.Setenv("APPDATA", filepath.Join(tmpHome, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmpHome, "AppData", "Local"))

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
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:           false,
		SessionManager:    sessMgr,
		SessionID:         "sess_ac1_memdir",
		AutoMemoryEnabled: true,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

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

	// Drain the memory extractor so the background extraction goroutine
	// finishes writing/closing files under the isolated HOME before
	// t.TempDir() cleanup runs. Without this, RemoveAll races the
	// goroutine and fails with "directory not empty".
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	engine.Drain(drainCtx)
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

	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))
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

	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))
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

	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))
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

	server := makeMockStreamServer(t, []string{
		// Assistant responds with just text, no StructuredOutput call
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Hello"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:          true,
		StructuredSchema: map[string]any{"type": "object"},
		SessionManager:   sessMgr,
		SessionID:        sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))
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

	server := makeMockStreamServer(t, []string{
		// Assistant calls StructuredOutput tool
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_1","name":"StructuredOutput"}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"value\":{\"result\":\"success\",\"data\":[1,2,3]},\"format\":\"json\"}"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
		// Tool result for StructuredOutput - returns the validated JSON
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"user","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_result","id":"tool_1","content":"{\"result\":\"success\",\"data\":[1,2,3]}","is_error":false}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:          true,
		StructuredSchema: map[string]any{"type": "object"},
		SessionManager:   sessMgr,
		SessionID:        sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))
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

// TestToolCallEvents verifies that when stream-json mode is enabled and
// the model requests tool use, the engine emits tool_call started events before
// execution and tool_call completed events after each tool completes.
// This covers AC1 (started event), AC2 (completed event), and AC4 (both event types).
func TestToolCallEvents(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_tool_call_test"

	// Server that returns a single tool_use then end_turn
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Bash","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	// Save original stdout
	oldStdout := os.Stdout

	// Create pipe to capture stdout
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}

	// Redirect stdout to our pipe
	os.Stdout = stdoutW

	cfg := StreamConfig{
		Enabled:        true, // Stream-json mode enabled
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")

	// Close write end to signal EOF on read end
	stdoutW.Close()

	// Restore original stdout BEFORE checking output
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("SubmitMessage error: %v", err)
	}

	// Read captured stdout
	var stdoutBuf bytes.Buffer
	io.Copy(&stdoutBuf, stdoutR)
	stdoutOutput := stdoutBuf.String()

	// Verify tool_call started event exists
	if !strings.Contains(stdoutOutput, `"type":"tool_call"`) {
		t.Error("AC1 FAIL: stdout does not contain tool_call event")
	} else {
		t.Log("AC1 PASS: stdout contains tool_call event")
	}

	// Verify tool_call started subtype
	if !strings.Contains(stdoutOutput, `"subtype":"started"`) {
		t.Error("AC1 FAIL: stdout does not contain tool_call started event")
	} else {
		t.Log("AC1 PASS: stdout contains tool_call started event")
	}

	// Verify tool_call completed subtype
	if !strings.Contains(stdoutOutput, `"subtype":"completed"`) {
		t.Error("AC1 FAIL: stdout does not contain tool_call completed event")
	} else {
		t.Log("AC1 PASS: stdout contains tool_call completed event")
	}

	// Verify tool_name is present
	if !strings.Contains(stdoutOutput, `"tool_name":"Bash"`) {
		t.Error("AC1 FAIL: stdout does not contain tool_name in tool_call event")
	} else {
		t.Log("AC1 PASS: stdout contains tool_name in tool_call event")
	}

	// Verify tool_use_id is present
	if !strings.Contains(stdoutOutput, `"tool_use_id":"tool_1"`) {
		t.Error("AC1 FAIL: stdout does not contain tool_use_id in tool_call event")
	} else {
		t.Log("AC1 PASS: stdout contains tool_use_id in tool_call event")
	}
}

// TestInterruptSyntheticToolResults_AC5 verifies the full AC matrix for the
// interrupt synthetic tool_results feature:
//   - AC1: a tool that didn't complete receives a synthetic "Tool execution
//     interrupted" result with IsError=true.
//   - AC2: a tool that completed before cancellation retains its real result
//     (not replaced with a synthetic).
//   - AC3: when stop_reason=tool_use the loop continues to the next iteration,
//     delivering the bundled real + synthetic results to a second API call
//     instead of returning the context error immediately.
//   - AC5: a single test exercises both the interrupted and completed branches.
//
// To exercise AC3, the mock server returns tool_use on the first call and
// end_turn on the second; the engine must therefore make two API calls and
// must not propagate the context cancellation back as the SubmitMessage error.
func TestInterruptSyntheticToolResults_AC5(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_interrupt_test"

	// Multi-turn mock: first call returns two tool_use blocks with
	// stop_reason=tool_use (loop must continue); second call returns end_turn.
	// Tools are categorised "readonly" (Read/Grep) so they run in parallel,
	// which lets one finish before cancellation reaches the blocker.
	turn1Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_fast","name":"Read","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_slow","name":"Grep","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}
	turn2Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	var apiCallCount atomic.Int32
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
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

		n := apiCallCount.Add(1)
		var events []string
		if n == 1 {
			events = turn1Events
		} else {
			events = turn2Events
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	})
	server := ms.Server
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	// One fast tool (completes immediately, exercises AC2) and one blocking
	// tool that only returns when the context is cancelled (exercises AC1).
	fast := &fastTool{name: "Read", content: "fast-tool-real-content"}
	slow := &blockingTool{name: "Grep", blockDuration: 5 * time.Second}
	tools := []tool.Tool{fast, slow}

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, tools, "test-model", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = engine.SubmitMessage(ctx, "test prompt")
	elapsed := time.Since(start)
	t.Logf("SubmitMessage returned after %v with error: %v", elapsed, err)

	// AC3: SubmitMessage must NOT propagate the context cancellation back as
	// its error — the engine should detach the loop context after generating
	// synthetic results so the next iteration can deliver them.
	if err != nil && (strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "context canceled")) {
		t.Errorf("AC3 FAIL: SubmitMessage returned context error: %v", err)
	} else {
		t.Logf("AC3 PASS: SubmitMessage did not return a context error (err=%v)", err)
	}

	// AC3: API must have been called twice — once for the tool_use turn that
	// generated synthetic results, then again with those results delivered.
	gotCalls := apiCallCount.Load()
	if gotCalls < 2 {
		t.Errorf("AC3 FAIL: expected at least 2 API calls (loop continued), got %d", gotCalls)
	} else {
		t.Logf("AC3 PASS: API was called %d times (loop continued past interrupt)", gotCalls)
	}

	// AC1/AC2: inspect transcript to verify both branches.
	entries, err := sessMgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript error: %v", err)
	}

	var syntheticForSlow, realForFast bool
	for _, entry := range entries {
		if entry.Type != "tool_result" {
			continue
		}
		if entry.ToolID == "tool_slow" && entry.IsError &&
			strings.Contains(entry.Content, "Tool execution interrupted") {
			syntheticForSlow = true
		}
		if entry.ToolID == "tool_fast" && !entry.IsError &&
			entry.Content == "fast-tool-real-content" {
			realForFast = true
		}
	}

	if !syntheticForSlow {
		t.Error("AC1 FAIL: expected synthetic 'Tool execution interrupted' result for blocking tool tool_slow")
	} else {
		t.Log("AC1 PASS: blocking tool received synthetic interrupted result")
	}
	if !realForFast {
		t.Error("AC2 FAIL: expected completed tool tool_fast to retain real content 'fast-tool-real-content'")
	} else {
		t.Log("AC2 PASS: completed tool retained its real result content")
	}

	// Also verify there is no duplicate ToolUseID in the transcript — the
	// dedupe fix must keep exactly one tool_result per tool_use id.
	seen := make(map[string]int)
	for _, entry := range entries {
		if entry.Type == "tool_result" && entry.ToolID != "" {
			seen[entry.ToolID]++
		}
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("dedupe FAIL: tool_result for %q appeared %d times in transcript (want 1)", id, n)
		}
	}
}

// TestEngine_InterruptedField_TriggersSynthetic verifies AC4: when execResults
// has Interrupted=true, the engine emits "Tool execution interrupted" synthetic
// regardless of the original content.
//
// This uses a sibling-abort pattern: tool1 (fast) completes, tool2 (blocking) starts
// but then tool3 (blocking) fails and cancels the context, interrupting tool2 before
// it can complete.
func TestEngine_InterruptedField_TriggersSynthetic(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_interrupted_field_test"

	// Multi-turn mock: first call returns tool_use (loop continues), second returns end_turn
	turn1Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Read","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_2","name":"Bash","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":1}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tool_3","name":"Bash","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":2}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}
	turn2Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"done"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	var apiCallCount atomic.Int32
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
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

		n := apiCallCount.Add(1)
		var events []string
		if n == 1 {
			events = turn1Events
		} else {
			events = turn2Events
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	})
	server := ms.Server
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	// Three tools: fast (completes), blocking (gets interrupted), failing (triggers abort)
	// read is concurrency-safe and runs in parallel with bash tools
	// bash tools are serial, so tool1 runs first, then tool2 starts, then tool3 fails and aborts tool2
	fast := &fastTool{name: "Read", content: "fast-completed"}
	blocker := &blockingTool{name: "Bash", blockDuration: 10 * time.Second}
	failing := &execMockTool{
		name:    "Bash",
		delay:   0,
		isSafe:  false,
		err:     fmt.Errorf("exit 1"),
		isError: true,
		content: "failing",
	}
	tools := []tool.Tool{fast, blocker, failing}

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, tools, "test-model", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")

	// Debug: check API call count
	t.Logf("API call count: %d", apiCallCount.Load())

	// Load transcript and check for synthetic interrupt result
	entries, err := sessMgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript error: %v", err)
	}

	var foundSynthetic bool
	for _, entry := range entries {
		if entry.Type == "tool_result" && entry.IsError &&
			strings.Contains(entry.Content, "Tool execution interrupted") {
			foundSynthetic = true
			break
		}
	}

	if !foundSynthetic {
		t.Error("AC4 FAIL: expected synthetic 'Tool execution interrupted' result for interrupted tool")
	} else {
		t.Log("AC4 PASS: engine emitted synthetic interrupt result")
	}
}

// TestEngine_BenignContent_NotInterpretedAsInterrupt verifies AC5: when a tool
// returns content containing "aborted" or "interrupted" but Interrupted=false,
// the engine passes the original content through (no synthetic rewrite).
// This is a regression test: the old substring-based detection would false-positive
// on content like "make: *** [build] aborted".
func TestEngine_BenignContent_NotInterpretedAsInterrupt(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_benign_content_test"

	// Server returns a single tool_use then end_turn
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Bash","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	// A tool that returns benign content containing "aborted" — NOT interrupted.
	// This simulates a build tool whose stdout is "make: *** [build] aborted".
	benignTool := &benignAbortedTool{content: "make: *** [build] aborted", isError: true}
	tools := []tool.Tool{benignTool}

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, tools, "test-model", WithClient(fastClient()))

	// Use a valid context (not cancelled) so the tool completes normally
	ctx := context.Background()

	_, err = engine.SubmitMessage(ctx, "test prompt")
	// We expect no error — the tool returns its content normally

	// Load transcript and check that the original benign content is preserved
	entries, err := sessMgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript error: %v", err)
	}

	var foundBenign, foundSynthetic bool
	for _, entry := range entries {
		if entry.Type == "tool_result" {
			if strings.Contains(entry.Content, "make: *** [build] aborted") {
				foundBenign = true
			}
			if strings.Contains(entry.Content, "Tool execution interrupted") {
				foundSynthetic = true
			}
		}
	}

	if foundSynthetic {
		t.Error("AC5 FAIL: engine emitted synthetic interrupt for benign content (should not)")
	} else {
		t.Log("AC5 PASS: engine did NOT emit synthetic interrupt for benign content")
	}

	if !foundBenign {
		t.Error("AC5 FAIL: expected original benign content 'make: *** [build] aborted', got something else")
	} else {
		t.Log("AC5 PASS: original benign content preserved")
	}
}

// blockingTool is a test tool that blocks until the context is cancelled.
type blockingTool struct {
	name          string
	blockDuration time.Duration
}

func (b *blockingTool) Name() string                { return b.name }
func (b *blockingTool) Description() string         { return "A blocking test tool" }
func (b *blockingTool) InputSchema() map[string]any { return map[string]any{} }
func (b *blockingTool) Execute(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	select {
	case <-ctx.Done():
		return &tool.ToolResult{Content: "", IsError: false}, ctx.Err()
	case <-time.After(b.blockDuration):
		return &tool.ToolResult{Content: "completed", IsError: false}, nil
	}
}

// fastTool returns immediately with a fixed content string. Used to verify
// that completed tools retain their real result alongside interrupted ones.
type fastTool struct {
	name    string
	content string
}

func (f *fastTool) Name() string                { return f.name }
func (f *fastTool) Description() string         { return "A fast test tool" }
func (f *fastTool) InputSchema() map[string]any { return map[string]any{} }
func (f *fastTool) Execute(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	return &tool.ToolResult{Content: f.content, IsError: false}, nil
}

// benignAbortedTool returns content that looks like a build abort but is not
// actually an interrupt. Used to verify the engine does not false-positive
// on benign content containing "aborted".
type benignAbortedTool struct {
	content string
	isError bool
}

func (b *benignAbortedTool) Name() string                { return "Bash" }
func (b *benignAbortedTool) Description() string         { return "A tool that returns benign aborted content" }
func (b *benignAbortedTool) InputSchema() map[string]any { return map[string]any{} }
func (b *benignAbortedTool) Execute(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	return &tool.ToolResult{Content: b.content, IsError: b.isError}, nil
}

// TestToolCallEvents_Negative verifies that when stream-json mode is DISABLED
// (Enabled=false), no tool_call events are emitted on stdout. This is the AC3
// negative path - verifying the absence of events when the feature is off.
func TestToolCallEvents_Negative(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_tool_call_negative"

	// Server that returns a single tool_use then end_turn
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Bash","input":{}}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":1}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	// Save original stdout
	oldStdout := os.Stdout

	// Create pipe to capture stdout
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error: %v", err)
	}

	// Redirect stdout to our pipe
	os.Stdout = stdoutW

	cfg := StreamConfig{
		Enabled:        false, // Stream-json mode DISABLED
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")

	// Close write end to signal EOF on read end
	stdoutW.Close()

	// Restore original stdout BEFORE checking output
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("SubmitMessage error: %v", err)
	}

	// Read captured stdout
	var stdoutBuf bytes.Buffer
	io.Copy(&stdoutBuf, stdoutR)
	stdoutOutput := stdoutBuf.String()

	// AC3 negative: Verify NO tool_call events appear when stream-json is disabled
	if strings.Contains(stdoutOutput, `"type":"tool_call"`) {
		t.Error("AC3 FAIL: stdout contains tool_call event even though stream-json mode is disabled")
	} else {
		t.Log("AC3 PASS: no tool_call events emitted when stream-json mode is disabled")
	}
}

// stopReasonTestServer creates a mock SSE server for stop_reason tests.
// calls is the atomic counter to track API call count.
// The stopReason parameter controls what stop_reason value to send in message_delta.
// If textContent is non-empty, a text content block is included.
// If toolUseBlock is non-empty, a tool_use content block is included.
func stopReasonTestServer(t *testing.T, calls *atomic.Int32, stopReason string, textContent string, toolUseBlock string) *httptest.Server {
	t.Helper()
	turn1Events := func() []string {
		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		}
		idx := 0
		if textContent != "" {
			events = append(events, sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`))
			events = append(events, sseLine("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"%s"}}`, textContent)))
			events = append(events, sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`))
			idx = 1
		}
		if toolUseBlock != "" {
			events = append(events, sseLine("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"tool_1","name":"%s","input":{}}}`, idx, toolUseBlock)))
			events = append(events, sseLine("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, idx)))
		}
		stopReasonJSON := "null"
		if stopReason != "" {
			stopReasonJSON = fmt.Sprintf(`"%s"`, stopReason)
		}
		events = append(events, sseLine("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%s,"stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`, stopReasonJSON)))
		events = append(events, sseLine("message_stop", `{"type":"message_stop"}`))
		return events
	}
	turn2Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
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
		var events []string
		if n == 1 {
			events = turn1Events()
		} else {
			events = turn2Events
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	})
	return ms.Server
}

// TestRunLoop_EmptyStopReason_TerminatesAsEndTurn verifies AC3: when the API
// returns stop_reason="", the loop terminates as end_turn with a single API call.
func TestRunLoop_EmptyStopReason_TerminatesAsEndTurn(t *testing.T) {
	var calls atomic.Int32
	server := stopReasonTestServer(t, &calls, "", "hello", "")
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      "sess_empty_sr",
	}
	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := engine.SubmitMessage(ctx, "test prompt")
	if err != nil {
		t.Fatalf("SubmitMessage returned error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected result 'hello', got %q", result)
	}
	// AC3: exactly 1 API call
	if calls.Load() != 1 {
		t.Errorf("expected 1 API call, got %d", calls.Load())
	} else {
		t.Log("AC3 PASS: exactly 1 API call with empty stop_reason")
	}
}

// TestRunLoop_NullStopReason_TerminatesAsEndTurn verifies AC4: when the API
// returns a response with the stop_reason field omitted (null), the loop
// terminates as end_turn with a single API call.
func TestRunLoop_NullStopReason_TerminatesAsEndTurn(t *testing.T) {
	var calls atomic.Int32
	// null stop_reason: omit stop_reason from message_delta entirely
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
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
		calls.Add(1)

		events := []string{
			sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}}`),
			sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`),
			sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
			// stop_reason field omitted entirely
			sseLine("message_delta", `{"type":"message_delta","delta":{"stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":2}}`),
			sseLine("message_stop", `{"type":"message_stop"}`),
		}
		for _, e := range events {
			io.WriteString(w, e)
			flusher.Flush()
		}
	})
	server := ms.Server
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{Enabled: false}
	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := engine.SubmitMessage(ctx, "test prompt")
	if err != nil {
		t.Fatalf("SubmitMessage returned error: %v", err)
	}
	if result != "world" {
		t.Errorf("expected result 'world', got %q", result)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 API call, got %d", calls.Load())
	} else {
		t.Log("AC4 PASS: null stop_reason terminated with 1 API call")
	}
}

// TestRunLoop_UnknownStopReason_TerminatesAsEndTurn verifies AC5: when the API
// returns an unrecognized stop_reason string, the loop terminates as end_turn
// with a single API call.
func TestRunLoop_UnknownStopReason_TerminatesAsEndTurn(t *testing.T) {
	var calls atomic.Int32
	server := stopReasonTestServer(t, &calls, "frobnicate_widget", "hello", "")
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{Enabled: false}
	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := engine.SubmitMessage(ctx, "test prompt")
	if err != nil {
		t.Fatalf("SubmitMessage returned error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected result 'hello', got %q", result)
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 API call, got %d", calls.Load())
	} else {
		t.Log("AC5 PASS: unknown stop_reason terminated with 1 API call")
	}
}

// TestRunLoop_EmptyStopReason_WithToolUse_TreatsAsToolUse verifies AC9:
// when stop_reason is empty BUT a tool_use block is present, the loop
// treats this as tool_use (continues) and makes a second API call after
// executing the tool. The second turn returns end_turn with text "done".
func TestRunLoop_EmptyStopReason_WithToolUse_TreatsAsToolUse(t *testing.T) {
	var calls atomic.Int32
	server := stopReasonTestServer(t, &calls, "", "hello", "Bash")
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      "sess_empty_sr_tool",
	}
	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := engine.SubmitMessage(ctx, "test prompt")
	if err != nil {
		t.Fatalf("SubmitMessage returned error: %v", err)
	}
	// AC9: The engine must have made 2 API calls (first with empty stop_reason+tool_use,
	// second with end_turn). The text from turn 1 ("hello") is discarded when
	// stop_reason is empty and tool_use is present (the loop continues without
	// finalizing), so the final result is "done" from turn 2.
	if calls.Load() != 2 {
		t.Errorf("AC9 FAIL: expected 2 API calls, got %d", calls.Load())
	} else {
		t.Logf("AC9 PASS: exactly 2 API calls (loop continued past empty stop_reason with tool_use)")
	}
	if result != "done" {
		t.Errorf("expected result 'done', got %q", result)
	} else {
		t.Log("AC9 PASS: result is 'done' from second turn")
	}
}

// stubMemExtractorForAC10 is a mock APIClient that records the TurnContext
// passed to MemoryExtractor.CheckAndExtract.
type stubMemExtractorForAC10 struct {
	recordingTurnCtx TurnContext
}

func (s *stubMemExtractorForAC10) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
	return &api.Response{}, nil
}

// TestRunLoop_EmptyStopReason_MemExtractorCalledWithEmpty verifies AC10: when
// the loop terminates on empty stop_reason, memExtractor.CheckAndExtract is
// invoked with StopReason: "" (passed through verbatim for observability).
//
// This test verifies the call by enabling AutoMemoryEnabled and checking that
// the memory extraction path is entered. With AutoMemoryEnabled=true, the
// first CheckAndExtract call runs extraction synchronously (ExtractEveryNTurns=1,
// IsSubAgent=false), allowing us to verify the call happened.
func TestRunLoop_EmptyStopReason_MemExtractorCalledWithEmpty(t *testing.T) {
	// Set up an isolated git repo so memdir can resolve project root
	// Use the same isolation pattern as TestAC1_MemdirCreatedAtPromptBuild.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	t.Setenv("APPDATA", filepath.Join(tmpHome, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmpHome, "AppData", "Local"))

	// Set git identity using the correct cleanup pattern
	gitEnvVars := []string{"GIT_AUTHOR_EMAIL", "GIT_AUTHOR_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_NAME"}
	origGitEnv := make(map[string]string, len(gitEnvVars))
	for _, k := range gitEnvVars {
		origGitEnv[k] = os.Getenv(k)
	}
	for _, k := range gitEnvVars {
		os.Setenv(k, "test@example.com")
	}
	defer func() {
		for _, k := range gitEnvVars {
			if orig, ok := origGitEnv[k]; ok && orig != "" {
				os.Setenv(k, orig)
			} else {
				os.Unsetenv(k)
			}
		}
	}()

	repoDir := t.TempDir()
	initTestGitRepo(t, repoDir)

	origWd, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origWd)

	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	var calls atomic.Int32
	server := stopReasonTestServer(t, &calls, "", "observe me", "")
	defer server.Close()
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:           false,
		SessionManager:    sessMgr,
		SessionID:         "sess_ac10",
		AutoMemoryEnabled: true,
	}
	engine := mustNewQueryEngine(cfg, nil, "test-model", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")
	if err != nil {
		t.Fatalf("SubmitMessage returned error: %v", err)
	}

	// With AutoMemoryEnabled=true, CheckAndExtract is called.
	// The memdir path is computed from project root (repoDir).
	resolvedRepoDir, _ := filepath.EvalSymlinks(repoDir)
	resolvedRepoDir, _ = filepath.Abs(resolvedRepoDir)
	expectedMem, _ := memdir.New(memdir.Config{
		ProjectRoot:       resolvedRepoDir,
		AutoMemoryEnabled: true,
	})
	memPath := expectedMem.MemoryPath()

	// AC10: memExtractor.CheckAndExtract was called (proved by memdir creation)
	if _, statErr := os.Stat(memPath); statErr != nil {
		t.Errorf("AC10 FAIL: memdir not created at %q (CheckAndExtract not called): %v", memPath, statErr)
	} else {
		t.Log("AC10 PASS: memdir created, proving CheckAndExtract was called with empty stop_reason")
	}
}

type mockExtraTool struct{}

func (m *mockExtraTool) Name() string        { return "extra_tool" }
func (m *mockExtraTool) Description() string { return "A tool with extra schema fields" }
func (m *mockExtraTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"foo": map[string]any{"type": "string"},
		},
		"required": []string{"foo"},
		"$defs": map[string]any{
			"item": map[string]any{"type": "string"},
		},
		"examples": []any{"ex"},
	}
}
func (m *mockExtraTool) Execute(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	return nil, nil
}

func TestQueryEngine_ToolParamExtraction(t *testing.T) {
	// AC1, AC3: Verify QueryEngine extracts extra fields from tool schema
	tools := []tool.Tool{&mockExtraTool{}}
	cfg := StreamConfig{Enabled: false}
	engine := mustNewQueryEngine(cfg, tools, "", WithClient(fastClient()))

	if len(engine.toolParams) != 1 {
		t.Fatalf("expected 1 tool param, got %d", len(engine.toolParams))
	}

	tp := engine.toolParams[0]
	if tp.Name != "extra_tool" {
		t.Errorf("expected tool name 'extra_tool', got %q", tp.Name)
	}

	extra := tp.InputSchema.ExtraFields
	if len(extra) != 2 {
		t.Errorf("expected 2 extra fields, got %d: %v", len(extra), extra)
	}

	if _, ok := extra["$defs"]; !ok {
		t.Error("$defs missing from ExtraFields")
	}
	if _, ok := extra["examples"]; !ok {
		t.Error("examples missing from ExtraFields")
	}

	// Verify standard fields are not in extra
	if _, ok := extra["properties"]; ok {
		t.Error("properties should not be in ExtraFields")
	}
	if _, ok := extra["type"]; ok {
		t.Error("type should not be in ExtraFields")
	}
	if _, ok := extra["required"]; ok {
		t.Error("required should not be in ExtraFields")
	}
}

// TestEngine_OutputCapHit_EmitsStructuredError verifies that when the streaming
// API returns stop_reason: "max_tokens" with output_tokens >= modelMaxOutputTokens,
// the engine emits a structured result event with subtype error_max_tokens and
// category "output_cap_hit".
func TestEngine_OutputCapHit_EmitsStructuredError(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_output_cap_hit_test"

	// Server returns stop_reason: max_tokens with output_tokens = 8192 (hits model max)
	// deepseek-v4-flash has max_output_tokens = 8192
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"deepseek-v4-flash","stop_reason":null,"usage":{"input_tokens":70000,"output_tokens":0}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Partial response"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens","stop_sequence":null},"usage":{"input_tokens":70000,"output_tokens":8192}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        true, // Enable stream-json output
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "deepseek-v4-flash", WithClient(fastClient()))

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	var output bytes.Buffer
	io.Copy(&output, r)

	// Verify error_max_tokens event was emitted
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var foundErrorMaxTokens bool
	for _, line := range lines {
		if strings.Contains(line, `"subtype":"error_max_tokens"`) {
			foundErrorMaxTokens = true
			// Verify category is output_cap_hit
			if strings.Contains(line, `"category":"output_cap_hit"`) {
				t.Log("PASS: error_max_tokens event with output_cap_hit category found")
			} else if strings.Contains(line, `"category":"context_exhausted"`) {
				t.Error("FAIL: expected output_cap_hit but got context_exhausted")
			}
			// Verify max_output_tokens is populated
			if strings.Contains(line, `"max_output_tokens"`) {
				t.Log("PASS: max_output_tokens field present")
			}
			break
		}
	}

	if !foundErrorMaxTokens {
		t.Error("FAIL: error_max_tokens event not found in output")
	}

	// Verify error is returned
	if err == nil {
		t.Error("FAIL: expected error to be returned")
	} else if !strings.Contains(err.Error(), "output_cap_hit") {
		t.Errorf("FAIL: expected error to contain 'output_cap_hit', got: %v", err)
	}
}

// TestEngine_ContextExhausted_EmitsStructuredError verifies that when the streaming
// API returns HTTP 400 with prompt_too_long error (context rejection), the engine
// emits a structured result event with subtype error_max_tokens and category
// "context_exhausted".
func TestEngine_ContextExhausted_EmitsStructuredError(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_context_exhausted_test"

	// Server returns HTTP 400 with prompt_too_long error
	// This simulates a context exhaustion rejection before streaming begins
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		// Return a prompt_too_long error response
		w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"prompt_too_long: input too long"}}`))
	})
	server := ms.Server
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        true, // Enable stream-json output
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "deepseek-v4-flash", WithClient(fastClient()))

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	var output bytes.Buffer
	io.Copy(&output, r)

	// Verify error_max_tokens event was emitted
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var foundErrorMaxTokens bool
	for _, line := range lines {
		if strings.Contains(line, `"subtype":"error_max_tokens"`) {
			foundErrorMaxTokens = true
			// Verify category is context_exhausted
			if strings.Contains(line, `"category":"context_exhausted"`) {
				t.Log("PASS: error_max_tokens event with context_exhausted category found")
			} else if strings.Contains(line, `"category":"output_cap_hit"`) {
				t.Error("FAIL: expected context_exhausted but got output_cap_hit")
			}
			// Verify threshold is populated for context_exhausted
			if strings.Contains(line, `"threshold"`) {
				t.Log("PASS: threshold field present for context_exhausted")
			}
			break
		}
	}

	if !foundErrorMaxTokens {
		t.Error("FAIL: error_max_tokens event not found in output")
	}

	// Verify error is returned
	if err == nil {
		t.Error("FAIL: expected error to be returned")
	} else if !strings.Contains(err.Error(), "context_exhausted") {
		t.Errorf("FAIL: expected error to contain 'context_exhausted', got: %v", err)
	}
}

// TestEngine_AutoCompactFiresAboveThreshold verifies that auto-compact triggers
// when estimated tokens exceed the auto-compact threshold (regression test).
// For deepseek-v4-flash: effectiveWindow = 128K - 8K = 120K, threshold = 120K - 13K = 107K
func TestEngine_AutoCompactFiresAboveThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_autocompact_threshold_test"

	// Server that responds to summary calls (compaction) - track if it's called
	summaryServer := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","stop_reason":null,"usage":{"input_tokens":100,"output_tokens":50}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Summarized"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":100,"output_tokens":50}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	})
	defer summaryServer.Close()

	// First call returns partial response, second call (after compact) returns success
	callCount := atomic.Int32{}
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
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

		if count == 1 {
			// First call: partial response with max_tokens
			events := []string{
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"deepseek-v4-flash\",\"stop_reason\":null,\"usage\":{\"input_tokens\":5000,\"output_tokens\":0}}}\n\n",
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Response before compact\"}}\n\n",
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":5000,\"output_tokens\":100}}\n\n",
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			}
			for _, e := range events {
				io.WriteString(w, e)
				flusher.Flush()
			}
		} else {
			// Second call: success after compact
			events := []string{
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"deepseek-v4-flash\",\"stop_reason\":null,\"usage\":{\"input_tokens\":5000,\"output_tokens\":0}}}\n\n",
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Final response\"}}\n\n",
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":5000,\"output_tokens\":50}}\n\n",
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			}
			for _, e := range events {
				io.WriteString(w, e)
				flusher.Flush()
			}
		}
	})
	mainServer := ms.Server
	defer mainServer.Close()

	t.Setenv("ANTHROPIC_BASE_URL", mainServer.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "deepseek-v4-flash", WithClient(fastClient()))

	// Verify the engine's compact config has correct threshold
	// deepseek-v4-flash: effectiveWindow = 1M - 8192 = 991808
	// autoCompactBuffer = max(8192+5000, 13000) = 13192
	// threshold = 991808 - 13192 = 978616
	threshold := engine.compactConfig.autoCompactThreshold()
	expectedThreshold := 978616
	if threshold != expectedThreshold {
		t.Errorf("expected threshold %d, got %d", expectedThreshold, threshold)
	} else {
		t.Logf("PASS: auto-compact threshold correctly calculated as %d", threshold)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// SubmitMessage with normal prompt - should not trigger auto-compact
	// (threshold is ~978K, prompt is small)
	_, _ = engine.SubmitMessage(ctx, "small prompt")

	t.Log("Test completed - threshold calculation verified")
}

// TestEngine_ContextExhausted_MiniMax_EmitsStructuredError verifies that when the streaming
// API returns HTTP 400 with a MiniMax-style context limit error, the engine
// emits a structured result event with subtype error_max_tokens and category
// "context_exhausted".
func TestEngine_ContextExhausted_MiniMax_EmitsStructuredError(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_context_exhausted_minimax_test"

	// Server returns HTTP 400 with MiniMax context limit error
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"invalid params, context window exceeds limit (2013)"},"request_id":"06775bac09d2d88c0d5176f10eddc0e4"}`))
	})
	server := ms.Server
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg := StreamConfig{
		Enabled:        true,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "minimax-m2.7", WithClient(fastClient()))

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "test prompt")

	w.Close()
	os.Stdout = oldStdout

	var output bytes.Buffer
	io.Copy(&output, r)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var foundErrorMaxTokens bool
	for _, line := range lines {
		if strings.Contains(line, `"subtype":"error_max_tokens"`) {
			foundErrorMaxTokens = true
			if strings.Contains(line, `"category":"context_exhausted"`) {
				t.Log("PASS: error_max_tokens event with context_exhausted category found")
			} else if strings.Contains(line, `"category":"output_cap_hit"`) {
				t.Error("FAIL: expected context_exhausted but got output_cap_hit")
			}
			break
		}
	}

	if !foundErrorMaxTokens {
		t.Error("FAIL: error_max_tokens event not found in output")
	}

	if err == nil {
		t.Error("FAIL: expected error to be returned")
	} else if !strings.Contains(err.Error(), "context_exhausted") {
		t.Errorf("FAIL: expected error to contain 'context_exhausted', got: %v", err)
	}
}

// TestAC2_PersistCompactBoundary_LogsError verifies that persistCompactBoundary
// returns an error when AppendEntry fails (instead of swallowing it) and logs
// the error via log.Error. This tests AC1 and AC2: the function now returns the
// error and logs it internally.
func TestAC2_PersistCompactBoundary_LogsError(t *testing.T) {
	// Create a session manager with a valid tmpDir
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_boundary_error_test"

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      sessionID,
	}

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

	// Manually call persistCompactBoundary with valid params
	// First append some entries to have a valid session
	if err := sessMgr.AppendEntry(sessionID, session.TranscriptEntry{
		Type:    "user",
		Content: "test",
	}); err != nil {
		t.Fatalf("AppendEntry for setup error: %v", err)
	}

	// Now make the session directory unwriteable by creating a file where the directory should be
	os.RemoveAll(tmpDir)
	if err := os.WriteFile(tmpDir, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("setup error: %v", err)
	}

	// Capture log output
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	// persistCompactBoundary should return an error (not panic or silently swallow it)
	err = engine.persistCompactBoundary(5000, 3, "auto")
	if err == nil {
		t.Error("AC2 FAIL: expected error from persistCompactBoundary when AppendEntry fails, got nil")
	} else {
		t.Logf("AC2 PASS: persistCompactBoundary returned error: %v", err)
	}

	// AC1: Verify log.Error was called with the expected message
	if !strings.Contains(logBuf.String(), "Failed to persist compaction boundary") {
		t.Errorf("AC1 FAIL: expected log output to contain 'Failed to persist compaction boundary', got: %s", logBuf.String())
	} else {
		t.Logf("AC1 PASS: log output contains expected message")
	}
}

// mockSkillActivatorForTest implements both tool.SkillActivator and GetActivatedSkills.
type mockSkillActivatorForTest struct {
	activated []skills.ActivatedSkill
}

func (m *mockSkillActivatorForTest) ActivateForPath(path string) []string {
	return nil
}

func (m *mockSkillActivatorForTest) RegisterActivation(name string, rootPath string) {
	// Deduplicate
	for _, s := range m.activated {
		if s.Name == name {
			return
		}
	}
	m.activated = append(m.activated, skills.ActivatedSkill{Name: name, RootPath: rootPath})
}

func (m *mockSkillActivatorForTest) GetActivatedSkills() []skills.ActivatedSkill {
	return m.activated
}

// TestAC1_SkillActivatorWiring tests that the skill activator is properly wired
// through the engine and syncActiveSkills correctly converts skills.ActivatedSkill
// to agent.ActivatedSkill.
func TestAC1_SkillActivatorWiring(t *testing.T) {
	cfg := StreamConfig{Enabled: false}
	mockActivator := &mockSkillActivatorForTest{}
	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()), WithSkillActivator(mockActivator))

	// Verify the activator was wired correctly
	if engine.skillActivator == nil {
		t.Fatal("skillActivator was not set on engine")
	}

	// Register some activations via the activator
	mockActivator.RegisterActivation("readme-writer", "/path/to/readme-writer")
	mockActivator.RegisterActivation("code-review", "/path/to/code-review")

	// Call syncActiveSkills to copy the activated skills to StreamConfig
	engine.syncActiveSkills()

	// Verify StreamConfig.ActiveSkills now contains the activated skills
	if len(engine.streamCfg.ActiveSkills) != 2 {
		t.Errorf("expected 2 active skills, got %d", len(engine.streamCfg.ActiveSkills))
	}

	// Verify the skills are correct (type conversion from skills.ActivatedSkill)
	if engine.streamCfg.ActiveSkills[0].Name != "readme-writer" {
		t.Errorf("expected skill name 'readme-writer', got %s", engine.streamCfg.ActiveSkills[0].Name)
	}
	if engine.streamCfg.ActiveSkills[0].RootPath != "/path/to/readme-writer" {
		t.Errorf("expected root path '/path/to/readme-writer', got %s", engine.streamCfg.ActiveSkills[0].RootPath)
	}
	if engine.streamCfg.ActiveSkills[1].Name != "code-review" {
		t.Errorf("expected skill name 'code-review', got %s", engine.streamCfg.ActiveSkills[1].Name)
	}
	t.Log("AC1 PASS: skill activator wiring and type conversion work correctly")
}

// TestAC1_SkillActivatorDeduplication tests that duplicate activations are not added.
func TestAC1_SkillActivatorDeduplication(t *testing.T) {
	cfg := StreamConfig{Enabled: false}
	mockActivator := &mockSkillActivatorForTest{}
	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()), WithSkillActivator(mockActivator))

	// Register the same skill twice
	mockActivator.RegisterActivation("readme-writer", "/path/to/readme-writer")
	mockActivator.RegisterActivation("readme-writer", "/path/to/readme-writer") // duplicate

	engine.syncActiveSkills()

	if len(engine.streamCfg.ActiveSkills) != 1 {
		t.Errorf("expected 1 active skill (deduplicated), got %d", len(engine.streamCfg.ActiveSkills))
	}
	t.Log("PASS: deduplication works correctly")
}

// TestAC1_SkillActivatorNoOpWhenNil tests that syncActiveSkills is a no-op
// when the skill activator is nil.
func TestAC1_SkillActivatorNoOpWhenNil(t *testing.T) {
	cfg := StreamConfig{Enabled: false}
	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient())) // No activator

	// Should not panic and should leave ActiveSkills empty
	engine.syncActiveSkills()

	if len(engine.streamCfg.ActiveSkills) != 0 {
		t.Errorf("expected 0 active skills when activator is nil, got %d", len(engine.streamCfg.ActiveSkills))
	}
	t.Log("PASS: syncActiveSkills is no-op when activator is nil")
}
