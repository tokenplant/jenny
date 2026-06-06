// Package agent provides the core agent loop.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// RebuildMessages converts transcript entries to API messages for resume.
// This is used when resuming a session with -r flag to reconstruct the
// conversation history from the persisted transcript.
//
// Message ordering rules:
//   - User messages are appended directly
//   - Assistant messages with tool_use are held in pending state
//   - When a tool_result is encountered, the pending assistant is flushed first,
//     then the tool_result is placed in a new user message (per API spec)
//   - Final assistant messages without tool_use are flushed at end
func RebuildMessages(entries []session.TranscriptEntry) []api.Message {
	var messages []api.Message
	var currentAssistant *api.Message

	for _, entry := range entries {
		switch entry.Type {
		case "user":
			// Flush any pending assistant message
			if currentAssistant != nil {
				messages = append(messages, *currentAssistant)
				currentAssistant = nil
			}
			messages = append(messages, api.Message{
				Role:    "user",
				Content: entry.Content,
			})

		case "assistant":
			// Flush any pending assistant message
			if currentAssistant != nil {
				messages = append(messages, *currentAssistant)
			}
			toolUseBlocks := make([]api.ToolUseBlock, 0, len(entry.ToolUse))
			for _, tu := range entry.ToolUse {
				toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: tu.Input,
				})
			}
			currentAssistant = &api.Message{
				Role:    "assistant",
				Content: entry.Content,
				ToolUse: toolUseBlocks,
			}

		case "tool_result":
			// Tool results must be in a user message, not attached to assistant's tool_use.
			// Flush any pending assistant message first (tool_use goes in assistant, tool_result in user).
			if currentAssistant != nil {
				messages = append(messages, *currentAssistant)
				currentAssistant = nil
			}
			// Create a user message with the tool result
			messages = append(messages, api.Message{
				Role: "user",
				ToolResults: []api.ToolResultBlock{
					{
						ToolUseID: entry.ToolID,
						Content:   entry.Content,
					},
				},
			})
		}
	}

	// Flush any pending assistant message
	if currentAssistant != nil {
		messages = append(messages, *currentAssistant)
	}

	return messages
}

// MaxIterations is the maximum number of loop iterations to prevent infinite loops.
const MaxIterations = 100

// defaultSystemPrompt is the system prompt sent to the API.
const defaultSystemPrompt = "You are an AI assistant that can use tools to help answer user questions. When you use tools, carefully review the results and incorporate them into your response."

// SessionID generates a new session ID using the session package.
// Returns an error if UUID generation fails.
func SessionID() (string, error) {
	id, err := session.NewSessionID()
	if err != nil {
		return "", fmt.Errorf("generating session ID: %w", err)
	}
	return id, nil
}

// StreamConfig holds configuration for streaming output.
type StreamConfig struct {
	Enabled         bool
	Verbose         bool
	IncludePartial  bool
	SessionID       string
	SessionManager  *session.Manager
	HistoryMessages []api.Message // Messages loaded from transcript for resume
	IsResume        bool          // True when resuming an existing session (skip duplicate user message persistence)
}

// ToolParam represents a tool parameter for the API.
type ToolParam = api.ToolParam

// ToolInputSchema represents the input schema for a tool.
type ToolInputSchema = api.ToolInputSchema

// Message represents a message in the conversation.
type Message = api.Message

