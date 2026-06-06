// Package api provides the Anthropic API client.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ipy/jenny/internal/log"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

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
