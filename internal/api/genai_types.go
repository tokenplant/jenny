package api

// GenAIRequest represents a request to the Gemini API.
type GenAIRequest struct {
	Contents          []GenAIContent    `json:"contents"`
	Tools             []GenAITool       `json:"tools,omitempty"`
	GenerationConfig  *GenAIGenerationConfig `json:"generationConfig,omitempty"`
	SystemInstruction *GenAIContent     `json:"systemInstruction,omitempty"`
}

// GenAIContent represents a content object in the Gemini API.
type GenAIContent struct {
	Role  string      `json:"role,omitempty"`
	Parts []GenAIPart `json:"parts"`
}

// GenAIPart represents a part of a content object.
type GenAIPart struct {
	Text             string                `json:"text,omitempty"`
	InlineData       *GenAIBlob            `json:"inlineData,omitempty"`
	FunctionCall     *GenAIFunctionCall    `json:"functionCall,omitempty"`
	FunctionResponse *GenAIFunctionResponse `json:"functionResponse,omitempty"`
	Thought          bool                  `json:"thought,omitempty"` // For thinking/reasoning
}

// GenAIBlob represents inline data (e.g. images).
type GenAIBlob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// GenAIFunctionCall represents a function call from the model.
type GenAIFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// GenAIFunctionResponse represents a response to a function call.
type GenAIFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// GenAITool represents a tool definition.
type GenAITool struct {
	FunctionDeclarations []GenAIFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// GenAIFunctionDeclaration represents a function declaration.
type GenAIFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// GenAIGenerationConfig represents generation configuration.
type GenAIGenerationConfig struct {
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	TopK            *int     `json:"topK,omitempty"`
	CandidateCount  *int     `json:"candidateCount,omitempty"`
}

// GenAIResponse represents a response from the Gemini API.
type GenAIResponse struct {
	Candidates     []GenAICandidate `json:"candidates"`
	UsageMetadata  *GenAIUsage      `json:"usageMetadata,omitempty"`
	ModelVersion   string           `json:"modelVersion"`
}

// GenAICandidate represents a candidate in the response.
type GenAICandidate struct {
	Content       GenAIContent       `json:"content"`
	FinishReason  string             `json:"finishReason"`
	Index         int                `json:"index"`
	SafetyRatings []GenAISafetyRating `json:"safetyRatings,omitempty"`
}

// GenAISafetyRating represents a safety rating.
type GenAISafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

// GenAIUsage represents usage metadata.
type GenAIUsage struct {
	PromptTokenCount         int `json:"promptTokenCount"`
	CandidatesTokenCount     int `json:"candidatesTokenCount"`
	TotalTokenCount          int `json:"totalTokenCount"`
	CachedContentTokenCount  int `json:"cachedContentTokenCount,omitempty"`
	ThoughtsTokenCount       int `json:"thoughtsTokenCount,omitempty"`
}

// GenAIStreamResponse represents a chunk in a streaming response.
// Note: Gemini streaming returns a JSON array of GenAIResponse objects over time,
// or a single array if using the "alt=sse" parameter it might be different.
// Actually, the default stream is a JSON array that grows. We should use alt=sse if supported.
type GenAIStreamResponse GenAIResponse
