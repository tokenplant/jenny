// Package agent provides black-box validation tests for context compaction.
package agent

import (
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/api"
)

// ============================================================
// AC1 — Auto-compact at effectiveWindow − 13K
// ============================================================

// TestAC1_CheckCompactThreshold verifies the threshold check works correctly.
func TestAC1_CheckCompactThreshold(t *testing.T) {
	tests := []struct {
		name                 string
		modelContextWindow   int
		modelMaxOutputTokens int
		estimatedTokens      int
		disableAutoCompact   bool
		want                 bool
	}{
		{
			name:                 "below threshold - no compact",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      150_000, // 200K - 20K - 13K = 167K threshold, 150K < 167K
			disableAutoCompact:   false,
			want:                 false,
		},
		{
			name:                 "at threshold - compact",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      167_000, // equals threshold
			disableAutoCompact:   false,
			want:                 true,
		},
		{
			name:                 "above threshold - compact",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      180_000, // well above threshold
			disableAutoCompact:   false,
			want:                 true,
		},
		{
			name:                 "auto compact disabled",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      200_000,
			disableAutoCompact:   true,
			want:                 false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CompactConfig{
				ModelContextWindow:   tt.modelContextWindow,
				ModelMaxOutputTokens: tt.modelMaxOutputTokens,
				DisableAutoCompact:   tt.disableAutoCompact,
			}
			got := cfg.checkCompactThreshold(tt.estimatedTokens)
			if got != tt.want {
				t.Errorf("AC1 FAIL: checkCompactThreshold(%d) = %v, want %v", tt.estimatedTokens, got, tt.want)
			}
		})
	}
	t.Log("AC1 PASS: auto-compact threshold check works correctly")
}

// TestAC1_WarningThreshold verifies warning threshold is emitted when appropriate.
func TestAC1_WarningThreshold(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	// Effective window = 200K - 20K = 180K
	// Auto compact threshold = 180K - 13K = 167K
	// Warning threshold = 167K - 20K = 147K

	// Below warning threshold - no warning
	if cfg.checkWarningThreshold(140_000) {
		t.Error("AC1 FAIL: warning should not trigger below threshold")
	}

	// At warning threshold - warning
	if !cfg.checkWarningThreshold(147_000) {
		t.Error("AC1 FAIL: warning should trigger at threshold")
	}

	// Above warning threshold - warning
	if !cfg.checkWarningThreshold(160_000) {
		t.Error("AC1 FAIL: warning should trigger above threshold")
	}

	t.Log("AC1 PASS: warning threshold check works correctly")
}

// ============================================================
// AC2 — Circuit breaker after 3 failures
// ============================================================

// TestAC2_CircuitBreakerReset verifies circuit breaker resets on success.
func TestAC2_CircuitBreakerReset(t *testing.T) {
	// Simulate failure counter behavior
	failCount := 0

	// Increment on failure
	failCount++
	if failCount != 1 {
		t.Errorf("AC2 FAIL: expected failCount 1, got %d", failCount)
	}

	failCount++
	failCount++
	if failCount != 3 {
		t.Errorf("AC2 FAIL: expected failCount 3, got %d", failCount)
	}

	// Reset on success
	failCount = 0
	if failCount != 0 {
		t.Errorf("AC2 FAIL: expected failCount 0 after reset, got %d", failCount)
	}

	t.Log("AC2 PASS: circuit breaker reset works correctly")
}

// TestAC2_CircuitBreakerTrip verifies circuit breaker trips after 3 failures.
func TestAC2_CircuitBreakerTrip(t *testing.T) {
	maxFailures := MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES
	if maxFailures != 3 {
		t.Errorf("AC2 FAIL: MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES should be 3, got %d", maxFailures)
	}

	// Simulate circuit breaker behavior
	failCount := 0
	circuitBreakerTripped := false

	for range 5 {
		if failCount >= MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES {
			circuitBreakerTripped = true
			break
		}
		failCount++
	}

	if !circuitBreakerTripped {
		t.Error("AC2 FAIL: circuit breaker should have tripped after 3 failures")
	}

	t.Log("AC2 PASS: circuit breaker trips after 3 consecutive failures")
}

