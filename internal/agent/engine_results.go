// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/session"
)

// executeAndProcessTools converts tool_use blocks to executor format, emits
// tool_call started/completed events, executes tools, syncs active skills, and
// persists tool results to transcript. Returns the collected tool results, a
// boolean indicating whether synthetic (interrupted) results were generated,
// and any execution error.
func (e *QueryEngine) executeAndProcessTools(ctx context.Context, toolUseBlocks []api.ToolUseBlock, sessionID, cwd string) ([]api.ToolResult, bool, error) {
	// Convert API tool use blocks to executor format
	execBlocks := make([]toolUseBlock, 0, len(toolUseBlocks))
	for _, tb := range toolUseBlocks {
		execBlocks = append(execBlocks, toolUseBlock{
			ID:    tb.ID,
			Name:  tb.Name,
			Input: tb.Input,
		})
	}

	// AC1: Emit tool_call started events before execution begins
	if e.streamCfg.Enabled {
		for _, block := range execBlocks {
			msg := StreamMessage{
				Type:            "tool_call",
				Subtype:         "started",
				ToolName:        block.Name,
				ToolUseID:       block.ID,
				SessionID:       sessionID,
				ParentToolUseID: nil,
				Uuid:            GenerateUUID(),
			}
			data, _ := json.Marshal(msg)
			fmt.Fprintln(os.Stdout, string(data))
		}
	}

	// AC8: Recover secrets from tool inputs before execution
	if e.secretRedactor != nil && e.secretRedactor.Enabled() {
		for i, block := range execBlocks {
			if inputJSON, err := json.Marshal(block.Input); err == nil {
				recovered := e.secretRedactor.Recover(string(inputJSON))
				var ri map[string]any
				if err := json.Unmarshal([]byte(recovered), &ri); err == nil {
					execBlocks[i].Input = ri
				}
			}
		}
	}

	// Execute all tools using the parallel executor with cross-turn state support
	executor := NewToolExecutorWithStreamConfig(e.tools, cwd, &e.streamCfg)
	execResults, err := executor.Execute(ctx, execBlocks)
	if err != nil {
		return nil, false, fmt.Errorf("executing tools: %w", err)
	}

	// Sync active skills after tool execution (they may have changed)
	e.syncActiveSkills()

	// AC1/AC2: Pre-compute per-tool interrupt status so we can decide whether
	// to emit the executor's partial result or a synthetic "interrupted"
	// replacement. We need this decision BEFORE appending to toolResults to
	// avoid duplicate ToolUseID entries in the user message (fixes
	// iter88-dup-tool-results).
	interrupted := make([]bool, len(execResults))
	if ctx.Err() != nil {
		for i, res := range execResults {
			interrupted[i] = res.Interrupted
		}
	}

	// Process results and collect for API response
	var toolResults []api.ToolResult
	hasSynthetic := false
	for i, res := range execResults {
		// AC3: Capture structured output result if StructuredOutput was called
		if i < len(execBlocks) && execBlocks[i].Name == "StructuredOutput" && !res.IsError {
			e.structuredOutputResult = res.Content
		}

		// AC1: For interrupted tools, replace executor's partial result with a
		// single synthetic "Tool execution interrupted" entry. This both
		// preserves the model-facing contract (one tool_result per tool_use)
		// and avoids duplicates in the user message.
		emitContent := res.Content
		emitIsError := res.IsError
		emitToolUseID := res.ToolUseID
		if interrupted[i] {
			emitContent = "Tool execution interrupted"
			emitIsError = true
			emitToolUseID = execBlocks[i].ID
			hasSynthetic = true
		}

		// AC1: Redact secrets from tool result content before emitting
		if e.secretRedactor != nil && e.secretRedactor.Enabled() {
			emitContent = e.secretRedactor.Redact(emitContent)
		}
		toolResults = append(toolResults, api.ToolResult{
			ToolUseID: emitToolUseID,
			Content:   emitContent,
			IsError:   emitIsError,
		})

		// Persist tool result to transcript AFTER assistant message
		if e.sessionManager != nil {
			if err := e.sessionManager.AppendEntry(sessionID, session.TranscriptEntry{
				Type:    session.EntryTypeToolResult,
				ToolID:  emitToolUseID,
				Content: emitContent,
				IsError: emitIsError,
				CWD:     cwd,
			}); err != nil {
				return nil, false, fmt.Errorf("persisting tool result to transcript: %w", err)
			}
		}

		if e.streamCfg.Enabled {
			// AC2: Emit tool_call completed event before tool_result wrapper
			completedMsg := StreamMessage{
				Type:            "tool_call",
				Subtype:         "completed",
				ToolUseID:       emitToolUseID,
				IsError:         emitIsError,
				SessionID:       sessionID,
				ParentToolUseID: nil,
				Uuid:            GenerateUUID(),
			}
			data, _ := json.Marshal(completedMsg)
			fmt.Fprintln(os.Stdout, string(data))

			// Output user message wrapper for the tool result (AC3)
			userContent := []map[string]any{
				{"type": api.BlockTypeToolResult, "tool_use_id": emitToolUseID, "content": emitContent, "is_error": emitIsError},
			}
			// Format tool_use_result for user event
			var toolUseResult any
			if emitIsError {
				toolUseResult = fmt.Sprintf("Error: %s", emitContent)
			} else {
				toolUseResult = map[string]any{"stdout": emitContent, "stderr": "", "interrupted": false, "isImage": false, "noOutputExpected": false}
			}
			msg := StreamMessage{
				Type:            "user",
				SessionID:       sessionID,
				ParentToolUseID: nil,
				Uuid:            GenerateUUID(),
				Message:         map[string]any{"role": "user", "content": userContent},
				Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
				ToolUseResult:   toolUseResult,
			}
			data, _ = json.Marshal(msg)
			fmt.Fprintln(os.Stdout, string(data))
		}
	}

	return toolResults, hasSynthetic, nil
}

