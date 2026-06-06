// Package api provides the Anthropic API client.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ipy/jenny/internal/log"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// DefaultIdleTimeout is the default timeout for idle watchdog (30 seconds).
const DefaultIdleTimeout = 30 * time.Second

// DefaultFallbackTimeout is the default timeout for non-streaming fallback (~5 min).
const DefaultFallbackTimeout = 5 * time.Minute

// Client wraps the Anthropic SDK client.
type Client struct {
	client anthropic.Client
	model  string
}

// defaultModel is the default model used when ANTHROPIC_MODEL is not set.
const defaultModel = "deepseek-v4-flash"

// NewClient creates a new API client.
func NewClient() (*Client, error) {
	return NewClientWithModel("")
}

// NewClientWithModel creates a new API client with an optional model override.
// If model is empty, reads from ANTHROPIC_MODEL environment variable.
func NewClientWithModel(model string) (*Client, error) {
	// Read ANTHROPIC_MODEL from environment (SDK handles BASE_URL and AUTH_TOKEN automatically)
	if model == "" {
		model = os.Getenv("ANTHROPIC_MODEL")
	}
	if model == "" {
		model = defaultModel
	}

	// SDK's NewClient already reads ANTHROPIC_BASE_URL and ANTHROPIC_AUTH_TOKEN
	client := anthropic.NewClient()

	return &Client{
		client: client,
		model:  model,
	}, nil
}

// SetModel sets the model to use.
func (c *Client) SetModel(model string) {
	c.model = model
}

// GetModel returns the model being used.
func (c *Client) GetModel() string {
	return c.model
}

// Message represents a message in the conversation.
type Message struct {
	Role        string
	Content     string
	ToolUse     []ToolUseBlock
	ToolResults []ToolResultBlock
}

// ToolUseBlock represents a tool use block in a message.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolResultBlock represents a tool result block in a message.
type ToolResultBlock struct {
	ToolUseID string
	Content   string
}

// ToolUse represents a tool call from the model.
type ToolUse struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResult represents a tool result to send back to the model.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// StopReason represents why the model stopped generating.
type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonToolUse   StopReason = "tool_use"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonStopSeq   StopReason = "stop_sequence"
)

// Response represents the API response.
type Response struct {
	Content    []ContentBlock
	StopReason StopReason
	Model      string
	Usage      Usage
	Error      string
}

// ContentBlock represents a block of content in the response.
type ContentBlock struct {
	Type      string
	Text      string
	ToolUse   *ToolUse
	ToolID    string
	ToolName  string
	ToolInput map[string]any
}

// Usage represents token usage information.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// SendMessage sends a message to the API and returns the response.
func (c *Client) SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string) (*Response, error) {
	log.Debug("Sending message", "model", c.model)
	log.Debug("System prompt", "prompt", systemPrompt)
	log.Debug("Number of tools", "count", len(tools))
	for _, t := range tools {
		log.Debug("Tool registered", "name", t.Name, "description", t.Description)
	}

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

		// Add tool_result blocks if present
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
	for _, t := range tools {
		sdkTools = append(sdkTools, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Type:       constant.Object("object"),
				Properties: t.InputSchema.Properties,
				Required:   t.InputSchema.Required,
			},
		}})
	}

	// Build request
	body := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 8192,
	}
	body.Messages = sdkMessages
	if systemPrompt != "" {
		body.System = []anthropic.TextBlockParam{{Text: systemPrompt}}
	}
	if len(sdkTools) > 0 {
		body.Tools = sdkTools
	}

	// Send request
	resp, err := c.client.Messages.New(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("API error: %v", err)
	}

	// Convert response
	response := &Response{
		Model:      string(resp.Model),
		StopReason: StopReason(string(resp.StopReason)),
	}

	// Convert usage
	if resp.Usage.InputTokens > 0 {
		response.Usage.InputTokens = int(resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens > 0 {
		response.Usage.OutputTokens = int(resp.Usage.OutputTokens)
	}

	// Convert content blocks using type switch
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			response.Content = append(response.Content, ContentBlock{
				Type: "text",
				Text: block.Text,
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
		}
	}

	return response, nil
}

// streamAccumulator tracks content blocks during streaming.
type streamAccumulator struct {
	blocks     []ContentBlock // Content blocks by index
	texts      []string       // Accumulated text per block index
	usage      Usage
	stopReason StopReason
}

// newStreamAccumulator creates a new stream accumulator.
func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{
		blocks: make([]ContentBlock, 0),
		texts:  make([]string, 0),
	}
}

// ensureBlock ensures a block exists at the given index.
func (acc *streamAccumulator) ensureBlock(index int) {
	for len(acc.blocks) <= index {
		acc.blocks = append(acc.blocks, ContentBlock{})
		acc.texts = append(acc.texts, "")
	}
}

// appendText appends text to the block at the given index.
func (acc *streamAccumulator) appendText(index int, text string) {
	acc.ensureBlock(index)
	acc.texts[index] += text
	acc.blocks[index].Text = acc.texts[index]
}

// setBlockType sets the type of a block at the given index.
func (acc *streamAccumulator) setBlockType(index int, blockType string) {
	acc.ensureBlock(index)
	acc.blocks[index].Type = blockType
}

// setUsage sets the usage from a message_delta event.
func (acc *streamAccumulator) setUsage(usage Usage) {
	acc.usage = usage
}

// setStopReason sets the stop reason from a message_delta event.
func (acc *streamAccumulator) setStopReason(reason StopReason) {
	acc.stopReason = reason
}

// getBlocks returns the accumulated content blocks.
func (acc *streamAccumulator) getBlocks() []ContentBlock {
	return acc.blocks
}

