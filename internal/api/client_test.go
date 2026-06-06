package api

import (
	"os"
	"testing"
	"time"
)

func TestNewClientWithModelEnvVar(t *testing.T) {
	// Save original env var
	origModel := os.Getenv("ANTHROPIC_MODEL")
	defer func() {
		if origModel != "" {
			os.Setenv("ANTHROPIC_MODEL", origModel)
		} else {
			os.Unsetenv("ANTHROPIC_MODEL")
		}
	}()

	// Set ANTHROPIC_MODEL env var
	os.Setenv("ANTHROPIC_MODEL", "test-env-model")

	// Create client with empty model - should use env var
	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The client should have picked up the env var
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.GetModel() != "test-env-model" {
		t.Errorf("expected model 'test-env-model', got %q", client.GetModel())
	}
}

func TestNewClientWithModelEmpty(t *testing.T) {
	// Save original env var
	origModel := os.Getenv("ANTHROPIC_MODEL")
	defer func() {
		if origModel != "" {
			os.Setenv("ANTHROPIC_MODEL", origModel)
		} else {
			os.Unsetenv("ANTHROPIC_MODEL")
		}
	}()

	// Ensure ANTHROPIC_MODEL is not set
	os.Unsetenv("ANTHROPIC_MODEL")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	// Should use default model when env var is not set
	if client.GetModel() != defaultModel {
		t.Errorf("expected model %q, got %q", defaultModel, client.GetModel())
	}
}

func TestNewClientWithModelOverride(t *testing.T) {
	// Save original env var
	origModel := os.Getenv("ANTHROPIC_MODEL")
	defer func() {
		if origModel != "" {
			os.Setenv("ANTHROPIC_MODEL", origModel)
		} else {
			os.Unsetenv("ANTHROPIC_MODEL")
		}
	}()

	// Set ANTHROPIC_MODEL env var
	os.Setenv("ANTHROPIC_MODEL", "env-model")

	// Create client with model override - should use override
	client, err := NewClientWithModel("override-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	// Override should take precedence over env var
	if client.GetModel() != "override-model" {
		t.Errorf("expected model 'override-model', got %q", client.GetModel())
	}
}

func TestDefaultModelConstant(t *testing.T) {
	// Verify defaultModel constant is defined and non-empty
	if defaultModel == "" {
		t.Error("defaultModel should not be empty")
	}
	// Verify it matches the expected model string
	if defaultModel != "deepseek-v4-flash" {
		t.Errorf("expected defaultModel 'deepseek-v4-flash', got %q", defaultModel)
	}
}

func TestStreamAccumulator(t *testing.T) {
	acc := newStreamAccumulator()

	// Test appendText for text block - must set type first (as in streaming)
	acc.setBlockType(0, "text")
	acc.appendText(0, "Hello")
	acc.appendText(0, " World")
	if acc.texts[0] != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", acc.texts[0])
	}
	if acc.blocks[0].Text != "Hello World" {
		t.Errorf("expected block text 'Hello World', got %q", acc.blocks[0].Text)
	}

	// Test setBlockType
	acc.setBlockType(1, "tool_use")
	if acc.blocks[1].Type != "tool_use" {
		t.Errorf("expected block type 'tool_use', got %q", acc.blocks[1].Type)
	}

	// Test setUsage
	acc.setUsage(Usage{InputTokens: 100, OutputTokens: 50})
	if acc.usage.InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %d", acc.usage.InputTokens)
	}
	if acc.usage.OutputTokens != 50 {
		t.Errorf("expected output tokens 50, got %d", acc.usage.OutputTokens)
	}

	// Test setStopReason
	acc.setStopReason(StopReasonEndTurn)
	if acc.stopReason != StopReasonEndTurn {
		t.Errorf("expected stop reason 'end_turn', got %q", acc.stopReason)
	}

	// Test getBlocks
	blocks := acc.getBlocks()
	if len(blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("expected block 0 type 'text', got %q", blocks[0].Type)
	}
	if blocks[1].Type != "tool_use" {
		t.Errorf("expected block 1 type 'tool_use', got %q", blocks[1].Type)
	}
}

func TestStreamAccumulatorEnsureBlock(t *testing.T) {
	acc := newStreamAccumulator()

	// Ensure block at index 5 should create intermediate blocks
	acc.ensureBlock(5)
	if len(acc.blocks) != 6 {
		t.Errorf("expected 6 blocks, got %d", len(acc.blocks))
	}
	if len(acc.texts) != 6 {
		t.Errorf("expected 6 texts, got %d", len(acc.texts))
	}
}

func TestDefaultIdleTimeout(t *testing.T) {
	if DefaultIdleTimeout != 30*time.Second {
		t.Errorf("expected DefaultIdleTimeout to be 30s, got %v", DefaultIdleTimeout)
	}
}

func TestDefaultFallbackTimeout(t *testing.T) {
	if DefaultFallbackTimeout != 5*time.Minute {
		t.Errorf("expected DefaultFallbackTimeout to be 5m, got %v", DefaultFallbackTimeout)
	}
}

func TestStreamResult(t *testing.T) {
	result := &StreamResult{
		Blocks: []ContentBlock{
			{Type: "text", Text: "Hello"},
			{Type: "tool_use", ToolID: "toolu_123", ToolName: "Read"},
		},
		StopReason: StopReasonEndTurn,
		Usage:      Usage{InputTokens: 100, OutputTokens: 50},
	}

	if len(result.Blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(result.Blocks))
	}
	if result.StopReason != StopReasonEndTurn {
		t.Errorf("expected stop reason 'end_turn', got %q", result.StopReason)
	}
	if result.Usage.InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %d", result.Usage.InputTokens)
	}
}

func TestStreamContentBlock(t *testing.T) {
	block := StreamContentBlock{
		Index: 1,
		Block: ContentBlock{
			Type:      "tool_use",
			ToolID:    "toolu_123",
			ToolName:  "Read",
			ToolInput: map[string]any{"file_path": "/tmp/test.txt"},
		},
	}

	if block.Index != 1 {
		t.Errorf("expected index 1, got %d", block.Index)
	}
	if block.Block.Type != "tool_use" {
		t.Errorf("expected type 'tool_use', got %q", block.Block.Type)
	}
	if block.Block.ToolID != "toolu_123" {
		t.Errorf("expected tool ID 'toolu_123', got %q", block.Block.ToolID)
	}
}
