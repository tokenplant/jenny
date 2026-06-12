// Package agent provides the core agent loop and query engine.
package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/api"
)

// contentBlockStopEvent represents a content_block_stop stream event with minimal fields.
type contentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// messageStopEvent represents a message_stop stream event with minimal fields.
type messageStopEvent struct {
	Type string `json:"type"`
}

// MinimalContentBlock represents a minimal content block for serialization.
// Only relevant fields based on block type are included.
// Implements json.Marshaler for custom serialization without zero-value padding.
type MinimalContentBlock struct {
	Type      string
	Thinking  string
	Signature string
	Text      string
	ID        string
	Name      string
	Input     any
}

func (m MinimalContentBlock) MarshalJSON() ([]byte, error) {
	// Build fields in order: type first, then only non-empty fields
	fields := []any{`"type":` + encodeString(m.Type)}

	switch m.Type {
	case "thinking":
		if m.Thinking != "" {
			fields = append(fields, `"thinking":`+encodeString(m.Thinking))
		}
		if m.Signature != "" {
			fields = append(fields, `"signature":`+encodeString(m.Signature))
		}
	case "text":
		// Always include text field even when empty (per reference format for content_block_start)
		fields = append(fields, `"text":`+encodeString(m.Text))
	case "tool_use":
		fields = append(fields, `"id":`+encodeString(m.ID))
		fields = append(fields, `"name":`+encodeString(m.Name))
		if m.Input != nil {
			inputBytes, err := json.Marshal(m.Input)
			if err != nil {
				return nil, err
			}
			fields = append(fields, `"input":`+string(inputBytes))
		}
	case "redacted_thinking":
		if m.Text != "" {
			fields = append(fields, `"data":`+encodeString(m.Text))
		}
	}

	return []byte("{" + joinFields(fields) + "}"), nil
}

// MinimalDelta represents a minimal delta for message_delta events.
type MinimalDelta struct {
	Type        string
	Thinking    string
	PartialJSON string
	Signature   string
	StopReason  string
	StopSeq     *string // Use *string so it can marshal as null when empty
	Text        string
}

func (m MinimalDelta) MarshalJSON() ([]byte, error) {
	var fields []any

	switch m.Type {
	case "thinking_delta":
		fields = []any{`"type":"thinking_delta"`}
		if m.Thinking != "" {
			fields = append(fields, `"thinking":`+encodeString(m.Thinking))
		}
		if m.Signature != "" {
			fields = append(fields, `"signature":`+encodeString(m.Signature))
		}
	case "text_delta":
		fields = []any{`"type":"text_delta"`}
		if m.Text != "" {
			fields = append(fields, `"text":`+encodeString(m.Text))
		}
	case "input_json_delta":
		fields = []any{`"type":"input_json_delta"`}
		if m.PartialJSON != "" {
			fields = append(fields, `"partial_json":`+encodeString(m.PartialJSON))
		}
	case "signature_delta":
		fields = []any{`"type":"signature_delta"`}
		if m.Signature != "" {
			fields = append(fields, `"signature":`+encodeString(m.Signature))
		}
	case "message_delta":
		// Reference format: delta has stop_reason/stop_sequence directly, no nested type field
		// Always include stop_reason and stop_sequence (possibly null)
		fields = []any{}
		if m.StopReason != "" {
			fields = append(fields, `"stop_reason":`+encodeString(m.StopReason))
		} else {
			fields = append(fields, `"stop_reason":null`)
		}
		if m.StopSeq != nil {
			fields = append(fields, `"stop_sequence":`+encodeString(*m.StopSeq))
		} else {
			fields = append(fields, `"stop_sequence":null`)
		}
	default:
		fields = []any{`"type":` + encodeString(m.Type)}
	}

	return []byte("{" + joinFields(fields) + "}"), nil
}

// MinimalMessage represents a minimal message for message_start events.
// Uses *string for StopReason and StopSeq so they marshal as null when empty.
type MinimalMessage struct {
	ID         string
	Type       string
	Role       string
	Model      string
	Content    any
	Usage      *StreamUsage
	StopReason *string
	StopSeq    *string
}

