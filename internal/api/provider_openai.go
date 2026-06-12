// Package api provides the OpenAI API client.
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

// openAIProvider implements the Provider interface using a lightweight HTTP client.
type openAIProvider struct {
	client      *HTTPClient
	model       string
	maxTokens   int
	retryConfig RetryConfig
}

// newOpenAIProvider creates a new OpenAI provider.
func newOpenAIProvider(model string) (*openAIProvider, error) {
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		return nil, errors.New("OPENAI_BASE_URL is required for OpenAI provider")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is required for OpenAI provider")
	}

	if model == "" {
		model = os.Getenv("OPENAI_DEFAULT_MODEL")
	}
	if model == "" {
		return nil, errors.New("OPENAI_DEFAULT_MODEL is required when using OpenAI provider")
	}

	timeout := ResolveTimeout(os.Getenv("API_TIMEOUT_MS"))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	return &openAIProvider{
		client:      NewHTTPClient(timeout),
		model:       model,
		maxTokens:   64000,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// Kind returns the provider kind.
func (p *openAIProvider) Kind() ProviderKind {
	return ProviderOpenAI
}

// SetModel sets the model.
func (p *openAIProvider) SetModel(model string) {
	p.model = model
}

// GetModel returns the model.
func (p *openAIProvider) GetModel() string {
	return p.model
}

// SetMaxTokensOverride sets the max_tokens override.
func (p *openAIProvider) SetMaxTokensOverride(maxTokens int) {
	p.maxTokens = maxTokens
}

// SetRetryConfig sets the retry configuration.
func (p *openAIProvider) SetRetryConfig(cfg RetryConfig) {
	p.retryConfig = cfg
}

// SendMessage sends a non-streaming message.
func (p *openAIProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	return p.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return p.doSendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
	}, false)
}

// sendWithRetry executes a function with retry logic.
func (p *openAIProvider) sendWithRetry(ctx context.Context, fn func(context.Context) (*Response, error), isBackground bool) (*Response, error) {
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
func (p *openAIProvider) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	log.Debug("OpenAI provider sending message", "model", p.model)

	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

	fullSystemPrompt := systemPrompt
	if systemPromptSuffix != "" {
		fullSystemPrompt = systemPrompt + "\n\n" + systemPromptSuffix
	}
	sdkMessages := p.buildMessages(messages, toolResults, fullSystemPrompt)

	var sdkTools []OpenAITool
	if len(tools) > 0 {
		sdkTools = p.buildTools(tools)
	}

	maxTokens := int64(p.maxTokens)
	if maxTokens == 0 {
		maxTokens = 64000
	}

	reqBody := OpenAIRequest{
		Model:               p.model,
		Messages:            sdkMessages,
		MaxCompletionTokens: &maxTokens,
		Tools:               sdkTools,
	}

	url := fmt.Sprintf("%s/chat/completions", os.Getenv("OPENAI_BASE_URL"))
	headers := http.Header{}
	headers.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("OPENAI_API_KEY")))

	var openAIResp OpenAIResponse
	if err := p.client.Request(ctx, "POST", url, headers, reqBody, &openAIResp); err != nil {
		return nil, err
	}

	return p.parseResponse(&openAIResp)
}

