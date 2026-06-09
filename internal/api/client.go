// Package api provides the Anthropic API client.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/log"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
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
	model             string
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

// SendMessage sends a message to the API and returns the response.
func (c *Client) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	// Wrap the actual send logic with retry
	return c.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return c.doSendMessage(ctx, messages, tools, toolResults, systemPrompt)
	}, c.isBackground)
}

// ValidateMessagesMedia validates media in messages before sending to the API.
// It checks for data URIs and raw base64 image headers, enforcing:
// - Maximum100 media items per request
// - Maximum 5 MB per base64-encoded image
// Returns a CannotRetryError if validation fails.
func ValidateMessagesMedia(messages []Message) error {
	totalMedia := 0
	for _, msg := range messages {
		for _, tr := range msg.ToolResults {
			count, maxSize, err := countMediaInContent(tr.Content)
			if err != nil {
				return &CannotRetryError{
					Message:    err.Error(),
					StatusCode: 400,
				}
			}
			totalMedia += count
			if maxSize > MaxBase64ImageSize {
				return &CannotRetryError{
					Message:    "image exceeds maximum allowed size of 5 MB",
					StatusCode: 400,
				}
			}
		}
	}
	if totalMedia > MaxMediaItemsPerRequest {
		return &CannotRetryError{
			Message:    "request contains too many media items (max 100)",
			StatusCode: 400,
		}
	}
	return nil
}

// countMediaInContent counts media items and finds the largest decoded size in content.
// Returns count, largest decoded size found, and any error.
func countMediaInContent(content string) (count int, largestSize int, err error) {
	if content == "" {
		return 0, 0, nil
	}

	const dataURIPrefix = "data:image/"
	const base64Marker = ";base64,"

	// Find all data URIs and count them, extracting size when possible
	// A data URI is identified by "data:image/<fmt>;base64,<payload>"
	for {
		idx := strings.Index(content, dataURIPrefix)
		if idx == -1 {
			break
		}

		count++

		rest := content[idx+len(dataURIPrefix):]
		// Find where the MIME type ends (semicolon before "base64,")
		semiIdx := strings.Index(rest, ";")
		if semiIdx == -1 {
			// Malformed; skip past this prefix
			content = content[idx+len(dataURIPrefix):]
			continue
		}

		base64Idx := strings.Index(rest[semiIdx:], base64Marker)
		if base64Idx == -1 {
			// Malformed; skip past this prefix
			content = content[idx+len(dataURIPrefix):]
			continue
		}

		// Start of base64 payload in rest
		payloadStartInRest := semiIdx + base64Idx + len(base64Marker)
		payload := rest[payloadStartInRest:]

		// Find end of base64 - look for either:
		// 1. A non-base64 character, OR
		// 2. The start of another "data:image/" (which would be inside the base64 as text)
		base64EndInPayload := 0
		for i := 0; i < len(payload); i++ {
			c := rune(payload[i])
			// Skip whitespace; allow newlines in MIME-formatted base64
			if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
				continue
			}
			if !isBase64Char(c) {
				base64EndInPayload = i
				break
			}
			// Check if this could be the start of "data:image/" inside the base64
			if i+11 <= len(payload) && strings.HasPrefix(payload[i:i+11], "data:image/") {
				base64EndInPayload = i
				break
			}
		}
		if base64EndInPayload == 0 {
			base64EndInPayload = len(payload)
		}

		// Calculate absolute positions in original string
		payloadStart := idx + len(dataURIPrefix) + payloadStartInRest
		payloadEnd := payloadStart + base64EndInPayload

		if base64EndInPayload > 0 {
			cleaned := cleanBase64Fragment(payload[:base64EndInPayload])
			decoded := make([]byte, base64.StdEncoding.DecodedLen(len(cleaned)))
			_, decodeErr := base64.StdEncoding.Decode(decoded, []byte(cleaned))
			if decodeErr == nil && len(decoded) > largestSize {
				largestSize = len(decoded)
			}
		}

		// Move past this data URI for next search
		content = content[payloadEnd:]
	}

	// Scan for raw image headers in remaining content (not inside data URIs)
	rawHeaders := []string{"/9j/", "iVBOR", "R0lGOD", "UklGR"}
	for _, header := range rawHeaders {
		idx := 0
		for {
			pos := strings.Index(content[idx:], header)
			if pos == -1 {
				break
			}
			absPos := idx + pos

			after := content[absPos+len(header):]

			// Extract base64 after header - also stop at "data:image/" inside base64
			base64End := 0
			for i := 0; i < len(after); i++ {
				c := rune(after[i])
				// Skip whitespace; allow newlines in MIME-formatted base64
				if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
					continue
				}
				if !isBase64Char(c) {
					base64End = i
					break
				}
				// Check for embedded data URI start
				if i+11 <= len(after) && strings.HasPrefix(after[i:i+11], "data:image/") {
					base64End = i
					break
				}
			}
			if base64End == 0 {
				base64End = len(after)
			}

			if base64End >= 20 {
				cleaned := cleanBase64Fragment(after[:base64End])
				decoded := make([]byte, base64.StdEncoding.DecodedLen(len(cleaned)))
				_, decodeErr := base64.StdEncoding.Decode(decoded, []byte(cleaned))
				if decodeErr == nil {
					count++
					if len(decoded) > largestSize {
						largestSize = len(decoded)
					}
				} else if len(cleaned) > 20 {
					// Decode failed but we have a substantial base64 fragment.
					// Estimate size: each base64 char encodes 6 bits; 4 chars encode 3 bytes.
					estimatedSize := (len(cleaned) * 3) / 4
					if estimatedSize > largestSize {
						largestSize = estimatedSize
					}
				}
			}

			idx = absPos + len(header)
		}
	}

	return count, largestSize, nil
}

