// Package api provides the Anthropic API client.
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

// anthropicProvider implements the Provider interface using a lightweight HTTP client.
type anthropicProvider struct {
	client       *HTTPClient
	model        string
	maxTokens    int
	retryConfig  RetryConfig
	isBackground bool
}

// newAnthropicProvider creates a new Anthropic provider.
func newAnthropicProvider(model string) (*anthropicProvider, error) {
	if model == "" {
		model = os.Getenv("ANTHROPIC_MODEL")
	}
	if model == "" {
		model = defaultModel
	}

	timeout := ResolveTimeout(os.Getenv("API_TIMEOUT_MS"))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	return &anthropicProvider{
		client:      NewHTTPClient(timeout),
		model:       model,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// Kind returns the provider kind.
func (p *anthropicProvider) Kind() ProviderKind {
	return ProviderAnthropic
}

// SetModel sets the model.
func (p *anthropicProvider) SetModel(model string) {
	p.model = model
}

// GetModel returns the model.
func (p *anthropicProvider) GetModel() string {
	return p.model
}

// SetBackground sets whether this is a background classifier call.
func (p *anthropicProvider) SetBackground(isBackground bool) {
	p.isBackground = isBackground
}

// SetMaxTokensOverride sets the max_tokens override.
func (p *anthropicProvider) SetMaxTokensOverride(maxTokens int) {
	p.maxTokens = maxTokens
}

// SetRetryConfig sets the retry configuration.
func (p *anthropicProvider) SetRetryConfig(cfg RetryConfig) {
	p.retryConfig = cfg
}

// SendMessage sends a non-streaming message.
func (p *anthropicProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt []string, systemPromptSuffix string) (*Response, error) {
	return p.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return p.doSendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
	}, p.isBackground)
}

// sendWithRetry executes a function with retry logic.
func (p *anthropicProvider) sendWithRetry(ctx context.Context, fn func(context.Context) (*Response, error), isBackground bool) (*Response, error) {
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
func (p *anthropicProvider) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt []string, systemPromptSuffix string) (*Response, error) {
	log.Debug("Anthropic provider sending message", "model", p.model)

	if err := ValidateMessagesMedia(messages); err != nil {
		return nil, err
	}

	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: true})

	sdkMessages := p.buildMessages(messages, toolResults)
	sdkTools := p.buildTools(tools)

	maxTokens := p.maxTokens
	if maxTokens == 0 {
		maxTokens = 32000
	}

	reqBody := AnthropicRequest{
		Model:     p.model,
		Messages:  sdkMessages,
		MaxTokens: maxTokens,
		Tools:     sdkTools,
	}

	if len(systemPrompt) > 0 || systemPromptSuffix != "" {
		if len(systemPrompt) > 0 {
			reqBody.System = append(reqBody.System, AnthropicContentBlock{
				Type:         BlockTypeText,
				Text:         strings.Join(systemPrompt, "\n\n"),
				CacheControl: &AnthropicCacheControl{Type: "ephemeral"},
			})
		}
		if systemPromptSuffix != "" {
			reqBody.System = append(reqBody.System, AnthropicContentBlock{
				Type: BlockTypeText,
				Text: systemPromptSuffix,
			})
		}
	}

	url := fmt.Sprintf("%s/v1/messages", os.Getenv("ANTHROPIC_BASE_URL"))
	headers := p.buildHeaders()

	var anthropicResp AnthropicResponse
	if err := p.client.Request(ctx, "POST", url, headers, reqBody, &anthropicResp); err != nil {
		return nil, err
	}

	return p.parseResponse(&anthropicResp)
}

