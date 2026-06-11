package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/testutil/mockapi"
)

func TestNewClientWithModelEnvVar(t *testing.T) {
	t.Setenv("ANTHROPIC_MODEL", "test-env-model")

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
	t.Setenv("ANTHROPIC_MODEL", "")

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
	t.Setenv("ANTHROPIC_MODEL", "env-model")

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
	if defaultModel != "claude-opus-4-5-20251101" {
		t.Errorf("expected defaultModel 'claude-opus-4-5-20251101', got %q", defaultModel)
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
	// Test that web_search uses ToolParam with input_schema (universal normalization)
	// Previously used WebSearchTool20250305Param but that lacks input_schema
	// which causes errors with providers like MiniMax
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

	sdkTool := toolToSDK(webSearchTool, false)

	// Verify it uses OfTool variant (universal path for web_search)
	if sdkTool.OfTool == nil {
		t.Fatal("expected OfTool to be non-nil for web_search")
	}

	// Verify input_schema is present
	if sdkTool.OfTool.InputSchema.Properties == nil {
		t.Fatal("expected input_schema.Properties to be non-nil")
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

	sdkTool := toolToSDK(tool, false)

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

	sdkTool := toolToSDK(tool, false)

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

func TestValidateMessagesMedia_ProseTextWithMagicBytes(t *testing.T) {
	// AC1: Prose text containing a raw image header sequence followed by
	// alphanumeric text does NOT produce a false positive error.
	// Prose must be in ToolResultBlock.Content to reach countMediaInContent.
	messages := []Message{
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "toolu_1",
					Content:   "Please analyze the image at /9j/4AAQSkZJR and tell me what it shows",
				},
			},
		},
	}
	err := ValidateMessagesMedia(messages)
	if err != nil {
		t.Fatalf("expected no error for prose text with magic byte sequence, got %v", err)
	}
}

func TestValidateMessagesMedia_MultilineRawBase64(t *testing.T) {
	// AC2: A multiline (MIME-formatted with \n separators) raw-base64 image
	// is correctly detected as one image item
	messages := []Message{
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "toolu_1",
					Content:   "/9j/AAAABJRU\n5ErkJggg==", // JPEG header with newline
				},
			},
		},
	}
	err := ValidateMessagesMedia(messages)
	if err != nil {
		t.Fatalf("expected no error for multiline raw base64, got %v", err)
	}
}