// cleanBase64Fragment builds a whitespace-stripped base64 string for decoding.
// It iterates through s and collects only base64 chars (not \n, \r, \t, space).
func cleanBase64Fragment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
			continue
		}
		if isBase64Char(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func isBase64Char(c rune) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '='
}

// doSendMessage performs the actual message sending (used by retry logic).
func (c *Client) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	log.Debug("Sending message", "model", c.model)
	log.Debug("System prompt", "prompt", systemPrompt)
	log.Debug("Number of tools", "count", len(tools))
	for _, t := range tools {
		log.Debug("Tool registered", "name", t.Name, "description", t.Description)
	}

	// Validate media before sending
	if err := ValidateMessagesMedia(messages); err != nil {
		return nil, err
	}

	// Convert messages to SDK format
	sdkMessages := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		contentBlocks := make([]anthropic.ContentBlockParamUnion, 0)

		// Add text content if present
		if msg.Content != "" {
			contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{Text: msg.Content},
			})
		}

		// Add tool_use blocks if present
		for _, tu := range msg.ToolUse {
			contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
				OfToolUse: &anthropic.ToolUseBlockParam{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: tu.Input,
				},
			})
		}

		// Add tool_result blocks if present (deduplicated as safety net for DeepSeek)
		dedupedResults := deduplicateToolResults(msg.ToolResults)
		for _, tr := range dedupedResults {
			contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
				OfToolResult: &anthropic.ToolResultBlockParam{
					ToolUseID: tr.ToolUseID,
					Content: []anthropic.ToolResultBlockParamContentUnion{
						{OfText: &anthropic.TextBlockParam{Text: tr.Content}},
					},
					Type: "tool_result",
				},
			})
		}

		sdkMessages = append(sdkMessages, anthropic.MessageParam{
			Role:    anthropic.MessageParamRole(msg.Role),
			Content: contentBlocks,
		})
	}

	// Add standalone tool results as user messages if there are any (backward compat)
	for _, tr := range toolResults {
		sdkMessages = append(sdkMessages, anthropic.MessageParam{
			Role: "user",
			Content: []anthropic.ContentBlockParamUnion{
				{
					OfToolResult: &anthropic.ToolResultBlockParam{
						ToolUseID: tr.ToolUseID,
						Content: []anthropic.ToolResultBlockParamContentUnion{
							{OfText: &anthropic.TextBlockParam{Text: tr.Content}},
						},
						Type: "tool_result",
					},
				},
			},
		})
	}

	// Convert tools to SDK format
	sdkTools := make([]anthropic.ToolUnionParam, 0, len(tools))
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	for i, t := range tools {
		sdkTools = append(sdkTools, toolToSDK(t, i == len(tools)-1, baseURL))
	}

	// Build request
	maxTokens := 64000
	if c.maxTokensOverride > 0 {
		maxTokens = c.maxTokensOverride
	}
	body := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(maxTokens),
	}
	body.Messages = sdkMessages
	if systemPrompt != "" {
		body.System = []anthropic.TextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	}
	if len(sdkTools) > 0 {
		body.Tools = sdkTools
	}

	// Send request
	resp, err := c.client.Messages.New(ctx, body)
	if err != nil {
		// Wrap SDK errors to extract status code for retry logic
		wrappedErr := wrapSDKError(err)
		return nil, wrappedErr
	}

	// Convert response
	response := &Response{
		Model:      string(resp.Model),
		StopReason: StopReason(string(resp.StopReason)),
	}

	// Convert usage (AC1: extract all four token types)
	if resp.Usage.InputTokens > 0 {
		response.Usage.InputTokens = int(resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens > 0 {
		response.Usage.OutputTokens = int(resp.Usage.OutputTokens)
	}
	if resp.Usage.CacheReadInputTokens > 0 {
		response.Usage.CacheReadInputTokens = int(resp.Usage.CacheReadInputTokens)
	}
	if resp.Usage.CacheCreationInputTokens > 0 {
		response.Usage.CacheCreationInputTokens = int(resp.Usage.CacheCreationInputTokens)
	}

	// Convert content blocks using type switch
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			response.Content = append(response.Content, ContentBlock{
				Type: "text",
				Text: block.Text,
			})
		case "thinking":
			// Pass thinking + signature through so the non-streaming
			// fallback path emits an assistant envelope with its own
			// `type: "thinking"` block (AC6).
			response.Content = append(response.Content, ContentBlock{
				Type:      "thinking",
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "tool_use":
			// Parse the input JSON
			var input map[string]any
			if err := json.Unmarshal(block.Input, &input); err != nil {
				input = make(map[string]any)
			}
			response.Content = append(response.Content, ContentBlock{
				Type:      "tool_use",
				ToolID:    block.ID,
				ToolName:  block.Name,
				ToolInput: input,
			})
		case "web_search_tool_result":
			// Process web search results - extract error codes if present
			webSearchData := &WebSearchResultData{
				ToolUseID: block.ToolUseID,
			}
			// Check if this is an error response - WebSearchToolResultBlockContentUnion
			// has AsResponseWebSearchToolResultError method that returns the error if present
			errResult := block.Content.AsResponseWebSearchToolResultError()
			if errResult.ErrorCode != "" {
				webSearchData.IsError = true
				webSearchData.ErrorCode = string(errResult.ErrorCode)
			}
			response.Content = append(response.Content, ContentBlock{
				Type:            "web_search_tool_result",
				WebSearchResult: webSearchData,
			})
		}
	}

	return response, nil
}

// wrapSDKError wraps SDK errors to extract HTTP status code for retry logic.
func wrapSDKError(err error) error {
	if err == nil {
		return nil
	}

	// Use errors.As with SDK's typed error to properly extract status code
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr); apiErr != nil {
		// Extract Retry-After from response headers if present
		var retryAfter *time.Duration
		if apiErr.Response != nil {
			if retryAfterStr := apiErr.Response.Header.Get("Retry-After"); retryAfterStr != "" {
				if seconds, parseErr := strconv.ParseFloat(retryAfterStr, 64); parseErr == nil {
					ms := int64(seconds * 1000)
					d := time.Duration(ms) * time.Millisecond
					retryAfter = &d
				}
			}
		}

		isPermanent := apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 &&
			apiErr.StatusCode != 429 && apiErr.StatusCode != 408 && apiErr.StatusCode != 409
		return &RetryableHTTPError{
			StatusCode:  apiErr.StatusCode,
			Message:     err.Error(),
			IsPermanent: isPermanent,
			RetryAfter:  retryAfter,
		}
	}

	// For unknown errors, return as-is (will be handled as connection errors if retryable)
	return err
}

