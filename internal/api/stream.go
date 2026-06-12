// Package api provides the Anthropic API client.
package api

import (
	"encoding/json"
	"time"
)

// DefaultIdleTimeout is the default timeout for idle watchdog (30 seconds).
const DefaultIdleTimeout = 30 * time.Second

// DefaultFallbackTimeout is the default timeout for non-streaming fallback (~5 min).
const DefaultFallbackTimeout = 5 * time.Minute

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

// mergeUsage merges non-zero fields from usage into the accumulator.
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
	IsPermanent     bool // True for errors that should not be retried (e.g., 4xx except 429)
	Model           string
	MaxTokensErr    *MaxTokensError // Set when stop_reason is "max_tokens"
	ContextRejected bool            // True when HTTP 400 / prompt_too_long was received
	StreamComplete  bool            // True when message_stop event was received
}
