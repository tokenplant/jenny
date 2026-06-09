// Package api provides the Anthropic API client.
package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ipy/jenny/internal/log"

	"github.com/anthropics/anthropic-sdk-go"
)
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
	Blocks          []ContentBlock
	StopReason      StopReason
	Usage           Usage
	Error           string
	Model           string
	MaxTokensErr    *MaxTokensError // Set when stop_reason is "max_tokens"
	ContextRejected bool            // True when HTTP 400 / prompt_too_long was received
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

		// Universal normalization gateway: normalize messages and tools before serialization
		messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: true})

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

			// Add tool_result blocks if present (already deduplicated in NormalizeMessages)
			for _, tr := range msg.ToolResults {
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
		for i, t := range tools {
			sdkTools = append(sdkTools, toolToSDK(t, i == len(tools)-1))
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
			// Detect HTTP 400 / prompt_too_long for context_exhausted categorization
			if isPromptTooLongError(preStreamErr) {
				result.ContextRejected = true
				// Pre-set MaxTokensErr for context_exhausted before fallback
				// (in case fallback also fails with same error)
				result.MaxTokensErr = categorizeMaxTokensError(c.model, 0, true)
			}
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
			// Detect HTTP 400 / prompt_too_long for context_exhausted categorization
			if isPromptTooLongError(stream.Err()) {
				result.ContextRejected = true
			}
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

		// Detect and categorize max_tokens scenarios:
		// 1. Normal streaming completion with stop_reason: max_tokens
		// 2. Pre-stream HTTP 400 rejection (ContextRejected flag is set)
		if result.StopReason == StopReasonMaxTokens && result.Error == "" {
			result.MaxTokensErr = categorizeMaxTokensError(result.Model, result.Usage.OutputTokens, result.ContextRejected)
		} else if result.ContextRejected && result.Error != "" {
			// Pre-stream HTTP 400 rejection - set context_exhausted error
			// even though no streaming response was received
			result.MaxTokensErr = categorizeMaxTokensError(result.Model, 0, true)
		}
	}()

	return blocksChan, result
}