// streamAccumulator tracks content blocks during streaming.
type streamAccumulator struct {
	blocks        []ContentBlock // Content blocks by index
	texts         []string       // Accumulated text per block index
	thinking      []string       // Accumulated thinking per block index
	signatures    []string       // Accumulated signature per block index
	toolInputJSON map[int]string // Accumulated partial JSON for tool_use blocks
	model         string
	usage         Usage
	stopReason    StopReason
}

// newStreamAccumulator creates a new stream accumulator.
func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{
		blocks:        make([]ContentBlock, 0),
		texts:         make([]string, 0),
		thinking:      make([]string, 0),
		signatures:    make([]string, 0),
		toolInputJSON: make(map[int]string),
	}
}

// ensureBlock ensures a block exists at the given index.
func (acc *streamAccumulator) ensureBlock(index int) {
	for len(acc.blocks) <= index {
		acc.blocks = append(acc.blocks, ContentBlock{})
		acc.texts = append(acc.texts, "")
		acc.thinking = append(acc.thinking, "")
		acc.signatures = append(acc.signatures, "")
	}
}

// appendText appends text to the block at the given index.
func (acc *streamAccumulator) appendText(index int, text string) {
	acc.ensureBlock(index)
	acc.texts[index] += text
	acc.blocks[index].Text = acc.texts[index]
}

