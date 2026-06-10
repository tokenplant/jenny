// Package api provides the OpenAI API client.
package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/log"
)

// openAIProvider implements the Provider interface using the OpenAI Chat API.
type openAIProvider struct {
	model      string
	baseURL    string
	apiKey     string
	maxTokens  int
	wireAPI    string // "chat" or "responses" (responses not yet supported)
	httpClient *http.Client
}

// newOpenAIProvider creates a new OpenAI provider.
func newOpenAIProvider(model string) (*openAIProvider, error) {
	baseURL := strings.TrimRight(os.Getenv("OPENAI_BASE_URL"), "/")
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

	wireAPI := os.Getenv("OPENAI_WIRE_API")
	if wireAPI == "" {
		wireAPI = "chat"
	}

	// Check for unsupported responses API
	if wireAPI == "responses" {
		return nil, errors.New("OpenAI Responses API not yet supported; use OPENAI_WIRE_API=chat or unset")
	}

	return &openAIProvider{
		model:      model,
		baseURL:    baseURL,
		apiKey:     apiKey,
		maxTokens:  64000,
		wireAPI:    wireAPI,
		httpClient: &http.Client{Timeout: 1 * time.Hour},
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

// SendMessage sends a non-streaming message.
func (p *openAIProvider) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	log.Debug("OpenAI provider sending message", "model", p.model)

	// Normalize messages
	messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

	// Build OpenAI request
	openAIMessages, err := p.buildMessages(messages, toolResults, systemPrompt)
	if err != nil {
		return nil, err
	}

	// Build tools
	openAITools := p.buildTools(tools)

	// Build request body
	body := map[string]any{
		"model":    p.model,
		"messages": openAIMessages,
	}

	if len(openAITools) > 0 {
		body["tools"] = openAITools
	}

	maxTokens := p.maxTokens
	if maxTokens == 0 {
		maxTokens = 64000
	}
	body["max_tokens"] = maxTokens

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var openAIResp OpenAIChatResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return p.parseResponse(openAIResp)
}

// buildMessages converts api.Message slices to OpenAI message format.
func (p *openAIProvider) buildMessages(messages []Message, toolResults []ToolResult, systemPrompt string) ([]map[string]any, error) {
	var openAIMessages []map[string]any

	// Add system prompt as first message
	if systemPrompt != "" {
		openAIMessages = append(openAIMessages, map[string]any{
			"role":    "system",
			"content": systemPrompt,
		})
	}

	// Convert messages
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if msg.Content != "" {
				openAIMessages = append(openAIMessages, map[string]any{
					"role":    "user",
					"content": msg.Content,
				})
			}

		case "assistant":
			assistantMsg := map[string]any{
				"role": "assistant",
			}

			if len(msg.ToolUse) > 0 {
				// Assistant message with tool_calls
				toolCalls := make([]map[string]any, 0, len(msg.ToolUse))
				for _, tu := range msg.ToolUse {
					inputJSON, err := json.Marshal(tu.Input)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal tool input: %w", err)
					}
					toolCalls = append(toolCalls, map[string]any{
						"id":       tu.ID,
						"type":     "function",
						"function": map[string]any{"name": tu.Name, "arguments": string(inputJSON)},
					})
				}
				assistantMsg["tool_calls"] = toolCalls
			} else if msg.Content != "" {
				assistantMsg["content"] = msg.Content
			}

			openAIMessages = append(openAIMessages, assistantMsg)
		}
	}

	// Add tool results
	for _, tr := range toolResults {
		openAIMessages = append(openAIMessages, map[string]any{
			"role":         "tool",
			"tool_call_id": tr.ToolUseID,
			"content":      tr.Content,
		})
	}

	// Also add tool_results from messages
	for _, msg := range messages {
		for _, tr := range msg.ToolResults {
			openAIMessages = append(openAIMessages, map[string]any{
				"role":         "tool",
				"tool_call_id": tr.ToolUseID,
				"content":      tr.Content,
			})
		}
	}

	return openAIMessages, nil
}

// buildTools converts api.ToolParam slices to OpenAI tools format.
func (p *openAIProvider) buildTools(tools []ToolParam) []map[string]any {
	if len(tools) == 0 {
		return nil
	}

	openAITools := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		tool := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  p.buildInputSchema(t.InputSchema),
			},
		}
		openAITools = append(openAITools, tool)
	}

	return openAITools
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

	// Pass through extra fields ($defs, etc.)
	maps.Copy(result, schema.ExtraFields)

	return result
}

