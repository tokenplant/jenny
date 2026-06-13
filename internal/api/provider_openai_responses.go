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
// The Responses API uses a flat list of input items. function_call and
// function_call_output are top-level items, NOT nested inside messages.
func (p *openAIResponsesProvider) buildInput(messages []Message, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) []any {
	var input []any

	if systemPrompt != "" {
		input = append(input, map[string]any{
			"type": "message",
			"role": RoleSystem,
			"content": []map[string]any{
				{"type": "input_text", "text": systemPrompt},
			},
		})
	}
	if systemPromptSuffix != "" {
		input = append(input, map[string]any{
			"type": "message",
			"role": RoleSystem,
			"content": []map[string]any{
				{"type": "input_text", "text": systemPromptSuffix},
			},
		})
	}

	for _, msg := range messages {
		switch msg.Role {
		case RoleUser:
			if msg.Content != "" {
				input = append(input, map[string]any{
					"type": "message",
					"role": RoleUser,
					"content": []map[string]any{
						{"type": "input_text", "text": msg.Content},
					},
				})
			}
			// Emit function_call_output as top-level items (Responses API format)
			for _, tr := range msg.ToolResults {
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": tr.ToolUseID,
					"output":  tr.Content,
				})
			}

		case RoleAssistant:
			// Emit reasoning as top-level item
			if msg.Thinking != "" {
				input = append(input, map[string]any{
					"type": "reasoning",
					"summary": []map[string]any{
						{"type": "summary_text", "text": msg.Thinking},
					},
				})
			}
			// Emit assistant text as a message
			if msg.Content != "" {
				input = append(input, map[string]any{
					"type": "message",
					"role": RoleAssistant,
					"content": []map[string]any{
						{"type": "output_text", "text": msg.Content},
					},
				})
			}
			// Emit function_call as top-level items (Responses API format)
			for _, tu := range msg.ToolUse {
				inputJSON, _ := json.Marshal(tu.Input)
				input = append(input, map[string]any{
					"type":      "function_call",
					"id":        tu.ID,
					"name":      tu.Name,
					"arguments": string(inputJSON),
					"call_id":   tu.ID,
				})
			}
		}
	}

	// Append standalone tool results as top-level function_call_output items
	for _, tr := range toolResults {
		input = append(input, map[string]any{
			"type":    "function_call_output",
			"call_id": tr.ToolUseID,
			"output":  tr.Content,
		})
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
			var summaryText string
			for _, s := range item.Summary {
				if s.Text != "" {
					summaryText += s.Text
				}
			}
			response.Content = append(response.Content, ContentBlock{
				Type:     BlockTypeThinking,
				Thinking: summaryText,
			})

		case "message":
			for _, block := range item.Content {
				switch block.Type {
				case "output_text":
					if block.Text != "" {
						response.Content = append(response.Content, ContentBlock{
							Type: BlockTypeText,
							Text: block.Text,
						})
					}
				}
			}

		case "function_call":
			hasToolCalls = true
			var input map[string]any
			if item.Arguments != "" {
				json.Unmarshal([]byte(item.Arguments), &input)
			}
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			response.Content = append(response.Content, ContentBlock{
				Type:      BlockTypeToolUse,
				ToolID:    callID,
				ToolName:  item.Name,
				ToolInput: input,
			})
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

// SendMessageStream sends a streaming message using the OpenAI Responses API.
// Emits Anthropic-compatible stream events for consistency with Claude Code output format.
func (p *openAIResponsesProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		log.Debug("OpenAI Responses API streaming message", "model", p.model)

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
			Stream:          true,
		}

		if p.effort != "" {
			reqBody.ReasoningConfig = &OpenAIResponsesReasoningConfig{
				Effort: p.effort,
			}
		}

		url := fmt.Sprintf("%s/v1/responses", os.Getenv("OPENAI_BASE_URL"))
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
			if isPromptTooLongOpenAIResponses(err) {
				result.ContextRejected = true
				result.MaxTokensErr = &MaxTokensError{
					Category: CategoryContextExhausted,
					Model:    p.model,
				}
			}
			return
		}
		defer body.Close()

		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		watchdogCtx, cancelWatchdog := context.WithCancel(ctx)
		defer cancelWatchdog()

		go func() {
			select {
			case <-idleTimer.C:
				log.Warn("OpenAI Responses: Idle timeout reached")
				result.Error = "idle timeout"
				body.Close()
			case <-watchdogCtx.Done():
			}
		}()

		acc := newResponsesStreamAccumulator()
		hasCompleted := false
		scanner := NewSSEScanner(body)

		// Emit message_start
		messageID := fmt.Sprintf("msg_openai_resp_%s", GenerateShortID())
		blocksChan <- StreamContentBlock{
			Type: "stream_event",
			RawEvent: AnthropicStreamEvent{
				Type: EventMessageStart,
				Message: &AnthropicResponse{
					ID:    messageID,
					Type:  "message",
					Role:  RoleAssistant,
					Model: p.model,
					Usage: AnthropicUsage{},
				},
			},
		}

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

			var event OpenAIResponsesStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				log.Error("OpenAI Responses: failed to unmarshal event", "error", err, "data", data)
				continue
			}

			hasCompleted = p.processResponsesStreamEvent(&event, acc, blocksChan, result) || hasCompleted
		}

		if scanner.Err() != nil && result.Error == "" {
			result.Error = scanner.Err().Error()
		}

		if isPromptTooLongOpenAIResponses(errors.New(result.Error)) {
			result.ContextRejected = true
		}

		if !hasCompleted && result.Error == "" {
			result.Error = "stream incomplete: no completion event"
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

		// Emit message_delta + message_stop
		stopReasonStr := string(acc.stopReason)
		blocksChan <- StreamContentBlock{
			Type: "stream_event",
			RawEvent: AnthropicStreamEvent{
				Type: EventMessageDelta,
				Delta: &AnthropicStreamDelta{
					StopReason: stopReasonStr,
				},
				Usage: &AnthropicUsage{
					OutputTokens: result.Usage.OutputTokens,
				},
			},
		}
		blocksChan <- StreamContentBlock{
			Type: "stream_event",
			RawEvent: AnthropicStreamEvent{
				Type: EventMessageStop,
			},
		}

		result.StreamComplete = hasCompleted
		result.Blocks = acc.finalize()
		result.StopReason = acc.stopReason
		result.Model = p.model
	}()

	return blocksChan, result
}

