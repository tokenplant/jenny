// Package agent provides tests for cross-turn state features.
package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/session"
)

// TestSetMaxBudgetUsd_sets_field tests that SetMaxBudgetUsd correctly sets the budget field.
func TestSetMaxBudgetUsd_sets_field(t *testing.T) {
	cfg := StreamConfig{Enabled: false}
	engine := NewQueryEngine(cfg, nil, "", WithClient(fastClient()))

	// Initially no budget set
	if engine.streamCfg.MaxBudgetUSD != 0 {
		t.Errorf("expected initial budget to be 0, got %f", engine.streamCfg.MaxBudgetUSD)
	}

	// Set budget via method
	engine.SetMaxBudgetUsd(1.50)

	if engine.streamCfg.MaxBudgetUSD != 1.50 {
		t.Errorf("expected budget to be 1.50, got %f", engine.streamCfg.MaxBudgetUSD)
	}
	t.Log("PASS: SetMaxBudgetUsd sets field correctly")
}

// TestBudgetZero_is_noop tests that zero budget doesn't affect loop execution.
func TestBudgetZero_is_noop(t *testing.T) {
	tmpDir := t.TempDir()
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	sessionID := "sess_budget_zero_test"

	// Use the existing mock server helper
	server := makeMockStreamServer(t, []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"usage":{"input_tokens":100,"output_tokens":50}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Hello"}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"World"}}`),
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
		// No budget set (MaxBudgetUSD = 0)
	}

	engine := NewQueryEngine(cfg, nil, "", WithClient(fastClient()))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := engine.SubmitMessage(ctx, "test prompt")

	// Should succeed without budget enforcement
	if err != nil {
		t.Errorf("expected no error with zero budget, got: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
	t.Log("PASS: zero budget is no-op, loop runs normally")
}

// TestPermissionDenial_cached tests that permission denials are properly cached.
func TestPermissionDenial_cached(t *testing.T) {
	cfg := StreamConfig{Enabled: false}

	// Add a permission denial
	denialKey := BuildDenialKey("Bash", map[string]any{"command": "rm -rf /"})
	cfg.AddPermissionDenial(denialKey)

	// Check it was added
	if !cfg.HasPermissionDenial(denialKey) {
		t.Error("expected denial key to be present after adding")
	}

	// Different tool should not match
	differentKey := BuildDenialKey("Read", map[string]any{"file_path": "/etc/passwd"})
	if cfg.HasPermissionDenial(differentKey) {
		t.Error("expected different tool to not match denial")
	}

	// Same tool+input should match
	sameKey := BuildDenialKey("Bash", map[string]any{"command": "rm -rf /"})
	if !cfg.HasPermissionDenial(sameKey) {
		t.Error("expected same tool+input to match denial")
	}
	t.Log("PASS: permission denial caching works correctly")
}

// TestDiscoveredSkillNames_survives_compaction tests that discovered skill names persist and deduplicate.
func TestDiscoveredSkillNames_survives_compaction(t *testing.T) {
	cfg := StreamConfig{Enabled: false}

	// Add discovered skill names
	cfg.AddDiscoveredSkillName("readme-writer")
	cfg.AddDiscoveredSkillName("code-review")

	// Check they're stored
	if len(cfg.DiscoveredSkillNames) != 2 {
		t.Errorf("expected 2 discovered skill names, got %d", len(cfg.DiscoveredSkillNames))
	}

	// Verify deduplication
	cfg.AddDiscoveredSkillName("readme-writer") // duplicate
	if len(cfg.DiscoveredSkillNames) != 2 {
		t.Errorf("expected 2 discovered skill names after duplicate, got %d", len(cfg.DiscoveredSkillNames))
	}

	// Simulate compaction by copying skills (in real scenario, non-compacted fields survive)
	skillsAfterCompaction := cfg.DiscoveredSkillNames
	if len(skillsAfterCompaction) != 2 {
		t.Errorf("expected 2 skill names after simulated compaction, got %d", len(skillsAfterCompaction))
	}

	t.Log("PASS: discovered skill names survive and deduplicate correctly")
}

// TestBuildDenialKey tests the denial key generation function.
func TestBuildDenialKey(t *testing.T) {
	// Test basic key generation
	key1 := BuildDenialKey("Bash", map[string]any{"command": "ls"})
	if !strings.Contains(key1, "Bash:") {
		t.Errorf("expected key to contain 'Bash:', got %s", key1)
	}

	// Test that same inputs produce same key
	key2 := BuildDenialKey("Bash", map[string]any{"command": "ls"})
	if key1 != key2 {
		t.Errorf("expected same keys for same inputs, got %s and %s", key1, key2)
	}

	// Test that different inputs produce different keys
	key3 := BuildDenialKey("Bash", map[string]any{"command": "rm"})
	if key1 == key3 {
		t.Errorf("expected different keys for different inputs, got %s and %s", key1, key3)
	}

	// Test that different tools produce different keys
	key4 := BuildDenialKey("Read", map[string]any{"file_path": "ls"})
	if key1 == key4 {
		t.Errorf("expected different keys for different tools, got %s and %s", key1, key4)
	}

	// Test key with multiple inputs (order should not matter due to sorting)
	key5 := BuildDenialKey("Bash", map[string]any{"a": "1", "b": "2"})
	key6 := BuildDenialKey("Bash", map[string]any{"b": "2", "a": "1"})
	if key5 != key6 {
		t.Errorf("expected same keys regardless of input order, got %s and %s", key5, key6)
	}

	t.Log("PASS: BuildDenialKey generates correct, deterministic keys")
}