// parseResponse converts an OpenAI Chat response to api.Response.
func (p *openAIProvider) parseResponse(resp OpenAIChatResponse) (*Response, error) {
	response := &Response{
		Model: resp.Model,
	}

	if len(resp.Choices) == 0 {
		return response, nil
	}

	choice := resp.Choices[0]

	// Map stop reason
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

	// Map content
	// AC4: Prepend thinking block if reasoning_content is present
	if choice.Message.ReasoningContent != "" {
		response.Content = append(response.Content, ContentBlock{
			Type:     "thinking",
			Thinking: choice.Message.ReasoningContent,
		})
	}
	if choice.Message.Content != "" && choice.Message.Content != "null" {
		response.Content = append(response.Content, ContentBlock{
			Type: "text",
			Text: choice.Message.Content,
		})
	}

	// Map tool calls
	for _, tc := range choice.Message.ToolCalls {
		var input map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
				input = make(map[string]any)
			}
		}
		response.Content = append(response.Content, ContentBlock{
			Type:      "tool_use",
			ToolID:    tc.ID,
			ToolName:  tc.Function.Name,
			ToolInput: input,
		})
	}

	// Map usage
	if resp.Usage.PromptTokens > 0 {
		response.Usage.InputTokens = resp.Usage.PromptTokens
	}
	if resp.Usage.CompletionTokens > 0 {
		response.Usage.OutputTokens = resp.Usage.CompletionTokens
	}
	// AC5: Parse cached_tokens into CacheReadInputTokens
	if resp.Usage.PromptTokensDetails.CachedTokens > 0 {
		response.Usage.CacheReadInputTokens = resp.Usage.PromptTokensDetails.CachedTokens
	}

	return response, nil
}

// SendMessageStream sends a streaming message.
func (p *openAIProvider) SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	go func() {
		defer close(blocksChan)

		log.Debug("OpenAI provider streaming message", "model", p.model)

		// ... (setup logic)
		messages, tools, _ = NormalizeMessages(messages, tools, Capabilities{SupportsPromptCaching: false})

		// ... (request prep)
		openAIMessages, err := p.buildMessages(messages, toolResults, systemPrompt)
		if err != nil {
			result.Error = err.Error()
			return
		}

		openAITools := p.buildTools(tools)
		body := map[string]any{
			"model":    p.model,
			"messages": openAIMessages,
			"stream":   true,
		}
		if len(openAITools) > 0 {
			body["tools"] = openAITools
		}
		maxTokens := p.maxTokens
		if maxTokens == 0 {
			maxTokens = 64000
		}
		body["max_tokens"] = maxTokens
		jsonBody, err := json.Marshal(body)
		if err != nil {
			result.Error = fmt.Sprintf("failed to marshal request: %v", err)
			return
		}

		// Create request
		url := p.baseURL + "/chat/completions"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
		if err != nil {
			result.Error = fmt.Sprintf("failed to create request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Accept", "text/event-stream")

		// Send request
		resp, err := p.httpClient.Do(req)
		if err != nil {
			result.Error = fmt.Sprintf("request failed: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			result.Error = fmt.Sprintf("API error %d: %s", resp.StatusCode, string(bodyBytes))
			return
		}

		// Parse SSE stream
		reader := NewSSEReader(resp.Body)
		accumulator := newOpenAIStreamAccumulator()

		hasStopReason := false

		if idleTimeout <= 0 {
			idleTimeout = DefaultIdleTimeout
		}
		idleTimer := time.NewTimer(idleTimeout)
		defer idleTimer.Stop()

		watchdogCtx, cancelWatchdog := context.WithCancel(ctx)
		defer cancelWatchdog()

		go func() {
			select {
			case <-idleTimer.C:
				log.Warn("OpenAI: Idle timeout reached")
				result.Error = "idle timeout"
				// Note: http.Client context cancellation is handled by reader unblocking
			case <-watchdogCtx.Done():
			}
		}()

		for {
			// ReadEvent might block
			event, err := reader.ReadEvent()

			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				if result.Error == "" {
					result.Error = fmt.Sprintf("stream read error: %v", err)
				}
				return
			}

			// Passthrough raw event
			blocksChan <- StreamContentBlock{
				Type:     "stream_event",
				RawEvent: event,
			}

			// Reset idle timer
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(idleTimeout)

			if event.Data == "[DONE]" {
				break
			}

			// Parse chunk
			var chunk OpenAIStreamChunk
			if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
				continue // Skip malformed chunks
			}

			// Process chunk
			hasStopReason = p.processStreamChunk(chunk, accumulator, blocksChan, result) || hasStopReason
		}

		// Check if stream completed properly
		if !hasStopReason && result.Error == "" {
			// Stream ended without a finish reason - mark as incomplete
			result.Error = "stream incomplete: no stop reason"
		}

		// Stream is complete if we received a stop reason
		result.StreamComplete = hasStopReason

		// Finalize and set result
		result.Blocks = accumulator.finalize()
		result.StopReason = accumulator.stopReason
		result.Model = p.model
	}()

	return blocksChan, result
}

