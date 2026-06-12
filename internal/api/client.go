// Package api provides the Anthropic API client.
package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/log"
)

// Requester defines the interface for an API client capable of sending
// messages and managing streaming sessions with fallback and retries.
type Requester interface {
	SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error)
	SendMessageStream(
		ctx context.Context,
		messages []Message,
		tools []ToolParam,
		toolResults []ToolResult,
		systemPrompt string,
		systemPromptSuffix string,
		idleTimeout time.Duration,
		fallbackTimeout time.Duration,
		onStreamingFallback func(context.Context) (*Response, error),
	) (<-chan StreamContentBlock, *StreamResult)
	SetMaxTokensOverride(maxTokens int)
	SetRetryConfig(cfg RetryConfig)
	SetBackground(isBackground bool)
}

// Client wraps an API provider.
type Client struct {
	provider          Provider
	maxTokensOverride int
	retryConfig       RetryConfig
}

// defaultModel is the default model used when ANTHROPIC_MODEL is not set.
const defaultModel = "claude-opus-4-5-20251101"

// ModelParamsInfo holds model-specific parameters for context management.
type ModelParamsInfo struct {
	ContextWindow   int
	MaxOutputTokens int
}

// ModelParams returns the context window and max output tokens for a model.
func ModelParams(model string) ModelParamsInfo {
	switch model {
	case "deepseek-v4-flash", "deepseek-v4-pro":
		return ModelParamsInfo{ContextWindow: 1_000_000, MaxOutputTokens: 8_192}
	default:
		return ModelParamsInfo{ContextWindow: 200_000, MaxOutputTokens: 20_000}
	}
}

// ResolveTimeout parses API_TIMEOUT_MS env var and returns a time.Duration.
// Returns 1 hour default if the env var is empty, invalid, or <= 0.
func ResolveTimeout(envValue string) time.Duration {
	if envValue == "" {
		return 1 * time.Hour
	}
	ms, err := strconv.Atoi(envValue)
	if err != nil || ms <= 0 {
		return 1 * time.Hour
	}
	return time.Duration(ms) * time.Millisecond
}

// NewClient creates a new API client.
func NewClient() (*Client, error) {
	return NewClientWithModel("")
}

// NewClientWithModel creates a new API client with an optional model override.
// If model is empty, reads from environment variables.
// Provider selection order: OpenAI > GenAI (Gemini / Vertex AI) > Anthropic.
// For OpenAI, if OPENAI_WIRE_API=responses, uses the Responses API provider.
func NewClientWithModel(model string) (*Client, error) {
	// OpenAI provider takes precedence
	if os.Getenv("OPENAI_BASE_URL") != "" {
		// Check wire API selection
		wireAPI := os.Getenv("OPENAI_WIRE_API")
		if wireAPI == "responses" {
			provider, err := newOpenAIResponsesProvider(model)
			if err != nil {
				return nil, fmt.Errorf("failed to create OpenAI Responses API provider: %w", err)
			}
			return &Client{
				provider:    provider,
				retryConfig: DefaultRetryConfig(),
			}, nil
		}

		provider, err := newOpenAIProvider(model)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenAI provider: %w", err)
		}
		return &Client{
			provider:    provider,
			retryConfig: DefaultRetryConfig(),
		}, nil
	}

	// GenAI provider (Gemini API or Vertex AI)
	if isGenAIEnvSet() {
		provider, err := newGenAIProvider(model)
		if err != nil {
			return nil, fmt.Errorf("failed to create GenAI provider: %w", err)
		}
		return &Client{
			provider:    provider,
			retryConfig: DefaultRetryConfig(),
		}, nil
	}

	// Default: Anthropic provider
	provider, err := newAnthropicProvider(model)
	if err != nil {
		return nil, fmt.Errorf("failed to create Anthropic provider: %w", err)
	}
	return &Client{
		provider:    provider,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// isGenAIEnvSet reports whether any of the genai-related environment
// variables are set, which would trigger selection of the genai provider.
func isGenAIEnvSet() bool {
	if os.Getenv("GENAI_API_KEY") != "" {
		return true
	}
	if os.Getenv("GENAI_BASE_URL") != "" {
		return true
	}
	if os.Getenv("GOOGLE_API_KEY") != "" || os.Getenv("GEMINI_API_KEY") != "" {
		return true
	}
	if os.Getenv("GOOGLE_CLOUD_PROJECT") != "" &&
		(os.Getenv("GOOGLE_CLOUD_LOCATION") != "" || os.Getenv("GOOGLE_CLOUD_REGION") != "") {
		return true
	}
	if os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "1" || os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "true" {
		return true
	}
	return false
}

// SetModel sets the model to use.
func (c *Client) SetModel(model string) {
	if setter, ok := c.provider.(interface{ SetModel(string) }); ok {
		setter.SetModel(model)
	}
}

// GetModel returns the model being used.
func (c *Client) GetModel() string {
	if modeler, ok := c.provider.(interface{ GetModel() string }); ok {
		return modeler.GetModel()
	}
	return ""
}

// SetMaxTokensOverride sets the max_tokens override for API requests.
func (c *Client) SetMaxTokensOverride(maxTokens int) {
	c.maxTokensOverride = maxTokens
	if setter, ok := c.provider.(interface{ SetMaxTokensOverride(int) }); ok {
		setter.SetMaxTokensOverride(maxTokens)
	}
}

// SendMessage sends a message to the API and returns the response.
func (c *Client) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	return c.provider.SendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
}

