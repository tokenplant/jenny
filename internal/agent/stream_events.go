// Package agent provides the core agent loop and query engine.
package agent

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// StreamEvent represents the inner event object in a stream_event envelope.
// Field order matters for JSON serialization: type must be first.
type StreamEvent struct {
	Type string `json:"type"`
}

// MessageStartEvent represents a message_start stream event with minimal fields.
type MessageStartEvent struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// ContentBlockStartEvent represents a content_block_start stream event with minimal fields.
type ContentBlockStartEvent struct {
	Type         string          `json:"type"`
	Index        int             `json:"index"`
	ContentBlock json.RawMessage `json:"content_block"`
}

// ContentBlockDeltaEvent represents a content_block_delta stream event with minimal fields.
type ContentBlockDeltaEvent struct {
	Type  string          `json:"type"`
	Index int             `json:"index"`
	Delta json.RawMessage `json:"delta"`
	Usage *Usage          `json:"usage,omitempty"`
}

// ContentBlockStopEvent represents a content_block_stop stream event with minimal fields.
type ContentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// MessageDeltaEvent represents a message_delta stream event with minimal fields.
type MessageDeltaEvent struct {
	Type  string          `json:"type"`
	Delta json.RawMessage `json:"delta"`
	Usage *Usage          `json:"usage,omitempty"`
}

// MessageStopEvent represents a message_stop stream event with minimal fields.
type MessageStopEvent struct {
	Type string `json:"type"`
}

// MinimalContentBlock represents a minimal content block for serialization.
// Only relevant fields based on block type are included.
type MinimalContentBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
}

// MinimalDelta represents a minimal delta for message_delta events.
type MinimalDelta struct {
	Type        string `json:"type"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Signature   string `json:"signature,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
	StopSeq     string `json:"stop_sequence,omitempty"`
}

