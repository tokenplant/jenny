package api

import (
	"os"
	"strings"
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

func TestToolToSDK_WebSearchMaxUses(t *testing.T) {
	// Test that web_search with MaxUses set uses WebSearchTool20250305Param with max_uses=8
	maxUses := int64(8)
	webSearchTool := ToolParam{
		Name:        "web_search",
		Description: "Web search tool",
		InputSchema: ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
			Required:   []string{},
		},
		MaxUses: &maxUses,
	}

	sdkTool := toolToSDK(webSearchTool)

	// Verify it uses OfWebSearchTool20250305 variant
	if sdkTool.OfWebSearchTool20250305 == nil {
		t.Fatal("expected OfWebSearchTool20250305 to be non-nil for web_search with MaxUses")
	}

	// Verify MaxUses is set to 8
	if !sdkTool.OfWebSearchTool20250305.MaxUses.Valid() {
		t.Fatal("expected MaxUses to be valid")
	}
	if sdkTool.OfWebSearchTool20250305.MaxUses.Value != 8 {
		t.Errorf("expected MaxUses=8, got %d", sdkTool.OfWebSearchTool20250305.MaxUses.Value)
	}
}

func TestToolToSDK_GenericTool(t *testing.T) {
	// Test that non-web_search tools use the generic ToolParam
	tool := ToolParam{
		Name:        "read",
		Description: "Read tool",
		InputSchema: ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
			Required:   []string{},
		},
	}

	sdkTool := toolToSDK(tool)

	// Verify it uses OfTool variant
	if sdkTool.OfTool == nil {
		t.Fatal("expected OfTool to be non-nil for generic tool")
	}
	if sdkTool.OfWebSearchTool20250305 != nil {
		t.Error("expected OfWebSearchTool20250305 to be nil for generic tool")
	}
}

func TestToolToSDK_WebSearchWithoutMaxUses(t *testing.T) {
	// Test that web_search without MaxUses uses the generic ToolParam
	tool := ToolParam{
		Name:        "web_search",
		Description: "Web search tool",
		InputSchema: ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{},
			Required:   []string{},
		},
		MaxUses: nil,
	}

	sdkTool := toolToSDK(tool)

	// Verify it uses OfTool variant (not OfWebSearchTool20250305)
	if sdkTool.OfTool == nil {
		t.Fatal("expected OfTool to be non-nil for web_search without MaxUses")
	}
	if sdkTool.OfWebSearchTool20250305 != nil {
		t.Error("expected OfWebSearchTool20250305 to be nil for web_search without MaxUses")
	}
}

func TestValidateMessagesMedia_NoMedia(t *testing.T) {
	// AC6: Pass trivially when no image data is present (backward compat)
	messages := []Message{
		{Role: "user", Content: "Hello, world!"},
		{Role: "assistant", Content: "Hi there!"},
	}
	err := ValidateMessagesMedia(messages)
	if err != nil {
		t.Fatalf("expected no error for messages without media, got %v", err)
	}
}

func TestValidateMessagesMedia_DataURI(t *testing.T) {
	// AC7: data URI detection
	messages := []Message{
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "toolu_1",
					Content:   "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
				},
			},
		},
	}
	err := ValidateMessagesMedia(messages)
	if err != nil {
		t.Fatalf("expected no error for valid data URI, got %v", err)
	}
}

func TestValidateMessagesMedia_TooManyImages(t *testing.T) {
	// AC3: Returns CannotRetryError when total media items exceed 100
	messages := []Message{
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "toolu_1",
					Content:   strings.Repeat("data:image/png;base64,iVBORw0KGgo=", 101),
				},
			},
		},
	}
	err := ValidateMessagesMedia(messages)
	if err == nil {
		t.Fatal("expected error for too many images")
	}
	cannotRetry, ok := err.(*CannotRetryError)
	if !ok {
		t.Fatalf("expected CannotRetryError, got %T", err)
	}
	if !strings.Contains(cannotRetry.Message, "too many media items") {
		t.Errorf("expected message about too many media items, got %q", cannotRetry.Message)
	}
}

func TestValidateMessagesMedia_OversizedImage(t *testing.T) {
	// AC4: Returns CannotRetryError when any base64 image exceeds 5 MB
	// Create a SINGLE large base64 image (one PNG header, one long data line)
	// 5 MB =5,242,880 bytes; in base64 that needs ~6,990,507 characters
	// Use a PNG header followed by ~7MB of 'A' chars (valid base64)
	header := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	// Create a single-line base64 string that exceeds 5 MB when decoded
	// Each base64 char represents 6 bits; 4 chars represent 3 bytes
	// So7M base64 chars ≈ 5.25 MB
	padding := strings.Repeat("A", 7*1024*1024)
	largeBase64 := header + padding
	messages := []Message{
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "toolu_1",
					Content:   largeBase64,
				},
			},
		},
	}
	err := ValidateMessagesMedia(messages)
	if err == nil {
		t.Fatal("expected error for oversized image")
	}
	cannotRetry, ok := err.(*CannotRetryError)
	if !ok {
		t.Fatalf("expected CannotRetryError, got %T", err)
	}
	if !strings.Contains(cannotRetry.Message, "exceeds maximum allowed size") {
		t.Errorf("expected message about image size, got %q", cannotRetry.Message)
	}
}

func TestValidateMessagesMedia_MixedContent(t *testing.T) {
	// AC7: Mixed content with text, tool results, and images
	messages := []Message{
		{Role: "user", Content: "What do you see?"},
		{
			Role:    "assistant",
			Content: "I see an image",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "toolu_1",
					Content:   "Here is the result: data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
				},
			},
		},
		{Role: "user", Content: "Thanks!"},
	}
	err := ValidateMessagesMedia(messages)
	if err != nil {
		t.Fatalf("expected no error for mixed content with valid image, got %v", err)
	}
}

func TestValidateMessagesMedia_RawBase64Headers(t *testing.T) {
	// Test detection of raw base64 image headers without data URI prefix
	messages := []Message{
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "toolu_1",
					Content:   "/9j/AAAABJRU5ErkJggg==", // JPEG header
				},
			},
		},
	}
	err := ValidateMessagesMedia(messages)
	if err != nil {
		t.Fatalf("expected no error for raw JPEG header, got %v", err)
	}
}

func TestMaxMediaItemsPerRequestConstant(t *testing.T) {
	if MaxMediaItemsPerRequest != 100 {
		t.Errorf("expected MaxMediaItemsPerRequest to be 100, got %d", MaxMediaItemsPerRequest)
	}
}

func TestMaxBase64ImageSizeConstant(t *testing.T) {
	if MaxBase64ImageSize != 5*1024*1024 {
		t.Errorf("expected MaxBase64ImageSize to be 5*1024*1024, got %d", MaxBase64ImageSize)
	}
}
