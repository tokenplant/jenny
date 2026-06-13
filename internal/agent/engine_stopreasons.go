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
)

// handleStopReason processes the model's stop_reason and returns the result
// string, error, and a boolean indicating whether the loop should continue.
// When shouldContinue is true, messages is modified in-place to include any
// tool_result user messages needed for the next iteration.
func (e *QueryEngine) handleStopReason(
	ctx context.Context,
	resp *api.Response,
	streamResult api.StreamResult,
	textOutput strings.Builder,
	sessionID string,
	toolResults []api.ToolResult,
	toolUseBlocks []api.ToolUseBlock,
	thinkingBlocks []thinkingBlock,
	assistantMsg *api.Message,
	messages *[]api.Message,
) (result string, err error, shouldContinue bool) {

	switch resp.StopReason {
	case api.StopReasonEndTurn:
		return e.handleStopEndTurn(ctx, resp, textOutput, sessionID, toolResults, assistantMsg, messages)

	case api.StopReasonToolUse:
		return e.handleStopToolUse(toolResults, messages)

	case api.StopReasonMaxTokens:
		return e.handleStopMaxTokens(resp, streamResult, textOutput, sessionID)

	case api.StopReasonStopSeq:
		return e.handleStopStopSeq(ctx, resp, textOutput, sessionID, assistantMsg, messages)

	default:
		// Empty or unrecognized stop_reason: treat as end_turn (terminal).
		// Defensive: if tool_use blocks are present, continue the loop to keep
		// the chain valid (the API requires tool_use to be answered with tool_result).
		if len(toolUseBlocks) > 0 {
			if len(toolResults) > 0 {
				userMsg := buildToolResultUserMsg(toolResults)
				*messages = append(*messages, userMsg)
			}
			return "", nil, true
		}
		result, err := e.finalizeAsEndTurn(ctx, resp, textOutput, sessionID, assistantMsg, *messages)
		return result, err, false
		}
}

// handleStopEndTurn processes the end_turn stop reason: validates structured
// output, determines final result, emits NDJSON success result, resets compaction
// counter, runs memory extraction, and returns the final result.
func (e *QueryEngine) handleStopEndTurn(
	ctx context.Context,
	resp *api.Response,
	textOutput strings.Builder,
	sessionID string,
	toolResults []api.ToolResult,
	assistantMsg *api.Message,
	messages *[]api.Message,
) (string, error, bool) {
	// AC3: Enforce structured output at end of turn
	if e.structuredOutputTool != nil && !e.structuredOutputTool.IsEmitted() {
		return "", fmt.Errorf("structured output not emitted"), false
	}
	// AC3: Determine final result - use structured output if available
	var finalResult string
	if e.secretRedactor != nil && e.secretRedactor.Enabled() {
		finalResult = e.secretRedactor.Recover(textOutput.String())
		if e.structuredOutputTool != nil && e.structuredOutputTool.IsEmitted() && e.structuredOutputResult != "" {
			finalResult = e.secretRedactor.Recover(e.structuredOutputResult)
		}
	} else {
		finalResult = textOutput.String()
		if e.structuredOutputTool != nil && e.structuredOutputTool.IsEmitted() && e.structuredOutputResult != "" {
			finalResult = e.structuredOutputResult
		}
	}
	if len(toolResults) > 0 {
		// Send tool results back to model before ending
		userMsg := buildToolResultUserMsg(toolResults)
		*messages = append(*messages, userMsg)
		// end_turn means the model is done - output final result
		if e.streamCfg.Enabled {
			e.emitSuccessResult(resp, finalResult, sessionID)
		}
		// AC2: Reset compaction failure counter on successful API response
		e.resetCompactFailCount()
		return finalResult, nil, false
	}
	// Output final result
	if e.streamCfg.Enabled {
		e.emitSuccessResult(resp, finalResult, sessionID)
	}
	// AC2: Reset compaction failure counter on successful API response
	e.resetCompactFailCount()

	// Check and run memory extraction before returning
	if e.memExtractor != nil && resp.StopReason != "" {
		e.memExtractor.CheckAndExtract(ctx, TurnContext{
			StopReason:       resp.StopReason,
			AssistantMessage: assistantMsg,
			TotalMessages:    len(*messages),
			RecentMessages:   *messages,
		})
	}

	return finalResult, nil, false
}

