// Package agent provides the core agent loop.
package agent

import (
	"context"
	crypto_rand "crypto/rand"
	"fmt"
	"os"
	"strings"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/cli"
	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/mcp"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
)

// Ensure ReadFileCache type is used (via StreamConfig field)
var _ *tool.ReadFileCache

// chainParticipantTypes are entry types that produce chain participant messages
// in RebuildMessages. These are the types that generate non-empty API messages.
var chainParticipantTypes = map[string]bool{
	"user":        true,
	"assistant":   true,
	"tool_result": true,
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
	Enabled              bool
	Verbose              bool
	IncludePartial       bool
	SessionID            string
	SessionManager       *session.Manager
	HistoryMessages      []api.Message               // Messages loaded from transcript for resume
	IsResume             bool                        // True when resuming an existing session (skip duplicate user message persistence)
	MaxBudgetUSD         float64                     // Budget limit in USD (0 = no limit)
	MaxBudgetCNY         float64                     // Budget limit in CNY (0 = no limit)
	MaxTurns             int                         // Maximum turns (0 = unlimited)
	MCPConfig            map[string]mcp.MCPServerDef // Loaded MCP server configurations
	CustomSystemPrompt   string                      // Custom system prompt; replaces defaults when set
	AppendSystemPrompt   string                      // Content appended after assembled prompt
	OverrideSystemPrompt bool                        // When true, suppresses AppendSystemPrompt
	ReadFileCache        *tool.ReadFileCache         // Cache for read-before-write enforcement
	AutoMemoryEnabled    bool                        // Whether auto-memory is enabled
	MemoryContent        string                      // Memory content to inject into system prompt
	Skills               []skills.Skill              // Discovered skills for manifest
	IsForkChild          bool                        // True when this session is a fork child (subagent spawned another agent)
	StructuredSchema     map[string]any              // JSON schema for structured output (AC1, AC4: non-interactive only)
	StructuredDenyRules  []string                    // Tool names to deny; checked by engine to enforce AC1
	IsNamedAgent         bool                        // True when this session is a named swarm agent
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
	for range MaxIterations {
		// Send message to API (pass nil for toolResults as we include them in messages)
		resp, err := client.SendMessage(ctx, messages, apiTools, nil, systemPrompt)
		if err != nil {
			return "", fmt.Errorf("API error: %v", err)
		}

		// Process response content
		var textOutput strings.Builder
		var toolResults []api.ToolResult
		var toolUseBlocks []api.ToolUseBlock

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				textOutput.WriteString(block.Text)
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
				result, err := t.Execute(context.Background(), block.ToolInput, cwd)
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
				return textOutput.String(), nil
			}
			return textOutput.String(), nil

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
			return textOutput.String(), fmt.Errorf("max tokens reached")

		case api.StopReasonStopSeq:
			return textOutput.String(), nil
		}

		// If we get here without text output and without tool results, something is wrong
		if textOutput.String() == "" && len(toolResults) == 0 && len(toolUseBlocks) == 0 {
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
// Field order matches the headless-agent reference format: type, then event|message|payload,
// then session_id, parent_tool_use_id, uuid, then remaining fields.
type StreamMessage struct {
	Type              string             `json:"type"`
	Subtype           string             `json:"subtype,omitempty"`
	IsError           bool               `json:"is_error"`
	DurationMs        int64              `json:"duration_ms,omitempty"`
	DurationAPIMs     int64              `json:"duration_api_ms,omitempty"`
	NumTurns          int                `json:"num_turns,omitempty"`
	Result            string             `json:"result,omitempty"`
	StopReason        string             `json:"stop_reason,omitempty"`
	SessionID         string             `json:"session_id,omitempty"`
	ParentToolUseID   *string            `json:"parent_tool_use_id"`
	TotalCostUSD      float64            `json:"total_cost_usd,omitempty"`
	TotalCostCNY      float64            `json:"total_cost_cny,omitempty"`
	Uuid              string             `json:"uuid,omitempty"`
	Usage             *Usage             `json:"usage,omitempty"`
	ModelUsage        any                `json:"modelUsage,omitempty"`
	PermissionDenials []PermissionDenial `json:"permission_denials,omitempty"`
	FastModeState     string             `json:"fast_mode_state,omitempty"`
	// Legacy/optional fields (not in result reference but used by other event types)
	Event          any                   `json:"event,omitempty"`
	Message        any                   `json:"message,omitempty"`
	Content        string                `json:"content,omitempty"`
	Model          string                `json:"model,omitempty"`
	ToolName       string                `json:"tool_name,omitempty"`
	ToolInput      any                   `json:"input,omitempty"`
	ToolUseID      string                `json:"tool_use_id,omitempty"`
	IsPartial      bool                  `json:"is_partial,omitempty"`
	ErrorMaxTokens *ErrorMaxTokensDetail `json:"error_max_tokens,omitempty"`
	Timestamp      string                `json:"timestamp,omitempty"`
	ToolUseResult  any                   `json:"tool_use_result,omitempty"`
}

// PermissionDenial represents a tool use denial for permission_denials array.
type PermissionDenial struct {
	ToolName  string `json:"tool_name,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	ToolInput any    `json:"tool_input,omitempty"`
}

// ErrorMaxTokensDetail holds structured information for error_max_tokens result events.
type ErrorMaxTokensDetail struct {
	Category        string `json:"category"`
	OutputTokens    int    `json:"output_tokens,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	InputTokens     int    `json:"input_tokens,omitempty"`
	Threshold       int    `json:"threshold,omitempty"`
}

// GenerateUUID generates a random UUID v4.
func GenerateUUID() string {
	b := make([]byte, 16)
	_, _ = crypto_rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Usage represents token usage information for streaming output.
type Usage struct {
	InputTokens              int            `json:"input_tokens"`
	OutputTokens             int            `json:"output_tokens"`
	CacheReadInputTokens     int            `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int            `json:"cache_creation_input_tokens,omitempty"`
	ServerToolUse            *ServerToolUse `json:"server_tool_use"`
	ServiceTier              string         `json:"service_tier,omitempty"`
	CacheCreation            *CacheCreation `json:"cache_creation"`
	InferenceGeo             string         `json:"inference_geo,omitempty"`
	Iterations               []any          `json:"iterations,omitempty"`
	Speed                    string         `json:"speed,omitempty"`
}

// ServerToolUse represents server-side tool use statistics.
type ServerToolUse struct {
	WebSearchRequests int `json:"web_search_requests"`
	WebFetchRequests  int `json:"web_fetch_requests"`
}

// CacheCreation represents cache creation token statistics.
type CacheCreation struct {
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
}

// RunStream executes the agent loop with streaming JSON output.
// It outputs NDJSON lines to stdout for each message.
// Uses SSE streaming for API calls when cfg.Enabled is true.
// AC4: Refactored to delegate to QueryEngine while preserving all existing behavior.
func RunStream(ctx context.Context, prompt string, tools []tool.Tool, cwd string, cfg StreamConfig, model string) (string, string, error) {
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
	engine := NewQueryEngine(cfg, tools, model)

	// Emit system/init line once at start of stream-json mode (AC1-AC6)
	if cfg.Enabled {
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name()
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