// processStreamChunk processes a single OpenAI stream chunk.
// Returns true if a stop reason was set.
func (p *openAIProvider) processStreamChunk(chunk OpenAIStreamChunk, acc *openAIStreamAccumulator, blocksChan chan<- StreamContentBlock, result *StreamResult) bool {
	if chunk.Model != "" {
		result.Model = chunk.Model
	}

	if len(chunk.Choices) == 0 {
		return false
	}

	choice := chunk.Choices[0]
	hasStopReason := choice.FinishReason != ""

	// Update stop reason
	switch choice.FinishReason {
	case "stop":
		acc.setStopReason(StopReasonEndTurn)
	case "tool_calls":
		acc.setStopReason(StopReasonToolUse)
	case "length":
		acc.setStopReason(StopReasonMaxTokens)
	}

	// Process delta
	delta := choice.Delta

	// Process content - emit incrementally as content arrives
	if delta.Content != "" && delta.Content != "null" {
		acc.appendContent(delta.Content)
		// Emit text block with accumulated content
		blocksChan <- StreamContentBlock{
			Block: ContentBlock{
				Type: "text",
				Text: acc.getContent(),
			},
		}
	}

	// AC6: Emit thinking_delta events for reasoning_content deltas
	if delta.ReasoningContent != "" {
		acc.appendThinking(delta.ReasoningContent)
		blocksChan <- StreamContentBlock{
			Block: ContentBlock{
				Type:     "thinking",
				Thinking: acc.getThinking(),
			},
		}
	}

	// Process tool calls - emit incrementally as tool input accumulates
	for _, tc := range delta.ToolCalls {
		acc.appendToolCall(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
		// Emit partial tool_use block as arguments stream in
		if toolBlock := acc.getToolUseBlock(tc.Index); toolBlock != nil {
			blocksChan <- StreamContentBlock{
				Block: *toolBlock,
			}
		}
	}

	// Process usage
	if chunk.Usage != nil {
		if chunk.Usage.PromptTokens > 0 {
			result.Usage.InputTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			result.Usage.OutputTokens = chunk.Usage.CompletionTokens
		}
		// AC8: Map cached_tokens to CacheReadInputTokens
		if chunk.Usage.PromptTokensDetails.CachedTokens > 0 {
			result.Usage.CacheReadInputTokens = chunk.Usage.PromptTokensDetails.CachedTokens
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
	ID       string
	Name     string
	Args     string
	Input    map[string]any
	Complete bool
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
		// Try to parse arguments
		if tc.Input == nil {
			var input map[string]any
			if err := json.Unmarshal([]byte(tc.Args), &input); err == nil {
				tc.Input = input
			}
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

	// AC7: Emit thinking block before text block
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

	// Add tool use blocks
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

// OpenAI API types

// OpenAI API types

// PromptTokensDetails represents the prompt tokens details in OpenAI API responses.
type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// OpenAIChatResponse is the response from the OpenAI Chat API.
type OpenAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int                 `json:"prompt_tokens"`
		CompletionTokens    int                 `json:"completion_tokens"`
		TotalTokens         int                 `json:"total_tokens"`
		PromptTokensDetails PromptTokensDetails `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// OpenAIStreamChunk is a chunk from the OpenAI streaming response.
type OpenAIStreamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int                 `json:"prompt_tokens"`
		CompletionTokens    int                 `json:"completion_tokens"`
		TotalTokens         int                 `json:"total_tokens"`
		PromptTokensDetails PromptTokensDetails `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// SSEReader reads Server-Sent Events from a reader.
// Uses bufio.Reader with a large buffer to handle large responses.
type SSEReader struct {
	reader *bufio.Reader
}

// SSEEvent represents a single SSE event.
type SSEEvent struct {
	Event string // event type (e.g., "ping")
	Data  string // data content
}

// NewSSEReader creates a new SSE reader with a large buffer.
func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{
		reader: bufio.NewReaderSize(r, 1<<20), // 1MB buffer for large responses
	}
}

// ReadEvent reads the next SSE event, properly parsing the SSE protocol.
// Handles long lines by accumulating data until newline is found.
func (s *SSEReader) ReadEvent() (*SSEEvent, error) {
	var eventType string
	var data strings.Builder

	for {
		// Read bytes until newline using ReadBytes
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF && (data.Len() > 0 || eventType != "") {
				// Return last event if we have content
				return &SSEEvent{Event: eventType, Data: data.String()}, nil
			}
			return nil, err
		}

		// Remove trailing \r\n or \n
		lineStr := strings.TrimRight(string(line), "\r\n")
		if lineStr == "" {
			// Empty line marks end of event
			if data.Len() > 0 || eventType != "" {
				return &SSEEvent{Event: eventType, Data: data.String()}, nil
			}
			continue
		}

		// Parse SSE field
		if after, ok := strings.CutPrefix(lineStr, "event:"); ok {
			eventType = after
			eventType = strings.TrimSpace(eventType)
		} else if after, ok := strings.CutPrefix(lineStr, "data:"); ok {
			dataStr := after
			dataStr = strings.TrimSpace(dataStr)
			if data.Len() > 0 {
				data.WriteString("\n")
			}
			data.WriteString(dataStr)
		}
		// Ignore other SSE fields (id:, retry:, etc.)
	}
}