// appendThinking appends thinking text to the block at the given index.
func (acc *streamAccumulator) appendThinking(index int, thinking string) {
	acc.ensureBlock(index)
	acc.thinking[index] += thinking
	acc.blocks[index].Thinking = acc.thinking[index]
}

// appendSignature appends signature text to the block at the given index.
func (acc *streamAccumulator) appendSignature(index int, signature string) {
	acc.ensureBlock(index)
	acc.signatures[index] += signature
	acc.blocks[index].Signature = acc.signatures[index]
}

// setBlockType sets the type of a block at the given index.
func (acc *streamAccumulator) setBlockType(index int, blockType string) {
	acc.ensureBlock(index)
	acc.blocks[index].Type = blockType
}

// setModel sets the model from a message_start event.
func (acc *streamAccumulator) setModel(model string) {
	acc.model = model
}

// getModel returns the captured model.
func (acc *streamAccumulator) getModel() string {
	return acc.model
}

// mergeUsage merges non-zero fields from usage into the accumulator,
// allowing message_start (input tokens) and message_delta (output tokens)
// to contribute independently without overwriting each other.
func (acc *streamAccumulator) mergeUsage(usage Usage) {
	if usage.InputTokens > 0 {
		acc.usage.InputTokens = usage.InputTokens
	}
	if usage.OutputTokens > 0 {
		acc.usage.OutputTokens = usage.OutputTokens
	}
	if usage.CacheReadInputTokens > 0 {
		acc.usage.CacheReadInputTokens = usage.CacheReadInputTokens
	}
	if usage.CacheCreationInputTokens > 0 {
		acc.usage.CacheCreationInputTokens = usage.CacheCreationInputTokens
	}
}

// setUsage sets the usage from a message_delta event.
func (acc *streamAccumulator) setUsage(usage Usage) {
	acc.mergeUsage(usage)
}