// processResponsesStreamEvent processes a single OpenAI Responses API stream event
// and emits Anthropic-compatible stream events.
func (p *openAIResponsesProvider) processResponsesStreamEvent(event *OpenAIResponsesStreamEvent, acc *responsesStreamAccumulator, blocksChan chan<- StreamContentBlock, result *StreamResult) bool {
	switch event.Type {
	case "response.created", "response.in_progress":
		// Track model from response object
		if event.Response != nil && event.Response.Model != "" {
			result.Model = event.Response.Model
		}

	case "response.output_item.added":
		if event.Item != nil {
			id := event.Item.CallID
			if id == "" {
				id = event.Item.ID
			}
			acc.startOutputItem(event.OutputIndex, event.Item.Type, id)
			if event.Item.Type == "function_call" && event.Item.Name != "" {
				item := acc.getOutputItem(event.OutputIndex)
				if item != nil {
					item.toolName = event.Item.Name
				}
			}
		}

	case "response.reasoning_summary_text.delta":
		if event.Delta != "" {
			if !acc.thinkingStarted {
				acc.thinkingStarted = true
				acc.thinkingIndex = acc.nextBlockIndex()
				blocksChan <- StreamContentBlock{
					Type: "stream_event",
					RawEvent: AnthropicStreamEvent{
						Type:  EventContentBlockStart,
						Index: acc.thinkingIndex,
						ContentBlock: &AnthropicContentBlock{
							Type: BlockTypeThinking,
						},
					},
				}
			}
			acc.thinking += event.Delta
			blocksChan <- StreamContentBlock{
				Type: "stream_event",
				RawEvent: AnthropicStreamEvent{
					Type:  EventContentBlockDelta,
					Index: acc.thinkingIndex,
					Delta: &AnthropicStreamDelta{
						Type:     DeltaTypeThinking,
						Thinking: event.Delta,
					},
				},
			}
		}

	case "response.output_text.delta":
		if event.Delta != "" {
			// Close thinking if still open
			if acc.thinkingStarted && !acc.thinkingStopped {
				acc.thinkingStopped = true
				blocksChan <- StreamContentBlock{
					Type: "stream_event",
					RawEvent: AnthropicStreamEvent{
						Type:  EventContentBlockStop,
						Index: acc.thinkingIndex,
					},
				}
				blocksChan <- StreamContentBlock{Block: ContentBlock{
					Type: BlockTypeThinking, Thinking: acc.thinking,
				}}
			}
			if !acc.contentStarted {
				acc.contentStarted = true
				acc.contentIndex = acc.nextBlockIndex()
				blocksChan <- StreamContentBlock{
					Type: "stream_event",
					RawEvent: AnthropicStreamEvent{
						Type:  EventContentBlockStart,
						Index: acc.contentIndex,
						ContentBlock: &AnthropicContentBlock{
							Type: BlockTypeText,
							Text: "",
						},
					},
				}
			}
			acc.content += event.Delta
			blocksChan <- StreamContentBlock{
				Type: "stream_event",
				RawEvent: AnthropicStreamEvent{
					Type:  EventContentBlockDelta,
					Index: acc.contentIndex,
					Delta: &AnthropicStreamDelta{
						Type: DeltaTypeText,
						Text: event.Delta,
					},
				},
			}
		}

	case "response.function_call_arguments.delta":
		if event.Delta != "" {
			// Close thinking/text blocks before tool args
			if acc.thinkingStarted && !acc.thinkingStopped {
				acc.thinkingStopped = true
				blocksChan <- StreamContentBlock{
					Type: "stream_event",
					RawEvent: AnthropicStreamEvent{
						Type:  EventContentBlockStop,
						Index: acc.thinkingIndex,
					},
				}
				blocksChan <- StreamContentBlock{Block: ContentBlock{
					Type: BlockTypeThinking, Thinking: acc.thinking,
				}}
			}
			if acc.contentStarted && !acc.contentStopped {
				acc.contentStopped = true
				blocksChan <- StreamContentBlock{
					Type: "stream_event",
					RawEvent: AnthropicStreamEvent{
						Type:  EventContentBlockStop,
						Index: acc.contentIndex,
					},
				}
				blocksChan <- StreamContentBlock{Block: ContentBlock{
					Type: BlockTypeText, Text: acc.content,
				}}
			}

			item := acc.getOutputItem(event.OutputIndex)
			if item != nil {
				if !item.toolStarted {
					item.toolStarted = true
					item.toolBlockIndex = acc.nextBlockIndex()
					blocksChan <- StreamContentBlock{
						Type: "stream_event",
						RawEvent: AnthropicStreamEvent{
							Type:  EventContentBlockStart,
							Index: item.toolBlockIndex,
							ContentBlock: &AnthropicContentBlock{
								Type: BlockTypeToolUse,
								ID:   item.id,
								Name: item.toolName,
							},
						},
					}
				}
				item.args += event.Delta
				blocksChan <- StreamContentBlock{
					Type: "stream_event",
					RawEvent: AnthropicStreamEvent{
						Type:  EventContentBlockDelta,
						Index: item.toolBlockIndex,
						Delta: &AnthropicStreamDelta{
							Type:        DeltaTypeInputJSON,
							PartialJSON: event.Delta,
						},
					},
				}
			}
		}

	case "response.output_item.done":
		if event.Item != nil && event.Item.Type == "function_call" {
			item := acc.getOutputItem(event.OutputIndex)
			if item != nil && item.toolStarted {
				blocksChan <- StreamContentBlock{
					Type: "stream_event",
					RawEvent: AnthropicStreamEvent{
						Type:  EventContentBlockStop,
						Index: item.toolBlockIndex,
					},
				}
				var input map[string]any
				if item.args != "" {
					json.Unmarshal([]byte(item.args), &input)
				}
				blocksChan <- StreamContentBlock{Block: ContentBlock{
					Type:      BlockTypeToolUse,
					ToolID:    item.id,
					ToolName:  item.toolName,
					ToolInput: input,
				}}
			}
		}

	case "response.completed":
		hasToolCalls := false
		// Close any remaining open blocks
		if acc.thinkingStarted && !acc.thinkingStopped {
			acc.thinkingStopped = true
			blocksChan <- StreamContentBlock{
				Type: "stream_event",
				RawEvent: AnthropicStreamEvent{
					Type:  EventContentBlockStop,
					Index: acc.thinkingIndex,
				},
			}
			blocksChan <- StreamContentBlock{Block: ContentBlock{
				Type: BlockTypeThinking, Thinking: acc.thinking,
			}}
		}
		if acc.contentStarted && !acc.contentStopped {
			acc.contentStopped = true
			blocksChan <- StreamContentBlock{
				Type: "stream_event",
				RawEvent: AnthropicStreamEvent{
					Type:  EventContentBlockStop,
					Index: acc.contentIndex,
				},
			}
			blocksChan <- StreamContentBlock{Block: ContentBlock{
				Type: BlockTypeText, Text: acc.content,
			}}
		}

		// Parse final usage from the completed response
		if event.Response != nil {
			result.Usage.InputTokens = event.Response.Usage.InputTokens
			result.Usage.OutputTokens = event.Response.Usage.OutputTokens
			if event.Response.Usage.PromptTokens > 0 {
				result.Usage.InputTokens = event.Response.Usage.PromptTokens
			}
			if event.Response.Usage.CompletionTokens > 0 {
				result.Usage.OutputTokens = event.Response.Usage.CompletionTokens
			}

			// Check output items for tool calls
			for _, item := range event.Response.Output {
				if item.Type == "function_call" {
					hasToolCalls = true
				}
			}
		}

		if hasToolCalls {
			acc.stopReason = StopReasonToolUse
		} else {
			acc.stopReason = StopReasonEndTurn
		}
		return true

	case "response.failed":
		if event.Response != nil {
			result.Error = fmt.Sprintf("response failed: status=%s", event.Response.Status)
		} else {
			result.Error = "response failed"
		}
		return true

	case "response.content_part.added":
		// Track function call name from content_part
		if event.Part != nil && event.Part.Type == "function_call" {
			item := acc.getOutputItem(event.OutputIndex)
			if item != nil {
				item.toolName = event.Part.Name
			}
		}
	}

	return false
}