// SendMessageStream sends a streaming message to the API.
func (c *Client) SendMessageStream(
	ctx context.Context,
	messages []Message,
	tools []ToolParam,
	toolResults []ToolResult,
	systemPrompt string,
	systemPromptSuffix string,
	idleTimeout time.Duration,
	fallbackTimeout time.Duration,
	onStreamingFallback func(context.Context) (*Response, error),
) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		// Delegate to provider's streaming method
		contentChan, providerResult := c.provider.SendMessageStream(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix, idleTimeout)

		// Check for fallback conditions before streaming
		// If fallback might be needed, buffer content blocks but not stream_event blocks.
		// This ensures partial content is discarded when fallback is used.
		shouldFallback := onStreamingFallback != nil

		var pendingIndex int
		var pendingBlocks []StreamContentBlock // Buffer content blocks until we know stream is complete

		for block := range contentChan {
			if block.Type == "stream_event" {
				// Always pass through stream_event blocks for IncludePartial consumers
				blocksChan <- block
			} else if !shouldFallback {
				// Stream directly when no fallback will be used
				blocksChan <- StreamContentBlock{Index: pendingIndex, Block: block.Block}
				pendingIndex++
			} else {
				// Buffer content blocks when fallback might be needed
				pendingBlocks = append(pendingBlocks, StreamContentBlock{Index: pendingIndex, Block: block.Block})
				pendingIndex++
			}
		}

		// Copy result
		result.Blocks = providerResult.Blocks
		result.StopReason = providerResult.StopReason
		result.Usage = providerResult.Usage
		result.Error = providerResult.Error
		result.IsPermanent = providerResult.IsPermanent
		result.Model = providerResult.Model
		result.MaxTokensErr = providerResult.MaxTokensErr
		result.ContextRejected = providerResult.ContextRejected
		result.StreamComplete = providerResult.StreamComplete

		// Check if stream was incomplete (no message_stop event)
		streamIncomplete := !providerResult.StreamComplete
		isIdleTimeout := strings.Contains(result.Error, "idle timeout")

		// Handle fallback if needed
		if shouldFallback && (streamIncomplete || isIdleTimeout || len(result.Blocks) == 0) {
			if result.IsPermanent {
				log.Debug("Streaming failed with permanent error, skipping fallback", "error", result.Error)
			} else {
				log.Debug("Streaming incomplete or error, attempting fallback", "error", result.Error, "streamIncomplete", streamIncomplete, "isIdleTimeout", isIdleTimeout)
				// Stream was incomplete - discard pending blocks and use fallback
				fallbackCtx, fallbackCancel := context.WithTimeout(ctx, fallbackTimeout)
				defer fallbackCancel()
				resp, err := onStreamingFallback(fallbackCtx)
				if err != nil {
					log.Debug("Streaming fallback failed", "error", err)
					result.Error = err.Error()
					return
				}
				log.Debug("Streaming fallback succeeded")
				result.Blocks = resp.Content
				result.StopReason = resp.StopReason
				result.Usage = resp.Usage
			}
		} else {
			// Stream completed successfully - emit buffered blocks
			for _, block := range pendingBlocks {
				blocksChan <- block
			}
		}
	}()

	return blocksChan, result
}

