package api

import "encoding/json"

// OpenAIRequest represents a chat completion request.
type OpenAIRequest struct {
	Model               string               `json:"model"`
	Messages            []OpenAIMessage      `json:"messages"`
	Tools               []OpenAITool         `json:"tools,omitempty"`
	MaxCompletionTokens *int64               `json:"max_completion_tokens,omitempty"`
	Stream              bool                 `json:"stream,omitempty"`
	StreamOptions       *OpenAIStreamOptions `json:"stream_options,omitempty"`
	ReasoningEffort     string               `json:"reasoning_effort,omitempty"`
	ExtraBody           map[string]any       `json:"extra_body,omitempty"`
}

// OpenAIMessage represents a message in the OpenAI chat format.
type OpenAIMessage struct {
	Role             string           `json:"role"`
	Content          json.RawMessage  `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

// GetContent returns the content as a string, handling both string and array formats.
func (m OpenAIMessage) GetContent() string {
	if len(m.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	// If not a string, it might be an array of blocks (vision etc)
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" {
				return b.Text // Simplified: return first text block
			}
		}
	}
	return string(m.Content)
}

// SetContent sets the content as a JSON string.
func (m *OpenAIMessage) SetContent(s string) {
	m.Content, _ = json.Marshal(s)
}

// OpenAITool represents a tool definition.
type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction represents a function definition.
type OpenAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// OpenAIToolCall represents a tool call in a message.
type OpenAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAIFunctionCall `json:"function"`
}

// OpenAIFunctionCall represents the function details in a tool call.
type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OpenAIStreamOptions represents options for streaming responses.
type OpenAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// OpenAIResponse represents a chat completion response.
type OpenAIResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

// OpenAIChoice represents a single choice in the response.
type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// OpenAIUsage represents token usage in the response.
type OpenAIUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// OpenAIStreamChunk represents a single chunk in a streaming response.
type OpenAIStreamChunk struct {
	ID      string               `json:"id"`
	Model   string               `json:"model"`
	Choices []OpenAIStreamChoice `json:"choices"`
	Usage   *OpenAIUsage         `json:"usage,omitempty"`
}

// OpenAIStreamChoice represents a single choice in a stream chunk.
type OpenAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        OpenAIStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

// OpenAIStreamDelta represents the change in the message in a stream chunk.
type OpenAIStreamDelta struct {
	Role             string                 `json:"role,omitempty"`
	Content          string                 `json:"content,omitempty"`
	ReasoningContent string                 `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIStreamToolCall `json:"tool_calls,omitempty"`
}

// OpenAIStreamToolCall represents a tool call delta in a stream chunk.
type OpenAIStreamToolCall struct {
	Index    int                  `json:"index"`
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function OpenAIStreamFunction `json:"function,omitempty"`
}

// OpenAIStreamFunction represents the function details delta in a stream chunk.
type OpenAIStreamFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
