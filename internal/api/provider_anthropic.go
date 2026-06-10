// Package api provides the Anthropic API client.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/ipy/jenny/internal/log"
)

// anthropicProvider implements the Provider interface using the Anthropic SDK.
type anthropicProvider struct {
	client       anthropic.Client
	model        string
	maxTokens    int
	retryConfig  RetryConfig
	isBackground bool
}

// newAnthropicProvider creates a new Anthropic provider.
func newAnthropicProvider(model string) (*anthropicProvider, error) {
	// Read model from environment if not provided
	if model == "" {
		model = os.Getenv("ANTHROPIC_MODEL")
	}
	if model == "" {
		model = defaultModel
	}

	// Build client options: beta headers + request timeout
	opts := []option.RequestOption{}

	// Default prompt-caching beta header
	opts = append(opts, option.WithHeader("anthropic-beta", string(anthropic.AnthropicBetaPromptCaching2024_07_31)))

	// Additional beta headers from ANTHROPIC_BETAS env var
	betas := os.Getenv("ANTHROPIC_BETAS")
	if betas != "" {
		for beta := range strings.SplitSeq(betas, ",") {
			beta = strings.TrimSpace(beta)
			if beta != "" {
				opts = append(opts, option.WithHeaderAdd("anthropic-beta", beta))
			}
		}
	}

	// Request timeout from API_TIMEOUT_MS env var
	timeout := ResolveTimeout(os.Getenv("API_TIMEOUT_MS"))
	opts = append(opts, option.WithRequestTimeout(timeout))

	// SDK's NewClient already reads ANTHROPIC_BASE_URL and ANTHROPIC_AUTH_TOKEN.
	client := anthropic.NewClient(opts...)

	return &anthropicProvider{
		client:      client,
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

// SendMessage sends a non-streaming message.
func (p *anthropicProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	// Wrap with retry
	return p.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return p.doSendMessage(ctx, messages, tools, toolResults, systemPrompt)
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
			var retryableErr *RetryableHTTPError
			if errors.As(err, &retryableErr) && retryableErr != nil {
				statusCode := retryableErr.StatusCode

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

				if retryableErr.IsPermanent || !isRetryable(statusCode, nil) {
					return nil, err
				}

				lastErr = err
			} else {
				if !isRetryable(0, err) {
					return nil, err
				}
				lastErr = err
			}
		} else if resp != nil && resp.Error != "" {
			return resp, nil
		} else {
			return resp, nil
		}

		if attempt < cfg.MaxRetries {
			var retryAfter *time.Duration
			if retryableErr, ok := lastErr.(*RetryableHTTPError); ok {
				retryAfter = retryableErr.RetryAfter
			}

			delay := computeBackoff(attempt, cfg, retryAfter)

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				// Continue
			}
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("max retries exhausted")
}

// doSendMessage performs the actual message sending.
func (p *anthropicProvider) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	log.Debug("Sending message", "model", p.model)
	log.Debug("System prompt", "prompt", systemPrompt)
	log.Debug("Number of tools", "count", len(tools))
	for _, t := range tools {
		log.Debug("Tool registered", "name", t.Name, "description", t.Description)
	}

	// Validate media before sending
	if err := ValidateMessagesMedia(messages); err != nil {
		return nil, err
	}

	// Universal normalization gateway
	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: true})

	// Convert messages to SDK format
	sdkMessages := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		contentBlocks := make([]anthropic.ContentBlockParamUnion, 0)

		if msg.Content != "" {
			contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{Text: msg.Content},
			})
		}

		for _, tu := range msg.ToolUse {
			contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
				OfToolUse: &anthropic.ToolUseBlockParam{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: tu.Input,
				},
			})
		}

		for _, tr := range msg.ToolResults {
			contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
				OfToolResult: &anthropic.ToolResultBlockParam{
					ToolUseID: tr.ToolUseID,
					Content: []anthropic.ToolResultBlockParamContentUnion{
						{OfText: &anthropic.TextBlockParam{Text: tr.Content}},
					},
					Type: "tool_result",
				},
			})
		}

		sdkMessages = append(sdkMessages, anthropic.MessageParam{
			Role:    anthropic.MessageParamRole(msg.Role),
			Content: contentBlocks,
		})
	}

	// Add standalone tool results as user messages
	for _, tr := range toolResults {
		sdkMessages = append(sdkMessages, anthropic.MessageParam{
			Role: "user",
			Content: []anthropic.ContentBlockParamUnion{
				{
					OfToolResult: &anthropic.ToolResultBlockParam{
						ToolUseID: tr.ToolUseID,
						Content: []anthropic.ToolResultBlockParamContentUnion{
							{OfText: &anthropic.TextBlockParam{Text: tr.Content}},
						},
						Type: "tool_result",
					},
				},
			},
		})
	}

	// Convert tools to SDK format
	sdkTools := make([]anthropic.ToolUnionParam, 0, len(tools))
	for i, t := range tools {
		sdkTools = append(sdkTools, toolToSDK(t, i == len(tools)-1))
	}

	// Build request
	maxTokens := p.maxTokens
	if maxTokens == 0 {
		maxTokens = 64000
	}
	body := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: int64(maxTokens),
	}
	body.Messages = sdkMessages
	if systemPrompt != "" {
		body.System = []anthropic.TextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}}
	}
	if len(sdkTools) > 0 {
		body.Tools = sdkTools
	}

	// Send request
	resp, err := p.client.Messages.New(ctx, body)
	if err != nil {
		wrappedErr := wrapSDKError(err)
		return nil, wrappedErr
	}

	// Convert response
	response := &Response{
		Model:      string(resp.Model),
		StopReason: StopReason(string(resp.StopReason)),
	}

	if resp.Usage.InputTokens > 0 {
		response.Usage.InputTokens = int(resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens > 0 {
		response.Usage.OutputTokens = int(resp.Usage.OutputTokens)
	}
	if resp.Usage.CacheReadInputTokens > 0 {
		response.Usage.CacheReadInputTokens = int(resp.Usage.CacheReadInputTokens)
	}
	if resp.Usage.CacheCreationInputTokens > 0 {
		response.Usage.CacheCreationInputTokens = int(resp.Usage.CacheCreationInputTokens)
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			response.Content = append(response.Content, ContentBlock{
				Type: "text",
				Text: block.Text,
			})
		case "thinking":
			response.Content = append(response.Content, ContentBlock{
				Type:      "thinking",
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "tool_use":
			var input map[string]any
			if err := json.Unmarshal(block.Input, &input); err != nil {
				input = make(map[string]any)
			}
			response.Content = append(response.Content, ContentBlock{
				Type:      "tool_use",
				ToolID:    block.ID,
				ToolName:  block.Name,
				ToolInput: input,
			})
		case "web_search_tool_result":
			webSearchData := &WebSearchResultData{
				ToolUseID: block.ToolUseID,
			}
			errResult := block.Content.AsResponseWebSearchToolResultError()
			if errResult.ErrorCode != "" {
				webSearchData.IsError = true
				webSearchData.ErrorCode = string(errResult.ErrorCode)
			}
			response.Content = append(response.Content, ContentBlock{
				Type:            "web_search_tool_result",
				WebSearchResult: webSearchData,
			})
		}
	}

	return response, nil
}

// SendMessageStream sends a streaming message.
func (p *anthropicProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (<-chan ContentBlock, *StreamResult) {
	blocksChan := make(chan ContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		// Validate media before sending
		if err := ValidateMessagesMedia(messages); err != nil {
			result.Error = err.Error()
			return
		}

		// Universal normalization gateway
		messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: true})

		// Convert messages to SDK format
		sdkMessages := make([]anthropic.MessageParam, 0, len(messages))
		for _, msg := range messages {
			contentBlocks := make([]anthropic.ContentBlockParamUnion, 0)

			if msg.Content != "" {
				contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
					OfText: &anthropic.TextBlockParam{Text: msg.Content},
				})
			}

			for _, tu := range msg.ToolUse {
				contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    tu.ID,
						Name:  tu.Name,
						Input: tu.Input,
					},
				})
			}

			for _, tr := range msg.ToolResults {
				contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
					OfToolResult: &anthropic.ToolResultBlockParam{
						ToolUseID: tr.ToolUseID,
						Content: []anthropic.ToolResultBlockParamContentUnion{
							{OfText: &anthropic.TextBlockParam{Text: tr.Content}},
						},
						Type: "tool_result",
					},
				})
			}

			sdkMessages = append(sdkMessages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRole(msg.Role),
				Content: contentBlocks,
			})
		}

		// Add standalone tool results as user messages
		for _, tr := range toolResults {
			sdkMessages = append(sdkMessages, anthropic.MessageParam{
				Role: "user",
				Content: []anthropic.ContentBlockParamUnion{
					{
						OfToolResult: &anthropic.ToolResultBlockParam{
							ToolUseID: tr.ToolUseID,
							Content: []anthropic.ToolResultBlockParamContentUnion{
								{OfText: &anthropic.TextBlockParam{Text: tr.Content}},
							},
							Type: "tool_result",
						},
					},
				},
			})
		}

		// Convert tools to SDK format
		sdkTools := make([]anthropic.ToolUnionParam, 0, len(tools))
		for i, t := range tools {
			sdkTools = append(sdkTools, toolToSDK(t, i == len(tools)-1))
		}

		// Build request
		maxTokens := p.maxTokens
		if maxTokens == 0 {
			maxTokens = 64000
		}
		body := anthropic.MessageNewParams{
			Model:     anthropic.Model(p.model),
			MaxTokens: int64(maxTokens),
		}
		body.Messages = sdkMessages
		if systemPrompt != "" {
			body.System = []anthropic.TextBlockParam{{
				Text:         systemPrompt,
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			}}
		}
		if len(sdkTools) > 0 {
			body.Tools = sdkTools
		}

		log.Debug("Starting streaming request", "model", p.model)

		// Create stream
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		stream := p.client.Messages.NewStreaming(streamCtx, body)

		// Check for pre-stream error
		if stream.Err() != nil {
			preStreamErr := stream.Err()
			log.Warn("Stream pre-error detected, falling back", "error", preStreamErr)
			if isPromptTooLongError(preStreamErr) {
				result.ContextRejected = true
				result.MaxTokensErr = categorizeMaxTokensError(p.model, 0, true)
			}
			result.Error = preStreamErr.Error()
			return
		}

		acc := newStreamAccumulator()
		hasMessageStart := false
		hasMessageStop := false
		var pendingBlocks []ContentBlock

		idleTimer := time.NewTimer(DefaultIdleTimeout)

	streamLoop:
		for {
			streamReady := stream.Next()

			select {
			case <-idleTimer.C:
				log.Warn("Idle timeout reached")
				cancel()
				result.Error = "idle timeout"
				return
			default:
				if !streamReady {
					break streamLoop
				}
				if !idleTimer.Stop() {
					<-idleTimer.C
				}
				idleTimer.Reset(DefaultIdleTimeout)
			}

			event := stream.Current()

			variant := event.AsAny()
			switch e := variant.(type) {
			case anthropic.MessageStartEvent:
				hasMessageStart = true
				acc.setModel(string(e.Message.Model))
				if e.Message.Usage.InputTokens > 0 {
					acc.setUsage(Usage{
						InputTokens:              int(e.Message.Usage.InputTokens),
						CacheReadInputTokens:     int(e.Message.Usage.CacheReadInputTokens),
						CacheCreationInputTokens: int(e.Message.Usage.CacheCreationInputTokens),
					})
				}
				log.Debug("Stream: message_start")

			case anthropic.ContentBlockStartEvent:
				index := int(e.Index)
				log.Debug("Stream: content_block_start", "index", index, "type", e.ContentBlock.Type)
				switch e.ContentBlock.Type {
				case "text":
					acc.setBlockType(index, "text")
				case "tool_use":
					acc.setBlockType(index, "tool_use")
					acc.blocks[index].ToolID = e.ContentBlock.ID
					acc.blocks[index].ToolName = e.ContentBlock.Name
				default:
					acc.setBlockType(index, e.ContentBlock.Type)
				}

			case anthropic.ContentBlockDeltaEvent:
				index := int(e.Index)
				delta := e.Delta
				if delta.Text != "" && acc.blocks[index].Type == "text" {
					acc.appendText(index, delta.Text)
				}
				if delta.Thinking != "" {
					acc.appendThinking(index, delta.Thinking)
				}
				if delta.Signature != "" {
					acc.appendSignature(index, delta.Signature)
				}
				if delta.PartialJSON != "" {
					acc.appendToolInputJSON(index, delta.PartialJSON)
					acc.finalizeToolInput(index)
				}
				log.Debug("Stream: content_block_delta", "index", index, "text", delta.Text)

			case anthropic.ContentBlockStopEvent:
				index := int(e.Index)
				log.Debug("Stream: content_block_stop", "index", index)
				acc.finalizeToolInput(index)
				acc.ensureBlock(index)
				pendingBlocks = append(pendingBlocks, acc.blocks[index])

			case anthropic.MessageDeltaEvent:
				if e.Usage.InputTokens > 0 || e.Usage.OutputTokens > 0 {
					acc.setUsage(Usage{
						InputTokens:              int(e.Usage.InputTokens),
						OutputTokens:             int(e.Usage.OutputTokens),
						CacheReadInputTokens:     int(e.Usage.CacheReadInputTokens),
						CacheCreationInputTokens: int(e.Usage.CacheCreationInputTokens),
					})
				}
				if e.Delta.StopReason != "" {
					acc.setStopReason(StopReason(e.Delta.StopReason))
				}
				log.Debug("Stream: message_delta")

			case anthropic.MessageStopEvent:
				hasMessageStop = true
				result.StreamComplete = true
				log.Debug("Stream: message_stop")
			}
		}

		// Check for stream errors
		if stream.Err() != nil {
			log.Warn("Stream error", "error", stream.Err())
			result.Error = stream.Err().Error()
			if isPromptTooLongError(stream.Err()) {
				result.ContextRejected = true
			}
		}

		// Check if we need fallback
		shouldFallback := !hasMessageStart || !hasMessageStop || result.Error != ""
		if shouldFallback {
			log.Warn("Stream incomplete, triggering fallback", "hasMessageStart", hasMessageStart, "hasMessageStop", hasMessageStop, "error", result.Error)
			// Fallback is handled by the caller (Client.SendMessageStream)
		}

		// Send buffered blocks to channel
		for _, block := range pendingBlocks {
			blocksChan <- block
		}
		result.Blocks = acc.getBlocks()
		result.StopReason = acc.stopReason
		result.Usage = acc.usage
		result.Model = acc.getModel()

		// Detect and categorize max_tokens scenarios
		if result.StopReason == StopReasonMaxTokens && result.Error == "" {
			result.MaxTokensErr = categorizeMaxTokensError(result.Model, result.Usage.OutputTokens, result.ContextRejected)
		} else if result.ContextRejected && result.Error != "" {
			result.MaxTokensErr = categorizeMaxTokensError(result.Model, 0, true)
		}
	}()

	return blocksChan, result
}