// deduplicateToolResults removes duplicate tool_result blocks by ToolUseID.
// When duplicates are found, the last occurrence wins (last-writer-wins strategy).
func deduplicateToolResults(results []ToolResultBlock) []ToolResultBlock {
	seen := make(map[string]int) // map ToolUseID -> index in result
	var unique []ToolResultBlock

	for _, tr := range results {
		if idx, exists := seen[tr.ToolUseID]; exists {
			// Replace the existing entry with the newer one
			unique[idx] = tr
		} else {
			seen[tr.ToolUseID] = len(unique)
			unique = append(unique, tr)
		}
	}

	return unique
}

// Message represents a message in the conversation.
// Internal fields (IsVirtual, ID, Timestamp, Type) are used for transcript
// management but are stripped during API serialization.
type Message struct {
	Role        string            `json:"role"`
	Content     string            `json:"content,omitempty"`
	ToolUse     []ToolUseBlock    `json:"tool_use,omitempty"`
	ToolResults []ToolResultBlock `json:"tool_results,omitempty"`

	// Internal fields - not serialized to API
	IsVirtual bool   `json:"-"`
	ID        string `json:"-"`
	Type      string `json:"-"`
	Timestamp int64  `json:"-"`
}

// IsAPISafe returns true if this message should be sent to the API.
// Virtual messages and progress messages are not API-safe.
func (m *Message) IsAPISafe() bool {
	if m.IsVirtual {
		return false
	}
	if m.Type == "progress" {
		return false
	}
	return true
}

// ToolUseBlock represents a tool use block in a message.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolResultBlock represents a tool result block in a message.
type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool `json:"-"` // Error flag - not serialized to API
}

// ToolUse represents a tool call from the model.
type ToolUse struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResult represents a tool result to send back to the model.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// StopReason represents why the model stopped generating.
type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonToolUse   StopReason = "tool_use"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonStopSeq   StopReason = "stop_sequence"
)

// MaxTokensCategory represents the category of a max_tokens stop reason.
type MaxTokensCategory string

const (
	// CategoryOutputCapHit means the model hit its per-response output token limit.
	// This occurs when output_tokens >= modelMaxOutputTokens.
	CategoryOutputCapHit MaxTokensCategory = "output_cap_hit"
	// CategoryContextExhausted means the request was rejected due to context length.
	// This occurs when the provider returns a prompt_too_long class error.
	CategoryContextExhausted MaxTokensCategory = "context_exhausted"
)

// MaxTokensError is returned when the streaming API returns stop_reason: "max_tokens".
// It distinguishes between output cap hits and context exhaustion for structured error reporting.
type MaxTokensError struct {
	Category        MaxTokensCategory
	Model           string
	OutputTokens    int
	MaxOutputTokens int
	InputTokens     int
	Threshold       int // autoCompactThreshold for context_exhausted
}

func (e *MaxTokensError) Error() string {
	return fmt.Sprintf("max tokens reached: %s", e.Category)
}

// IsMaxTokensError checks if err is a MaxTokensError and returns it along with true,
// or returns nil, false if it's a different error type.
func IsMaxTokensError(err error) (*MaxTokensError, bool) {
	if err == nil {
		return nil, false
	}
	var mte *MaxTokensError
	if errors.As(err, &mte) {
		return mte, true
	}
	return nil, false
}

// Response represents the API response.
type Response struct {
	Content    []ContentBlock
	StopReason StopReason
	Model      string
	Usage      Usage
	Error      string
}

// ContentBlock represents a block of content in the response.
type ContentBlock struct {
	Type      string
	Text      string
	Thinking  string
	Signature string
	ToolUse   *ToolUse
	ToolID    string
	ToolName  string
	ToolInput map[string]any

	// WebSearchResult holds web_search_tool_result data when Type is "web_search_tool_result".
	WebSearchResult *WebSearchResultData
}

// WebSearchResultData holds web search result information including error codes.
type WebSearchResultData struct {
	ToolUseID string
	IsError   bool
	ErrorCode string // e.g., "invalid_tool_input", "max_uses_exceeded"
}

// Usage represents token usage information.
type Usage struct {
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

// ToolParam represents a tool parameter for the API.
type ToolParam struct {
	Name        string
	Description string
	InputSchema ToolInputSchema
	MaxUses     *int64
}

// ToolInputSchema represents the input schema for a tool.
type ToolInputSchema struct {
	Type        string
	Properties  map[string]any
	Required    []string
	ExtraFields map[string]any // carries $defs and other non-standard schema keys
}