// handleStopToolUse processes the tool_use stop reason: appends tool results
// as a user message and signals the loop to continue.
func (e *QueryEngine) handleStopToolUse(toolResults []api.ToolResult, messages *[]api.Message) (string, error, bool) {
	// Continue the loop to let the model process tool results
	if len(toolResults) > 0 {
		userMsg := buildToolResultUserMsg(toolResults)
		*messages = append(*messages, userMsg)
	}
	return "", nil, true
}

// handleStopMaxTokens processes the max_tokens stop reason: emits structured
// error_max_tokens result and returns the error.
func (e *QueryEngine) handleStopMaxTokens(resp *api.Response, streamResult api.StreamResult, textOutput strings.Builder, sessionID string) (string, error, bool) {
	// AC1: Emit structured error_max_tokens result event
	if e.streamCfg.Enabled && streamResult.MaxTokensErr != nil {
		mte := streamResult.MaxTokensErr
		threshold := e.compactConfig.autoCompactThreshold()
		errMsg := fmt.Sprintf("max tokens reached: %s", mte.Category)
		msg := StreamMessage{
			Type:            "result",
			Subtype:         "error_max_tokens",
			Result:          errMsg,
			SessionID:       sessionID,
			ParentToolUseID: nil,
			Uuid:            GenerateUUID(),
			Model:           mte.Model,
			IsError:         true,
			Usage: &Usage{
				InputTokens:              resp.Usage.InputTokens,
				OutputTokens:             mte.OutputTokens,
				CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
				CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
			},
			StopReason:    string(resp.StopReason),
			DurationMs:    time.Since(e.startTime).Milliseconds(),
			DurationAPIMs: e.totalAPIDurationMs,
			TotalCostUSD:  e.costState.TotalCostUSD,
			ModelUsage:    e.buildModelUsage(),
			ErrorMaxTokens: &ErrorMaxTokensDetail{
				Category:        string(mte.Category),
				OutputTokens:    mte.OutputTokens,
				MaxOutputTokens: mte.MaxOutputTokens,
				InputTokens:     resp.Usage.InputTokens,
				Threshold:       threshold,
			},
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))
	}
	category := api.MaxTokensCategory("unknown")
	if streamResult.MaxTokensErr != nil {
		category = streamResult.MaxTokensErr.Category
	}
	return textOutput.String(), fmt.Errorf("max tokens reached: %s", category), false
}

// handleStopStopSeq processes the stop_sequence stop reason: emits success
// result, resets compaction counter, runs memory extraction, returns result.
func (e *QueryEngine) handleStopStopSeq(
	ctx context.Context,
	resp *api.Response,
	textOutput strings.Builder,
	sessionID string,
	assistantMsg *api.Message,
	messages *[]api.Message,
) (string, error, bool) {
	if e.streamCfg.Enabled {
		e.emitSuccessResult(resp, textOutput.String(), sessionID)
	}
	// AC2: Reset compaction failure counter on successful API response
	e.resetCompactFailCount()

	// Check and run memory extraction before returning
	if e.memExtractor != nil {
		e.memExtractor.CheckAndExtract(ctx, TurnContext{
			StopReason:       resp.StopReason,
			AssistantMessage: assistantMsg,
			TotalMessages:    len(*messages),
			RecentMessages:   *messages,
		})
	}

	result := textOutput.String()
	if e.secretRedactor != nil && e.secretRedactor.Enabled() {
		result = e.secretRedactor.Recover(textOutput.String())
	}
	return result, nil, false
}

// emitSuccessResult emits a stream-json success result event with the given
// final result and model response metadata.
func (e *QueryEngine) emitSuccessResult(resp *api.Response, finalResult, sessionID string) {
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
		Type:          "result",
		Subtype:       "success",
		Result:        finalResult,
		SessionID:     sessionID,
		Uuid:          GenerateUUID(),
		Usage:         usage,
		IsError:       false,
		StopReason:    string(resp.StopReason),
		DurationMs:    time.Since(e.startTime).Milliseconds(),
		DurationAPIMs: e.totalAPIDurationMs,
		NumTurns:      e.turnCount,
		TotalCostUSD:  e.costState.TotalCostUSD,
		ModelUsage:    e.buildModelUsage(),
		FastModeState: "off",
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stdout, string(data))
}
