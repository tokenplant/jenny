package api

import "encoding/json"

// AnthropicRequest represents a request to the Anthropic Messages API.
type AnthropicRequest struct {
	Model             string                  `json:"model"`
	Messages          []AnthropicMessage      `json:"messages"`
	System            []AnthropicContentBlock `json:"system,omitempty"`
	MaxTokens         int                     `json:"max_tokens"`
	StopSequences     []string                `json:"stop_sequences,omitempty"`
	Stream            bool                    `json:"stream,omitempty"`
	Temperature       *float64                `json:"temperature,omitempty"`
	TopP              *float64                `json:"top_p,omitempty"`
	TopK              *int                    `json:"top_k,omitempty"`
	Tools             []AnthropicTool         `json:"tools,omitempty"`
}

// AnthropicMessage represents a message in the Anthropic Messages API.
type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

// AnthropicContentBlock represents a content block in an Anthropic message.
type AnthropicContentBlock struct {
	Type         string                `json:"type"`
	Text         string                `json:"text,omitempty"`
	Thinking     string                `json:"thinking,omitempty"`     // For thinking
	Signature    string                `json:"signature,omitempty"`    // For thinking
	ID           string                `json:"id,omitempty"`           // For tool_use
	Name         string                `json:"name,omitempty"`         // For tool_use
	Input        any                   `json:"input,omitempty"`        // For tool_use
	ToolUseID    string                `json:"tool_use_id,omitempty"`  // For tool_result
	Content      json.RawMessage       `json:"content,omitempty"`      // For tool_result (Polymorphic: string or array)
	IsError      bool                  `json:"is_error,omitempty"`     // For tool_result
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// GetContent returns the tool_result content as a string.
func (b AnthropicContentBlock) GetContent() string {
	if len(b.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(b.Content, &s); err == nil {
		return s
	}
	return string(b.Content)
}

// SetContent sets the tool_result content as a JSON string.
func (b *AnthropicContentBlock) SetContent(s string) {
	b.Content, _ = json.Marshal(s)
}

// AnthropicCacheControl represents cache control settings for a content block.
type AnthropicCacheControl struct {
	Type string `json:"type"`
}

// AnthropicTool represents a tool definition for Anthropic.
type AnthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  AnthropicInputSchema   `json:"input_schema"`
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// AnthropicInputSchema represents the input schema for an Anthropic tool.
type AnthropicInputSchema struct {
	Type        string         `json:"type"`
	Properties  map[string]any `json:"properties"`
	Required    []string       `json:"required,omitempty"`
	ExtraFields map[string]any `json:"-"` // Not serialized directly
}

// MarshalJSON implements custom marshaling for AnthropicInputSchema to include ExtraFields.
func (s AnthropicInputSchema) MarshalJSON() ([]byte, error) {
	type Alias AnthropicInputSchema
	aux := &struct {
		Type string `json:"type"`
		Alias
	}{
		Type:  s.Type,
		Alias: (Alias)(s),
	}
	if aux.Type == "" {
		aux.Type = "object"
	}

	data, err := json.Marshal(aux)
	if err != nil {
		return nil, err
	}

	if len(s.ExtraFields) == 0 {
		return data, nil
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	for k, v := range s.ExtraFields {
		m[k] = v
	}

	return json.Marshal(m)
}

// AnthropicResponse represents a response from the Anthropic Messages API.
type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence string                  `json:"stop_sequence"`
	Usage        AnthropicUsage          `json:"usage"`
}

// AnthropicUsage represents token usage in an Anthropic response.
type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// AnthropicStreamEvent represents a generic event in an Anthropic stream.
type AnthropicStreamEvent struct {
	Type         string                  `json:"type"`
	Message      *AnthropicResponse      `json:"message,omitempty"`       // message_start
	Index        int                     `json:"index,omitempty"`         // content_block_start, content_block_delta
	ContentBlock *AnthropicContentBlock `json:"content_block,omitempty"` // content_block_start
	Delta        *AnthropicStreamDelta   `json:"delta,omitempty"`         // content_block_delta, message_delta
	Usage        *AnthropicUsage         `json:"usage,omitempty"`         // message_delta
}

// AnthropicStreamDelta represents a delta in an Anthropic stream.
type AnthropicStreamDelta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	Signature    string `json:"signature,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}
