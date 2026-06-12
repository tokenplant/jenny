// Package api provides the OpenAI Responses API provider.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/log"
)

// openAIResponsesProvider implements the Provider interface using the OpenAI Responses API.
type openAIResponsesProvider struct {
	client      *HTTPClient
	model       string
	maxTokens   int
	retryConfig RetryConfig
	effort      string
}

// newOpenAIResponsesProvider creates a new OpenAI Responses API provider.
func newOpenAIResponsesProvider(model string) (*openAIResponsesProvider, error) {
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		return nil, errors.New("OPENAI_BASE_URL is required for OpenAI Responses API")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is required for OpenAI Responses API")
	}

	if model == "" {
		model = os.Getenv("OPENAI_DEFAULT_MODEL")
	}
	if model == "" {
		return nil, errors.New("OPENAI_DEFAULT_MODEL is required when using OpenAI Responses API")
	}

	timeout := ResolveTimeout(os.Getenv("API_TIMEOUT_MS"))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	return &openAIResponsesProvider{
		client:      NewHTTPClient(timeout),
		model:       model,
		maxTokens:   64000,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// Kind returns the provider kind.
func (p *openAIResponsesProvider) Kind() ProviderKind {
	return ProviderOpenAIResponses
}

// SetModel sets the model.
func (p *openAIResponsesProvider) SetModel(model string) {
	p.model = model
}

// GetModel returns the model.
func (p *openAIResponsesProvider) GetModel() string {
	return p.model
}

// SetMaxTokensOverride sets the max_tokens override.
func (p *openAIResponsesProvider) SetMaxTokensOverride(maxTokens int) {
	p.maxTokens = maxTokens
}

// SetRetryConfig sets the retry configuration.
func (p *openAIResponsesProvider) SetRetryConfig(cfg RetryConfig) {
	p.retryConfig = cfg
}

// SetThinkingConfig sets the thinking configuration.
func (p *openAIResponsesProvider) SetThinkingConfig(cfg ThinkingConfig) {
	if cfg.Effort != "" {
		p.effort = cfg.Effort
	}
}

// SendMessage sends a non-streaming message.
func (p *openAIResponsesProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	return p.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return p.doSendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
	}, false)
}

// sendWithRetry executes a function with retry logic.
func (p *openAIResponsesProvider) sendWithRetry(ctx context.Context, fn func(context.Context) (*Response, error), isBackground bool) (*Response, error) {
	cfg := p.retryConfig
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 10
	}

	var lastErr error
	consecutive529 := 0

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		resp, err := fn(ctx)

		if err != nil {
			var httpErr *HTTPError
			if errors.As(err, &httpErr) {
				statusCode := httpErr.StatusCode

				if isBackground && statusCode == StatusProxyError {
					return nil, &CannotRetryError{
						Message:    "Background request rejected with 529 Overloaded",
						StatusCode: statusCode,
					}
				}

				if statusCode == StatusProxyError {
					consecutive529++
					if consecutive529 > cfg.Max529Retries {
						return nil, &CannotRetryError{
							Message:    "Repeated 529 Overloaded errors",
							StatusCode: statusCode,
						}
					}
				} else {
					consecutive529 = 0
				}

				isPermanent := statusCode >= 400 && statusCode < 500 &&
					statusCode != 429 && statusCode != 408 && statusCode != 409
				retryableErr := &RetryableHTTPError{
					StatusCode:  statusCode,
					Message:     err.Error(),
					IsPermanent: isPermanent,
				}

				if retryableErr.IsPermanent || !isRetryable(statusCode, nil) {
					return nil, retryableErr
				}

				lastErr = retryableErr
			} else {
				if !isRetryable(0, err) {
					return nil, err
				}
				lastErr = err
			}
		} else {
			return resp, nil
		}

		if attempt < cfg.MaxRetries {
			delay := computeBackoff(attempt, cfg, nil)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("max retries exhausted")
}

// doSendMessage performs the actual message sending.
func (p *openAIResponsesProvider) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	log.Debug("OpenAI Responses API sending message", "model", p.model)

	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

	input := p.buildInput(messages, toolResults, systemPrompt, systemPromptSuffix)

	var sdkTools []OpenAIResponsesTool
	if len(tools) > 0 {
		sdkTools = p.buildTools(tools)
	}

	maxTokens := int64(p.maxTokens)
	if maxTokens == 0 {
		maxTokens = 64000
	}

	reqBody := OpenAIResponsesRequest{
		Model:           p.model,
		Input:           input,
		MaxOutputTokens: &maxTokens,
		Tools:           sdkTools,
	}

	// Add reasoning config if effort is set
	if p.effort != "" {
		reqBody.ReasoningConfig = &OpenAIResponsesReasoningConfig{
			Effort: p.effort,
		}
	}

	url := fmt.Sprintf("%s/v1/responses", os.Getenv("OPENAI_BASE_URL"))
	headers := http.Header{}
	headers.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("OPENAI_API_KEY")))

	var resp OpenAIResponsesResponse
	if err := p.client.Request(ctx, "POST", url, headers, reqBody, &resp); err != nil {
		return nil, err
	}

	return p.parseResponse(&resp)
}

