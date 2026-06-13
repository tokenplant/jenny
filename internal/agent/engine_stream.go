// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/tool"
)

// TurnCount returns the current turn count for diagnostics.
func (e *QueryEngine) TurnCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.turnCount
}

// Model returns the resolved model name (from flags or ANTHROPIC_MODEL env var).
func (e *QueryEngine) Model() string {
	return e.model
}

func (e *QueryEngine) buildModelUsage() any {
	if e.costState == nil || e.costState.ModelUsage == nil {
		return map[string]any{}
	}
	result := make(map[string]any)
	for model, usage := range e.costState.ModelUsage {
		params := api.ModelParams(model)
		result[model] = map[string]any{
			"inputTokens":              usage.InputTokens,
			"outputTokens":             usage.OutputTokens,
			"cacheReadInputTokens":     usage.CacheReadInputTokens,
			"cacheCreationInputTokens": usage.CacheCreationInputTokens,
			"webSearchRequests":        0,
			"contextWindow":            params.ContextWindow,
			"maxOutputTokens":          params.MaxOutputTokens,
		}
	}
	return result
}

// Drain waits for any in-progress memory extraction to complete.
// Used during shutdown to ensure clean termination.
func (e *QueryEngine) Drain(ctx context.Context) {
	if e.memExtractor == nil {
		return
	}
	e.memExtractor.Drain(ctx)
}

// drainTaskCompletions drains pending task completions from the TaskManager.
// AC3: Completions are injected as synthetic tool_results in the message chain.
func (e *QueryEngine) drainTaskCompletions() []tool.TaskCompletion {
	tm := e.getTaskManager()
	if tm == nil {
		return nil
	}
	return tm.DrainCompletions()
}

// finalizeAsEndTurn handles finalization for the end_turn stop reason and for
// empty/unrecognized stop_reason values (treated as terminal). It returns the
// final text result and nil error on success.
func (e *QueryEngine) finalizeAsEndTurn(ctx context.Context, resp *api.Response, textOutput strings.Builder, sessionID string, assistantMsg *api.Message, messages []api.Message) (string, error) {
	// AC3: Enforce structured output at end of turn
	if e.structuredOutputTool != nil && !e.structuredOutputTool.IsEmitted() {
		return "", fmt.Errorf("structured output not emitted")
	}
	// AC3: Determine final result - use structured output if available
	finalResult := textOutput.String()
	if e.structuredOutputTool != nil && e.structuredOutputTool.IsEmitted() && e.structuredOutputResult != "" {
		finalResult = e.structuredOutputResult
	}
	// Output final result
	if e.streamCfg.Enabled {
		usage := &Usage{
			InputTokens:              resp.Usage.InputTokens,
			OutputTokens:             resp.Usage.OutputTokens,
			CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
			ServerToolUse:            &ServerToolUse{},
			ServiceTier:              "standard",
			CacheCreation:            &CacheCreation{},
			InferenceGeo:             "",
			Iterations:               []any{},
			Speed:                    "standard",
		}
		msg := StreamMessage{
			Type:            "result",
			Subtype:         "success",
			Result:          finalResult,
			SessionID:       sessionID,
			ParentToolUseID: nil,
			Uuid:            GenerateUUID(),
			Usage:           usage,
			IsError:         false,
			StopReason:      string(resp.StopReason),
			DurationMs:      time.Since(e.startTime).Milliseconds(),
			DurationAPIMs:   e.totalAPIDurationMs,
			NumTurns:        e.turnCount,
			TotalCostUSD:    e.costState.TotalCostUSD,
			ModelUsage:      e.buildModelUsage(),
			FastModeState:   "off",
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))
	}
	// AC2: Reset compaction failure counter on successful API response
	e.resetCompactFailCount()

	// Check and run memory extraction before returning.
	// StopReason is passed through verbatim (may be "" for empty stop_reason).
	if e.memExtractor != nil {
		e.memExtractor.CheckAndExtract(ctx, TurnContext{
			StopReason: resp.StopReason,

			AssistantMessage: assistantMsg,
			TotalMessages:    len(messages),
			RecentMessages:   messages,
		})
	}

	return finalResult, nil
}