func (m MinimalMessage) MarshalJSON() ([]byte, error) {
	fields := []any{`"id":` + encodeString(m.ID), `"type":` + encodeString(m.Type), `"role":` + encodeString(m.Role)}

	if m.Model != "" {
		fields = append(fields, `"model":`+encodeString(m.Model))
	}

	if m.Content != nil {
		contentBytes, err := json.Marshal(m.Content)
		if err != nil {
			return nil, err
		}
		fields = append(fields, `"content":`+string(contentBytes))
	}

	// Always include stop_reason and stop_sequence (possibly null per reference format)
	if m.StopReason != nil {
		fields = append(fields, `"stop_reason":`+encodeString(*m.StopReason))
	} else {
		fields = append(fields, `"stop_reason":null`)
	}
	if m.StopSeq != nil {
		fields = append(fields, `"stop_sequence":`+encodeString(*m.StopSeq))
	} else {
		fields = append(fields, `"stop_sequence":null`)
	}

	if m.Usage != nil {
		usageBytes, err := json.Marshal(m.Usage)
		if err != nil {
			return nil, err
		}
		fields = append(fields, `"usage":`+string(usageBytes))
	}

	return []byte("{" + joinFields(fields) + "}"), nil
}

// StreamUsage represents a minimal usage object for stream events.
// Field order matches the reference format: input_tokens, cache_creation_input_tokens,
// cache_read_input_tokens, output_tokens, service_tier.
type StreamUsage struct {
	InputTokens              int    `json:"input_tokens"`
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int    `json:"cache_read_input_tokens"`
	OutputTokens             int    `json:"output_tokens"`
	ServiceTier              string `json:"service_tier"`
}

func encodeString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func joinFields(fields []any) string {
	var result strings.Builder
	for i, f := range fields {
		if i > 0 {
			result.WriteString(",")
		}
		result.WriteString(fmt.Sprintf("%v", f))
	}
	return result.String()
}

// joinMessageFields joins fields for JSON serialization with proper comma handling.
func joinMessageFields(fields []any) string {
	var result strings.Builder
	for i, f := range fields {
		if i > 0 {
			result.WriteString(",")
		}
		result.WriteString(fmt.Sprintf("%v", f))
	}
	return result.String()
}

// TransformStreamEvent transforms an SDK stream event to a minimal JSON representation.
// This ensures only relevant fields are serialized without zero-value padding.
func TransformStreamEvent(event any) (json.RawMessage, error) {
	if anthropicEvent, ok := event.(api.AnthropicStreamEvent); ok {
		switch anthropicEvent.Type {
		case "message_start":
			return transformMessageStart(anthropicEvent)
		case "content_block_start":
			return transformContentBlockStart(anthropicEvent)
		case "content_block_delta":
			return transformContentBlockDelta(anthropicEvent)
		case "content_block_stop":
			return transformContentBlockStop(anthropicEvent)
		case "message_delta":
			return transformMessageDelta(anthropicEvent)
		case "message_stop":
			return transformMessageStop(anthropicEvent)
		}
	}
	// Fallback: marshal as-is but this may include zero-value fields
	return json.Marshal(event)
}

func transformMessageStart(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	// Build minimal message using proper struct with MarshalJSON
	// Always populate all usage fields (even with 0 values) per reference format
	// Field order: input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens
	usage := &StreamUsage{
		InputTokens:              e.Message.Usage.InputTokens,
		CacheCreationInputTokens: e.Message.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     e.Message.Usage.CacheReadInputTokens,
		OutputTokens:             e.Message.Usage.OutputTokens,
		ServiceTier:              "standard",
	}

	msg := struct {
		Type    string         `json:"type"`
		Message MinimalMessage `json:"message"`
	}{
		Type: "message_start",
		Message: MinimalMessage{
			ID:      e.Message.ID,
			Type:    "message",
			Role:    "assistant",
			Model:   e.Message.Model,
			Content: []any{},
			Usage:   usage,
		},
	}
	// Set StopReason as *string - nil means null, pointer means value
	if e.Message.StopReason != "" {
		msg.Message.StopReason = &e.Message.StopReason
	}
	// Set StopSeq as *string - nil means null, pointer means value
	if e.Message.StopSequence != "" {
		msg.Message.StopSeq = &e.Message.StopSequence
	}
	return json.Marshal(msg)
}

