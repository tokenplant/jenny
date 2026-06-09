// Package api provides the Anthropic API client.
package api

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// MaxMediaItemsPerRequest is the maximum number of media items allowed per request.
const MaxMediaItemsPerRequest = 100

// MaxBase64ImageSize is the maximum size in bytes for a base64-encoded image.
const MaxBase64ImageSize = 5 * 1024 * 1024

// DefaultIdleTimeout is the default timeout for idle watchdog (30 seconds).
const DefaultIdleTimeout = 30 * time.Second

// DefaultFallbackTimeout is the default timeout for non-streaming fallback (~5 min).
const DefaultFallbackTimeout = 5 * time.Minute

// Client wraps the Anthropic SDK client.
type Client struct {
	client            anthropic.Client
	model string
	maxTokensOverride int // Override for max_tokens; 0 means use default
	retryConfig       RetryConfig
	isBackground      bool
}

// defaultModel is the default model used when ANTHROPIC_MODEL is not set.
const defaultModel = "claude-opus-4-5-20251101"

// NewClient creates a new API client.
func NewClient() (*Client, error) {
	return NewClientWithModel("")
}

// NewClientWithModel creates a new API client with an optional model override.
// If model is empty, reads from ANTHROPIC_MODEL environment variable.
func NewClientWithModel(model string) (*Client, error) {
	// Read ANTHROPIC_MODEL from environment (SDK handles BASE_URL and AUTH_TOKEN automatically)
	if model == "" {
		model = os.Getenv("ANTHROPIC_MODEL")
	}
	if model == "" {
		model = defaultModel
	}

	// SDK's NewClient already reads ANTHROPIC_BASE_URL and ANTHROPIC_AUTH_TOKEN.
	//
	// WithRequestTimeout(1h) bypasses the SDK's
	// CalculateNonStreamingTimeout 10-minute guard, which would otherwise
	// reject any non-streaming request whose expected wall-time
	// (maxTokens * 1h / 128000) exceeds 10 minutes. For our universal
	// 64000-token budget that is ~30 minutes, so without this override
	// the streaming fallback path would never complete. The streaming
	// path has no such guard; the request timeout only caps the
	// per-attempt wall time.
	client := anthropic.NewClient(
		option.WithHeader("anthropic-beta", string(anthropic.AnthropicBetaPromptCaching2024_07_31)),
		option.WithRequestTimeout(1*time.Hour),
	)

	return &Client{
		client:      client,
		model:       model,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// SetModel sets the model to use.
func (c *Client) SetModel(model string) {
	c.model = model
}

// GetModel returns the model being used.
func (c *Client) GetModel() string {
	return c.model
}

// SetMaxTokensOverride sets the max_tokens override for API requests.
func (c *Client) SetMaxTokensOverride(maxTokens int) {
	c.maxTokensOverride = maxTokens
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