// Run executes the agent loop with the given prompt and tools.
func Run(ctx context.Context, prompt string, tools []tool.Tool, cwd string) (string, error) {
	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return "", fmt.Errorf("failed to create API client: %v", err)
	}

	// Get working directory
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			cwd = "/"
		}
	}

	// System prompt (sent as top-level parameter, not as a role:system message)
	systemPrompt := defaultSystemPrompt

	// Initialize messages with user message only (system prompt goes to top-level parameter)
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	// Convert tools to API format
	apiTools := make([]ToolParam, 0, len(tools))
	for _, t := range tools {
		schema := t.InputSchema()
		props := make(map[string]any)
		if p, ok := schema["properties"].(map[string]any); ok {
			props = p
		}
		var required []string
		if req, ok := schema["required"].([]string); ok {
			required = req
		}
		apiTools = append(apiTools, ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		})
	}

	// Main agent loop
	for i := 0; i < MaxIterations; i++ {
		// Send message to API (pass nil for toolResults as we include them in messages)
		resp, err := client.SendMessage(ctx, messages, apiTools, nil, systemPrompt)
		if err != nil {
			return "", fmt.Errorf("API error: %v", err)
		}

		// Process response content
		var textOutput string
		var toolResults []api.ToolResult
		var toolUseBlocks []api.ToolUseBlock

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				textOutput += block.Text
			case "tool_use":
				// Collect tool_use blocks for the assistant message
				toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
					ID:    block.ToolID,
					Name:  block.ToolName,
					Input: block.ToolInput,
				})

				// Find and execute the tool
				t := tool.FindTool(tools, block.ToolName)
				if t == nil {
					toolResults = append(toolResults, api.ToolResult{
						ToolUseID: block.ToolID,
						Content:   fmt.Sprintf("Error: Unknown tool '%s'", block.ToolName),
						IsError:   true,
					})
					continue
				}

				// Execute tool
				result, err := t.Execute(block.ToolInput, cwd)
				if err != nil {
					toolResults = append(toolResults, api.ToolResult{
						ToolUseID: block.ToolID,
						Content:   fmt.Sprintf("Error executing tool: %v", err),
						IsError:   true,
					})
					continue
				}

				toolResults = append(toolResults, api.ToolResult{
					ToolUseID: block.ToolID,
					Content:   result.Content,
					IsError:   result.IsError,
				})
			}
		}

		// Build and append assistant message with text and tool_use blocks
		assistantMsg := api.Message{
			Role:    "assistant",
			Content: textOutput,
		}
		if len(toolUseBlocks) > 0 {
			assistantMsg.ToolUse = toolUseBlocks
		}
		if textOutput != "" || len(toolUseBlocks) > 0 {
			messages = append(messages, assistantMsg)
		}

		// Handle stop reason
		switch resp.StopReason {
		case api.StopReasonEndTurn:
			if len(toolResults) > 0 {
				// Send tool results back to model before ending
				userMsg := api.Message{
					Role:        "user",
					ToolResults: make([]api.ToolResultBlock, 0, len(toolResults)),
				}
				for _, tr := range toolResults {
					userMsg.ToolResults = append(userMsg.ToolResults, api.ToolResultBlock{
						ToolUseID: tr.ToolUseID,
						Content:   tr.Content,
					})
				}
				messages = append(messages, userMsg)
				// Don't continue - end_turn means the model is done
				// Return textOutput which may be empty if model already provided final answer
				return textOutput, nil
			}
			return textOutput, nil

		case api.StopReasonToolUse:
			// Continue the loop to let the model process tool results
			if len(toolResults) > 0 {
				userMsg := api.Message{
					Role:        "user",
					ToolResults: make([]api.ToolResultBlock, 0, len(toolResults)),
				}
				for _, tr := range toolResults {
					userMsg.ToolResults = append(userMsg.ToolResults, api.ToolResultBlock{
						ToolUseID: tr.ToolUseID,
						Content:   tr.Content,
					})
				}
				messages = append(messages, userMsg)
			}
			continue

		case api.StopReasonMaxTokens:
			return textOutput, fmt.Errorf("max tokens reached")

		case api.StopReasonStopSeq:
			return textOutput, nil
		}

		// If we get here without text output and without tool results, something is wrong
		if textOutput == "" && len(toolResults) == 0 && len(toolUseBlocks) == 0 {
			return "", fmt.Errorf("unexpected empty response")
		}
	}

	return "", fmt.Errorf("max iterations (%d) exceeded", MaxIterations)
}