// wrapSDKError wraps SDK errors to extract HTTP status code for retry logic.
func wrapSDKError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr *anthropic.Error
	if errors.As(err, &apiErr); apiErr != nil {
		var retryAfter *time.Duration
		if apiErr.Response != nil {
			if retryAfterStr := apiErr.Response.Header.Get("Retry-After"); retryAfterStr != "" {
				if seconds, parseErr := strconv.ParseFloat(retryAfterStr, 64); parseErr == nil {
					ms := int64(seconds * 1000)
					d := time.Duration(ms) * time.Millisecond
					retryAfter = &d
				}
			}
		}

		isPermanent := apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 &&
			apiErr.StatusCode != 429 && apiErr.StatusCode != 408 && apiErr.StatusCode != 409
		return &RetryableHTTPError{
			StatusCode:  apiErr.StatusCode,
			Message:     err.Error(),
			IsPermanent: isPermanent,
			RetryAfter:  retryAfter,
		}
	}

	return err
}

// toolToSDK converts a ToolParam to an SDK ToolUnionParam.
func toolToSDK(t ToolParam, isLast bool) anthropic.ToolUnionParam {
	props := t.InputSchema.Properties
	if props == nil {
		props = make(map[string]any)
	}
	required := t.InputSchema.Required
	if required == nil {
		required = []string{}
	}

	inputSchema := anthropic.ToolInputSchemaParam{
		Type:       constant.Object("object"),
		Properties: props,
		Required:   required,
	}

	if len(t.InputSchema.ExtraFields) > 0 {
		inputSchema.ExtraFields = t.InputSchema.ExtraFields
	}

	tool := &anthropic.ToolParam{
		Name:        t.Name,
		Description: anthropic.String(t.Description),
		InputSchema: inputSchema,
	}
	if isLast {
		tool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return anthropic.ToolUnionParam{OfTool: tool}
}

// modelMaxOutputTokens returns the max output tokens for a given model.
func modelMaxOutputTokens(model string) int {
	switch model {
	case "deepseek-v4-flash":
		return 8192
	case "deepseek-v4":
		return 8192
	default:
		return 20000
	}
}

// categorizeMaxTokensError creates a MaxTokensError from streaming results.
func categorizeMaxTokensError(model string, outputTokens int, contextRejected bool) *MaxTokensError {
	maxOutputTokens := modelMaxOutputTokens(model)

	if contextRejected {
		return &MaxTokensError{
			Category:        CategoryContextExhausted,
			Model:           model,
			OutputTokens:    outputTokens,
			MaxOutputTokens: 0,
		}
	}

	if outputTokens >= maxOutputTokens {
		return &MaxTokensError{
			Category:        CategoryOutputCapHit,
			Model:           model,
			OutputTokens:    outputTokens,
			MaxOutputTokens: maxOutputTokens,
		}
	}

	return &MaxTokensError{
		Category:        CategoryOutputCapHit,
		Model:           model,
		OutputTokens:    outputTokens,
		MaxOutputTokens: maxOutputTokens,
	}
}

// isPromptTooLongError checks if the given error indicates a context exhaustion.
func isPromptTooLongError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "prompt_too_long") || strings.Contains(errMsg, "context window exceeds limit") {
			return true
		}
	}

	var retryErr *RetryableHTTPError
	if errors.As(err, &retryErr) && retryErr.StatusCode == 400 {
		errMsg := strings.ToLower(retryErr.Message)
		if strings.Contains(errMsg, "prompt_too_long") || strings.Contains(errMsg, "context window exceeds limit") {
			return true
		}
	}

	return false
}