func transformContentBlockStart(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	// Build minimal content_block based on type using struct with custom MarshalJSON
	cb := MinimalContentBlock{Type: e.ContentBlock.Type}

	switch e.ContentBlock.Type {
	case "thinking":
		// Always include thinking and signature in content_block_start (empty if not yet streamed)
		// Reference format includes these even if empty
		cb.Thinking = e.ContentBlock.Thinking
		cb.Signature = e.ContentBlock.Signature
	case "text":
		// Always include text field even when empty (per reference format for content_block_start)
		cb.Text = e.ContentBlock.Text
	case "tool_use":
		cb.ID = e.ContentBlock.ID
		cb.Name = e.ContentBlock.Name
		if e.ContentBlock.Input != nil {
			cb.Input = e.ContentBlock.Input
		}
	}

	// Reference order: type, index, content_block
	msg := struct {
		Type         string              `json:"type"`
		Index        int                 `json:"index"`
		ContentBlock MinimalContentBlock `json:"content_block"`
	}{
		Type:         "content_block_start",
		Index:        e.Index,
		ContentBlock: cb,
	}
	return json.Marshal(msg)
}

func transformContentBlockDelta(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	// Build minimal delta using struct with custom MarshalJSON
	delta := MinimalDelta{Type: e.Delta.Type}

	switch e.Delta.Type {
	case "thinking_delta":
		if e.Delta.Thinking != "" {
			delta.Thinking = e.Delta.Thinking
		}
		if e.Delta.Signature != "" {
			delta.Signature = e.Delta.Signature
		}
	case "text_delta":
		if e.Delta.Text != "" {
			delta.Text = e.Delta.Text
		}
	case "input_json_delta":
		if e.Delta.PartialJSON != "" {
			delta.PartialJSON = e.Delta.PartialJSON
		}
	case "signature_delta":
		if e.Delta.Signature != "" {
			delta.Signature = e.Delta.Signature
		}
	}

	msg := struct {
		Type  string       `json:"type"`
		Index int          `json:"index"`
		Delta MinimalDelta `json:"delta"`
	}{
		Type:  "content_block_delta",
		Index: e.Index,
		Delta: delta,
	}
	return json.Marshal(msg)
}

func transformContentBlockStop(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	event := contentBlockStopEvent{
		Type:  "content_block_stop",
		Index: e.Index,
	}
	return json.Marshal(event)
}

func transformMessageDelta(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	// Always emit delta for message_delta (per reference format)
	delta := MinimalDelta{Type: "message_delta"}
	if e.Delta.StopReason != "" {
		delta.StopReason = e.Delta.StopReason
	}
	// Use pointer for StopSeq so it marshals as null when empty
	if e.Delta.StopSequence != "" {
		delta.StopSeq = &e.Delta.StopSequence
	}

	msg := struct {
		Type  string       `json:"type"`
		Delta MinimalDelta `json:"delta"`
		Usage *StreamUsage `json:"usage,omitempty"`
	}{
		Type:  "message_delta",
		Delta: delta,
	}

	// Add usage if present
	// Field order: input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens
	if e.Usage != nil && (e.Usage.InputTokens > 0 || e.Usage.OutputTokens > 0 ||
		e.Usage.CacheReadInputTokens > 0 || e.Usage.CacheCreationInputTokens > 0) {
		msg.Usage = &StreamUsage{
			InputTokens:              e.Usage.InputTokens,
			CacheCreationInputTokens: e.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     e.Usage.CacheReadInputTokens,
			OutputTokens:             e.Usage.OutputTokens,
			ServiceTier:              "standard",
		}
	}

	return json.Marshal(msg)
}

func transformMessageStop(e api.AnthropicStreamEvent) (json.RawMessage, error) {
	event := messageStopEvent{
		Type: "message_stop",
	}
	return json.Marshal(event)
}