// RunSimple is a simpler version of Run that handles basic text interactions.
func RunSimple(ctx context.Context, prompt string, tools []tool.Tool) (string, error) {
	return Run(ctx, prompt, tools, "")
}

// StreamMessage represents a message in the stream-json output.
type StreamMessage struct {
	Type       string `json:"type"`
	Content    string `json:"content,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Result     string `json:"result,omitempty"`
	Model      string `json:"model,omitempty"`
	Usage      *Usage `json:"usage,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolInput  any    `json:"tool_input,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	IsPartial  bool   `json:"is_partial,omitempty"`
	MessageIdx int    `json:"message_idx,omitempty"`
}

// Usage represents token usage information for streaming output.
type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// RunStream executes the agent loop with streaming JSON output.
// It outputs NDJSON lines to stdout for each message.
// Uses SSE streaming for API calls when cfg.Enabled is true.
func RunStream(ctx context.Context, prompt string, tools []tool.Tool, cwd string, cfg StreamConfig, model string) (string, string, error) {
	// Use provided session ID or generate a new one
	sessionID := cfg.SessionID
	if sessionID == "" {
		newSessionID, err := SessionID()
		if err != nil {
			return "", "", fmt.Errorf("generating session ID: %v", err)
		}
		sessionID = newSessionID
	}

	// Create API client with optional model override
	client, err := api.NewClientWithModel(model)
	if err != nil {
		return "", "", fmt.Errorf("failed to create API client: %v", err)
	}

	// Get working directory
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			cwd = "/"
		}
	}

	// System prompt (sent as top-level parameter, not as a role:system message)
	systemPrompt := defaultSystemPrompt

	// Initialize messages: use history if resuming, otherwise create new user message
	var messages []api.Message
	if len(cfg.HistoryMessages) > 0 {
		messages = cfg.HistoryMessages
		// Append the new prompt as a user message
		messages = append(messages, api.Message{
			Role:    "user",
			Content: prompt,
		})
		// For resumed sessions, check if the user message is a duplicate of one already in the transcript
		skipUserPersist := false
		if cfg.SessionManager != nil && cfg.IsResume {
			exists, err := cfg.SessionManager.UserMessageExists(sessionID, prompt)
			if err != nil {
				return "", "", fmt.Errorf("checking for duplicate user message: %w", err)
			}
			skipUserPersist = exists
		}
		// Persist user message to transcript unless it's a duplicate
		if cfg.SessionManager != nil && !skipUserPersist {
			if err := cfg.SessionManager.AppendEntry(sessionID, session.TranscriptEntry{
				Type:    "user",
				Content: prompt,
			}); err != nil {
				return "", "", fmt.Errorf("persisting user message to transcript: %w", err)
			}
		}
	} else {
		messages = []api.Message{
			{
				Role:    "user",
				Content: prompt,
			},
		}
		// Persist initial user message to transcript (only for new sessions)
		if cfg.SessionManager != nil {
			if err := cfg.SessionManager.AppendEntry(sessionID, session.TranscriptEntry{
				Type:    "user",
				Content: prompt,
			}); err != nil {
				return "", "", fmt.Errorf("persisting user message to transcript: %w", err)
			}
		}
	}

	// Convert tools to API format
	apiTools := make([]ToolParam, 0, len(tools))
	for _, t := range tools {
		schema := t.InputSchema()
		props := make(map[string]any)
		if p, ok := schema["properties"].(map[string]any); ok {
			props = p
		}
		var required []string
		if req, ok := schema["required"].([]string); ok {
			required = req
		}
		apiTools = append(apiTools, ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		})
	}

	// Main agent loop
	for i := 0; i < MaxIterations; i++ {
		// Emit stream_request_start before each API iteration (AC4)
		if cfg.Enabled {
			msg := StreamMessage{
				Type: "stream_request_start",
			}
			data, _ := json.Marshal(msg)
			fmt.Fprintln(os.Stdout, string(data))
		}

		// Create fallback function for streaming failures (AC3)
		fallbackFn := func(fallbackCtx context.Context) (*api.Response, error) {
			return client.SendMessage(fallbackCtx, messages, apiTools, nil, systemPrompt)
		}

		// Use streaming API (AC1)
		blocksChan, streamResult := client.SendMessageStream(
			ctx,
			messages,
			apiTools,
			nil,
			systemPrompt,
			api.DefaultIdleTimeout,
			api.DefaultFallbackTimeout,
			fallbackFn,
		)

		// Process streaming blocks
		var textOutput string
		var toolResults []api.ToolResult
		var toolUseBlocks []api.ToolUseBlock
		var modelName string

		// Process blocks as they arrive
		for block := range blocksChan {
			switch block.Block.Type {
			case "text":
				textOutput += block.Block.Text
				if cfg.Enabled && cfg.IncludePartial {
					// Output partial text as we receive it
					msg := StreamMessage{
						Type:       "message",
						Content:    block.Block.Text,
						SessionID:  sessionID,
						IsPartial:  true,
						MessageIdx: i,
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
			case "tool_use":
				// Collect tool_use blocks for the assistant message
				toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
					ID:    block.Block.ToolID,
					Name:  block.Block.ToolName,
					Input: block.Block.ToolInput,
				})

				if cfg.Enabled {
					// Output tool use event
					msg := StreamMessage{
						Type:       "tool_use",
						SessionID:  sessionID,
						ToolName:   block.Block.ToolName,
						ToolInput:  block.Block.ToolInput,
						MessageIdx: i,
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
			}
		}

		// Check if streaming completed with error
		if streamResult.Error != "" && len(streamResult.Blocks) == 0 {
			return "", sessionID, fmt.Errorf("streaming error: %v", streamResult.Error)
		}

		// Use results from streaming (or fallback)
		resp := &api.Response{
			Content:    streamResult.Blocks,
			StopReason: streamResult.StopReason,
			Usage:      streamResult.Usage,
		}
		if resp.Model == "" {
			resp.Model = modelName
		}

		// Build and append assistant message with text and tool_use blocks
		assistantMsg := api.Message{
			Role:    "assistant",
			Content: textOutput,
		}
		if len(toolUseBlocks) > 0 {
			assistantMsg.ToolUse = toolUseBlocks
		}
		if textOutput != "" || len(toolUseBlocks) > 0 {
			messages = append(messages, assistantMsg)
		}

		// Persist assistant message to transcript BEFORE tool execution (AC3 ordering)
		if cfg.SessionManager != nil && (textOutput != "" || len(toolUseBlocks) > 0) {
			entry := session.TranscriptEntry{
				Type:    "assistant",
				Content: textOutput,
			}
			for _, tu := range toolUseBlocks {
				entry.ToolUse = append(entry.ToolUse, session.ToolUse{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: tu.Input,
				})
			}
			if err := cfg.SessionManager.AppendEntry(sessionID, entry); err != nil {
				return "", "", fmt.Errorf("persisting assistant message to transcript: %w", err)
			}
		}

		// Now execute all tools and collect results
		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}

			// Find and execute the tool
			t := tool.FindTool(tools, block.ToolName)
			var result *tool.ToolResult
			var errContent string

			if t == nil {
				errContent = fmt.Sprintf("Error: Unknown tool '%s'", block.ToolName)
				toolResults = append(toolResults, api.ToolResult{
					ToolUseID: block.ToolID,
					Content:   errContent,
					IsError:   true,
				})
			} else {
				// Execute tool
				execResult, err := t.Execute(block.ToolInput, cwd)
				if err != nil {
					errContent = fmt.Sprintf("Error executing tool: %v", err)
					toolResults = append(toolResults, api.ToolResult{
						ToolUseID: block.ToolID,
						Content:   errContent,
						IsError:   true,
					})
				} else {
					result = execResult
					toolResults = append(toolResults, api.ToolResult{
						ToolUseID: block.ToolID,
						Content:   result.Content,
						IsError:   result.IsError,
					})
				}
			}

			// Persist tool result to transcript AFTER assistant message (AC3 ordering)
			entryContent := errContent
			isError := true
			if result != nil {
				entryContent = result.Content
				isError = result.IsError
			}
			if cfg.SessionManager != nil {
				if err := cfg.SessionManager.AppendEntry(sessionID, session.TranscriptEntry{
					Type:    "tool_result",
					ToolID:  block.ToolID,
					Content: entryContent,
					IsError: isError,
				}); err != nil {
					return "", "", fmt.Errorf("persisting tool result to transcript: %w", err)
				}
			}

			if cfg.Enabled {
				// Output tool result event
				msg := StreamMessage{
					Type:       "tool_result",
					SessionID:  sessionID,
					Content:    entryContent,
					IsError:    isError,
					MessageIdx: i,
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
		}

		// Handle stop reason
		switch resp.StopReason {
		case api.StopReasonEndTurn:
			if len(toolResults) > 0 {
				// Send tool results back to model before ending
				userMsg := api.Message{
					Role:        "user",
					ToolResults: make([]api.ToolResultBlock, 0, len(toolResults)),
				}
				for _, tr := range toolResults {
					userMsg.ToolResults = append(userMsg.ToolResults, api.ToolResultBlock{
						ToolUseID: tr.ToolUseID,
						Content:   tr.Content,
					})
				}
				messages = append(messages, userMsg)
				// end_turn means the model is done - output final result
				if cfg.Enabled {
					msg := StreamMessage{
						Type:      "result",
						Result:    textOutput,
						SessionID: sessionID,
						Model:     resp.Model,
						Usage: &Usage{
							InputTokens:  resp.Usage.InputTokens,
							OutputTokens: resp.Usage.OutputTokens,
						},
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
				return textOutput, sessionID, nil
			}
			// Output final result
			if cfg.Enabled {
				msg := StreamMessage{
					Type:      "result",
					Result:    textOutput,
					SessionID: sessionID,
					Model:     resp.Model,
					Usage: &Usage{
						InputTokens:  resp.Usage.InputTokens,
						OutputTokens: resp.Usage.OutputTokens,
					},
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			return textOutput, sessionID, nil

		case api.StopReasonToolUse:
			// Continue the loop to let the model process tool results
			if len(toolResults) > 0 {
				userMsg := api.Message{
					Role:        "user",
					ToolResults: make([]api.ToolResultBlock, 0, len(toolResults)),
				}
				for _, tr := range toolResults {
					userMsg.ToolResults = append(userMsg.ToolResults, api.ToolResultBlock{
						ToolUseID: tr.ToolUseID,
						Content:   tr.Content,
					})
				}
				messages = append(messages, userMsg)
			}
			continue

		case api.StopReasonMaxTokens:
			return textOutput, sessionID, fmt.Errorf("max tokens reached")

		case api.StopReasonStopSeq:
			if cfg.Enabled {
				msg := StreamMessage{
					Type:      "result",
					Result:    textOutput,
					SessionID: sessionID,
					Model:     resp.Model,
					Usage: &Usage{
						InputTokens:  resp.Usage.InputTokens,
						OutputTokens: resp.Usage.OutputTokens,
					},
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			return textOutput, sessionID, nil
		}

		// If we get here without text output and without tool results, something is wrong
		if textOutput == "" && len(toolResults) == 0 && len(toolUseBlocks) == 0 {
			return "", sessionID, fmt.Errorf("unexpected empty response")
		}
	}

	return "", sessionID, fmt.Errorf("max iterations (%d) exceeded", MaxIterations)
}
