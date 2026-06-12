// Package api provides the Google GenAI API client (Gemini / Vertex AI).
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

// ProviderKind for the Google GenAI provider.
const ProviderGenAI ProviderKind = "genai"

// genaiProvider implements the Provider interface using a lightweight HTTP client.
type genaiProvider struct {
	client      *HTTPClient
	model       string
	maxTokens   int
	retryConfig RetryConfig
}

// newGenAIProvider creates a new GenAI provider.
func newGenAIProvider(model string) (*genaiProvider, error) {
	if model == "" {
		model = os.Getenv("GENAI_DEFAULT_MODEL")
	}
	if model == "" {
		return nil, errors.New("GENAI_DEFAULT_MODEL is required when using genai provider")
	}

	timeout := ResolveTimeout(os.Getenv("API_TIMEOUT_MS"))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	return &genaiProvider{
		client:      NewHTTPClient(timeout),
		model:       model,
		maxTokens:   64000,
		retryConfig: DefaultRetryConfig(),
	}, nil
}

// Kind returns the provider kind.
func (p *genaiProvider) Kind() ProviderKind {
	return ProviderGenAI
}

// SetModel sets the model.
func (p *genaiProvider) SetModel(model string) {
	p.model = model
}

// GetModel returns the model.
func (p *genaiProvider) GetModel() string {
	return p.model
}

// SetMaxTokensOverride sets the max output tokens override.
func (p *genaiProvider) SetMaxTokensOverride(maxTokens int) {
	p.maxTokens = maxTokens
}

// SetRetryConfig sets the retry configuration.
func (p *genaiProvider) SetRetryConfig(cfg RetryConfig) {
	p.retryConfig = cfg
}

// SendMessage sends a non-streaming message.
func (p *genaiProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	return p.sendWithRetry(ctx, func(ctx context.Context) (*Response, error) {
		return p.doSendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
	}, false)
}

// sendWithRetry executes a function with retry logic.
func (p *genaiProvider) sendWithRetry(ctx context.Context, fn func(context.Context) (*Response, error), isBackground bool) (*Response, error) {
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

// doSendMessage performs the actual non-streaming message send.
func (p *genaiProvider) doSendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error) {
	if err := ValidateMessagesMedia(messages); err != nil {
		return nil, err
	}

	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

	reqBody := GenAIRequest{
		Contents: p.buildContents(messages, toolResults),
		GenerationConfig: &GenAIGenerationConfig{
			MaxOutputTokens: &p.maxTokens,
		},
	}

	fullSystem := systemPrompt
	if systemPromptSuffix != "" {
		if fullSystem != "" {
			fullSystem += "\n\n"
		}
		fullSystem += systemPromptSuffix
	}
	if fullSystem != "" {
		reqBody.SystemInstruction = &GenAIContent{
			Parts: []GenAIPart{{Text: fullSystem}},
		}
	}

	if len(tools) > 0 {
		reqBody.Tools = []GenAITool{{
			FunctionDeclarations: p.buildTools(tools),
		}}
	}

	apiKey := os.Getenv("GENAI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}

	baseURL := os.Getenv("GENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", baseURL, p.model, apiKey)
	headers := http.Header{}

	var genAIResp GenAIResponse
	if err := p.client.Request(ctx, "POST", url, headers, reqBody, &genAIResp); err != nil {
		return nil, err
	}

	return p.parseResponse(&genAIResp)
}

// isPromptTooLongGenAI returns true if the error indicates prompt too long.
func isPromptTooLongGenAI(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "prompt_too_long") || strings.Contains(msg, "context window exceeds limit")
}