// setStopReason sets the stop reason from a message_delta event.
func (acc *streamAccumulator) setStopReason(reason StopReason) {
	acc.stopReason = reason
}

// appendToolInputJSON appends partial JSON to the tool input accumulator.
func (acc *streamAccumulator) appendToolInputJSON(index int, partialJSON string) {
	acc.toolInputJSON[index] += partialJSON
}

// finalizeToolInput parses accumulated JSON into ToolInput for the block.
func (acc *streamAccumulator) finalizeToolInput(index int) {
	if jsonStr, ok := acc.toolInputJSON[index]; ok && jsonStr != "" {
		var input map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &input); err != nil {
			input = make(map[string]any)
		}
		acc.ensureBlock(index)
		acc.blocks[index].ToolInput = input
	}
}

// getBlocks returns the accumulated content blocks.
func (acc *streamAccumulator) getBlocks() []ContentBlock {
	return acc.blocks
}

// StreamContentBlock represents a completed content block from streaming.
// It can also carry raw SSE events for passthrough when IncludePartial is enabled.
type StreamContentBlock struct {
	Index    int
	Block    ContentBlock
	Type     string // "stream_event" for passthrough events
	RawEvent any    // the raw SDK event for passthrough
}

// StreamResult represents the result of a streaming session.
type StreamResult struct {
	Blocks     []ContentBlock
	StopReason StopReason
	Usage      Usage
	Error      string
	Model      string
}

