// Package agent provides the core agent loop and query engine.
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
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
		if m.Text != "" {
			fields = append(fields, `"text":`+encodeString(m.Text))
		}
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
	StopSeq     string
	Text        string
}

func (m MinimalDelta) MarshalJSON() ([]byte, error) {
	fields := []any{`"type":` + encodeString(m.Type)}

	switch m.Type {
	case "thinking_delta":
		if m.Thinking != "" {
			fields = append(fields, `"thinking":`+encodeString(m.Thinking))
		}
		if m.Signature != "" {
			fields = append(fields, `"signature":`+encodeString(m.Signature))
		}
	case "text_delta":
		if m.Text != "" {
			fields = append(fields, `"text":`+encodeString(m.Text))
		}
	case "input_json_delta":
		if m.PartialJSON != "" {
			fields = append(fields, `"partial_json":`+encodeString(m.PartialJSON))
		}
	case "signature_delta":
		if m.Signature != "" {
			fields = append(fields, `"signature":`+encodeString(m.Signature))
		}
	case "message_delta":
		// Reference format: delta has stop_reason/stop_sequence directly, no nested type
		if m.StopReason != "" {
			fields = append(fields, `"stop_reason":`+encodeString(m.StopReason))
		}
		if m.StopSeq != "" {
			fields = append(fields, `"stop_sequence":`+encodeString(m.StopSeq))
		}
	}

	return []byte("{" + joinFields(fields) + "}"), nil
}

// MinimalMessage represents a minimal message for message_start events.
type MinimalMessage struct {
	ID         string
	Type       string
	Role       string
	Model      string
	Content    any
	StopReason string
	StopSeq    string
	Usage      *StreamUsage
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

	if m.Usage != nil {
		usageBytes, err := json.Marshal(m.Usage)
		if err != nil {
			return nil, err
		}
		fields = append(fields, `"usage":`+string(usageBytes))
	}

	if m.StopReason != "" {
		fields = append(fields, `"stop_reason":`+encodeString(m.StopReason))
	}
	if m.StopSeq != "" {
		fields = append(fields, `"stop_sequence":`+encodeString(m.StopSeq))
	}

	return []byte("{" + joinFields(fields) + "}"), nil
}

// StreamUsage represents a minimal usage object for stream events.
type StreamUsage struct {
	InputTokens              int    `json:"input_tokens,omitempty"`
	OutputTokens             int    `json:"output_tokens,omitempty"`
	CacheReadInputTokens     int    `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens,omitempty"`
	ServiceTier              string `json:"service_tier,omitempty"`
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
	// Build minimal message using proper struct with MarshalJSON
	usage := &StreamUsage{}
	if e.Message.Usage.InputTokens > 0 {
		usage.InputTokens = int(e.Message.Usage.InputTokens)
	}
	if e.Message.Usage.CacheReadInputTokens > 0 {
		usage.CacheReadInputTokens = int(e.Message.Usage.CacheReadInputTokens)
	}
	if e.Message.Usage.CacheCreationInputTokens > 0 {
		usage.CacheCreationInputTokens = int(e.Message.Usage.CacheCreationInputTokens)
	}
	if e.Message.Usage.OutputTokens > 0 {
		usage.OutputTokens = int(e.Message.Usage.OutputTokens)
	}
	usage.ServiceTier = "standard"

	msg := struct {
		Type    string         `json:"type"`
		Message MinimalMessage `json:"message"`
	}{
		Type: "message_start",
		Message: MinimalMessage{
			ID:         string(e.Message.ID),
			Type:       "message",
			Role:       "assistant",
			Model:      string(e.Message.Model),
			Content:    []any{},
			Usage:      usage,
			StopReason: string(e.Message.StopReason),
			StopSeq:    e.Message.StopSequence,
		},
	}
	return json.Marshal(msg)
}

func transformContentBlockStart(e anthropic.ContentBlockStartEvent) (json.RawMessage, error) {
	// Build minimal content_block based on type using struct with custom MarshalJSON
	cb := MinimalContentBlock{Type: string(e.ContentBlock.Type)}

	switch e.ContentBlock.Type {
	case "thinking":
		// Always include thinking and signature in content_block_start (empty if not yet streamed)
		// Reference format includes these even if empty
		cb.Thinking = e.ContentBlock.Thinking
		cb.Signature = e.ContentBlock.Signature
	case "text":
		if e.ContentBlock.Text != "" {
			cb.Text = e.ContentBlock.Text
		}
	case "tool_use":
		cb.ID = e.ContentBlock.ID
		cb.Name = e.ContentBlock.Name
		if e.ContentBlock.Input != nil {
			cb.Input = e.ContentBlock.Input
		}
	case "redacted_thinking":
		if e.ContentBlock.Data != "" {
			cb.Text = e.ContentBlock.Data
		}
	}

	// Reference order: type, index, content_block
	msg := struct {
		Type         string              `json:"type"`
		Index        int                 `json:"index"`
		ContentBlock MinimalContentBlock `json:"content_block"`
	}{
		Type:         "content_block_start",
		Index:        int(e.Index),
		ContentBlock: cb,
	}
	return json.Marshal(msg)
}

func transformContentBlockDelta(e anthropic.ContentBlockDeltaEvent) (json.RawMessage, error) {
	// Build minimal delta using struct with custom MarshalJSON
	delta := MinimalDelta{Type: string(e.Delta.Type)}

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
		Index: int(e.Index),
		Delta: delta,
	}
	return json.Marshal(msg)
}

func transformContentBlockStop(e anthropic.ContentBlockStopEvent) (json.RawMessage, error) {
	event := contentBlockStopEvent{
		Type:  "content_block_stop",
		Index: int(e.Index),
	}
	return json.Marshal(event)
}

func transformMessageDelta(e anthropic.MessageDeltaEvent) (json.RawMessage, error) {
	// Build minimal delta - only include if stop_reason or stop_sequence is present
	hasContent := e.Delta.StopReason != "" || e.Delta.StopSequence != ""

	msg := struct {
		Type  string        `json:"type"`
		Delta *MinimalDelta `json:"delta,omitempty"`
		Usage *StreamUsage  `json:"usage,omitempty"`
	}{
		Type: "message_delta",
	}

	if hasContent {
		delta := MinimalDelta{Type: "message_delta"}
		if e.Delta.StopReason != "" {
			delta.StopReason = string(e.Delta.StopReason)
		}
		if e.Delta.StopSequence != "" {
			delta.StopSeq = e.Delta.StopSequence
		}
		msg.Delta = &delta
	}

	// Add usage if present
	if e.Usage.InputTokens > 0 || e.Usage.OutputTokens > 0 ||
		e.Usage.CacheReadInputTokens > 0 || e.Usage.CacheCreationInputTokens > 0 {
		msg.Usage = &StreamUsage{
			InputTokens:              int(e.Usage.InputTokens),
			OutputTokens:             int(e.Usage.OutputTokens),
			CacheReadInputTokens:     int(e.Usage.CacheReadInputTokens),
			CacheCreationInputTokens: int(e.Usage.CacheCreationInputTokens),
			ServiceTier:              "standard",
		}
	}

	return json.Marshal(msg)
}

func transformMessageStop(e anthropic.MessageStopEvent) (json.RawMessage, error) {
	event := messageStopEvent{
		Type: "message_stop",
	}
	return json.Marshal(event)
}

// TimestampNow returns current timestamp in RFC3339Nano format.
func TimestampNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