// SendMessageStream sends a streaming message.
func (p *genaiProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		if err := ValidateMessagesMedia(messages); err != nil {
			result.Error = err.Error()
			return
		}

		messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

		reqBody := GenAIRequest{
			Contents: p.buildContents(messages, toolResults),
			GenerationConfig: &GenAIGenerationConfig{
				MaxOutputTokens: &p.maxTokens,
			},
		}

		fullSystem := systemPrompt
		if systemPromptSuffix != "" {
			if fullSystem != "" {
				fullSystem += "\n\n"
			}
			fullSystem += systemPromptSuffix
		}
		if fullSystem != "" {
			reqBody.SystemInstruction = &GenAIContent{
				Parts: []GenAIPart{{Text: fullSystem}},
			}
		}

		if len(tools) > 0 {
			reqBody.Tools = []GenAITool{{
				FunctionDeclarations: p.buildTools(tools),
			}}
		}

		apiKey := os.Getenv("GENAI_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
		}

		baseURL := os.Getenv("GENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com"
		}

		url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", baseURL, p.model, apiKey)
		headers := http.Header{}

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
			if isPromptTooLongGenAI(err) {
				result.ContextRejected = true
				result.MaxTokensErr = categorizeMaxTokensError(p.model, result.Usage.OutputTokens, true)
			}
			return
		}
		defer body.Close()

		acc := newGenAIStreamAccumulator()
		hasFinishReason := false
		scanner := NewSSEScanner(body)

		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		watchdogCtx, cancelWatchdog := context.WithCancel(ctx)
		defer cancelWatchdog()

		go func() {
			select {
			case <-idleTimer.C:
				log.Warn("GenAI: idle timeout reached")
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

			var resp GenAIResponse
			if err := json.Unmarshal([]byte(data), &resp); err != nil {
				log.Error("GenAI: failed to unmarshal chunk", "error", err, "data", data)
				continue
			}

			if stopReason, usage, model, ok := p.processStreamChunk(&resp, acc, blocksChan, result); ok {
				hasFinishReason = true
				if usage.OutputTokens > 0 || usage.InputTokens > 0 {
					result.Usage = usage
				}
				if model != "" {
					result.Model = model
				}
				if stopReason == StopReasonMaxTokens {
					result.MaxTokensErr = categorizeMaxTokensError(result.Model, result.Usage.OutputTokens, result.ContextRejected)
				}
			}
		}

		if scanner.Err() != nil && result.Error == "" {
			result.Error = scanner.Err().Error()
		}

		if isPromptTooLongGenAI(errors.New(result.Error)) {
			result.ContextRejected = true
		}

		if !hasFinishReason && result.Error == "" {
			result.Error = "stream incomplete: no finish reason"
		}

		if (result.StopReason == StopReasonMaxTokens || result.ContextRejected) && result.MaxTokensErr == nil {
			result.MaxTokensErr = categorizeMaxTokensError(result.Model, result.Usage.OutputTokens, result.ContextRejected)
		}

		result.StreamComplete = hasFinishReason
		result.Blocks = acc.finalize()
		result.StopReason = acc.stopReason
		if result.Model == "" {
			result.Model = p.model
		}
	}()

	return blocksChan, result
}

// processStreamChunk processes a single streaming chunk.
func (p *genaiProvider) processStreamChunk(resp *GenAIResponse, acc *genAIStreamAccumulator, blocksChan chan<- StreamContentBlock, result *StreamResult) (StopReason, Usage, string, bool) {
	if resp == nil {
		return "", Usage{}, "", false
	}

	usage := mapUsage(resp.UsageMetadata)
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		result.Usage = usage
	}

	model := p.model
	if resp.ModelVersion != "" {
		model = resp.ModelVersion
	}

	hasFinishReason := false
	for _, cand := range resp.Candidates {
		for _, part := range cand.Content.Parts {
			if part.Thought && part.Text != "" {
				acc.appendThinking(part.Text)
				blocksChan <- StreamContentBlock{
					Block: ContentBlock{
						Type:     "thinking",
						Thinking: acc.getThinking(),
					},
				}
			} else if part.Text != "" {
				acc.appendContent(part.Text)
				blocksChan <- StreamContentBlock{
					Block: ContentBlock{
						Type: "text",
						Text: acc.getContent(),
					},
				}
			}
			if part.FunctionCall != nil {
				acc.appendFunctionCall(part.FunctionCall)
				if block := acc.getFunctionCallBlock(); block != nil {
					blocksChan <- StreamContentBlock{Block: *block}
				}
			}
		}

		switch cand.FinishReason {
		case "STOP":
			acc.setStopReason(StopReasonEndTurn)
			hasFinishReason = true
		case "MAX_TOKENS":
			acc.setStopReason(StopReasonMaxTokens)
			hasFinishReason = true
		case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			acc.setStopReason(StopReasonStopSeq)
			hasFinishReason = true
		default:
			if cand.FinishReason != "" && cand.FinishReason != "FINISH_REASON_UNSPECIFIED" {
				acc.setStopReason(StopReason(cand.FinishReason))
				hasFinishReason = true
			}
		}
	}

	return acc.stopReason, result.Usage, model, hasFinishReason
}