// buildMessages converts api.Message slices to OpenAI format.
func (p *openAIProvider) buildMessages(messages []Message, toolResults []ToolResult, systemPrompt string) []OpenAIMessage {
	var sdkMessages []OpenAIMessage

	if systemPrompt != "" {
		sdkMsg := OpenAIMessage{Role: "system"}
		sdkMsg.SetContent(systemPrompt)
		sdkMessages = append(sdkMessages, sdkMsg)
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if msg.Content != "" {
				sdkMsg := OpenAIMessage{Role: "user"}
				sdkMsg.SetContent(msg.Content)
				sdkMessages = append(sdkMessages, sdkMsg)
			}

		case "assistant":
			sdkMsg := OpenAIMessage{Role: "assistant"}
			sdkMsg.SetContent(msg.Content)
			if len(msg.ToolUse) > 0 {
				sdkMsg.ToolCalls = make([]OpenAIToolCall, 0, len(msg.ToolUse))
				for _, tu := range msg.ToolUse {
					inputJSON, _ := json.Marshal(tu.Input)
					sdkMsg.ToolCalls = append(sdkMsg.ToolCalls, OpenAIToolCall{
						ID:   tu.ID,
						Type: "function",
						Function: OpenAIFunctionCall{
							Name:      tu.Name,
							Arguments: string(inputJSON),
						},
					})
				}
			}
			sdkMessages = append(sdkMessages, sdkMsg)
		}
	}

	for _, tr := range toolResults {
		sdkMsg := OpenAIMessage{Role: "tool", ToolCallID: tr.ToolUseID}
		sdkMsg.SetContent(tr.Content)
		sdkMessages = append(sdkMessages, sdkMsg)
	}

	for _, msg := range messages {
		for _, tr := range msg.ToolResults {
			sdkMsg := OpenAIMessage{Role: "tool", ToolCallID: tr.ToolUseID}
			sdkMsg.SetContent(tr.Content)
			sdkMessages = append(sdkMessages, sdkMsg)
		}
	}

	return sdkMessages
}

// buildTools converts api.ToolParam slices to OpenAI format.
func (p *openAIProvider) buildTools(tools []ToolParam) []OpenAITool {
	sdkTools := make([]OpenAITool, 0, len(tools))
	for _, t := range tools {
		sdkTools = append(sdkTools, OpenAITool{
			Type: "function",
			Function: OpenAIFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  p.buildInputSchema(t.InputSchema),
			},
		})
	}
	return sdkTools
}

// buildInputSchema converts ToolInputSchema to OpenAI parameters format.
func (p *openAIProvider) buildInputSchema(schema ToolInputSchema) map[string]any {
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

// parseResponse converts an OpenAI response to api.Response.
func (p *openAIProvider) parseResponse(resp *OpenAIResponse) (*Response, error) {
	response := &Response{
		Model: resp.Model,
	}

	if len(resp.Choices) == 0 {
		return response, nil
	}

	choice := resp.Choices[0]

	switch choice.FinishReason {
	case "stop":
		response.StopReason = StopReasonEndTurn
	case "tool_calls":
		response.StopReason = StopReasonToolUse
	case "length":
		response.StopReason = StopReasonMaxTokens
	default:
		response.StopReason = StopReason(choice.FinishReason)
	}

	if choice.Message.ReasoningContent != "" {
		response.Content = append(response.Content, ContentBlock{
			Type:     "thinking",
			Thinking: choice.Message.ReasoningContent,
		})
	}

	if content := choice.Message.GetContent(); content != "" {
		response.Content = append(response.Content, ContentBlock{
			Type: "text",
			Text: content,
		})
	}

	for _, tc := range choice.Message.ToolCalls {
		var input map[string]any
		if tc.Function.Arguments != "" {
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
		}
		response.Content = append(response.Content, ContentBlock{
			Type:      "tool_use",
			ToolID:    tc.ID,
			ToolName:  tc.Function.Name,
			ToolInput: input,
		})
	}

	response.Usage.InputTokens = resp.Usage.PromptTokens
	response.Usage.OutputTokens = resp.Usage.CompletionTokens
	response.Usage.CacheReadInputTokens = resp.Usage.PromptTokensDetails.CachedTokens

	return response, nil
}

// isPromptTooLongOpenAI returns true if the error indicates prompt too long.
func isPromptTooLongOpenAI(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "prompt_too_long") || strings.Contains(msg, "context window exceeds limit")
}