// buildInput builds the input for the Responses API from messages.
func (p *openAIResponsesProvider) buildInput(messages []Message, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) []any {
	var input []any

	// Add system message if present
	if systemPrompt != "" || systemPromptSuffix != "" {
		fullSystem := systemPrompt
		if systemPromptSuffix != "" {
			fullSystem = systemPrompt + "\n\n" + systemPromptSuffix
		}
		input = append(input, map[string]any{
			"type": "message",
			"role": "system",
			"content": []map[string]any{
				{"type": "input_text", "text": fullSystem},
			},
		})
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if msg.Content != "" {
				input = append(input, map[string]any{
					"type": "message",
					"role": "user",
					"content": []map[string]any{
						{"type": "input_text", "text": msg.Content},
					},
				})
			}

		case "assistant":
			content := []map[string]any{}
			if msg.Content != "" {
				content = append(content, map[string]any{
					"type": "output_text",
					"text": msg.Content,
				})
			}
			for _, tu := range msg.ToolUse {
				inputJSON, _ := json.Marshal(tu.Input)
				content = append(content, map[string]any{
					"type":      "function_call",
					"id":        tu.ID,
					"name":      tu.Name,
					"arguments": string(inputJSON),
				})
			}
			if len(content) > 0 {
				input = append(input, map[string]any{
					"type":    "message",
					"role":    "assistant",
					"content": content,
				})
			}
		}
	}

	for _, tr := range toolResults {
		input = append(input, map[string]any{
			"type": "message",
			"role": "user",
			"content": []map[string]any{
				{
					"type": "function_call_output",
					"call_id": tr.ToolUseID,
					"output":  tr.Content,
				},
			},
		})
	}

	// Handle tool results in messages
	for _, msg := range messages {
		for _, tr := range msg.ToolResults {
			input = append(input, map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{
						"type": "function_call_output",
						"call_id": tr.ToolUseID,
						"output":  tr.Content,
					},
				},
			})
		}
	}

	return input
}

// buildTools converts api.ToolParam slices to OpenAI Responses API format.
func (p *openAIResponsesProvider) buildTools(tools []ToolParam) []OpenAIResponsesTool {
	sdkTools := make([]OpenAIResponsesTool, 0, len(tools))
	for _, t := range tools {
		sdkTools = append(sdkTools, OpenAIResponsesTool{
			Type: "function",
			Function: OpenAIResponsesFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  p.buildInputSchema(t.InputSchema),
			},
		})
	}
	return sdkTools
}

// buildInputSchema converts ToolInputSchema to OpenAI Responses API parameters format.
func (p *openAIResponsesProvider) buildInputSchema(schema ToolInputSchema) map[string]any {
	result := map[string]any{
		"type": "object",
	}

	if len(schema.Properties) > 0 {
		result["properties"] = schema.Properties
	}

	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}

	for k, v := range schema.ExtraFields {
		result[k] = v
	}

	return result
}

// parseResponse converts an OpenAI Responses API response to api.Response.
func (p *openAIResponsesProvider) parseResponse(resp *OpenAIResponsesResponse) (*Response, error) {
	response := &Response{
		Model: resp.Model,
	}

	var hasToolCalls bool
	var stopReason StopReason

	for _, item := range resp.Output {
		switch item.Type {
		case "reasoning":
			// Handle reasoning blocks
			var summaryText string
			if item.Summary != nil && item.Summary.Text != "" {
				summaryText = item.Summary.Text
			}
			response.Content = append(response.Content, ContentBlock{
				Type:     "thinking",
				Thinking: summaryText,
			})

		case "message":
			for _, block := range item.Content {
				switch block.Type {
				case "output_text":
					if block.Text != "" {
						response.Content = append(response.Content, ContentBlock{
							Type: "text",
							Text: block.Text,
						})
					}

				case "function_call":
					hasToolCalls = true
					var input map[string]any
					if block.Arguments != "" {
						json.Unmarshal([]byte(block.Arguments), &input)
					}
					response.Content = append(response.Content, ContentBlock{
						Type:      "tool_use",
						ToolID:    block.ID,
						ToolName:  block.Name,
						ToolInput: input,
					})

				case "function_call_output":
					// Function call outputs are handled as user messages in next turn
				}
			}
		}
	}

	// Determine stop reason
	if hasToolCalls {
		stopReason = StopReasonToolUse
	} else {
		stopReason = StopReasonEndTurn
	}
	response.StopReason = stopReason

	// Parse usage
	response.Usage.InputTokens = resp.Usage.InputTokens
	response.Usage.OutputTokens = resp.Usage.OutputTokens

	return response, nil
}

// isPromptTooLongOpenAIResponses returns true if the error indicates prompt too long.
func isPromptTooLongOpenAIResponses(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "prompt_too_long") || strings.Contains(msg, "context window exceeds limit")
}

// SendMessageStream sends a streaming message (not yet implemented for Responses API).
func (p *openAIResponsesProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		log.Debug("OpenAI Responses API streaming message", "model", p.model)

		// For now, fall back to non-streaming
		resp, err := p.SendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
		if err != nil {
			result.Error = err.Error()
			return
		}

		// Emit blocks
		for _, block := range resp.Content {
			blocksChan <- StreamContentBlock{
				Block: block,
			}
		}

		result.Blocks = resp.Content
		result.StopReason = resp.StopReason
		result.Usage = resp.Usage
		result.Model = resp.Model
		result.StreamComplete = true
	}()

	return blocksChan, result
}