// StreamContentBlock represents a completed content block from streaming.
type StreamContentBlock struct {
	Index int
	Block ContentBlock
}

// StreamResult represents the result of a streaming session.
type StreamResult struct {
	Blocks     []ContentBlock
	StopReason StopReason
	Usage      Usage
	Error      string
}

// SendMessageStream sends a streaming message to the API.
// It yields completed content blocks via the blocks channel.
// If streaming fails, it triggers the fallback within the given fallbackTimeout.
// The idleTimeout controls the watchdog timer for each event.
func (c *Client) SendMessageStream(
	ctx context.Context,
	messages []Message,
	tools []ToolParam,
	toolResults []ToolResult,
	systemPrompt string,
	idleTimeout time.Duration,
	fallbackTimeout time.Duration,
	onStreamingFallback func(context.Context) (*Response, error),
) (<-chan StreamContentBlock, *StreamResult) {
	blocksChan := make(chan StreamContentBlock, 10)
	result := &StreamResult{}

	// Run streaming in a goroutine
	go func() {
		defer close(blocksChan)

		// Convert messages to SDK format (same as SendMessage)
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
		for _, t := range tools {
			sdkTools = append(sdkTools, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Type:       constant.Object("object"),
					Properties: t.InputSchema.Properties,
					Required:   t.InputSchema.Required,
				},
			}})
		}

		// Build request
		body := anthropic.MessageNewParams{
			Model:     anthropic.Model(c.model),
			MaxTokens: 8192,
		}
		body.Messages = sdkMessages
		if systemPrompt != "" {
			body.System = []anthropic.TextBlockParam{{Text: systemPrompt}}
		}
		if len(sdkTools) > 0 {
			body.Tools = sdkTools
		}

		log.Debug("Starting streaming request", "model", c.model)

		// Create stream with idle watchdog
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		stream := c.client.Messages.NewStreaming(ctx, body)

		acc := newStreamAccumulator()
		hasMessageStart := false
		hasMessageStop := false

		// Process stream events using iterator pattern
		// Use idle timeout check on each iteration
		lastEventTime := time.Now()

		for stream.Next() {
			// Check idle timeout
			if time.Since(lastEventTime) > idleTimeout {
				log.Warn("Idle timeout reached, cancelling stream")
				cancel()
				result.Error = "idle timeout"
				return
			}
			lastEventTime = time.Now()

			event := stream.Current()

			// Process the event
			variant := event.AsAny()
			switch e := variant.(type) {
			case anthropic.MessageStartEvent:
				hasMessageStart = true
				log.Debug("Stream: message_start")

			case anthropic.ContentBlockStartEvent:
				index := int(e.Index)
				log.Debug("Stream: content_block_start", "index", index, "type", e.ContentBlock.Type)
				// Determine block type from content_block.Type
				switch e.ContentBlock.Type {
				case "text":
					acc.setBlockType(index, "text")
				case "tool_use":
					// For tool_use, ID and Name are directly on ContentBlock
					acc.setBlockType(index, "tool_use")
					acc.blocks[index].ToolID = e.ContentBlock.ID
					acc.blocks[index].ToolName = e.ContentBlock.Name
					// Input will be populated via deltas
				default:
					acc.setBlockType(index, e.ContentBlock.Type)
				}

			case anthropic.ContentBlockDeltaEvent:
				index := int(e.Index)
				delta := e.Delta
				if delta.Text != "" {
					acc.appendText(index, delta.Text)
				} else if delta.PartialJSON != "" {
					// For partial JSON tool input, append to existing block
					acc.appendText(index, delta.PartialJSON)
				}
				log.Debug("Stream: content_block_delta", "index", index, "text", delta.Text)

			case anthropic.ContentBlockStopEvent:
				index := int(e.Index)
				log.Debug("Stream: content_block_stop", "index", index)
				// Yield the completed block
				acc.ensureBlock(index)
				blocksChan <- StreamContentBlock{Index: index, Block: acc.blocks[index]}

			case anthropic.MessageDeltaEvent:
				if e.Usage.InputTokens > 0 {
					acc.setUsage(Usage{
						InputTokens:  int(e.Usage.InputTokens),
						OutputTokens: int(e.Usage.OutputTokens),
					})
				}
				if e.Delta.StopDetails.Type != "" {
					acc.setStopReason(StopReason(e.Delta.StopDetails.Type))
				}
				log.Debug("Stream: message_delta")

			case anthropic.MessageStopEvent:
				hasMessageStop = true
				log.Debug("Stream: message_stop")
			}
		}

		// Check for stream errors
		if stream.Err() != nil {
			log.Warn("Stream error", "error", stream.Err())
			result.Error = stream.Err().Error()
		}

		// Check if we need fallback
		shouldFallback := !hasMessageStart || !hasMessageStop || result.Error != ""
		if shouldFallback {
			log.Warn("Stream incomplete, triggering fallback", "hasMessageStart", hasMessageStart, "hasMessageStop", hasMessageStop, "error", result.Error)
			if onStreamingFallback != nil {
				cancel()
				resp, err := onStreamingFallback(ctx)
				if err != nil {
					result.Error = err.Error()
					return
				}
				// Convert non-streaming response to stream result
				result.Blocks = resp.Content
				result.StopReason = resp.StopReason
				result.Usage = resp.Usage
				return
			}
		}

		// Stream completed successfully
		result.Blocks = acc.getBlocks()
		result.StopReason = acc.stopReason
		result.Usage = acc.usage
	}()

	return blocksChan, result
}

// ToolParam represents a tool parameter for the API.
type ToolParam struct {
	Name        string
	Description string
	InputSchema ToolInputSchema
}

// ToolInputSchema represents the input schema for a tool.
type ToolInputSchema struct {
	Type       string
	Properties map[string]any
	Required   []string
}
