// Package api provides the Anthropic API client.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/log"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)
// SendMessage sends a message to the API and returns the response.
func (c *Client) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	// Wrap the actual send logic with retry
	return c.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return c.doSendMessage(ctx, messages, tools, toolResults, systemPrompt)
	}, c.isBackground)
}
// doSendMessage performs the actual message sending (used by retry logic).
func (c *Client) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	log.Debug("Sending message", "model", c.model)
	log.Debug("System prompt", "prompt", systemPrompt)
	log.Debug("Number of tools", "count", len(tools))
	for _, t := range tools {
		log.Debug("Tool registered", "name", t.Name, "description", t.Description)
	}

	// Validate media before sending
	if err := ValidateMessagesMedia(messages); err != nil {
		return nil, err
	}

	// Universal normalization gateway: normalize messages and tools before serialization
	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: true})

	// Convert messages to SDK format
	sdkMessages := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		contentBlocks := make([]anthropic.ContentBlockParamUnion, 0)

		// Add text content if present
		if msg.Content != "" {
			contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
				OfText: &anthropic.TextBlockParam{Text: msg.Content},
			})
		}

		// Add tool_use blocks if present
		for _, tu := range msg.ToolUse {
			contentBlocks = append(contentBlocks, anthropic.ContentBlockParamUnion{
				OfToolUse: &anthropic.ToolUseBlockParam{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: tu.Input,
				},
			})
		}

		// Add tool_result blocks if present (already deduplicated in NormalizeMessages)
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

	// Add standalone tool results as user messages if there are any (backward compat)
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
	maxTokens := 64000
	if c.maxTokensOverride > 0 {
		maxTokens = c.maxTokensOverride
	}
	body := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
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
	resp, err := c.client.Messages.New(ctx, body)
	if err != nil {
		// Wrap SDK errors to extract status code for retry logic
		wrappedErr := wrapSDKError(err)
		return nil, wrappedErr
	}

	// Convert response
	response := &Response{
		Model:      string(resp.Model),
		StopReason: StopReason(string(resp.StopReason)),
	}

	// Convert usage (AC1: extract all four token types)
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

	// Convert content blocks using type switch
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			response.Content = append(response.Content, ContentBlock{
				Type: "text",
				Text: block.Text,
			})
		case "thinking":
			// Pass thinking + signature through so the non-streaming
			// fallback path emits an assistant envelope with its own
			// `type: "thinking"` block (AC6).
			response.Content = append(response.Content, ContentBlock{
				Type:      "thinking",
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "tool_use":
			// Parse the input JSON
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
			// Process web search results - extract error codes if present
			webSearchData := &WebSearchResultData{
				ToolUseID: block.ToolUseID,
			}
			// Check if this is an error response - WebSearchToolResultBlockContentUnion
			// has AsResponseWebSearchToolResultError method that returns the error if present
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
// wrapSDKError wraps SDK errors to extract HTTP status code for retry logic.
func wrapSDKError(err error) error {
	if err == nil {
		return nil
	}

	// Use errors.As with SDK's typed error to properly extract status code
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr); apiErr != nil {
		// Extract Retry-After from response headers if present
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

	// For unknown errors, return as-is (will be handled as connection errors if retryable)
	return err
}
// toolToSDK converts a ToolParam to an SDK ToolUnionParam.
// When isLast is true, cache_control is set on the tool to mark it as a cache breakpoint.
// Tool normalization (including __arg__ placeholder for empty properties) is handled
// by NormalizeMessages before this function is called.
//
// Note: web_search always uses ToolParam with input_schema to ensure compatibility
// with all providers (including MiniMax) that require input_schema on all tools.
// The MaxUses field on web_search is set on the ToolParam if provided.
func toolToSDK(t ToolParam, isLast bool) anthropic.ToolUnionParam {
	// Standard path: use ToolParam with input schema (already normalized)
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

	// Pass through extra fields ($defs, etc.) if present
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
// This is used to categorize max_tokens errors as output_cap_hit vs context_exhausted.
func modelMaxOutputTokens(model string) int {
	// Known model-specific max output tokens
	switch model {
	case "deepseek-v4-flash":
		return 8192
	case "deepseek-v4":
		return 8192
	default:
		// Default max output tokens (matches defaultModelMaxOutputTokens in compact.go)
		return 20000
	}
}
// categorizeMaxTokensError creates a MaxTokensError from streaming results.
// It determines the category (output_cap_hit vs context_exhausted) based on whether
// output_tokens reached the model's max output tokens OR the request was rejected
// with a prompt_too_long error.
func categorizeMaxTokensError(model string, outputTokens int, contextRejected bool) *MaxTokensError {
	maxOutputTokens := modelMaxOutputTokens(model)

	// context_exhausted is only set when the request was actually rejected
	// via HTTP 400 / prompt_too_long - NOT when streaming completed with low output
	if contextRejected {
		return &MaxTokensError{
			Category:        CategoryContextExhausted,
			Model:           model,
			OutputTokens:    outputTokens,
			MaxOutputTokens: 0, // Not applicable for context_exhausted
		}
	}

	// output_cap_hit: output reached the model's max (normal output cap hit)
	if outputTokens >= maxOutputTokens {
		return &MaxTokensError{
			Category:        CategoryOutputCapHit,
			Model:           model,
			OutputTokens:    outputTokens,
			MaxOutputTokens: maxOutputTokens,
		}
	}

	// This case should not occur in normal streaming - if we get here with
	// stop_reason=max_tokens but no context rejection and low output, it means
	// the model was limited without hitting the output cap. Treat as output_cap_hit
	// since context was not exhausted (the request completed successfully).
	return &MaxTokensError{
		Category:        CategoryOutputCapHit,
		Model:           model,
		OutputTokens:    outputTokens,
		MaxOutputTokens: maxOutputTokens,
	}
}
// isPromptTooLongError checks if the given error indicates a context exhaustion
// rejection via HTTP 400 with a prompt_too_long error.
func isPromptTooLongError(err error) bool {
	if err == nil {
		return false
	}

	// Check for SDK error with status code 400
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		// Check error message for prompt_too_long (provider-specific error text)
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "prompt_too_long") || strings.Contains(errMsg, "context window exceeds limit") {
			return true
		}
	}

	// Also check RetryableHTTPError which wraps SDK errors
	var retryErr *RetryableHTTPError
	if errors.As(err, &retryErr) && retryErr.StatusCode == 400 {
		errMsg := strings.ToLower(retryErr.Message)
		if strings.Contains(errMsg, "prompt_too_long") || strings.Contains(errMsg, "context window exceeds limit") {
			return true
		}
	}

	return false
}
