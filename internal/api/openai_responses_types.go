package api

// OpenAIResponsesRequest represents a request to the OpenAI Responses API.
type OpenAIResponsesRequest struct {
	Model            string                          `json:"model"`
	Input            any                             `json:"input"`
	MaxOutputTokens  *int64                          `json:"max_output_tokens,omitempty"`
	Tools           []OpenAIResponsesTool           `json:"tools,omitempty"`
	ReasoningConfig *OpenAIResponsesReasoningConfig `json:"reasoning_config,omitempty"`
	Stream           bool                            `json:"stream,omitempty"`
	StreamOptions    *OpenAIStreamOptions            `json:"stream_options,omitempty"`
}

// OpenAIResponsesReasoningConfig represents the reasoning configuration for the Responses API.
type OpenAIResponsesReasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

// OpenAIResponsesTool represents a tool definition for the Responses API.
type OpenAIResponsesTool struct {
	Type     string                           `json:"type"`
	Function OpenAIResponsesFunction          `json:"function"`
}

// OpenAIResponsesFunction represents a function definition for the Responses API.
type OpenAIResponsesFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// OpenAIResponsesResponse represents a response from the OpenAI Responses API.
type OpenAIResponsesResponse struct {
	ID    string                      `json:"id"`
	Model string                      `json:"model"`
	Output []OpenAIResponsesOutputItem `json:"output"`
	Usage OpenAIResponsesUsage        `json:"usage"`
}

// OpenAIResponsesOutputItem represents an item in the output array.
type OpenAIResponsesOutputItem struct {
	ID     string                             `json:"id"`
	Type   string                             `json:"type"`
	Role   string                             `json:"role,omitempty"`
	Status string                             `json:"status,omitempty"`
	// For message type
	Content []OpenAIResponsesContentBlock     `json:"content,omitempty"`
	// For reasoning type
	Summary   *OpenAIResponsesReasoningSummary `json:"summary,omitempty"`
}

// OpenAIResponsesReasoningSummary represents the summary of a reasoning block.
type OpenAIResponsesReasoningSummary struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
}

// OpenAIResponsesContentBlock represents a content block in a message.
type OpenAIResponsesContentBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	Index      int             `json:"index,omitempty"`
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Arguments  string          `json:"arguments,omitempty"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`
	Content    any             `json:"content,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
}

// OpenAIResponsesUsage represents token usage in the response.
type OpenAIResponsesUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	TotalTokens     int `json:"total_tokens"`
	PromptTokens    int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
}