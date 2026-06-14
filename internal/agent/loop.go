// Package agent provides the core agent loop.
package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/cli"
	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// Ensure ReadFileCache type is used (via StreamConfig field)
var _ *tool.ReadFileCache

// chainParticipantTypes are entry types that produce chain participant messages
// in RebuildMessages. These are the types that generate non-empty API messages.
var chainParticipantTypes = map[string]bool{
	session.EntryTypeUser:       true,
	session.EntryTypeAssistant:  true,
	session.EntryTypeToolResult: true,
}

// systemMessageTypes are entry types that become system role messages in the API chain.
// These are preserved in RebuildMessages to maintain session context markers.
var systemMessageTypes = map[string]bool{
	session.EntryTypeSystem: true,
}

// HasChainMessages reports whether at least one entry produces a chain participant
// message (user, assistant, tool_result) after filtering progress/ephemeral types.
// This is used to reject queue-only/empty transcripts during resume.
func HasChainMessages(entries []session.TranscriptEntry) bool {
	for _, entry := range entries {
		if chainParticipantTypes[entry.Type] {
			return true
		}
	}
	return false
}

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
		case session.EntryTypeUser:
			// Flush any pending assistant message
			if currentAssistant != nil {
				messages = append(messages, *currentAssistant)
				currentAssistant = nil
			}
			messages = append(messages, api.Message{
				Role:    api.RoleUser,
				Content: entry.Content,
			})

		case session.EntryTypeAssistant:
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
				Role:      api.RoleAssistant,
				Content:   entry.Content,
				ToolUse:   toolUseBlocks,
				Thinking:  entry.Thinking,
				Signature: entry.Signature,
			}

		case session.EntryTypeToolResult:
			// Tool results must be in a user message, not attached to assistant's tool_use.
			// Flush any pending assistant message first (tool_use goes in assistant, tool_result in user).
			if currentAssistant != nil {
				messages = append(messages, *currentAssistant)
				currentAssistant = nil
			}
			messages = append(messages, api.Message{
				Role: api.RoleUser,
				ToolResults: []api.ToolResultBlock{
					{
						ToolUseID: entry.ToolID,
						Content:   entry.Content,
						IsError:   entry.IsError,
					},
				},
			})

		case session.EntryTypeSystem:
			// Flush any pending assistant message first to maintain ordering
			if currentAssistant != nil {
				messages = append(messages, *currentAssistant)
				currentAssistant = nil
			}
			switch {
			case entry.Subtype == session.SubtypeCompactBoundary && entry.CompactMetadata != nil && entry.CompactMetadata.Summary != "":
				messages = append(messages, api.Message{
					Role:    api.RoleSystem,
					Content: compactSummaryPrefix + entry.CompactMetadata.Summary,
				})
			case entry.Subtype == session.SubtypeSystemReminder && entry.Content != "":
				// Restore system_reminder as a virtual user message so it appears
				// in the message chain identically to when it was first injected.
				messages = append(messages, api.Message{
					Role:      api.RoleUser,
					Content:   "[system]: " + entry.Content,
					IsVirtual: true,
				})
			case entry.Content != "":
				messages = append(messages, api.Message{
					Role:    api.RoleSystem,
					Content: entry.Content,
				})
			}
		}
	}

	// Flush any pending assistant message
	if currentAssistant != nil {
		messages = append(messages, *currentAssistant)
	}

	return messages
}

// defaultSystemPrompt is the system prompt sent to the API.
const defaultSystemPrompt = "You are an autonomous AI assistant with tools to search, read, write, and execute safe operations. You operate in a non-interactive mode."