// SendMessageStream sends a streaming message.
func (p *openAIProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		log.Debug("OpenAI provider streaming message", "model", p.model)

		messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

		fullSystemPrompt := systemPrompt
		if systemPromptSuffix != "" {
			fullSystemPrompt = systemPrompt + "\n\n" + systemPromptSuffix
		}
		sdkMessages := p.buildMessages(messages, toolResults, fullSystemPrompt)

		var sdkTools []OpenAITool
		if len(tools) > 0 {
			sdkTools = p.buildTools(tools)
		}

		maxTokens := int64(p.maxTokens)
		if maxTokens == 0 {
			maxTokens = 64000
		}

		reqBody := OpenAIRequest{
			Model:               p.model,
			Messages:            sdkMessages,
			MaxCompletionTokens: &maxTokens,
			Tools:               sdkTools,
			Stream:              true,
			StreamOptions:       &OpenAIStreamOptions{IncludeUsage: true},
		}

		url := fmt.Sprintf("%s/chat/completions", os.Getenv("OPENAI_BASE_URL"))
		headers := http.Header{}
		headers.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("OPENAI_API_KEY")))

		if idleTimeout <= 0 {
			idleTimeout = DefaultIdleTimeout
		}

		body, err := p.client.StreamRequest(ctx, "POST", url, headers, reqBody)
		if err != nil {
			var httpErr *HTTPError
			if errors.As(err, &httpErr) {
				result.IsPermanent = httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 &&
					httpErr.StatusCode != 429 && httpErr.StatusCode != 408 && httpErr.StatusCode != 409
			}
			result.Error = err.Error()
			if isPromptTooLongOpenAI(err) {
				result.ContextRejected = true
				result.MaxTokensErr = &MaxTokensError{
					Category:     CategoryContextExhausted,
					Model:        p.model,
					OutputTokens: result.Usage.OutputTokens,
				}
			}
			return
		}
		defer body.Close()

		acc := newOpenAIStreamAccumulator()
		hasStopReason := false
		scanner := NewSSEScanner(body)

		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		watchdogCtx, cancelWatchdog := context.WithCancel(ctx)
		defer cancelWatchdog()

		go func() {
			select {
			case <-idleTimer.C:
				log.Warn("OpenAI: Idle timeout reached")
				result.Error = "idle timeout"
				body.Close()
			case <-watchdogCtx.Done():
			}
		}()

		for {
			data, ok := scanner.Next()
			if !ok {
				break
			}

			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleTimeout)

			var chunk OpenAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				log.Error("OpenAI: failed to unmarshal chunk", "error", err, "data", data)
				continue
			}

			hasStopReason = p.processStreamChunk(&chunk, acc, blocksChan, result) || hasStopReason
		}

		if scanner.Err() != nil && result.Error == "" {
			result.Error = scanner.Err().Error()
		}

		if isPromptTooLongOpenAI(errors.New(result.Error)) {
			result.ContextRejected = true
		}

		if !hasStopReason && result.Error == "" {
			result.Error = "stream incomplete: no stop reason"
		}

		if (result.StopReason == StopReasonMaxTokens || result.ContextRejected) && result.MaxTokensErr == nil {
			result.MaxTokensErr = &MaxTokensError{
				Category:     CategoryOutputCapHit,
				Model:        p.model,
				OutputTokens: result.Usage.OutputTokens,
			}
			if result.ContextRejected {
				result.MaxTokensErr.Category = CategoryContextExhausted
			}
		}

		result.StreamComplete = hasStopReason
		result.Blocks = acc.finalize()
		result.StopReason = acc.stopReason
		result.Model = p.model
	}()

	return blocksChan, result
}