// MinimalMessage represents a minimal message for message_start events.
type MinimalMessage struct {
	ID         string          `json:"id,omitempty"`
	Type       string          `json:"type,omitempty"`
	Role       string          `json:"role,omitempty"`
	Model      string          `json:"model,omitempty"`
	Content    any             `json:"content,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"`
	StopSeq    string          `json:"stop_sequence,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
}

// MinimalUsage represents a minimal usage object for stream events.
type MinimalUsage struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// TransformStreamEvent transforms an SDK stream event to a minimal JSON representation.
// This ensures only relevant fields are serialized without zero-value padding.
func TransformStreamEvent(event any) (json.RawMessage, error) {
	switch e := event.(type) {
	case anthropic.MessageStartEvent:
		return transformMessageStart(e)
	case anthropic.ContentBlockStartEvent:
		return transformContentBlockStart(e)
	case anthropic.ContentBlockDeltaEvent:
		return transformContentBlockDelta(e)
	case anthropic.ContentBlockStopEvent:
		return transformContentBlockStop(e)
	case anthropic.MessageDeltaEvent:
		return transformMessageDelta(e)
	case anthropic.MessageStopEvent:
		return transformMessageStop(e)
	default:
		// Fallback: marshal as-is but this may include zero-value fields
		return json.Marshal(e)
	}
}

func transformMessageStart(e anthropic.MessageStartEvent) (json.RawMessage, error) {
	// Build minimal message - only include non-empty fields
	msg := make(map[string]any)

	// Always include type as first field
	msg["type"] = "message_start"

	// Add message fields
	message := make(map[string]any)
	if e.Message.ID != "" {
		message["id"] = e.Message.ID
	}
	message["type"] = "message"
	message["role"] = "assistant"
	if e.Message.Model != "" {
		message["model"] = string(e.Message.Model)
	}
	message["content"] = []any{}

	// Add usage if present
	usage := make(map[string]any)
	if e.Message.Usage.InputTokens > 0 {
		usage["input_tokens"] = int(e.Message.Usage.InputTokens)
	}
	if e.Message.Usage.CacheReadInputTokens > 0 {
		usage["cache_read_input_tokens"] = int(e.Message.Usage.CacheReadInputTokens)
	}
	if e.Message.Usage.CacheCreationInputTokens > 0 {
		usage["cache_creation_input_tokens"] = int(e.Message.Usage.CacheCreationInputTokens)
	}
	if e.Message.Usage.OutputTokens > 0 {
		usage["output_tokens"] = int(e.Message.Usage.OutputTokens)
	}
	if len(usage) > 0 {
		usage["service_tier"] = "standard"
		message["usage"] = usage
	}

	msg["message"] = message
	return json.Marshal(msg)
}

func transformContentBlockStart(e anthropic.ContentBlockStartEvent) (json.RawMessage, error) {
	// Build minimal event - type first
	event := make(map[string]any)
	event["type"] = "content_block_start"
	event["index"] = int(e.Index)

	// Build minimal content_block based on type
	cb := make(map[string]any)
	cb["type"] = string(e.ContentBlock.Type)

	switch e.ContentBlock.Type {
	case "thinking":
		cb["thinking"] = ""
		cb["signature"] = ""
	case "text":
		cb["text"] = ""
	case "tool_use":
		if e.ContentBlock.ID != "" {
			cb["id"] = e.ContentBlock.ID
		}
		if e.ContentBlock.Name != "" {
			cb["name"] = e.ContentBlock.Name
		}
		cb["input"] = map[string]any{}
	case "redacted_thinking":
		cb["data"] = ""
	}

	event["content_block"] = cb
	return json.Marshal(event)
}

func transformContentBlockDelta(e anthropic.ContentBlockDeltaEvent) (json.RawMessage, error) {
	// Build minimal event - type first
	event := make(map[string]any)
	event["type"] = "content_block_delta"
	event["index"] = int(e.Index)

	// Build minimal delta
	delta := make(map[string]any)
	delta["type"] = string(e.Delta.Type)

	if e.Delta.Thinking != "" {
		delta["thinking"] = e.Delta.Thinking
	}
	if e.Delta.PartialJSON != "" {
		delta["partial_json"] = e.Delta.PartialJSON
	}
	if e.Delta.Signature != "" {
		delta["signature"] = e.Delta.Signature
	}
	if e.Delta.Text != "" {
		delta["text"] = e.Delta.Text
	}

	event["delta"] = delta
	return json.Marshal(event)
}

func transformContentBlockStop(e anthropic.ContentBlockStopEvent) (json.RawMessage, error) {
	event := make(map[string]any)
	event["type"] = "content_block_stop"
	event["index"] = int(e.Index)
	return json.Marshal(event)
}

func transformMessageDelta(e anthropic.MessageDeltaEvent) (json.RawMessage, error) {
	// Build minimal event - type first
	event := make(map[string]any)
	event["type"] = "message_delta"

	// Build minimal delta - only stop_reason and stop_sequence
	delta := make(map[string]any)
	delta["type"] = "message_delta"

	if e.Delta.StopReason != "" {
		delta["stop_reason"] = string(e.Delta.StopReason)
	}
	if e.Delta.StopSequence != "" {
		delta["stop_sequence"] = e.Delta.StopSequence
	}

	event["delta"] = delta

	// Add usage if present
	usage := make(map[string]any)
	if e.Usage.InputTokens > 0 {
		usage["input_tokens"] = int(e.Usage.InputTokens)
	}
	if e.Usage.OutputTokens > 0 {
		usage["output_tokens"] = int(e.Usage.OutputTokens)
	}
	if e.Usage.CacheReadInputTokens > 0 {
		usage["cache_read_input_tokens"] = int(e.Usage.CacheReadInputTokens)
	}
	if e.Usage.CacheCreationInputTokens > 0 {
		usage["cache_creation_input_tokens"] = int(e.Usage.CacheCreationInputTokens)
	}
	if len(usage) > 0 {
		event["usage"] = usage
	}

	return json.Marshal(event)
}

func transformMessageStop(e anthropic.MessageStopEvent) (json.RawMessage, error) {
	event := make(map[string]any)
	event["type"] = "message_stop"
	return json.Marshal(event)
}

// BuildUserEvent builds a user event with timestamp and tool_use_result.
type UserEvent struct {
	Type            string          `json:"type"`
	Message         json.RawMessage `json:"message"`
	SessionID       string          `json:"session_id,omitempty"`
	ParentToolUseID *string         `json:"parent_tool_use_id,omitempty"`
	Uuid            string          `json:"uuid,omitempty"`
	Timestamp       string          `json:"timestamp,omitempty"`
	ToolUseResult   any             `json:"tool_use_result,omitempty"`
}

// BuildAssistantEvent builds an assistant event with full message shape.
type AssistantEvent struct {
	Type            string          `json:"type"`
	Message         json.RawMessage `json:"message"`
	ParentToolUseID *string         `json:"parent_tool_use_id,omitempty"`
	SessionID       string          `json:"session_id,omitempty"`
	Uuid            string          `json:"uuid,omitempty"`
}

// TimestampNow returns current timestamp in RFC3339Nano format.
func TimestampNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// FormatToolUseResult formats a tool result for tool_use_result field.
func FormatToolUseResult(content string, isError bool) string {
	if isError {
		return fmt.Sprintf("Error: %s", content)
	}
	return content
}