// buildMessages converts api.Message slices to Anthropic format.
// The last content block of the last non-empty message is marked with
// cache_control: ephemeral to enable rolling prefix caching. On turn N+1
// the prefix up to the previous turn's marker matches the cache from turn N.
func (p *anthropicProvider) buildMessages(messages []Message, toolResults []ToolResult) []AnthropicMessage {
	sdkMessages := make([]AnthropicMessage, 0, len(messages)+len(toolResults))
	for _, msg := range messages {
		contentBlocks := make([]AnthropicContentBlock, 0)

		if msg.Thinking != "" {
			contentBlocks = append(contentBlocks, AnthropicContentBlock{
				Type:      BlockTypeThinking,
				Thinking:  msg.Thinking,
				Signature: msg.Signature,
			})
		}

		if msg.Content != "" {
			contentBlocks = append(contentBlocks, AnthropicContentBlock{
				Type: BlockTypeText,
				Text: msg.Content,
			})
		}

		for _, tu := range msg.ToolUse {
			contentBlocks = append(contentBlocks, AnthropicContentBlock{
				Type:  BlockTypeToolUse,
				ID:    tu.ID,
				Name:  tu.Name,
				Input: tu.Input,
			})
		}

		for _, tr := range msg.ToolResults {
			block := AnthropicContentBlock{
				Type:      BlockTypeToolResult,
				ToolUseID: tr.ToolUseID,
			}
			block.SetContent(tr.Content)
			if tr.IsError {
				block.IsError = true
			}
			contentBlocks = append(contentBlocks, block)
		}

		sdkMessages = append(sdkMessages, AnthropicMessage{
			Role:    msg.Role,
			Content: contentBlocks,
		})
	}

	for _, tr := range toolResults {
		block := AnthropicContentBlock{
			Type:      BlockTypeToolResult,
			ToolUseID: tr.ToolUseID,
		}
		block.SetContent(tr.Content)
		if tr.IsError {
			block.IsError = true
		}
		sdkMessages = append(sdkMessages, AnthropicMessage{
			Role:    RoleUser,
			Content: []AnthropicContentBlock{block},
		})
	}

	// Rolling cache marker: mark the last content block of the last non-empty
	// message so the entire message history prefix is cached between turns.
	markLastMessageForCaching(sdkMessages)

	return sdkMessages
}

// markLastMessageForCaching adds cache_control: ephemeral to the last content
// block of the last message that has content. This enables Anthropic's prefix
// caching to cover the entire conversation history, with only newly appended
// messages incurring fresh processing on each turn.
func markLastMessageForCaching(messages []AnthropicMessage) {
	for i := len(messages) - 1; i >= 0; i-- {
		if len(messages[i].Content) > 0 {
			last := len(messages[i].Content) - 1
			messages[i].Content[last].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
			return
		}
	}
}

// buildTools converts api.ToolParam slices to Anthropic format.
func (p *anthropicProvider) buildTools(tools []ToolParam) []AnthropicTool {
	sdkTools := make([]AnthropicTool, 0, len(tools))
	for i, t := range tools {
		sdkTools = append(sdkTools, toolToSDK(t, i == len(tools)-1))
	}
	return sdkTools
}

// buildHeaders builds common Anthropic headers.
func (p *anthropicProvider) buildHeaders() http.Header {
	headers := http.Header{}
	token := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	headers.Set("x-api-key", token)
	headers.Set("Authorization", "Bearer "+token)
	headers.Set("anthropic-version", "2023-06-01")
	headers.Add("anthropic-beta", "prompt-caching-2024-07-31")

	betas := os.Getenv("ANTHROPIC_BETAS")
	if betas != "" {
		for _, beta := range strings.Split(betas, ",") {
			beta = strings.TrimSpace(beta)
			if beta != "" {
				headers.Add("anthropic-beta", beta)
			}
		}
	}

	return headers
}