// processStreamChunk processes a single OpenAI stream chunk.
func (p *openAIProvider) processStreamChunk(chunk *OpenAIStreamChunk, acc *openAIStreamAccumulator, blocksChan chan<- StreamContentBlock, result *StreamResult) bool {
	if chunk.Model != "" {
		result.Model = chunk.Model
	}

	if chunk.Usage != nil {
		result.Usage.InputTokens = chunk.Usage.PromptTokens
		result.Usage.OutputTokens = chunk.Usage.CompletionTokens
		result.Usage.CacheReadInputTokens = chunk.Usage.PromptTokensDetails.CachedTokens
	}

	if len(chunk.Choices) == 0 {
		return false
	}

	choice := chunk.Choices[0]
	hasStopReason := choice.FinishReason != ""

	switch choice.FinishReason {
	case "stop":
		acc.setStopReason(StopReasonEndTurn)
	case "tool_calls":
		acc.setStopReason(StopReasonToolUse)
	case "length":
		acc.setStopReason(StopReasonMaxTokens)
	}

	delta := choice.Delta

	if delta.Content != "" {
		acc.appendContent(delta.Content)
		blocksChan <- StreamContentBlock{
			Block: ContentBlock{
				Type: "text",
				Text: acc.getContent(),
			},
		}
	}

	if delta.ReasoningContent != "" {
		acc.appendThinking(delta.ReasoningContent)
		blocksChan <- StreamContentBlock{
			Block: ContentBlock{
				Type:     "thinking",
				Thinking: acc.getThinking(),
			},
		}
	}

	for _, tc := range delta.ToolCalls {
		acc.appendToolCall(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
		if toolBlock := acc.getToolUseBlock(tc.Index); toolBlock != nil {
			blocksChan <- StreamContentBlock{
				Block: *toolBlock,
			}
		}
	}

	return hasStopReason
}

// openAIStreamAccumulator accumulates streaming chunks.
type openAIStreamAccumulator struct {
	content    string
	thinking   string
	stopReason StopReason
	toolCalls  map[int]*toolCallAccumulator
}

// toolCallAccumulator accumulates tool call arguments.
type toolCallAccumulator struct {
	ID    string
	Name  string
	Args  string
	Input map[string]any
}

func newOpenAIStreamAccumulator() *openAIStreamAccumulator {
	return &openAIStreamAccumulator{
		toolCalls: make(map[int]*toolCallAccumulator),
	}
}

func (acc *openAIStreamAccumulator) appendContent(text string) {
	acc.content += text
}

func (acc *openAIStreamAccumulator) getContent() string {
	return acc.content
}

func (acc *openAIStreamAccumulator) appendThinking(text string) {
	acc.thinking += text
}

func (acc *openAIStreamAccumulator) getThinking() string {
	return acc.thinking
}

func (acc *openAIStreamAccumulator) setStopReason(reason StopReason) {
	if acc.stopReason == "" {
		acc.stopReason = reason
	}
}

func (acc *openAIStreamAccumulator) appendToolCall(index int, id, name, args string) {
	tc, exists := acc.toolCalls[index]
	if !exists {
		tc = &toolCallAccumulator{}
		acc.toolCalls[index] = tc
	}

	if id != "" {
		tc.ID = id
	}
	if name != "" {
		tc.Name = name
	}
	if args != "" {
		tc.Args += args
		var input map[string]any
		if err := json.Unmarshal([]byte(tc.Args), &input); err == nil {
			tc.Input = input
		}
	}
}

func (acc *openAIStreamAccumulator) getToolUseBlock(index int) *ContentBlock {
	if tc, exists := acc.toolCalls[index]; exists && tc.ID != "" {
		return &ContentBlock{
			Type:      "tool_use",
			ToolID:    tc.ID,
			ToolName:  tc.Name,
			ToolInput: tc.Input,
		}
	}
	return nil
}

func (acc *openAIStreamAccumulator) finalize() []ContentBlock {
	var blocks []ContentBlock

	if acc.thinking != "" {
		blocks = append(blocks, ContentBlock{
			Type:     "thinking",
			Thinking: acc.thinking,
		})
	}

	if acc.content != "" {
		blocks = append(blocks, ContentBlock{
			Type: "text",
			Text: acc.content,
		})
	}

	for i := 0; i < len(acc.toolCalls); i++ {
		if tc, exists := acc.toolCalls[i]; exists && tc.ID != "" {
			blocks = append(blocks, ContentBlock{
				Type:      "tool_use",
				ToolID:    tc.ID,
				ToolName:  tc.Name,
				ToolInput: tc.Input,
			})
		}
	}

	return blocks
}