// SendMessageStream sends a streaming message to the API.
// It yields completed content blocks via the blocks channel.
// If streaming fails, it triggers the fallback within the given fallbackTimeout.
// The idleTimeout controls the watchdog timer for each event.
func (c *Client) SendMessageStream(
	ctx context.Context,
	messages []Message,
	tools []ToolParam,
	toolResults []ToolResult,
	systemPrompt string,
	idleTimeout time.Duration,
	fallbackTimeout time.Duration,
	onStreamingFallback func(context.Context) (*Response, error),
) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	// Run streaming in a goroutine
	go func() {
		defer close(blocksChan)

		// Validate media before sending
		if err := ValidateMessagesMedia(messages); err != nil {
			result.Error = err.Error()
			return
		}

		// Convert messages to SDK format (same as SendMessage)
		sdkMessages := make([]anthropic.MessageParam, 0, len(messages))
		for _, msg := range messages {
			contentBlocks := make([]anthropic.ContentBlockParamUnion, 0)

			if msg.Content != "" {
				contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
					OfText: &anthropic.TextBlockParam{Text: msg.Content},
				})
			}

			for _, tu := range msg.ToolUse {
				contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    tu.ID,
						Name:  tu.Name,
						Input: tu.Input,
					},
				})
			}

			// Add tool_result blocks if present (deduplicated as safety net for DeepSeek)
			dedupedResults := deduplicateToolResults(msg.ToolResults)
			for _, tr := range dedupedResults {
				contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
					OfToolResult: &anthropic.ToolResultBlockParam{
						ToolUseID: tr.ToolUseID,
						Content: []anthropic.ToolResultBlockParamContentUnion{
							{OfText: &anthropic.TextBlockParam{Text: tr.Content}},
						},
						Type: "tool_result",
					},
				})
			}

			sdkMessages = append(sdkMessages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRole(msg.Role),
				Content: contentBlocks,
			})
		}

		// Add standalone tool results as user messages
		for _, tr := range toolResults {
			sdkMessages = append(sdkMessages, anthropic.MessageParam{
				Role: "user",
				Content: []anthropic.ContentBlockParamUnion{
					{
						OfToolResult: &anthropic.ToolResultBlockParam{
							ToolUseID: tr.ToolUseID,
							Content: []anthropic.ToolResultBlockParamContentUnion{
								{OfText: &anthropic.TextBlockParam{Text: tr.Content}},
							},
							Type: "tool_result",
						},
					},
				},
			})
		}

		// Convert tools to SDK format
		sdkTools := make([]anthropic.ToolUnionParam, 0, len(tools))
		baseURL := os.Getenv("ANTHROPIC_BASE_URL")
		for i, t := range tools {
			sdkTools = append(sdkTools, toolToSDK(t, i == len(tools)-1, baseURL))
		}

		// Build request
		maxTokens := 64000
		if c.maxTokensOverride > 0 {
			maxTokens = c.maxTokensOverride
		}
		body := anthropic.MessageNewParams{
			Model:     anthropic.Model(c.model),
			MaxTokens: int64(maxTokens),
		}
		body.Messages = sdkMessages
		if systemPrompt != "" {
			body.System = []anthropic.TextBlockParam{{
				Text:         systemPrompt,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			}}
		}
		if len(sdkTools) > 0 {
			body.Tools = sdkTools
		}

		log.Debug("Starting streaming request", "model", c.model)

		// Create stream
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		stream := c.client.Messages.NewStreaming(ctx, body)

		// Check for pre-stream error (e.g., 429/529 from HTTP request)
		// stream.Err() is set when NewStreaming receives an error from the HTTP request
		if stream.Err() != nil {
			preStreamErr := stream.Err()
			log.Warn("Stream pre-error detected, falling back", "error", preStreamErr)
			// Fall back to non-streaming, which has proper retry logic via sendWithRetry
			if onStreamingFallback != nil {
				fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), fallbackTimeout)
				defer fallbackCancel()
				resp, err := onStreamingFallback(fallbackCtx)
				if err != nil {
					result.Error = err.Error()
					return
				}
				result.Blocks = resp.Content
				result.StopReason = resp.StopReason
				result.Usage = resp.Usage
				return
			}
			result.Error = preStreamErr.Error()
			return
		}

		acc := newStreamAccumulator()
		hasMessageStart := false
		hasMessageStop := false
		// Buffer blocks to avoid leaking partial content when fallback is triggered
		var pendingBlocks []StreamContentBlock

		// Process stream events using iterator pattern
		// Use independent watchdog timer to detect idle timeout while stream.Next() blocks
		idleTimer := time.NewTimer(idleTimeout)

		// Use a labeled loop so we can break out when stream ends
	streamLoop:
		for {
			// Use select to wait on either stream events or idle timeout
			// This allows the watchdog to fire even while stream.Next() is blocked
			streamReady := stream.Next()

			// Check if idle timeout watchdog fired first
			select {
			case <-idleTimer.C:
				// Idle timeout fired - cancel stream and trigger fallback
				log.Warn("Idle timeout reached, triggering fallback")
				cancel()
				result.Error = "idle timeout"
				if onStreamingFallback != nil {
					fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), fallbackTimeout)
					defer fallbackCancel()
					resp, err := onStreamingFallback(fallbackCtx)
					if err != nil {
						result.Error = err.Error()
						return
					}
					result.Blocks = resp.Content
					result.StopReason = resp.StopReason
					result.Usage = resp.Usage
					return
				}
				return
			default:
				// Stream event arrived (or stream.Next() returned false)
				// If stream is not ready, exit the loop to post-loop error/fallback checking
				if !streamReady {
					break streamLoop
				}
				// Stream is ready - reset the watchdog timer since we received an event
				if !idleTimer.Stop() {
					<-idleTimer.C // Drain expired timer channel if necessary
				}
				idleTimer.Reset(idleTimeout)
			}

			event := stream.Current()

			// Process the event
			variant := event.AsAny()
			switch e := variant.(type) {
			case anthropic.MessageStartEvent:
				// Passthrough raw event for IncludePartial consumers
				blocksChan <- StreamContentBlock{Type: "stream_event", RawEvent: e}
				hasMessageStart = true
				acc.setModel(string(e.Message.Model))
				if e.Message.Usage.InputTokens > 0 {
					acc.setUsage(Usage{
						InputTokens:              int(e.Message.Usage.InputTokens),
						CacheReadInputTokens:     int(e.Message.Usage.CacheReadInputTokens),
						CacheCreationInputTokens: int(e.Message.Usage.CacheCreationInputTokens),
					})
				}
				log.Debug("Stream: message_start")

			case anthropic.ContentBlockStartEvent:
				// Passthrough raw event for IncludePartial consumers
				blocksChan <- StreamContentBlock{Type: "stream_event", RawEvent: e}
				index := int(e.Index)
				log.Debug("Stream: content_block_start", "index", index, "type", e.ContentBlock.Type)
				// Determine block type from content_block.Type
				switch e.ContentBlock.Type {
				case "text":
					acc.setBlockType(index, "text")
				case "tool_use":
					// For tool_use, ID and Name are directly on ContentBlock
					acc.setBlockType(index, "tool_use")
					acc.blocks[index].ToolID = e.ContentBlock.ID
					acc.blocks[index].ToolName = e.ContentBlock.Name
					// Input will be populated via deltas
				default:
					acc.setBlockType(index, e.ContentBlock.Type)
				}

			case anthropic.ContentBlockDeltaEvent:
				// Passthrough raw event for IncludePartial consumers
				blocksChan <- StreamContentBlock{Type: "stream_event", RawEvent: e}
				index := int(e.Index)
				delta := e.Delta
				// Only append text for text blocks; tool_use blocks should use PartialJSON
				if delta.Text != "" && acc.blocks[index].Type == "text" {
					acc.appendText(index, delta.Text)
				}
				if delta.Thinking != "" {
					acc.appendThinking(index, delta.Thinking)
				}
				if delta.Signature != "" {
					acc.appendSignature(index, delta.Signature)
				}
				// Always process partial JSON for tool input accumulation
				if delta.PartialJSON != "" {
					acc.appendToolInputJSON(index, delta.PartialJSON)
					acc.finalizeToolInput(index)
				}
				log.Debug("Stream: content_block_delta", "index", index, "text", delta.Text)

			case anthropic.ContentBlockStopEvent:
				// Passthrough raw event for IncludePartial consumers
				blocksChan <- StreamContentBlock{Type: "stream_event", RawEvent: e}
				index := int(e.Index)
				log.Debug("Stream: content_block_stop", "index", index)
				// Parse accumulated tool input JSON into ToolInput
				acc.finalizeToolInput(index)
				// Buffer block instead of sending immediately to avoid leaking partial content on fallback
				acc.ensureBlock(index)
				pendingBlocks = append(pendingBlocks, StreamContentBlock{Index: index, Block: acc.blocks[index]})

			case anthropic.MessageDeltaEvent:
				// Passthrough raw event for IncludePartial consumers
				blocksChan <- StreamContentBlock{Type: "stream_event", RawEvent: e}
				if e.Usage.InputTokens > 0 || e.Usage.OutputTokens > 0 {
					acc.setUsage(Usage{
						InputTokens:              int(e.Usage.InputTokens),
						OutputTokens:             int(e.Usage.OutputTokens),
						CacheReadInputTokens:     int(e.Usage.CacheReadInputTokens),
						CacheCreationInputTokens: int(e.Usage.CacheCreationInputTokens),
					})
				}
				if e.Delta.StopReason != "" {
					acc.setStopReason(StopReason(e.Delta.StopReason))
				}
				log.Debug("Stream: message_delta")

			case anthropic.MessageStopEvent:
				// Passthrough raw event for IncludePartial consumers
				blocksChan <- StreamContentBlock{Type: "stream_event", RawEvent: e}
				hasMessageStop = true
				log.Debug("Stream: message_stop")
			}
		}

		// Check for stream errors
		if stream.Err() != nil {
			log.Warn("Stream error", "error", stream.Err())
			result.Error = stream.Err().Error()
		}

		// Check if we need fallback
		shouldFallback := !hasMessageStart || !hasMessageStop || result.Error != ""
		if shouldFallback {
			log.Warn("Stream incomplete, triggering fallback", "hasMessageStart", hasMessageStart, "hasMessageStop", hasMessageStop, "error", result.Error)
			// Discard pending blocks - they will not be sent to channel
			if onStreamingFallback != nil {
				cancel()
				// Create a new context with fallback timeout since the original ctx is cancelled
				fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), fallbackTimeout)
				defer fallbackCancel()
				resp, err := onStreamingFallback(fallbackCtx)
				if err != nil {
					result.Error = err.Error()
					return
				}
				// Convert non-streaming response to stream result
				result.Blocks = resp.Content
				result.StopReason = resp.StopReason
				result.Usage = resp.Usage
				return
			}
		}

		// Stream completed successfully - send buffered blocks to channel
		for _, block := range pendingBlocks {
			blocksChan <- block
		}
		result.Blocks = acc.getBlocks()
		result.StopReason = acc.stopReason
		result.Usage = acc.usage
		result.Model = acc.getModel()
	}()

	return blocksChan, result
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
	ExtraFields map[string]any // NEW: carries $defs and other non-standard schema keys
}