// toLoopUsage converts api.Usage to *Usage (loop.go Usage type).
func toLoopUsage(src api.Usage) *Usage {
	return &Usage{
		InputTokens:              src.InputTokens,
		OutputTokens:             src.OutputTokens,
		CacheReadInputTokens:     src.CacheReadInputTokens,
		CacheCreationInputTokens: src.CacheCreationInputTokens,
	}
}

// thinkingBlock holds the text and optional signature of a thinking block
// collected during streaming or fallback processing. Used as the unit of
// emission by emitConsolidatedAssistant.
type thinkingBlock struct {
	Text      string
	Signature string
}

// emitConsolidatedAssistant writes ONE `type: "assistant"` envelope to stdout
// containing every collected block for the current API turn, in spec order:
// thinking blocks first (with omitempty signature), then the text block
// (omitted when empty), then tool_use blocks. The 17-line emission logic was
// previously duplicated at the streaming-path and fallback-path call sites;
// this helper consolidates them so envelope-shape changes only happen once.
func (e *QueryEngine) emitConsolidatedAssistant(
	sessionID string,
	thinkingBlocks []thinkingBlock,
	textOutput *strings.Builder,
	toolUseBlocks []api.ToolUseBlock,
	messageID string,
	stopReason string,
	stopSequence string,
	usage *Usage,
	model string,
) {
	if !e.streamCfg.Enabled {
		return
	}
	if len(thinkingBlocks) == 0 && textOutput.Len() == 0 && len(toolUseBlocks) == 0 {
		return
	}

	// Build content array with ordered fields per reference format
	// Reference order: thinking → text → tool_use
	// Each block needs ordered fields (type first) - use string construction to avoid map key ordering
	var contentFields []string
	for _, tb := range thinkingBlocks {
		// Reference order: type, thinking, signature
		blockFields := []string{
			`"type":"thinking"`,
			`"thinking":` + encodeString(tb.Text),
		}
		if tb.Signature != "" {
			blockFields = append(blockFields, `"signature":`+encodeString(tb.Signature))
		}
		contentFields = append(contentFields, "{"+strings.Join(blockFields, ",")+"}")
	}
	if textOutput.Len() > 0 {
		// Reference order: type, text
		contentFields = append(contentFields, `{"type":"text","text":`+encodeString(textOutput.String())+`}`)
	}
	for _, tb := range toolUseBlocks {
		// Reference order: type, id, name, input
		inputBytes, _ := json.Marshal(tb.Input)
		blockFields := []string{
			`"type":"tool_use"`,
			`"id":` + encodeString(tb.ID),
			`"name":` + encodeString(tb.Name),
			`"input":` + string(inputBytes),
		}
		contentFields = append(contentFields, "{"+strings.Join(blockFields, ",")+"}")
	}
	contentJSON := "[" + strings.Join(contentFields, ",") + "]"

	// Build full message structure per spec: id, type, role, model, content, stop_reason, stop_sequence, usage
	// Using ordered field construction to match reference format
	messageFields := []string{
		`"id":` + encodeString(messageID),
		`"type":"message"`,
		`"role":"assistant"`,
		`"model":` + encodeString(model),
		`"content":` + contentJSON,
	}

	// Always include stop_reason and stop_sequence (possibly null)
	if stopReason != "" {
		messageFields = append(messageFields, `"stop_reason":`+encodeString(stopReason))
	} else {
		messageFields = append(messageFields, `"stop_reason":null`)
	}
	if stopSequence != "" {
		messageFields = append(messageFields, `"stop_sequence":`+encodeString(stopSequence))
	} else {
		messageFields = append(messageFields, `"stop_sequence":null`)
	}

	// Include usage if present - reference order: input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens, service_tier
	if usage != nil {
		usageJSON := fmt.Sprintf(`{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"service_tier":"standard"}`,
			usage.InputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens, usage.OutputTokens)
		messageFields = append(messageFields, `"usage":`+usageJSON)
	}

	messageJSON := "{" + strings.Join(messageFields, ",") + "}"
	messageObj := json.RawMessage(messageJSON)

	// Reference order for assistant: type, message, parent_tool_use_id, session_id, uuid
	msg := StreamMessage{
		Type:            "assistant",
		Message:         messageObj,
		ParentToolUseID: nil,
		SessionID:       sessionID,
		Uuid:            GenerateUUID(),
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stdout, string(data))
}