// buildContents converts api.Message slices plus standalone tool results into GenAI format.
func (p *genaiProvider) buildContents(messages []Message, toolResults []ToolResult) []GenAIContent {
	contents := make([]GenAIContent, 0, len(messages)+len(toolResults))
	emittedStandalone := make(map[string]bool, len(toolResults))

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			parts := make([]GenAIPart, 0, 1+len(msg.ToolResults))
			if msg.Content != "" {
				parts = append(parts, GenAIPart{Text: msg.Content})
			}
			for _, tr := range msg.ToolResults {
				parts = append(parts, functionResponsePart(ToolResult{
					ToolUseID: tr.ToolUseID,
					Content:   tr.Content,
					IsError:   tr.IsError,
				}))
			}
			if len(parts) == 0 {
				continue
			}
			contents = append(contents, GenAIContent{Role: "user", Parts: parts})

		case "assistant":
			parts := make([]GenAIPart, 0, 1+len(msg.ToolUse))
			if msg.Content != "" {
				parts = append(parts, GenAIPart{Text: msg.Content})
			}
			for _, tu := range msg.ToolUse {
				parts = append(parts, GenAIPart{
					FunctionCall: &GenAIFunctionCall{
						Name: tu.Name,
						Args: tu.Input,
					},
				})
			}
			if len(parts) == 0 {
				continue
			}
			contents = append(contents, GenAIContent{Role: "model", Parts: parts})
		}
	}

	pending := make([]GenAIPart, 0, len(toolResults))
	for _, tr := range toolResults {
		if emittedStandalone[tr.ToolUseID] {
			continue
		}
		emittedStandalone[tr.ToolUseID] = true
		pending = append(pending, functionResponsePart(tr))
	}
	if len(pending) > 0 {
		contents = append(contents, GenAIContent{Role: "user", Parts: pending})
	}

	return contents
}

// functionResponsePart builds a Part carrying a function response.
func functionResponsePart(tr ToolResult) GenAIPart {
	response := map[string]any{}
	if tr.Content != "" {
		var parsed any
		if err := json.Unmarshal([]byte(tr.Content), &parsed); err == nil {
			response["output"] = parsed
		} else {
			response["output"] = tr.Content
		}
	}
	if tr.IsError {
		response["error"] = tr.Content
	}
	return GenAIPart{
		FunctionResponse: &GenAIFunctionResponse{
			Name:     toolNameFromID(tr.ToolUseID),
			Response: response,
		},
	}
}

// toolNameFromID returns a placeholder function name.
func toolNameFromID(id string) string {
	return "tool"
}

// buildTools converts api.ToolParam slices to GenAI format.
func (p *genaiProvider) buildTools(tools []ToolParam) []GenAIFunctionDeclaration {
	decls := make([]GenAIFunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		decl := GenAIFunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  p.buildInputSchema(t.InputSchema),
		}
		decls = append(decls, decl)
	}
	return decls
}