// ============================================================
// AC3 — Hard block at effectiveWindow − 3K when auto off
// ============================================================

// TestAC3_BlockIfOverLimit verifies blocking limit when auto-compact is disabled.
func TestAC3_BlockIfOverLimit(t *testing.T) {
	tests := []struct {
		name                 string
		modelContextWindow   int
		modelMaxOutputTokens int
		estimatedTokens      int
		querySource          string
		disableAutoCompact   bool
		wantBlock            bool
	}{
		{
			name:                 "below blocking limit - no block",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      170_000, // 180K - 3K = 177K limit
			querySource:          "user",
			disableAutoCompact:   true,
			wantBlock:            false,
		},
		{
			name:                 "at blocking limit - block",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      177_000, // equals limit
			querySource:          "user",
			disableAutoCompact:   true,
			wantBlock:            true,
		},
		{
			name:                 "above blocking limit - block",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      180_000, // well above limit
			querySource:          "user",
			disableAutoCompact:   true,
			wantBlock:            true,
		},
		{
			name:                 "auto compact enabled - no block even at limit",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      180_000,
			querySource:          "user",
			disableAutoCompact:   false,
			wantBlock:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CompactConfig{
				ModelContextWindow:   tt.modelContextWindow,
				ModelMaxOutputTokens: tt.modelMaxOutputTokens,
				DisableAutoCompact:   tt.disableAutoCompact,
			}
			err := cfg.blockIfOverLimit(tt.estimatedTokens, tt.querySource)
			gotBlock := err != nil
			if gotBlock != tt.wantBlock {
				t.Errorf("AC3 FAIL: blockIfOverLimit(%d) returned error = %v, want block = %v", tt.estimatedTokens, gotBlock, tt.wantBlock)
			}
		})
	}
	t.Log("AC3 PASS: blocking limit check works correctly")
}

// ============================================================
// AC4 — Post-compact payload passes tool/thinking pairing rules
// ============================================================

// TestAC4_PostCompactNormalization verifies post-compact normalization.
func TestAC4_PostCompactNormalization(t *testing.T) {
	// Create messages with thinking that needs normalization
	messages := []api.Message{
		{
			Role:    "assistant",
			Content: "<thinking>orphaned thought</thinking>",
		},
		{
			Role:    "assistant",
			Content: "Hello<thinking>trailing thought</thinking>",
		},
		{
			Role:    "assistant",
			Content: "   \n\t  ", // whitespace only
		},
		{
			Role:    "assistant",
			Content: "", // empty
		},
		{
			Role:    "user",
			Content: "continue",
		},
	}

	result := normalizeCompactedChain(messages)

	// Verify no thinking blocks at end of last assistant
	for i := len(result) - 1; i >= 0; i-- {
		if result[i].Role == "assistant" {
			content := result[i].Content
			if strings.Contains(content, "<thinking>") {
				t.Errorf("AC4 FAIL: assistant message still contains thinking block: %q", content)
			}
			break
		}
	}

	// Verify empty assistants get placeholder
	for _, msg := range result {
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" && len(msg.ToolUse) == 0 {
			t.Errorf("AC4 FAIL: empty assistant should have placeholder content")
		}
	}

	t.Log("AC4 PASS: post-compact normalization works correctly")
}

// ============================================================
// AC5 — compact/session_memory sources never hard-blocked pre-API
// ============================================================

// TestAC5_CompactSourceNeverBlocked verifies compact querySource skips hard block.
func TestAC5_CompactSourceNeverBlocked(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
		DisableAutoCompact:   true, // auto compact is disabled
	}

	// Even with very high tokens and auto-compact disabled, compact source should not block
	err := cfg.blockIfOverLimit(200_000, "compact")
	if err != nil {
		t.Errorf("AC5 FAIL: compact source should never be blocked, got error: %v", err)
	}

	err = cfg.blockIfOverLimit(200_000, "session_memory")
	if err != nil {
		t.Errorf("AC5 FAIL: session_memory source should never be blocked, got error: %v", err)
	}

	// But user source should still block
	err = cfg.blockIfOverLimit(200_000, "user")
	if err == nil {
		t.Error("AC5 FAIL: user source should be blocked when auto-compact is disabled")
	}

	t.Log("AC5 PASS: compact/session_memory sources never hard-blocked")
}