// Run executes the agent loop with the given prompt and tools.
// maxIterations controls the maximum loop iterations; <= 0 means unlimited.
func Run(ctx context.Context, prompt string, tools []tool.Tool, cwd string, maxIterations int) (string, error) {
	// Create API client
	client, err := api.NewClient()
	if err != nil {
		return "", fmt.Errorf("failed to create API client: %v", err)
	}

	// Get working directory
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			cwd, _ = os.UserHomeDir()
		}
	}

	// System prompt (sent as top-level parameter, not as a role:system message)
	systemPrompt := defaultSystemPrompt

	// Initialize messages with user message only (system prompt goes to top-level parameter)
	messages := []api.Message{
		{
			Role:    api.RoleUser,
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
		} else if reqAny, ok := schema["required"].([]any); ok {
			for _, r := range reqAny {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}
		// Extract extra fields ($defs, $schema, etc.) for third-party API compatibility
		extraFields := make(map[string]any)
		for k, v := range schema {
			if k != "type" && k != "properties" && k != "required" {
				extraFields[k] = v
			}
		}

		apiTools = append(apiTools, ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: ToolInputSchema{
				Type:        "object",
				Properties:  props,
				Required:    required,
				ExtraFields: extraFields,
			},
		})
	}

	// Main agent loop
	for i := 0; maxIterations <= 0 || i < maxIterations; i++ {
		// Send message to API (pass nil for toolResults as we include them in messages)
		resp, err := client.SendMessage(ctx, messages, apiTools, nil, []string{systemPrompt}, "")
		if err != nil {
			return "", fmt.Errorf("API error: %v", err)
		}

		// Process response content
		var textOutput strings.Builder
		var toolResults []api.ToolResult
		var toolUseBlocks []api.ToolUseBlock

		for _, block := range resp.Content {
			switch block.Type {
			case api.BlockTypeText:
				textOutput.WriteString(block.Text)
			case api.BlockTypeToolUse:
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
				result, err := t.Execute(ctx, block.ToolInput, cwd)
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
			Role:    api.RoleAssistant,
			Content: textOutput.String(),
		}
		if len(toolUseBlocks) > 0 {
			assistantMsg.ToolUse = toolUseBlocks
		}
		if textOutput.String() != "" || len(toolUseBlocks) > 0 {
			messages = append(messages, assistantMsg)
		}

		// Handle stop reason
		switch resp.StopReason {
		case api.StopReasonEndTurn:
			if len(toolResults) > 0 {
				// Send tool results back to model before ending
				userMsg := api.Message{
					Role:        api.RoleUser,
					ToolResults: make([]api.ToolResultBlock, 0, len(toolResults)),
				}
				for _, tr := range toolResults {
					userMsg.ToolResults = append(userMsg.ToolResults, api.ToolResultBlock{
						ToolUseID: tr.ToolUseID,
						Content:   tr.Content,
						IsError:   tr.IsError,
					})
				}
				messages = append(messages, userMsg)
				// Don't continue - end_turn means the model is done
				// Return textOutput which may be empty if model already provided final answer
				return textOutput.String(), nil
			}
			return textOutput.String(), nil

		case api.StopReasonToolUse:
			// Continue the loop to let the model process tool results
			if len(toolResults) > 0 {
				userMsg := api.Message{
					Role:        api.RoleUser,
					ToolResults: make([]api.ToolResultBlock, 0, len(toolResults)),
				}
				for _, tr := range toolResults {
					userMsg.ToolResults = append(userMsg.ToolResults, api.ToolResultBlock{
						ToolUseID: tr.ToolUseID,
						Content:   tr.Content,
						IsError:   tr.IsError,
					})
				}
				messages = append(messages, userMsg)
			}
			continue

		case api.StopReasonMaxTokens:
			return textOutput.String(), fmt.Errorf("max tokens reached")

		case api.StopReasonStopSeq:
			return textOutput.String(), nil
		}

		// If we get here without text output and without tool results, something is wrong
		if textOutput.String() == "" && len(toolResults) == 0 && len(toolUseBlocks) == 0 {
			return "", fmt.Errorf("unexpected empty response")
		}
	}

	return "", fmt.Errorf("max iterations (%d) exceeded", maxIterations)
}

// RunSimple is a simpler version of Run that handles basic text interactions.
// maxIterations controls the maximum loop iterations; <= 0 means unlimited.
func RunSimple(ctx context.Context, prompt string, tools []tool.Tool, maxIterations int) (string, error) {
	return Run(ctx, prompt, tools, "", maxIterations)
}

// RunStream executes the agent loop with streaming JSON output.
// It outputs NDJSON lines to stdout for each message.
// Uses SSE streaming for API calls when cfg.Enabled is true.
func RunStream(ctx context.Context, prompt string, tools []tool.Tool, cwd string, cfg *StreamConfig, model string, opts ...QueryEngineOption) (string, string, error) {
	// Use provided session ID or generate a new one
	sessionID := cfg.SessionID
	if sessionID == "" {
		newSessionID, err := SessionID()
		if err != nil {
			return "", "", fmt.Errorf("generating session ID: %v", err)
		}
		sessionID = newSessionID
		cfg.SessionID = sessionID
	}

	// AC1: Store IsForkChild in context so tools can check it
	ctx = context.WithValue(ctx, tool.ForkChildKey, cfg.IsForkChild)

	// AC1: Store IsNamedAgent in context so tools can check it (blocks nested named agents)
	ctx = context.WithValue(ctx, tool.NamedAgentKey, cfg.IsNamedAgent)

	// Create QueryEngine - it handles API client creation, cost state restoration,
	// tool parameter conversion, and the agent loop lifecycle
	engine, err := NewQueryEngine(cfg, tools, model, append(opts, WithCWD(cwd))...)
	if err != nil {
		return "", "", err
	}

	// Emit system/init line once at start of stream-json mode (AC1-AC6)
	if cfg.Enabled {
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name()
		}
		// Collect MCP server names from config
		mcpServerNames := make([]string, 0, len(cfg.MCPConfig))
		for name := range cfg.MCPConfig {
			mcpServerNames = append(mcpServerNames, name)
		}
		initMsg := cli.StreamMessage{
			Type:              "system",
			Subtype:           "init",
			SessionID:         sessionID,
			ParentToolUseID:   nil,
			Uuid:              GenerateUUID(),
			Model:             engine.Model(),
			CWD:               cwd,
			Tools:             toolNames,
			ClaudeCodeVersion: constants.Version,
			PermissionMode:    "default",
			FastModeState:     "off",
			OutputStyle:      "default",
			MCPServers:       mcpServerNames,
		}
		_ = cli.WriteStreamJSON(initMsg)
	}

	// AC4: Delegate to QueryEngine.SubmitMessage which handles:
	// - Persist-before-API ordering (AC1)
	// - Turn counter management (AC5)
	// - MaxTurns enforcement (AC2)
	// - Budget enforcement (AC2)
	// - Cost accumulation and flush (AC3)
	// - Stream-json emission, SSE streaming, tool execution
	result, err := engine.SubmitMessage(ctx, prompt)

	return result, sessionID, err
}