// buildInputSchema converts our JSON-Schema-ish ToolInputSchema into Gemini parameters format.
func (p *genaiProvider) buildInputSchema(schema ToolInputSchema) map[string]any {
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

// parseResponse converts a GenAIResponse to *api.Response.
func (p *genaiProvider) parseResponse(resp *GenAIResponse) (*Response, error) {
	if resp == nil {
		return &Response{}, nil
	}

	response := &Response{
		Model: p.model,
		Usage: mapUsage(resp.UsageMetadata),
	}
	if resp.ModelVersion != "" {
		response.Model = resp.ModelVersion
	}

	if len(resp.Candidates) == 0 {
		return response, nil
	}

	cand := resp.Candidates[0]
	switch cand.FinishReason {
	case "STOP":
		response.StopReason = StopReasonEndTurn
	case "MAX_TOKENS":
		response.StopReason = StopReasonMaxTokens
	case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		response.StopReason = StopReasonStopSeq
	default:
		if cand.FinishReason != "" && cand.FinishReason != "FINISH_REASON_UNSPECIFIED" {
			response.StopReason = StopReason(cand.FinishReason)
		} else {
			response.StopReason = StopReasonEndTurn
		}
	}

	for _, part := range cand.Content.Parts {
		if part.Thought && part.Text != "" {
			response.Content = append(response.Content, ContentBlock{
				Type:     "thinking",
				Thinking: part.Text,
			})
			continue
		}
		if part.Text != "" {
			response.Content = append(response.Content, ContentBlock{
				Type: "text",
				Text: part.Text,
			})
		}
		if part.FunctionCall != nil {
			response.Content = append(response.Content, ContentBlock{
				Type:      "tool_use",
				ToolID:    "", // Gemini REST API function calls don't always have IDs in the same way
				ToolName:  part.FunctionCall.Name,
				ToolInput: part.FunctionCall.Args,
			})
		}
	}

	return response, nil
}

// mapUsage converts GenAIUsage to api.Usage.
func mapUsage(u *GenAIUsage) Usage {
	if u == nil {
		return Usage{}
	}
	out := Usage{
		InputTokens:          u.PromptTokenCount,
		OutputTokens:         u.CandidatesTokenCount,
		CacheReadInputTokens: u.CachedContentTokenCount,
	}
	if u.ThoughtsTokenCount > 0 {
		out.OutputTokens += u.ThoughtsTokenCount
	}
	return out
}

// ---------------------------------------------------------------------------
// Streaming accumulator
// ---------------------------------------------------------------------------

type genAIStreamAccumulator struct {
	content    string
	thinking   string
	stopReason StopReason
	funcCalls  map[int]*GenAIFunctionCall
}

func newGenAIStreamAccumulator() *genAIStreamAccumulator {
	return &genAIStreamAccumulator{funcCalls: make(map[int]*GenAIFunctionCall)}
}

func (acc *genAIStreamAccumulator) appendContent(s string) { acc.content += s }
func (acc *genAIStreamAccumulator) getContent() string     { return acc.content }
func (acc *genAIStreamAccumulator) appendThinking(s string) {
	acc.thinking += s
}
func (acc *genAIStreamAccumulator) getThinking() string { return acc.thinking }
func (acc *genAIStreamAccumulator) setStopReason(r StopReason) {
	if acc.stopReason == "" {
		acc.stopReason = r
	}
}

func (acc *genAIStreamAccumulator) appendFunctionCall(fc *GenAIFunctionCall) {
	idx := len(acc.funcCalls)
	acc.funcCalls[idx] = fc
}

func (acc *genAIStreamAccumulator) getFunctionCallBlock() *ContentBlock {
	idx := len(acc.funcCalls) - 1
	if idx < 0 {
		return nil
	}
	fc, ok := acc.funcCalls[idx]
	if !ok || fc == nil {
		return nil
	}
	return &ContentBlock{
		Type:      "tool_use",
		ToolID:    "",
		ToolName:  fc.Name,
		ToolInput: fc.Args,
	}
}

func (acc *genAIStreamAccumulator) finalize() []ContentBlock {
	var blocks []ContentBlock
	if acc.thinking != "" {
		blocks = append(blocks, ContentBlock{Type: "thinking", Thinking: acc.thinking})
	}
	if acc.content != "" {
		blocks = append(blocks, ContentBlock{Type: "text", Text: acc.content})
	}
	for i := 0; i < len(acc.funcCalls); i++ {
		if fc, ok := acc.funcCalls[i]; ok && fc != nil {
			blocks = append(blocks, ContentBlock{
				Type:      "tool_use",
				ToolID:    "",
				ToolName:  fc.Name,
				ToolInput: fc.Args,
			})
		}
	}
	return blocks
}