// responsesStreamAccumulator accumulates OpenAI Responses API streaming events.
type responsesStreamAccumulator struct {
	content    string
	thinking   string
	stopReason StopReason
	items      map[int]*responsesOutputItem

	blockCounter    int
	thinkingStarted bool
	thinkingStopped bool
	thinkingIndex   int
	contentStarted  bool
	contentStopped  bool
	contentIndex    int
}

type responsesOutputItem struct {
	itemType       string
	id             string
	toolName       string
	args           string
	toolStarted    bool
	toolBlockIndex int
}

func newResponsesStreamAccumulator() *responsesStreamAccumulator {
	return &responsesStreamAccumulator{
		items: make(map[int]*responsesOutputItem),
	}
}

func (acc *responsesStreamAccumulator) nextBlockIndex() int {
	idx := acc.blockCounter
	acc.blockCounter++
	return idx
}

func (acc *responsesStreamAccumulator) startOutputItem(index int, itemType, id string) {
	acc.items[index] = &responsesOutputItem{
		itemType: itemType,
		id:       id,
	}
}

func (acc *responsesStreamAccumulator) getOutputItem(index int) *responsesOutputItem {
	return acc.items[index]
}

func (acc *responsesStreamAccumulator) finalize() []ContentBlock {
	var blocks []ContentBlock

	if acc.thinking != "" {
		blocks = append(blocks, ContentBlock{
			Type:     BlockTypeThinking,
			Thinking: acc.thinking,
		})
	}

	if acc.content != "" {
		blocks = append(blocks, ContentBlock{
			Type: BlockTypeText,
			Text: acc.content,
		})
	}

	for i := 0; i < len(acc.items); i++ {
		if item, ok := acc.items[i]; ok && item.itemType == "function_call" && item.id != "" {
			var input map[string]any
			if item.args != "" {
				json.Unmarshal([]byte(item.args), &input)
			}
			blocks = append(blocks, ContentBlock{
				Type:      BlockTypeToolUse,
				ToolID:    item.id,
				ToolName:  item.toolName,
				ToolInput: input,
			})
		}
	}

	return blocks
}
