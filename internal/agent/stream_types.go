// Package agent provides the core agent loop and query engine.
package agent

import (
	crypto_rand "crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/mcp"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
)

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

// StreamMessage represents a message in the stream-json output.
// Field order matches the headless-agent reference format: type, then event|message|payload,
// then parent_tool_use_id, session_id, uuid, then remaining fields.
type StreamMessage struct {
	Type              string             `json:"type"`
	Subtype           string             `json:"subtype,omitempty"`
	IsError           bool               `json:"is_error"`
	DurationMs        int64              `json:"duration_ms,omitempty"`
	DurationAPIMs     int64              `json:"duration_api_ms,omitempty"`
	NumTurns          int                `json:"num_turns,omitempty"`
	Result            string             `json:"result,omitempty"`
	StopReason        string             `json:"stop_reason,omitempty"`
	ParentToolUseID   *string            `json:"parent_tool_use_id,omitempty"`
	SessionID         string             `json:"session_id,omitempty"`
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

// MarshalJSON implements custom marshaling for StreamMessage to:
// - Omit parent_tool_use_id for result events (per reference format)
// - Maintain correct field ordering for result events
// - Use reference order for assistant events: type, message, parent_tool_use_id, session_id, uuid
func (s StreamMessage) MarshalJSON() ([]byte, error) {
	if s.Type == "result" {
		// Reference result order: type, subtype, is_error, duration_ms, duration_api_ms,
		// num_turns, result, stop_reason, session_id, total_cost_usd, usage, modelUsage,
		// permission_denials, fast_mode_state, uuid
		var fields []string
		fields = append(fields, `"type":`+encodeString(s.Type))
		if s.Subtype != "" {
			fields = append(fields, `"subtype":`+encodeString(s.Subtype))
		}
		fields = append(fields, `"is_error":`+boolString(s.IsError))
		// Always emit duration fields per reference format (even if 0)
		fields = append(fields, fmt.Sprintf(`"duration_ms":%d`, s.DurationMs))
		fields = append(fields, fmt.Sprintf(`"duration_api_ms":%d`, s.DurationAPIMs))
		if s.NumTurns != 0 {
			fields = append(fields, fmt.Sprintf(`"num_turns":%d`, s.NumTurns))
		}
		if s.Result != "" {
			fields = append(fields, `"result":`+encodeString(s.Result))
		}
		if s.StopReason != "" {
			fields = append(fields, `"stop_reason":`+encodeString(s.StopReason))
		}
		if s.SessionID != "" {
			fields = append(fields, `"session_id":`+encodeString(s.SessionID))
		}
		if s.TotalCostUSD != 0 {
			fields = append(fields, fmt.Sprintf(`"total_cost_usd":%g`, s.TotalCostUSD))
		}
		if s.Usage != nil {
			usageBytes, err := json.Marshal(s.Usage)
			if err != nil {
				return nil, err
			}
			fields = append(fields, `"usage":`+string(usageBytes))
		}
		if s.ModelUsage != nil {
			modelBytes, err := json.Marshal(s.ModelUsage)
			if err != nil {
				return nil, err
			}
			fields = append(fields, `"modelUsage":`+string(modelBytes))
		}
		// Always emit permission_denials as empty array when not set (per reference format)
		if len(s.PermissionDenials) > 0 {
			pdBytes, err := json.Marshal(s.PermissionDenials)
			if err != nil {
				return nil, err
			}
			fields = append(fields, `"permission_denials":`+string(pdBytes))
		} else {
			fields = append(fields, `"permission_denials":[]`)
		}
		if s.FastModeState != "" {
			fields = append(fields, `"fast_mode_state":`+encodeString(s.FastModeState))
		}
		if s.Uuid != "" {
			fields = append(fields, `"uuid":`+encodeString(s.Uuid))
		}
		return []byte("{" + strings.Join(fields, ",") + "}"), nil
	}

	// Assistant events: type, message, parent_tool_use_id, session_id, uuid
	if s.Type == "assistant" {
		var fields []string
		fields = append(fields, `"type":`+encodeString(s.Type))
		if s.Message != nil {
			switch m := s.Message.(type) {
			case json.RawMessage:
				fields = append(fields, `"message":`+string(m))
			default:
				msgBytes, _ := json.Marshal(s.Message)
				fields = append(fields, `"message":`+string(msgBytes))
			}
		}
		if s.ParentToolUseID != nil {
			fields = append(fields, `"parent_tool_use_id":`+encodeString(*s.ParentToolUseID))
		} else {
			fields = append(fields, `"parent_tool_use_id":null`)
		}
		if s.SessionID != "" {
			fields = append(fields, `"session_id":`+encodeString(s.SessionID))
		}
		if s.Uuid != "" {
			fields = append(fields, `"uuid":`+encodeString(s.Uuid))
		}
		return []byte("{" + strings.Join(fields, ",") + "}"), nil
	}

	// Default marshaling for all other event types - build manually to avoid recursion
	var fields []string
	fields = append(fields, `"type":`+encodeString(s.Type))
	if s.Subtype != "" {
		fields = append(fields, `"subtype":`+encodeString(s.Subtype))
	}
	if s.IsError {
		fields = append(fields, `"is_error":true`)
	}
	if s.DurationMs != 0 {
		fields = append(fields, fmt.Sprintf(`"duration_ms":%d`, s.DurationMs))
	}
	if s.DurationAPIMs != 0 {
		fields = append(fields, fmt.Sprintf(`"duration_api_ms":%d`, s.DurationAPIMs))
	}
	if s.NumTurns != 0 {
		fields = append(fields, fmt.Sprintf(`"num_turns":%d`, s.NumTurns))
	}
	if s.Result != "" {
		fields = append(fields, `"result":`+encodeString(s.Result))
	}
	if s.StopReason != "" {
		fields = append(fields, `"stop_reason":`+encodeString(s.StopReason))
	}
	if s.ParentToolUseID != nil {
		fields = append(fields, `"parent_tool_use_id":`+encodeString(*s.ParentToolUseID))
	} else {
		fields = append(fields, `"parent_tool_use_id":null`)
	}
	if s.SessionID != "" {
		fields = append(fields, `"session_id":`+encodeString(s.SessionID))
	}
	if s.TotalCostUSD != 0 {
		fields = append(fields, fmt.Sprintf(`"total_cost_usd":%g`, s.TotalCostUSD))
	}
	if s.TotalCostCNY != 0 {
		fields = append(fields, fmt.Sprintf(`"total_cost_cny":%g`, s.TotalCostCNY))
	}
	if s.Uuid != "" {
		fields = append(fields, `"uuid":`+encodeString(s.Uuid))
	}
	if s.Usage != nil {
		usageBytes, _ := json.Marshal(s.Usage)
		fields = append(fields, `"usage":`+string(usageBytes))
	}
	if s.ModelUsage != nil {
		modelBytes, _ := json.Marshal(s.ModelUsage)
		fields = append(fields, `"modelUsage":`+string(modelBytes))
	}
	if s.Event != nil {
		eventBytes, _ := json.Marshal(s.Event)
		fields = append(fields, `"event":`+string(eventBytes))
	}
	if s.Message != nil {
		// Message is already a json.RawMessage or map - marshal appropriately
		switch m := s.Message.(type) {
		case json.RawMessage:
			fields = append(fields, `"message":`+string(m))
		default:
			msgBytes, _ := json.Marshal(s.Message)
			fields = append(fields, `"message":`+string(msgBytes))
		}
	}
	if s.Content != "" {
		fields = append(fields, `"content":`+encodeString(s.Content))
	}
	if s.Model != "" {
		fields = append(fields, `"model":`+encodeString(s.Model))
	}
	if s.ToolName != "" {
		fields = append(fields, `"tool_name":`+encodeString(s.ToolName))
	}
	if s.ToolInput != nil {
		toolBytes, _ := json.Marshal(s.ToolInput)
		fields = append(fields, `"input":`+string(toolBytes))
	}
	if s.ToolUseID != "" {
		fields = append(fields, `"tool_use_id":`+encodeString(s.ToolUseID))
	}
	if s.IsPartial {
		fields = append(fields, `"is_partial":true`)
	}
	if s.ErrorMaxTokens != nil {
		errBytes, _ := json.Marshal(s.ErrorMaxTokens)
		fields = append(fields, `"error_max_tokens":`+string(errBytes))
	}
	if s.Timestamp != "" {
		fields = append(fields, `"timestamp":`+encodeString(s.Timestamp))
	}
	if s.ToolUseResult != nil {
		toolBytes, _ := json.Marshal(s.ToolUseResult)
		fields = append(fields, `"tool_use_result":`+string(toolBytes))
	}
	return []byte("{" + strings.Join(fields, ",") + "}"), nil
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
// Field order matches the reference format for result events:
// input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens.
type Usage struct {
	InputTokens              int            `json:"input_tokens"`
	CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int            `json:"cache_read_input_tokens"`
	OutputTokens             int            `json:"output_tokens"`
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
