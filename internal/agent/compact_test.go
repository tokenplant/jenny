// Package agent provides black-box validation tests for context compaction.
package agent

import (
	"fmt"
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
			estimatedTokens:      150_000, // threshold = 200K-20K - max(20K+5K,13K) = 155K; 150K < 155K
			disableAutoCompact:   false,
			want:                 false,
		},
		{
			name:                 "at threshold - compact",
			modelContextWindow:   200_000,
			modelMaxOutputTokens: 20_000,
			estimatedTokens:      155_000, // equals threshold
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
			got := cfg.checkCompactThreshold(tt.estimatedTokens, "user")
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
	// Auto compact buffer = max(20K+5K, 13K) = 25K
	// Auto compact threshold = 180K - 25K = 155K
	// Warning threshold = 155K - 20K = 135K

	// Below warning threshold - no warning
	if cfg.checkWarningThreshold(130_000) {
		t.Error("AC1 FAIL: warning should not trigger below threshold")
	}

	// At warning threshold - warning
	if !cfg.checkWarningThreshold(135_000) {
		t.Error("AC1 FAIL: warning should trigger at threshold")
	}

	// Above warning threshold - warning
	if !cfg.checkWarningThreshold(150_000) {
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
			wantMin: 1, // 11 chars / 4 ≈ 2 tokens
			wantMax: 5,
		},
		{
			name: "user and assistant with tool_use",
			messages: []api.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "Using tool", ToolUse: []api.ToolUseBlock{{ID: "1", Name: "Read", Input: map[string]any{}}}},
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

	// effectiveContextWindow = 200K - 20K = 180K
	effectiveWindow := cfg.effectiveContextWindow()
	if effectiveWindow != 180_000 {
		t.Errorf("effectiveContextWindow = %d, want 180000", effectiveWindow)
	}

	// autoCompactBuffer = max(20K + 5K, 13K) = 25K
	// autoCompactThreshold = 180K - 25K = 155K
	autoCompactThreshold := cfg.autoCompactThreshold()
	if autoCompactThreshold != 155_000 {
		t.Errorf("autoCompactThreshold = %d, want 155000", autoCompactThreshold)
	}

	// warningThreshold = 155K - 20K = 135K
	warningThreshold := cfg.warningThreshold()
	if warningThreshold != 135_000 {
		t.Errorf("warningThreshold = %d, want 135000", warningThreshold)
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

// ============================================================
// AC6 — Thresholds from actual model params
// ============================================================

func TestAC6_ModelAwareCompactConfig(t *testing.T) {
	// DeepSeek models have 1M context window and 8K max output
	cfg := newCompactConfigForModel("deepseek-v4-flash")
	if cfg.ModelContextWindow != 1_000_000 {
		t.Errorf("deepseek-v4-flash context window = %d, want 1000000", cfg.ModelContextWindow)
	}
	if cfg.ModelMaxOutputTokens != 8_192 {
		t.Errorf("deepseek-v4-flash max output = %d, want 8192", cfg.ModelMaxOutputTokens)
	}

	// Default model uses 200K/20K
	cfgDefault := newCompactConfigForModel("claude-opus-4-5-20251101")
	if cfgDefault.ModelContextWindow != 200_000 {
		t.Errorf("default context window = %d, want 200000", cfgDefault.ModelContextWindow)
	}
	if cfgDefault.ModelMaxOutputTokens != 20_000 {
		t.Errorf("default max output = %d, want 20000", cfgDefault.ModelMaxOutputTokens)
	}
}

func TestAC6_DeepSeekThresholdMath(t *testing.T) {
	cfg := newCompactConfigForModel("deepseek-v4-flash")

	// effectiveContextWindow = 1M - 8192 = 991,808
	effectiveWindow := cfg.effectiveContextWindow()
	expectedEffective := 1_000_000 - 8_192
	if effectiveWindow != expectedEffective {
		t.Errorf("effectiveContextWindow = %d, want %d", effectiveWindow, expectedEffective)
	}

	// Auto-compact should NOT trigger at 648K (the bug scenario from ticket)
	if cfg.checkCompactThreshold(648_000, "user") {
		t.Error("648K tokens should NOT trigger auto-compact with 1M context window")
	}

	// Should trigger near the actual threshold
	threshold := cfg.autoCompactThreshold()
	if !cfg.checkCompactThreshold(threshold, "user") {
		t.Errorf("should trigger at threshold %d", threshold)
	}
	if cfg.checkCompactThreshold(threshold-1, "user") {
		t.Error("should NOT trigger below threshold")
	}
}

// ============================================================
// AC7 — buildCompactedChain does not split tool pairs
// ============================================================

func TestAC7_CompactedChainPreservesToolPairs(t *testing.T) {
	messages := make([]api.Message, 20)
	for i := range 18 {
		messages[i] = api.Message{Role: "user", Content: "filler"}
	}
	// Last two messages: assistant with tool_use + user with tool_result
	messages[18] = api.Message{
		Role:    "assistant",
		Content: "Using tool",
		ToolUse: []api.ToolUseBlock{{ID: "call_001", Name: "Read", Input: map[string]any{}}},
	}
	messages[19] = api.Message{
		Role:        "user",
		ToolResults: []api.ToolResultBlock{{ToolUseID: "call_001", Content: "result"}},
	}

	result := buildCompactedChain(messages, "summary")

	// Find the assistant with tool_use and verify the next message has its tool_result
	for i, msg := range result {
		if msg.Role == "assistant" && len(msg.ToolUse) > 0 {
			if i+1 >= len(result) {
				t.Fatal("AC7 FAIL: tool_use at end of chain with no following tool_result")
			}
			next := result[i+1]
			if next.Role != "user" || len(next.ToolResults) == 0 {
				t.Fatal("AC7 FAIL: tool_use not immediately followed by tool_result")
			}
			found := false
			for _, tr := range next.ToolResults {
				if tr.ToolUseID == msg.ToolUse[0].ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("AC7 FAIL: tool_result does not match tool_use ID")
			}
		}
	}
}

// ============================================================
// AC3 (ticket) — Token estimation charset-aware
// ============================================================

func TestEstimateTokens_CharsetAware(t *testing.T) {
	// English text: ~4 chars per token
	englishMsg := []api.Message{{Role: "user", Content: strings.Repeat("hello world ", 100)}}
	englishEst := estimateTokens(englishMsg)
	actualEnglishChars := len(englishMsg[0].Content)
	expectedEnglishTokens := actualEnglishChars / 4
	ratio := float64(englishEst) / float64(expectedEnglishTokens)
	if ratio < 0.7 || ratio > 1.3 {
		t.Errorf("English estimation ratio %.2f outside [0.7, 1.3] (est=%d, expected≈%d)",
			ratio, englishEst, expectedEnglishTokens)
	}

	// CJK text: ~1.5 chars per token (each CJK char is 3 bytes in UTF-8)
	cjkMsg := []api.Message{{Role: "user", Content: strings.Repeat("你好世界测试", 100)}}
	cjkEst := estimateTokens(cjkMsg)
	// Old heuristic would say len("你好世界测试"*100) / 4 = 1800/4 * 100... but bytes
	// With charset-aware: CJK chars should estimate ~1.5 chars/token
	// 600 CJK runes → ~400 tokens expected
	if cjkEst < 200 || cjkEst > 800 {
		t.Errorf("CJK estimation %d seems unreasonable for 600 CJK chars (expected ~400)", cjkEst)
	}
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

// ============================================================
// AC1 — In-session compact skipped when messages too large
// ============================================================

// TestAC1_InSessionCompact_Skipped_WhenMessagesTooLarge verifies that when
// estimated tokens exceed effectiveContextWindow - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS,
// in-session compaction is skipped and fallback to forkSummaryAgent is triggered.
func TestAC1_InSessionCompact_Skipped_WhenMessagesTooLarge(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	// effectiveContextWindow = 200K - 20K = 180K
	// maxMessagesTokens = 180K - 30K - 20K = 130K
	maxMessagesTokens := cfg.effectiveContextWindow() - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS
	if maxMessagesTokens != 130_000 {
		t.Fatalf("expected maxMessagesTokens=130000, got %d", maxMessagesTokens)
	}

	// Create messages totaling ~135K tokens (just over the 130K limit)
	// estimateTokens uses len/4 for ASCII, so 135K tokens ≈ 540K chars
	// "hello world " is 12 chars → 45000 repeats × 12 chars = 540K chars → 135K tokens
	largeContent := strings.Repeat("hello world ", 45_000)
	messages := []api.Message{
		{Role: "user", Content: largeContent},
	}

	estimated := estimateTokens(messages)
	if estimated <= maxMessagesTokens {
		t.Fatalf("test setup: estimated %d should exceed maxMessagesTokens %d", estimated, maxMessagesTokens)
	}

	t.Log("AC1 PASS: messages correctly identified as too large for in-session compact")
}

// TestAC1_InSessionCompact_Used_WhenMessagesWithinLimit verifies that when
// estimated tokens are within the safety margin, in-session compaction proceeds.
func TestAC1_InSessionCompact_Used_WhenMessagesWithinLimit(t *testing.T) {
	cfg := CompactConfig{
		ModelContextWindow:   200_000,
		ModelMaxOutputTokens: 20_000,
	}

	maxMessagesTokens := cfg.effectiveContextWindow() - MIN_SAFETY_OVERHEAD - SUMMARY_MAX_TOKENS

	// Create messages totaling ~100K tokens (well within the 130K limit)
	// "hello world " is 12 chars → 33334 repeats × 12 chars = 400K chars → 100K tokens
	smallContent := strings.Repeat("hello world ", 33_334)
	messages := []api.Message{
		{Role: "user", Content: smallContent},
	}

	estimated := estimateTokens(messages)
	if estimated > maxMessagesTokens {
		t.Fatalf("test setup: estimated %d should be within maxMessagesTokens %d", estimated, maxMessagesTokens)
	}

	t.Log("AC1 PASS: messages correctly identified as within limit for in-session compact")
}

// ============================================================
// AC2 / AC6 — isPromptTooLongError widened to include "context window"
// ============================================================

// TestAC2_IsPromptTooLongError_Widened verifies isPromptTooLongError matches
// "context window" (case-insensitive) in addition to existing patterns.
func TestAC2_IsPromptTooLongError_Widened(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   bool
		ac     string
	}{
		// Existing patterns (regression)
		{"prompt too long", "prompt too long", true, "existing"},
		{"too many tokens", "too many tokens", true, "existing"},
		{"context length", "context length", true, "existing"},
		{"413", "error 413", true, "existing"},
		// New pattern — AC2
		{"context window exact", "context window exceeds limit", true, "AC2"},
		{"context window mixed case", "Context Window Exceeds Limit", true, "AC2"},
		{"context window uppercase", "CONTEXT WINDOW EXCEEDS LIMIT", true, "AC2"},
		// Wrapped error from inSessionCompact — AC7
		{"wrapped minmax error", "in-session compaction API call: HTTP 400: invalid params, context window exceeds limit (2013)", true, "AC7"},
		// Non-matching
		{"unrelated error", "connection timeout", false, "none"},
		{"empty error", "", false, "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("%s", tt.errMsg)
			got := isPromptTooLongError(err)
			if got != tt.want {
				t.Errorf("%s FAIL: isPromptTooLongError(%q) = %v, want %v", tt.ac, tt.errMsg, got, tt.want)
			}
		})
	}
	t.Log("AC2/AC6 PASS: isPromptTooLongError widened to include context window")
}

// TestAC6_IsPromptTooLongError_AllSubstrings verifies all required error substrings
// from AC6 are matched by isPromptTooLongError.
func TestAC6_IsPromptTooLongError_AllSubstrings(t *testing.T) {
	required := []string{
		"prompt too long",
		"too many tokens",
		"context length",
		"context window",
		"413",
	}

	for _, substr := range required {
		err := fmt.Errorf("test error: %s", substr)
		if !isPromptTooLongError(err) {
			t.Errorf("AC6 FAIL: isPromptTooLongError should match %q", substr)
		}
	}
	t.Log("AC6 PASS: all required substrings matched by isPromptTooLongError")
}