// handleStreamError processes streaming errors after block consumption.
// Returns:
//   - wasMediaError (bool): true if caller should retry with modified messages
//   - err (error): terminal error to return; nil when wasMediaError is true
//
// When wasMediaError is true, messages is modified in-place to strip the
// failing tool_result.
func (e *QueryEngine) handleStreamError(streamResult api.StreamResult, messages *[]api.Message, sessionID string) (bool, error) {
	// Check if this is a context_exhausted error from HTTP 400 rejection
	if streamResult.MaxTokensErr != nil && streamResult.MaxTokensErr.Category == api.CategoryContextExhausted {
		mte := streamResult.MaxTokensErr
		// Emit structured error_max_tokens result event for context_exhausted
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
				InputTokens:              streamResult.Usage.InputTokens,
				OutputTokens:             mte.OutputTokens,
				CacheReadInputTokens:     streamResult.Usage.CacheReadInputTokens,
				CacheCreationInputTokens: streamResult.Usage.CacheCreationInputTokens,
			},
			StopReason:    "max_tokens",
			DurationMs:    time.Since(e.startTime).Milliseconds(),
			DurationAPIMs: e.totalAPIDurationMs,
			TotalCostUSD:  e.costState.TotalCostUSD,
			ModelUsage:    e.buildModelUsage(),
			ErrorMaxTokens: &ErrorMaxTokensDetail{
				Category:        string(mte.Category),
				OutputTokens:    mte.OutputTokens,
				MaxOutputTokens: mte.MaxOutputTokens,
				InputTokens:     streamResult.Usage.InputTokens,
				Threshold:       threshold,
			},
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))
		e.incrementCompactFailCount()
		return false, fmt.Errorf("max tokens reached: %s", mte.Category)
	}

	// Emit standard error result for other streaming errors
	if e.streamCfg.Enabled {
		msg := StreamMessage{
			Type:            "result",
			Subtype:         "error",
			Result:          fmt.Sprintf("streaming error: %v", streamResult.Error),
			SessionID:       sessionID,
			ParentToolUseID: nil,
			Uuid:            GenerateUUID(),
			Model:           e.model,
			IsError:         true,
			StopReason:      "error",
			DurationMs:      time.Since(e.startTime).Milliseconds(),
			DurationAPIMs:   e.totalAPIDurationMs,
			TotalCostUSD:    e.costState.TotalCostUSD,
			ModelUsage:      e.buildModelUsage(),
		}
		data, _ := json.Marshal(msg)
		fmt.Fprintln(os.Stdout, string(data))
	}

	// AC4: Check if this is a media error - if so, strip the offending tool_result and retry
	var wasMediaError bool
	*messages, wasMediaError = HandleMediaErrorOnRetry(*messages, streamResult.Error)
	if wasMediaError {
		return true, nil // Retry with modified messages
	}
	// AC2: Non-user-abort error - increment compaction failure counter
	// Skip increment for user-initiated aborts (context cancellation, Esc, SIGINT, etc.)
	if !isUserAbortError(streamResult.Error) {
		e.incrementCompactFailCount()
	}
	return false, fmt.Errorf("streaming error: %v", streamResult.Error)
}

// buildToolResultUserMsg constructs a user message containing tool result blocks
// from the given tool results slice. Used by stop-reason cases that need to
// feed tool results back to the model.
func buildToolResultUserMsg(toolResults []api.ToolResult) api.Message {
	userMsg := api.Message{
		Role:        "user",
		ToolResults: make([]api.ToolResultBlock, 0, len(toolResults)),
	}
	for _, tr := range toolResults {
		userMsg.ToolResults = append(userMsg.ToolResults, api.ToolResultBlock{
			ToolUseID: tr.ToolUseID,
			Content:   tr.Content,
			IsError:   tr.IsError,
		})
	}
	return userMsg
}