// parseResponse converts an Anthropic response to api.Response.
func (p *anthropicProvider) parseResponse(resp *AnthropicResponse) (*Response, error) {
	response := &Response{
		Model:      resp.Model,
		StopReason: StopReason(resp.StopReason),
	}

	for _, block := range resp.Content {
		switch block.Type {
		case BlockTypeText:
			response.Content = append(response.Content, ContentBlock{
				Type: BlockTypeText,
				Text: block.Text,
			})
		case BlockTypeThinking:
			response.Content = append(response.Content, ContentBlock{
				Type:      BlockTypeThinking,
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case BlockTypeToolUse:
			var input map[string]any
			if inputVal, ok := block.Input.(map[string]any); ok {
				input = inputVal
			} else {
				input = make(map[string]any)
			}
			response.Content = append(response.Content, ContentBlock{
				Type:      BlockTypeToolUse,
				ToolID:    block.ID,
				ToolName:  block.Name,
				ToolInput: input,
			})
		}
	}

	response.Usage.InputTokens = resp.Usage.InputTokens
	response.Usage.OutputTokens = resp.Usage.OutputTokens
	response.Usage.CacheReadInputTokens = resp.Usage.CacheReadInputTokens
	response.Usage.CacheCreationInputTokens = resp.Usage.CacheCreationInputTokens

	return response, nil
}

// isPromptTooLongAnthropic returns true if the error indicates prompt too long.
func isPromptTooLongAnthropic(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "prompt_too_long") || strings.Contains(msg, "context window exceeds limit")
}

// SendMessageStream sends a streaming message.
func (p *anthropicProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		log.Debug("Anthropic provider streaming message", "model", p.model)

		if err := ValidateMessagesMedia(messages); err != nil {
			result.Error = err.Error()
			return
		}

		messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: true})

		sdkMessages := p.buildMessages(messages, toolResults)
		sdkTools := p.buildTools(tools)

		maxTokens := p.maxTokens
		if maxTokens == 0 {
			maxTokens = 32000
		}

		reqBody := AnthropicRequest{
			Model:     p.model,
			Messages:  sdkMessages,
			MaxTokens: maxTokens,
			Tools:     sdkTools,
			Stream:    true,
		}

		if len(systemPrompt) > 0 || systemPromptSuffix != "" {
			if len(systemPrompt) > 0 {
				reqBody.System = append(reqBody.System, AnthropicContentBlock{
					Type:         BlockTypeText,
					Text:         strings.Join(systemPrompt, "\n\n"),
					CacheControl: &AnthropicCacheControl{Type: "ephemeral"},
				})
			}
			if systemPromptSuffix != "" {
				reqBody.System = append(reqBody.System, AnthropicContentBlock{
					Type: BlockTypeText,
					Text: systemPromptSuffix,
				})
			}
		}

		url := fmt.Sprintf("%s/v1/messages", os.Getenv("ANTHROPIC_BASE_URL"))
		headers := p.buildHeaders()

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
			if isPromptTooLongAnthropic(err) {
				result.ContextRejected = true
				result.MaxTokensErr = categorizeMaxTokensError(p.model, result.Usage.OutputTokens, true)
			}
			return
		}
		defer body.Close()

		acc := newStreamAccumulator()
		hasMessageStart := false
		hasMessageStop := false
		scanner := NewSSEScanner(body)

		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		watchdogCtx, cancelWatchdog := context.WithCancel(ctx)
		defer cancelWatchdog()

		go func() {
			select {
			case <-idleTimer.C:
				log.Warn("Anthropic: Idle timeout reached")
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

			var event AnthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				log.Error("Anthropic: failed to unmarshal event", "error", err, "data", data)
				continue
			}

			blocksChan <- StreamContentBlock{
				Type:     "stream_event",
				RawEvent: event,
			}

			switch event.Type {
			case EventMessageStart:
				hasMessageStart = true
				if event.Message != nil {
					acc.setModel(event.Message.Model)
					acc.setUsage(Usage{
						InputTokens:              event.Message.Usage.InputTokens,
						CacheReadInputTokens:     event.Message.Usage.CacheReadInputTokens,
						CacheCreationInputTokens: event.Message.Usage.CacheCreationInputTokens,
					})
				}

			case EventContentBlockStart:
				index := event.Index
				if event.ContentBlock != nil {
					acc.setBlockType(index, event.ContentBlock.Type)
					if event.ContentBlock.Type == BlockTypeToolUse {
						acc.blocks[index].ToolID = event.ContentBlock.ID
						acc.blocks[index].ToolName = event.ContentBlock.Name
					}
					if event.ContentBlock.Type == BlockTypeThinking {
						acc.setBlockType(index, BlockTypeThinking)
						acc.appendThinking(index, event.ContentBlock.Thinking)
					}
				}

			case EventContentBlockDelta:
				index := event.Index
				if event.Delta != nil {
					if event.Delta.Text != "" {
						acc.appendText(index, event.Delta.Text)
					}
					if event.Delta.Thinking != "" {
						acc.setBlockType(index, BlockTypeThinking)
						acc.appendThinking(index, event.Delta.Thinking)
					}
					if event.Delta.Signature != "" {
						acc.appendSignature(index, event.Delta.Signature)
					}
					if event.Delta.PartialJSON != "" {
						acc.appendToolInputJSON(index, event.Delta.PartialJSON)
					}
				}

			case EventContentBlockStop:
				index := event.Index
				acc.finalizeToolInput(index)
				acc.ensureBlock(index)
				blocksChan <- StreamContentBlock{Block: acc.blocks[index]}

			case EventMessageDelta:
				if event.Usage != nil {
					acc.setUsage(Usage{
						InputTokens:              event.Usage.InputTokens,
						OutputTokens:             event.Usage.OutputTokens,
						CacheReadInputTokens:     event.Usage.CacheReadInputTokens,
						CacheCreationInputTokens: event.Usage.CacheCreationInputTokens,
					})
				}
				if event.Delta != nil && event.Delta.StopReason != "" {
					acc.setStopReason(StopReason(event.Delta.StopReason))
				}

			case EventMessageStop:
				hasMessageStop = true
			}
		}

		if scanner.Err() != nil && result.Error == "" {
			result.Error = scanner.Err().Error()
		}

		if isPromptTooLongAnthropic(errors.New(result.Error)) {
			result.ContextRejected = true
		}

		shouldFallback := !hasMessageStart || !hasMessageStop || result.Error != ""
		if shouldFallback {
			log.Warn("Anthropic: stream incomplete, triggering fallback", "hasMessageStart", hasMessageStart, "hasMessageStop", hasMessageStop, "error", result.Error)
		}

		result.StreamComplete = hasMessageStop
		result.Blocks = acc.getBlocks()
		result.StopReason = acc.stopReason
		result.Usage = acc.usage
		result.Model = acc.getModel()

		if (result.StopReason == StopReasonMaxTokens || result.ContextRejected) && result.MaxTokensErr == nil {
			result.MaxTokensErr = categorizeMaxTokensError(result.Model, result.Usage.OutputTokens, result.ContextRejected)
		}
	}()

	return blocksChan, result
}