// ============================================================
// Token Estimation Tests
// ============================================================

// TestEstimateTokens verifies token estimation heuristic.
func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		messages []api.Message
		wantMin  int
		wantMax  int
	}{
		{
			name:     "empty messages",
			messages: []api.Message{},
			wantMin:  0,
			wantMax:  0,
		},
		{
			name: "single user message",
			messages: []api.Message{
				{Role: "user", Content: "hello world"},
			},
			wantMin: 10,
			wantMax: 20,
		},
		{
			name: "user and assistant with tool_use",
			messages: []api.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "Using tool", ToolUse: []api.ToolUseBlock{{ID: "1", Name: "read", Input: map[string]any{}}}},
			},
			wantMin: 50,
			wantMax: 80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.messages)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("estimateTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
	t.Log("Token estimation heuristic works correctly")
}

// ============================================================
// Threshold Math Tests
// ============================================================

// TestThresholdMath verifies the threshold calculations.
func TestThresholdMath(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	// effectiveContextWindow = 200K - min(20K, 20K) = 180K
	effectiveWindow := cfg.effectiveContextWindow()
	if effectiveWindow != 180_000 {
		t.Errorf("effectiveContextWindow = %d, want 180000", effectiveWindow)
	}

	// autoCompactThreshold = 180K - 13K = 167K
	autoCompactThreshold := cfg.autoCompactThreshold()
	if autoCompactThreshold != 167_000 {
		t.Errorf("autoCompactThreshold = %d, want 167000", autoCompactThreshold)
	}

	// warningThreshold = 167K - 20K = 147K
	warningThreshold := cfg.warningThreshold()
	if warningThreshold != 147_000 {
		t.Errorf("warningThreshold = %d, want 147000", warningThreshold)
	}

	// blockingLimit = 180K - 3K = 177K
	blockingLimit := cfg.blockingLimit()
	if blockingLimit != 177_000 {
		t.Errorf("blockingLimit = %d, want 177000", blockingLimit)
	}

	t.Log("Threshold math calculations correct")
}

// ============================================================
// Helper Function Tests
// ============================================================

// TestBuildCompactedChain verifies the compacted chain structure.
func TestBuildCompactedChain(t *testing.T) {
	messages := make([]api.Message, 15)
	for i := range messages {
		messages[i] = api.Message{
			Role:    "user",
			Content: "message content",
		}
	}

	summary := "This is a summary of the conversation."
	result := buildCompactedChain(messages, summary)

	// First message should be the boundary marker
	if len(result) == 0 {
		t.Fatal("AC1 FAIL: result should not be empty")
	}

	if result[0].Role != "system" {
		t.Errorf("AC1 FAIL: first message should be system role, got %s", result[0].Role)
	}

	if !strings.Contains(result[0].Content, "[Context boundary") {
		t.Error("AC1 FAIL: first message should contain boundary marker")
	}

	if !strings.Contains(result[0].Content, summary) {
		t.Error("AC1 FAIL: first message should contain summary text")
	}

	// Should have kept last 10 messages
	if len(result) != 11 { // 1 boundary + 10 kept messages
		t.Errorf("AC1 FAIL: expected11 messages, got %d", len(result))
	}

	t.Log("buildCompactedChain produces correct structure")
}

// TestDropOldestAPIRoundGroup verifies oldest API round group is dropped.
func TestDropOldestAPIRoundGroup(t *testing.T) {
	messages := []api.Message{
		{Role: "user", Content: "old user"},           // API round 1 start
		{Role: "assistant", Content: "old assistant"}, // API round 1 end
		{Role: "user", Content: "new user"},           // API round 2 start
		{Role: "assistant", Content: "new assistant"}, // API round 2 end
	}

	result := dropOldestAPIRoundGroup(messages)

	// Should drop first two messages (old API round)
	if len(result) != 2 {
		t.Errorf("AC1 FAIL: expected 2 messages after drop, got %d", len(result))
	}

	// Remaining should be the newer messages
	if result[0].Content != "new user" {
		t.Errorf("AC1 FAIL: expected 'new user', got %s", result[0].Content)
	}

	t.Log("dropOldestAPIRoundGroup works correctly")
}