func TestValidateMessagesMedia_MultilineDataURI(t *testing.T) {
	// AC3: Data URIs with multiline base64 payloads are counted and sized correctly
	messages := []Message{
		{
			Role: "user",
			ToolResults: []ToolResultBlock{
				{
					ToolUseID: "toolu_1",
					Content:   "data:image/png;base64,iVBORw0KGgo\nAAAANSUhEUgAAAAEAAAAB\nCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
				},
			},
		},
	}
	err := ValidateMessagesMedia(messages)
	if err != nil {
		t.Fatalf("expected no error for multiline data URI, got %v", err)
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

// ---------------------------------------------------------------------------
// AC2: Beta header sent on the wire (non-streaming)
// ---------------------------------------------------------------------------

func TestClient_NonStreaming_SendsPromptCachingBetaHeader(t *testing.T) {
	var capturedBeta []string
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBeta = r.Header.Values("anthropic-beta")
		io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	client.SetMaxTokensOverride(8192)
	_, err := client.SendMessage(context.Background(), nil, nil, nil, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	// Should have at least one header
	if len(capturedBeta) == 0 {
		t.Fatal("expected at least one anthropic-beta header")
	}

	// Default beta should be present in one of the headers
	found := false
	for _, h := range capturedBeta {
		if strings.Contains(h, "prompt-caching-2024-07-31") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected anthropic-beta header to contain 'prompt-caching-2024-07-31', got %v", capturedBeta)
	}
}

// ---------------------------------------------------------------------------
// AC2: Additional ANTHROPIC_BETAS env var headers
// ---------------------------------------------------------------------------

func TestClient_ANTHROPIC_BETAS_AdditionalHeaders(t *testing.T) {
	var capturedBeta []string
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBeta = r.Header.Values("anthropic-beta")
		io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")
	t.Setenv("ANTHROPIC_BETAS", "beta1, beta2,beta3") // test spaces around commas

	client, _ := NewClientWithModel("m")
	client.SetMaxTokensOverride(8192)
	_, err := client.SendMessage(context.Background(), nil, nil, nil, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	// Should have at least 4 headers: prompt-caching + beta1 + beta2 + beta3
	if len(capturedBeta) < 4 {
		t.Fatalf("expected at least 4 anthropic-beta headers, got %d: %v", len(capturedBeta), capturedBeta)
	}

	// Build a combined string for checking (simpler approach for multiple headers)
	allBetas := strings.Join(capturedBeta, ",")

	// Default beta should be present
	if !strings.Contains(allBetas, "prompt-caching-2024-07-31") {
		t.Errorf("expected default beta 'prompt-caching-2024-07-31' in headers, got %v", capturedBeta)
	}
	// Additional betas should be present
	if !strings.Contains(allBetas, "beta1") {
		t.Errorf("expected 'beta1' in anthropic-beta headers, got %v", capturedBeta)
	}
	if !strings.Contains(allBetas, "beta2") {
		t.Errorf("expected 'beta2' in anthropic-beta headers, got %v", capturedBeta)
	}
	if !strings.Contains(allBetas, "beta3") {
		t.Errorf("expected 'beta3' in anthropic-beta headers, got %v", capturedBeta)
	}
}

func TestClient_ANTHROPIC_BETAS_Streaming(t *testing.T) {
	var capturedBeta []string
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"m\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBeta = r.Header.Values("anthropic-beta")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		for _, e := range events {
			w.Write([]byte(e))
		}
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")
	t.Setenv("ANTHROPIC_BETAS", "prompt_eval_cache,some_other_beta")

	client, _ := NewClientWithModel("m")
	blocksChan, _ := client.SendMessageStream(context.Background(), nil, nil, nil, "", 5*time.Second, 5*time.Second, nil)
	for range blocksChan {
		// drain
	}

	// Should have at least 3 headers: prompt-caching + prompt_eval_cache + some_other_beta
	if len(capturedBeta) < 3 {
		t.Fatalf("expected at least 3 anthropic-beta headers, got %d: %v", len(capturedBeta), capturedBeta)
	}

	// Build a combined string for checking
	allBetas := strings.Join(capturedBeta, ",")

	// Default beta + custom betas should be present
	if !strings.Contains(allBetas, "prompt-caching-2024-07-31") {
		t.Errorf("expected default beta 'prompt-caching-2024-07-31' in headers, got %v", capturedBeta)
	}
	if !strings.Contains(allBetas, "prompt_eval_cache") {
		t.Errorf("expected 'prompt_eval_cache' in anthropic-beta headers, got %v", capturedBeta)
	}
	if !strings.Contains(allBetas, "some_other_beta") {
		t.Errorf("expected 'some_other_beta' in anthropic-beta headers, got %v", capturedBeta)
	}
}

// ---------------------------------------------------------------------------
// AC3: API_TIMEOUT_MS env var
// ---------------------------------------------------------------------------

func TestResolveTimeout(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{"empty string returns1 hour", "", 1 * time.Hour},
		{"valid5 minutes", "300000", 5 * time.Minute},
		{"valid 1 hour", "3600000", 1 * time.Hour},
		{"invalid string returns 1 hour", "invalid", 1 * time.Hour},
		{"negative value returns 1 hour", "-1000", 1 * time.Hour},
		{"zero returns 1 hour", "0", 1 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveTimeout(tt.envValue)
			if got != tt.expected {
				t.Errorf("ResolveTimeout(%q) = %v, want %v", tt.envValue, got, tt.expected)
			}
		})
	}
}

func TestClient_API_TIMEOUT_MS_Creation(t *testing.T) {
	// Test that client creation works with API_TIMEOUT_MS set
	t.Setenv("API_TIMEOUT_MS", "300000") // 5 minutes

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestClient_API_TIMEOUT_MS_InvalidValue(t *testing.T) {
	// Test that invalid values fall back to default (1 hour)
	t.Setenv("API_TIMEOUT_MS", "invalid")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestClient_API_TIMEOUT_MS_NegativeValue(t *testing.T) {
	// Test that negative values fall back to default
	t.Setenv("API_TIMEOUT_MS", "-1000")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("NewClientWithModel error = %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// ---------------------------------------------------------------------------
// AC3: Beta header sent on the wire (streaming)
// ---------------------------------------------------------------------------

func TestClient_Streaming_SendsPromptCachingBetaHeader(t *testing.T) {
	var capturedBeta string
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"m\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		for _, e := range events {
			w.Write([]byte(e))
		}
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	blocksChan, _ := client.SendMessageStream(context.Background(), nil, nil, nil, "", 5*time.Second, 5*time.Second, nil)
	for range blocksChan {
		// drain
	}

	if !strings.Contains(capturedBeta, "prompt-caching-2024-07-31") {
		t.Errorf("expected anthropic-beta header to contain 'prompt-caching-2024-07-31', got %q", capturedBeta)
	}
}

func TestClient_Streaming_ThinkingAccumulation(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"m\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"thinking \"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hard\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-123\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		for _, e := range events {
			w.Write([]byte(e))
		}
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	blocksChan, result := client.SendMessageStream(context.Background(), nil, nil, nil, "", 5*time.Second, 5*time.Second, nil)
	var blocks []StreamContentBlock
	for b := range blocksChan {
		if b.Type != "stream_event" {
			blocks = append(blocks, b)
		}
	}

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}

	if blocks[0].Block.Type != "thinking" {
		t.Errorf("expected block type 'thinking', got %q", blocks[0].Block.Type)
	}
	if blocks[0].Block.Thinking != "thinking hard" {
		t.Errorf("expected thinking content 'thinking hard', got %q", blocks[0].Block.Thinking)
	}
	if blocks[0].Block.Signature != "sig-123" {
		t.Errorf("expected signature 'sig-123', got %q", blocks[0].Block.Signature)
	}

	if result.Blocks[0].Thinking != "thinking hard" {
		t.Errorf("expected result thinking content 'thinking hard', got %q", result.Blocks[0].Thinking)
	}
}

// ---------------------------------------------------------------------------
// AC4: System prompt cache_control regression (non-streaming)
// ---------------------------------------------------------------------------

func TestClient_SystemPrompt_HasCacheControl_Ephemeral(t *testing.T) {
	var capturedBody []byte
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	client.SetMaxTokensOverride(8192)
	_, err := client.SendMessage(context.Background(), nil, nil, nil, "system prompt content")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	system, ok := parsed["system"].([]any)
	if !ok || len(system) == 0 {
		t.Fatal("request body missing or empty system array")
	}
	sysBlock, ok := system[0].(map[string]any)
	if !ok {
		t.Fatal("system[0] is not a map")
	}
	cacheCtrl, ok := sysBlock["cache_control"].(map[string]any)
	if !ok {
		t.Fatal("system[0] missing cache_control")
	}
	if cacheCtrl["type"] != "ephemeral" {
		t.Errorf("system[0].cache_control.type = %q, want ephemeral", cacheCtrl["type"])
	}
}

// ---------------------------------------------------------------------------
// AC5: Tools-array cache breakpoint (last entry only)
// ---------------------------------------------------------------------------

func TestClient_Tools_LastEntryHasCacheControl_Ephemeral(t *testing.T) {
	var capturedBody []byte
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	tools := []ToolParam{
		{Name: "tool1", Description: "First tool", InputSchema: ToolInputSchema{Type: "object", Properties: map[string]any{}, Required: []string{}}},
		{Name: "tool2", Description: "Second tool", InputSchema: ToolInputSchema{Type: "object", Properties: map[string]any{}, Required: []string{}}},
		{Name: "tool3", Description: "Third tool", InputSchema: ToolInputSchema{Type: "object", Properties: map[string]any{}, Required: []string{}}},
	}
	client.SetMaxTokensOverride(8192)
	_, err := client.SendMessage(context.Background(), nil, tools, nil, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	toolsArr, ok := parsed["tools"].([]any)
	if !ok || len(toolsArr) != 3 {
		t.Fatalf("expected 3 tools, got %v", toolsArr)
	}

	// tools[0] and tools[1] should NOT have cache_control
	for i := range 2 {
		toolBlock, ok := toolsArr[i].(map[string]any)
		if !ok {
			t.Fatalf("tools[%d] is not a map", i)
		}
		if _, hasCacheCtrl := toolBlock["cache_control"]; hasCacheCtrl {
			t.Errorf("tools[%d] should NOT have cache_control, but does", i)
		}
	}

	// tools[2] (last) SHOULD have cache_control
	lastTool, ok := toolsArr[2].(map[string]any)
	if !ok {
		t.Fatal("tools[2] is not a map")
	}
	cacheCtrl, ok := lastTool["cache_control"].(map[string]any)
	if !ok {
		t.Fatal("tools[2] missing cache_control")
	}
	if cacheCtrl["type"] != "ephemeral" {
		t.Errorf("tools[2].cache_control.type = %q, want ephemeral", cacheCtrl["type"])
	}
}

// ---------------------------------------------------------------------------
// AC6: Zero tools is safe
// ---------------------------------------------------------------------------

func TestClient_NoTools_NoToolsCacheControl_NoPanic(t *testing.T) {
	var capturedBody []byte
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	client.SetMaxTokensOverride(8192)
	// Empty tools slice
	_, err := client.SendMessage(context.Background(), nil, []ToolParam{}, nil, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	// tools key should not exist or be empty
	if toolsArr, ok := parsed["tools"].([]any); ok && len(toolsArr) > 0 {
		t.Errorf("expected no tools in request body, got %d tools", len(toolsArr))
	}
}

// ---------------------------------------------------------------------------
// AC7: Usage tokens regression (cache_read and cache_creation)
// ---------------------------------------------------------------------------

func TestClient_NonStreaming_UsageTokensRegression(t *testing.T) {
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":500,"cache_creation_input_tokens":100}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	client.SetMaxTokensOverride(8192)
	resp, err := client.SendMessage(context.Background(), nil, nil, nil, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	if resp.Usage.CacheReadInputTokens != 500 {
		t.Errorf("CacheReadInputTokens = %d, want 500", resp.Usage.CacheReadInputTokens)
	}
	if resp.Usage.CacheCreationInputTokens != 100 {
		t.Errorf("CacheCreationInputTokens = %d, want 100", resp.Usage.CacheCreationInputTokens)
	}
}

func TestToolToSDK_ExtraFields(t *testing.T) {
	// AC3: Verify extra fields are preserved
	extraFields := map[string]any{
		"$defs": map[string]any{
			"item": map[string]any{
				"type": "string",
			},
		},
		"examples": []any{"example1"},
	}
	tool := ToolParam{
		Name:        "test_tool",
		Description: "A tool with extra fields",
		InputSchema: ToolInputSchema{
			Type:        "object",
			Properties:  map[string]any{"foo": map[string]any{"type": "string"}},
			Required:    []string{"foo"},
			ExtraFields: extraFields,
		},
	}

	sdkTool := toolToSDK(tool, false)

	if sdkTool.OfTool == nil {
		t.Fatal("expected OfTool to be non-nil")
	}

	// Verify ExtraFields are populated on the SDK param
	if len(sdkTool.OfTool.InputSchema.ExtraFields) != 2 {
		t.Errorf("expected 2 extra fields, got %d", len(sdkTool.OfTool.InputSchema.ExtraFields))
	}

	if _, ok := sdkTool.OfTool.InputSchema.ExtraFields["$defs"]; !ok {
		t.Error("expected $defs to be present in ExtraFields")
	}

	// Marshal to JSON to ensure it's serialized correctly
	data, err := json.Marshal(sdkTool.OfTool)
	if err != nil {
		t.Fatalf("failed to marshal tool: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal tool: %v", err)
	}

	inputSchema, ok := result["input_schema"].(map[string]any)
	if !ok {
		t.Fatal("input_schema not found in marshaled output")
	}

	if _, ok := inputSchema["$defs"]; !ok {
		t.Error("$defs missing from serialized input_schema")
	}
	if _, ok := inputSchema["examples"]; !ok {
		t.Error("examples missing from serialized input_schema")
	}
}

func TestToolToSDK_EmptyProperties(t *testing.T) {
	// AC2: Universal behavior - empty properties get __arg__ placeholder
	// This is now handled by NormalizeMessages before toolToSDK is called
	// This test verifies toolToSDK works with pre-normalized tools (already have __arg__)
	tool := ToolParam{
		Name:        "empty_tool",
		Description: "A tool with no properties",
		InputSchema: ToolInputSchema{
			Type:       "object",
			Properties: map[string]any{"__arg__": map[string]any{"type": "string", "description": "Placeholder"}},
			Required:   []string{},
		},
	}

	sdkTool := toolToSDK(tool, false)

	if sdkTool.OfTool == nil {
		t.Fatal("expected OfTool to be non-nil")
	}

	data, err := json.Marshal(sdkTool.OfTool)
	if err != nil {
		t.Fatalf("failed to marshal tool: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal tool: %v", err)
	}

	inputSchema, ok := result["input_schema"].(map[string]any)
	if !ok {
		t.Fatal("input_schema not found in marshaled output")
	}

	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties missing from serialized input_schema")
	}
	// Universal: pre-normalized empty properties have __arg__ placeholder
	if len(props) != 1 {
		t.Errorf("expected 1 placeholder property, got %d items", len(props))
	}
	if _, hasArg := props["__arg__"]; !hasArg {
		t.Error("expected __arg__ placeholder property")
	}
}

func TestClient_ToolResultDedup(t *testing.T) {
	// AC3: API serialization deduplicates tool_results as safety net
	var capturedBody []byte
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	client.SetMaxTokensOverride(8192)

	messages := []Message{
		{
			Role:    "user",
			Content: "Test",
			ToolResults: []ToolResultBlock{
				{ToolUseID: "id_1", Content: "First result for id_1"},
				{ToolUseID: "id_1", Content: "Second result for id_1 (duplicate)"},
			},
		},
	}

	_, err := client.SendMessage(context.Background(), messages, nil, nil, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	msgs, ok := parsed["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatal("request body missing or empty messages array")
	}

	userMsg, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatal("first message is not a map")
	}

	content, ok := userMsg["content"].([]any)
	if !ok {
		t.Fatal("message missing content array")
	}

	// Count tool_result blocks with tool_use_id = "id_1"
	toolResultCount := 0
	var lastContent string
	for _, block := range content {
		blockMap, ok := block.(map[string]any)
		if !ok {
			continue
		}
		if blockMap["type"] == "tool_result" {
			if tid, ok := blockMap["tool_use_id"].(string); ok && tid == "id_1" {
				toolResultCount++
				// Check content - should be the last writer's content
				contentArr, ok := blockMap["content"].([]any)
				if ok && len(contentArr) > 0 {
					if textBlock, ok := contentArr[0].(map[string]any); ok {
						if text, ok := textBlock["text"].(string); ok {
							lastContent = text
						}
					}
				}
			}
		}
	}

	if toolResultCount != 1 {
		t.Errorf("expected exactly 1 tool_result with id_1, got %d", toolResultCount)
	}

	// Last writer wins
	if lastContent != "Second result for id_1 (duplicate)" {
		t.Errorf("expected last content 'Second result for id_1 (duplicate)', got %q", lastContent)
	}
}

func TestDeduplicateToolResults(t *testing.T) {
	// Test the deduplicateToolResults helper directly
	tests := []struct {
		name     string
		input    []ToolResultBlock
		expected []ToolResultBlock
	}{
		{
			name:     "empty input",
			input:    []ToolResultBlock{},
			expected: []ToolResultBlock{},
		},
		{
			name: "no duplicates",
			input: []ToolResultBlock{
				{ToolUseID: "id_A", Content: "Result A"},
				{ToolUseID: "id_B", Content: "Result B"},
			},
			expected: []ToolResultBlock{
				{ToolUseID: "id_A", Content: "Result A"},
				{ToolUseID: "id_B", Content: "Result B"},
			},
		},
		{
			name: "duplicate IDs - last writer wins",
			input: []ToolResultBlock{
				{ToolUseID: "id_1", Content: "First"},
				{ToolUseID: "id_2", Content: "Second"},
				{ToolUseID: "id_1", Content: "Third (last)"},
			},
			// Order is maintained by first-seen, but content is replaced by last writer
			expected: []ToolResultBlock{
				{ToolUseID: "id_1", Content: "Third (last)"}, // id_1 replaced by last writer
				{ToolUseID: "id_2", Content: "Second"},       // id_2 unchanged
			},
		},
		{
			name: "all duplicates - last of each wins",
			input: []ToolResultBlock{
				{ToolUseID: "id_1", Content: "First"},
				{ToolUseID: "id_1", Content: "Second"},
				{ToolUseID: "id_1", Content: "Third"},
			},
			expected: []ToolResultBlock{
				{ToolUseID: "id_1", Content: "Third"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := deduplicateToolResults(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("expected %d results, got %d", len(tc.expected), len(result))
				return
			}
			for i, tr := range result {
				if tr.ToolUseID != tc.expected[i].ToolUseID {
					t.Errorf("result[%d].ToolUseID = %q, want %q", i, tr.ToolUseID, tc.expected[i].ToolUseID)
				}
				if tr.Content != tc.expected[i].Content {
					t.Errorf("result[%d].Content = %q, want %q", i, tr.Content, tc.expected[i].Content)
				}
			}
		})
	}
}

// TestNormalize_UniversalEmptySchemaPlaceholder tests that empty input_schema.properties
// gets __arg__ placeholder regardless of ANTHROPIC_BASE_URL.
func TestNormalize_UniversalEmptySchemaPlaceholder(t *testing.T) {
	// Three-URL matrix: Anthropic, MiniMax-like, DeepSeek-like
	urls := []string{
		"https://api.anthropic.com",
		"https://api.minimaxi.com/anthropic",
		"https://api.deepseek.com/v1",
	}

	for _, baseURL := range urls {
		t.Run(baseURL, func(t *testing.T) {
			t.Setenv("ANTHROPIC_BASE_URL", baseURL)

			tools := []ToolParam{
				{
					Name:        "empty_tool",
					Description: "A tool with no properties",
					InputSchema: ToolInputSchema{
						Type:       "object",
						Properties: nil,
						Required:   nil,
					},
				},
			}

			// NormalizeMessages should add __arg__ placeholder universally
			_, normalizedTools, logs := NormalizeMessages(nil, tools, Capabilities{})

			if len(normalizedTools) != 1 {
				t.Fatalf("expected 1 normalized tool, got %d", len(normalizedTools))
			}

			props := normalizedTools[0].InputSchema.Properties
			if props == nil {
				t.Fatal("properties should not be nil after normalization")
			}
			if len(props) != 1 {
				t.Errorf("expected 1 property (__arg__), got %d", len(props))
			}
			if _, hasArg := props["__arg__"]; !hasArg {
				t.Error("expected __arg__ placeholder property")
			}

			// Verify log was recorded
			if len(logs) == 0 {
				t.Error("expected normalization log entry for EmptySchemaPlaceholder")
			}
		})
	}
}

// TestNormalize_RoutesThroughNormalizeMessages verifies that NormalizeMessages is called
// and properly normalizes messages before serialization.
func TestNormalize_RoutesThroughNormalizeMessages(t *testing.T) {
	var capturedBody []byte
	ms := mockapi.NewMockServer()
	ms.SetPathHandler("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"}],"model":"m","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":5}}`
		w.Write([]byte(resp))
	})
	defer ms.Close()

	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("ANTHROPIC_API_KEY", "test-key-0000000000000000")

	client, _ := NewClientWithModel("m")
	client.SetMaxTokensOverride(8192)

	// Create a tool with empty properties - should get __arg__ via NormalizeMessages
	tools := []ToolParam{
		{
			Name:        "empty_tool",
			Description: "A tool with no properties",
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: nil,
				Required:   nil,
			},
		},
	}

	messages := []Message{
		{Role: "user", Content: "Test"},
	}

	_, err := client.SendMessage(context.Background(), messages, tools, nil, "")
	if err != nil {
		t.Fatalf("SendMessage error = %v", err)
	}

	// Parse the captured request body and verify __arg__ was added
	var parsed map[string]any
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	toolsArr, ok := parsed["tools"].([]any)
	if !ok || len(toolsArr) == 0 {
		t.Fatal("request body missing or empty tools array")
	}

	toolMap, ok := toolsArr[0].(map[string]any)
	if !ok {
		t.Fatal("first tool is not a map")
	}

	inputSchema, ok := toolMap["input_schema"].(map[string]any)
	if !ok {
		t.Fatal("tool missing input_schema")
	}

	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("input_schema missing properties")
	}

	// Verify __arg__ placeholder was added by NormalizeMessages
	if _, hasArg := props["__arg__"]; !hasArg {
		t.Error("expected __arg__ placeholder property in serialized request (NormalizeMessages should have added it)")
	}
}