// toolToSDK converts a ToolParam to an Anthropic tool definition.
func toolToSDK(t ToolParam, isLast bool) AnthropicTool {
	inputSchema := AnthropicInputSchema{
		Type:        "object",
		Properties:  t.InputSchema.Properties,
		Required:    t.InputSchema.Required,
		ExtraFields: t.InputSchema.ExtraFields,
	}

	tool := AnthropicTool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: inputSchema,
	}
	if isLast {
		tool.CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
	}
	return tool
}

// modelMaxOutputTokens returns the max output tokens for a given model.
func modelMaxOutputTokens(model string) int {
	switch model {
	case "deepseek-v4-flash", "deepseek-v4-pro":
		return 8192
	default:
		return 20000
	}
}

// categorizeMaxTokensError creates a MaxTokensError from streaming results.
func categorizeMaxTokensError(model string, outputTokens int, contextRejected bool) *MaxTokensError {
	maxOutputTokens := modelMaxOutputTokens(model)
	if contextRejected {
		return &MaxTokensError{Category: CategoryContextExhausted, Model: model, OutputTokens: outputTokens}
	}
	return &MaxTokensError{Category: CategoryOutputCapHit, Model: model, OutputTokens: outputTokens, MaxOutputTokens: maxOutputTokens}
}