// toolToSDK converts a ToolParam to an SDK ToolUnionParam.
// For web_search with MaxUses set, uses the specific WebSearchTool20250305Param
// to support definition-level max_uses enforcement (except for MiniMax).
// When isLast is true, cache_control is set on the tool to mark it as a cache breakpoint.
// baseURL is used for provider detection to apply MiniMax compatibility fix only when needed.
func toolToSDK(t ToolParam, isLast bool, baseURL string) anthropic.ToolUnionParam {
	provider := providerFromBaseURL(baseURL)

	// MiniMax compatibility: for MiniMax provider, web_search must use ToolParam
	// with input_schema, because WebSearchTool20250305Param has no input_schema
	// and MiniMax rejects tools with missing input_schema (error 2013).
	// AC3: Provider-aware: only use this path when provider is "minimax".
	if t.Name == "web_search" && provider == "minimax" {
		props := map[string]any{"query": map[string]any{"type": "string"}}
		inputSchema := anthropic.ToolInputSchemaParam{
			Type:       constant.Object("object"),
			Properties: props,
			Required:   []string{"query"},
		}
		tool := &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: inputSchema,
		}
		if isLast {
			tool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		return anthropic.ToolUnionParam{OfTool: tool}
	}

	// Standard path: WebSearchTool20250305Param when MaxUses is set (non-MiniMax only).
	if t.Name == "web_search" && t.MaxUses != nil {
		tool := &anthropic.WebSearchTool20250305Param{
			MaxUses: param.NewOpt(*t.MaxUses),
		}
		if isLast {
			tool.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		return anthropic.ToolUnionParam{OfWebSearchTool20250305: tool}
	}

	// MiniMax compatibility: add placeholder property only for MiniMax provider.
	// MiniMax rejects tools with empty properties object: "function name or parameters is empty (2013)".
	// AC2: Provider-aware: only add __arg__ when provider is "minimax".
	props := t.InputSchema.Properties
	if props == nil {
		props = make(map[string]any)
	}
	if provider == "minimax" && len(props) == 0 {
		props["__arg__"] = map[string]any{"type": "string", "description": "Placeholder argument for empty schema"}
	}
	required := t.InputSchema.Required
	if required == nil {
		required = []string{}
	}

	inputSchema := anthropic.ToolInputSchemaParam{
		Type:       constant.Object("object"),
		Properties: props,
		Required:   required,
	}

	// Pass through extra fields ($defs, etc.) if present (AC3)
	if len(t.InputSchema.ExtraFields) > 0 {
		inputSchema.ExtraFields = t.InputSchema.ExtraFields
	}

	tool := &anthropic.ToolParam{
		Name:        t.Name,
		Description: anthropic.String(t.Description),
		InputSchema: inputSchema,
	}
	if isLast {
		tool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return anthropic.ToolUnionParam{OfTool: tool}
}
