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
	"github.com/ipy/jenny/internal/git"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/memdir"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// SubmitMessage runs a single query turn: persist message, run agent loop,
// flush state on completion. Returns the text result and error.
func (e *QueryEngine) SubmitMessage(ctx context.Context, prompt string) (string, error) {
	e.mu.Lock()
	// Reset turn counter and start time for this submit
	e.turnCount = 0
	e.startTime = time.Now()
	sessionID := e.streamCfg.SessionID
	isResume := e.streamCfg.IsResume
	sessionManager := e.sessionManager
	historyMessages := e.streamCfg.HistoryMessages
	e.mu.Unlock()

	// Get working directory
	cwd, err := os.Getwd()
	if err != nil {
		cwd, _ = os.UserHomeDir()
	}
	e.mu.Lock()
	e.cwd = cwd
	e.mu.Unlock()

	// AC1: Persist user message to transcript BEFORE any API call
	if sessionManager != nil {
		// For resume sessions, check for duplicate user message
		skipUserPersist := false
		if isResume {
			exists, err := sessionManager.UserMessageExists(sessionID, prompt)
			if err != nil {
				return "", fmt.Errorf("checking for duplicate user message: %w", err)
			}
			skipUserPersist = exists
		}
		if !skipUserPersist {
			if err := sessionManager.AppendEntry(sessionID, session.TranscriptEntry{
				Type:    "user",
				Content: prompt,
				CWD:     cwd,
			}); err != nil {
				return "", fmt.Errorf("persisting user message to transcript: %w", err)
			}
		}
	}

	// AC1: Create memdir and inject memory content into system prompt
	if e.streamCfg.AutoMemoryEnabled {
		if gitRoot, err := git.GetRoot(cwd); err == nil {
			memdirCfg := memdir.Config{
				ProjectRoot:       gitRoot,
				AutoMemoryEnabled: true,
			}
			if m, err := memdir.New(memdirCfg); err == nil {
				_ = m.Create()
				// Read memory content to inject into system prompt
				if indexContent, err := m.ReadIndex(); err == nil && indexContent != "" {
					e.streamCfg.MemoryContent = indexContent
				}
			}
			// Initialize memory extractor with project root
			e.initMemoryExtractor(gitRoot)
		}
	}

	// Build messages slice - use history if resuming, otherwise start fresh
	var messages []api.Message
	if len(historyMessages) > 0 {
		// Use history and append the new prompt as a user message
		messages = append(historyMessages, api.Message{
			Role:    "user",
			Content: prompt,
		})
	} else {
		// Start fresh with just the user message
		messages = []api.Message{
			{
				Role:    "user",
				Content: prompt,
			},
		}
	}

	// Run the agent loop
	result, err := e.runLoop(ctx, messages, cwd, sessionID, "user")

	// AC3: Flush cost state on completion (success, error, or limit exceeded)
	e.mu.Lock()
	e.costState.LastSessionID = sessionID
	e.mu.Unlock()
	_ = SaveCostState(e.costState)

	return result, err
}

// runLoop implements the core agent loop. It iterates with the API,
// executing tools and accumulating cost, until the model signals
// end_turn or stop_sequence, or a limit is reached.
// querySource indicates the origin of the request ("user", "compact", "session_memory").
func (e *QueryEngine) runLoop(ctx context.Context, messages []api.Message, cwd, sessionID, querySource string) (string, error) {
	systemPrompt := AssembleSystemPrompt(e.streamCfg, e.tools, cwd)
	// Freeze the system prompt after first assembly so that subsequent turns
	// within the same session receive an identical string, protecting prompt caching.
	if e.streamCfg.CachedSystemPrompt == "" {
		e.streamCfg.CachedSystemPrompt = systemPrompt
		// Persist frozen system prompt to transcript for cross-process resume
		if e.sessionManager != nil && sessionID != "" {
			_ = e.sessionManager.AppendSystemPrompt(sessionID, systemPrompt)
		}
	}

	// AC3: When stream-json mode is active, redirect debug logs to stderr
	// to prevent interleaving with NDJSON output on stdout
	if e.streamCfg.Enabled {
		log.SetOutput(os.Stderr)
	}

	maxIterations := e.streamCfg.MaxIterations

	for i := 0; maxIterations <= 0 || i < maxIterations; i++ {
		// Check if context is already cancelled/timed out before attempting API call
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		e.mu.Lock()
		// AC2: maxTurns enforcement - check before each API call
		if e.maxTurns > 0 && e.turnCount >= e.maxTurns {
			e.mu.Unlock()
			// Emit error result if streaming enabled
			if e.streamCfg.Enabled {
				msg := StreamMessage{
					Type:            "result",
					Subtype:         "error",
					Result:          fmt.Sprintf("Maximum number of turns (%d) reached. stopping.", e.maxTurns),
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Model:           e.model,
					IsError:         true,
					StopReason:      "max_turns",
					DurationMs:      time.Since(e.startTime).Milliseconds(),
					DurationAPIMs:   e.totalAPIDurationMs,
					TotalCostUSD:    e.costState.TotalCostUSD,
					TotalCostCNY:    e.costState.TotalCostCNY,
					ModelUsage:      e.buildModelUsage(),
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			return "", fmt.Errorf("error_max_turns: limit reached at turn %d", e.turnCount)
		}
		// Increment turn counter at start of each API iteration
		e.turnCount++
		budgetUSD := e.streamCfg.MaxBudgetUSD
		budgetCNY := e.streamCfg.MaxBudgetCNY
		e.mu.Unlock()

		// AC3: Reset structured output tool at start of each turn
		if e.structuredOutputTool != nil {
			e.structuredOutputTool.Reset()
		}

		// AC2: Budget enforcement - check before each API call
		// Use CNY budget if currency is CNY, otherwise use USD budget
		currency := e.costState.Currency
		if currency == "CNY" && budgetCNY > 0 {
			if exceeded, _ := CheckBudgetExceeded(e.costState, budgetCNY, "CNY"); exceeded {
				if e.streamCfg.Enabled {
					msg := StreamMessage{
						Type:            "result",
						Subtype:         "error",
						Result:          fmt.Sprintf("budget exceeded: %.4f CNY > %.4f CNY limit", e.costState.TotalCostCNY, budgetCNY),
						SessionID:       sessionID,
						ParentToolUseID: nil,
						Uuid:            GenerateUUID(),
						Model:           e.model,
						IsError:         true,
						StopReason:      "budget_exceeded",
						DurationMs:      time.Since(e.startTime).Milliseconds(),
						DurationAPIMs:   e.totalAPIDurationMs,
						TotalCostUSD:    e.costState.TotalCostUSD,
						TotalCostCNY:    e.costState.TotalCostCNY,
						ModelUsage:      e.buildModelUsage(),
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
				return "", fmt.Errorf("budget exceeded: %.4f CNY > %.4f CNY limit", e.costState.TotalCostCNY, budgetCNY)
			}
		} else if budgetUSD > 0 {
			if exceeded, _ := CheckBudgetExceeded(e.costState, budgetUSD, "USD"); exceeded {
				if e.streamCfg.Enabled {
					msg := StreamMessage{
						Type:            "result",
						Subtype:         "error",
						Result:          fmt.Sprintf("budget exceeded: %.4f USD > %.4f USD limit", e.costState.TotalCostUSD, budgetUSD),
						SessionID:       sessionID,
						ParentToolUseID: nil,
						Uuid:            GenerateUUID(),
						Model:           e.model,
						IsError:         true,
						StopReason:      "budget_exceeded",
						DurationMs:      time.Since(e.startTime).Milliseconds(),
						DurationAPIMs:   e.totalAPIDurationMs,
						TotalCostUSD:    e.costState.TotalCostUSD,
						TotalCostCNY:    e.costState.TotalCostCNY,
						ModelUsage:      e.buildModelUsage(),
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
				return "", fmt.Errorf("budget exceeded: %.4f USD > %.4f USD limit", e.costState.TotalCostUSD, budgetUSD)
			}
		}

		// Emit stream_request_start before each API iteration (AC4)
		if e.streamCfg.Enabled {
			msg := StreamMessage{
				Type:            "stream_request_start",
				SessionID:       sessionID,
				ParentToolUseID: nil,
				Uuid:            GenerateUUID(),
			}
			data, _ := json.Marshal(msg)
			fmt.Fprintln(os.Stdout, string(data))
		}

		// AC3: Inject pending task completions as synthetic tool_results
		// before each API iteration so the model can process them
		completions := e.drainTaskCompletions()
		if len(completions) > 0 {
			userMsg := api.Message{
				Role:        "user",
				ToolResults: make([]api.ToolResultBlock, 0, len(completions)),
			}
			for _, c := range completions {
				userMsg.ToolResults = append(userMsg.ToolResults, api.ToolResultBlock{
					ToolUseID: "task_completed_" + c.TaskID,
					Content: fmt.Sprintf(
						`<task_completed task_id="%s" duration_seconds="%.1f" exit_code="%d"/>`,
						c.TaskID, c.DurationSeconds, c.ExitCode,
					),
					IsError: false,
				})
			}
			messages = append(messages, userMsg)
		}

		// AC1: Check compaction threshold before API request
		// Estimate tokens and check if auto-compact should trigger
		estimatedTokens := estimateTokens(messages)

		// Emit warning if approaching threshold
		if e.compactConfig.checkWarningThreshold(estimatedTokens) {
			EmitCompactWarning(estimatedTokens, e.compactConfig.warningThreshold())
		}

		// Check blocking limit when auto-compact is disabled (AC3)
		// compact/session_memory sources skip this check (AC5)
		if err := e.compactConfig.blockIfOverLimit(estimatedTokens, querySource); err != nil {
			return "", err
		}

		// AC1: Auto-compact if threshold exceeded and circuit breaker not tripped
		if e.compactConfig.checkCompactThreshold(estimatedTokens, querySource) {
			e.mu.Lock()
			circuitBreakerTripped := e.compactFailCount >= MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES
			e.mu.Unlock()

			if !circuitBreakerTripped {
				// Capture pre-compaction tokens for boundary persistence
				preCompactTokens := estimatedTokens

				// Attempt compaction
				compacted, err := e.compactMessages(ctx, messages, e.compactConfig, systemPrompt)
				if err == nil {
					// Compaction succeeded - normalize the compacted chain
					messages = normalizeCompactedChain(compacted)
					// Subtract 1 from len(compacted) because preservedCount tracks actual
					// preserved messages, not the boundary marker (which is len-1 of compacted)
					preservedCount := len(compacted) - 1
					// Persist compaction boundary to transcript for resume filtering
					if err := e.persistCompactBoundary(preCompactTokens, preservedCount, "auto"); err != nil {
						log.Error("Failed to persist compaction boundary", "error", err)
					}
					log.Debug("Context compaction succeeded", "newMessageCount", len(messages))
				} else {
					// Compaction failed - increment failure counter
					e.mu.Lock()
					e.compactFailCount++
					log.Warn("Context compaction failed", "error", err, "consecutiveFailures", e.compactFailCount)
					e.mu.Unlock()
				}
			} else {
				log.Debug("Auto-compact skipped: circuit breaker tripped")
			}
		}

		// Normalize messages before API request (content-level only, no structural transforms).
		// NormalizeMessagesAPI (which includes mergeConsecutiveSameRole) is intentionally
		// NOT called here — structural normalization would destroy cache continuity.
		// It is only used by the compaction path via normalizeCompactedChain.
		for i := range messages {
			messages[i] = normalizeNewMessage(messages[i])
		}

		// Create fallback function for streaming failures (AC3)
		fallbackFn := func(fallbackCtx context.Context) (*api.Response, error) {
			e.client.SetMaxTokensOverride(64000)
			return e.client.SendMessage(fallbackCtx, messages, e.toolParams, nil, systemPrompt, "")
		}

		// Use streaming API (AC1)
		// Track API call duration (AC3: duration_api_ms)
		apiStartTime := time.Now()
		dynamicSuffix := DynamicSystemSuffix(e.streamCfg, cwd)
		blocksChan, streamResult := e.client.SendMessageStream(
			ctx,
			messages,
			e.toolParams,
			nil,
			systemPrompt,
			dynamicSuffix,
			api.DefaultIdleTimeout,
			api.DefaultFallbackTimeout,
			fallbackFn,
		)

		// Process streaming blocks
		var textOutput strings.Builder
		var toolResults []api.ToolResult
		var toolUseBlocks []api.ToolUseBlock
		var thinkingBlocks []thinkingBlock

		// Process blocks as they arrive
		for block := range blocksChan {
			// Reset lastAPIStartTime for each block to track active streaming time?
			// Actually, totalAPIDurationMs should include the entire time the API is active.
			// ... (existing block processing)
			// Handle raw stream_event passthrough
			if block.Type == "stream_event" && e.streamCfg.Enabled && e.streamCfg.IncludePartial {
				// Capture message ID from MessageStartEvent
				if msgStart, ok := block.RawEvent.(api.AnthropicStreamEvent); ok && msgStart.Type == "message_start" && msgStart.Message != nil {
					e.currentMessageID = msgStart.Message.ID
					e.currentUsage = api.Usage{
						InputTokens:              msgStart.Message.Usage.InputTokens,
						CacheReadInputTokens:     msgStart.Message.Usage.CacheReadInputTokens,
						CacheCreationInputTokens: msgStart.Message.Usage.CacheCreationInputTokens,
					}
				}
				// Capture stop_reason and usage from MessageDeltaEvent
				if msgDelta, ok := block.RawEvent.(api.AnthropicStreamEvent); ok && msgDelta.Type == "message_delta" && msgDelta.Delta != nil {
					e.currentStopReason = msgDelta.Delta.StopReason
					e.currentStopSequence = msgDelta.Delta.StopSequence
					if msgDelta.Usage != nil && msgDelta.Usage.OutputTokens > 0 {
						e.currentUsage.OutputTokens = msgDelta.Usage.OutputTokens
					}
				}
				// Transform SDK event to minimal representation (AC1: no bloated zero-value fields)
				transformedEvent, err := TransformStreamEvent(block.RawEvent)
				if err != nil {
					continue
				}
				// Emit spec-compliant stream_event wire shape (AC5)
				msg := StreamMessage{
					Type:            "stream_event",
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Event:           transformedEvent,
				}
				data, err := json.Marshal(msg)
				if err == nil {
					fmt.Fprintln(os.Stdout, string(data))
				}
				continue
			}

			switch block.Block.Type {
			case "text":
				// AC1: Redact secrets from text output before writing
				if e.secretRedactor != nil && e.secretRedactor.Enabled() {
					textOutput.WriteString(e.secretRedactor.Redact(block.Block.Text))
				} else {
					textOutput.WriteString(block.Block.Text)
				}
			case "thinking":
				thinkingBlocks = append(thinkingBlocks, thinkingBlock{Text: block.Block.Thinking, Signature: block.Block.Signature})
			case "tool_use":
				// Collect tool_use blocks for the assistant message
				toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
					ID:    block.Block.ToolID,
					Name:  block.Block.ToolName,
					Input: block.Block.ToolInput,
				})
			case "web_search_tool_result":
				// AC5: Process web search results and surface error codes
				if block.Block.WebSearchResult != nil && block.Block.WebSearchResult.IsError {
					// Surface server error code as a tool result
					toolResults = append(toolResults, api.ToolResult{
						ToolUseID: block.Block.WebSearchResult.ToolUseID,
						Content:   fmt.Sprintf("web search error: %s", block.Block.WebSearchResult.ErrorCode),
						IsError:   true,
					})
				}
			}
		}
		e.totalAPIDurationMs += time.Since(apiStartTime).Milliseconds()

		// Emit ONE consolidated assistant message for all collected content from streaming
		// (AC1-AC4: one assistant event per API turn, not per tool_use block)
		e.emitConsolidatedAssistant(sessionID, thinkingBlocks, &textOutput, toolUseBlocks,
			e.currentMessageID, e.currentStopReason, e.currentStopSequence, toLoopUsage(e.currentUsage), e.model)

		// Check if streaming completed with error
		if streamResult.Error != "" && len(streamResult.Blocks) == 0 {
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
					TotalCostCNY:  e.costState.TotalCostCNY,
					ModelUsage:    e.buildModelUsage(),
					// Additional fields for error_max_tokens
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
				return "", fmt.Errorf("max tokens reached: %s", mte.Category)
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
					TotalCostCNY:    e.costState.TotalCostCNY,
					ModelUsage:      e.buildModelUsage(),
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}

			// AC4: Check if this is a media error - if so, strip the offending tool_result and retry
			var wasMediaError bool
			messages, wasMediaError = HandleMediaErrorOnRetry(messages, streamResult.Error)
			if wasMediaError {
				continue // Retry with modified messages
			}
			// AC2: Non-user-abort error - increment compaction failure counter
			// Skip increment for user-initiated aborts (context cancellation, Esc, SIGINT, etc.)
			if !isUserAbortError(streamResult.Error) {
				e.incrementCompactFailCount()
			}
			return "", fmt.Errorf("streaming error: %v", streamResult.Error)
		}

		// Use results from streaming (or fallback)
		resp := &api.Response{
			Content:    streamResult.Blocks,
			StopReason: streamResult.StopReason,
			Usage:      streamResult.Usage,
			Model:      streamResult.Model,
		}

		// Fallback block processing: if blocksChan was empty but streamResult.Blocks has content
		if textOutput.Len() == 0 && len(toolUseBlocks) == 0 && len(streamResult.Blocks) > 0 {
			// Collect pending web search results to emit user wrappers after assistant
			var pendingWebSearchResults []api.ContentBlock

			for _, block := range streamResult.Blocks {
				switch block.Type {
				case "text":
					// AC1: Redact secrets from text output in fallback path
					if e.secretRedactor != nil && e.secretRedactor.Enabled() {
						textOutput.WriteString(e.secretRedactor.Redact(block.Text))
					} else {
						textOutput.WriteString(block.Text)
					}
				case "thinking":
					thinkingBlocks = append(thinkingBlocks, thinkingBlock{Text: block.Thinking, Signature: block.Signature})
				case "tool_use":
					// Collect tool_use blocks for the assistant message
					toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
						ID:    block.ToolID,
						Name:  block.ToolName,
						Input: block.ToolInput,
					})
				case "web_search_tool_result":
					// AC5: Process web search results and surface error codes in fallback
					if block.WebSearchResult != nil && block.WebSearchResult.IsError {
						toolResults = append(toolResults, api.ToolResult{
							ToolUseID: block.WebSearchResult.ToolUseID,
							Content:   fmt.Sprintf("web search error: %s", block.WebSearchResult.ErrorCode),
							IsError:   true,
						})
					}
					pendingWebSearchResults = append(pendingWebSearchResults, block)
				}
			}

			// AC4: Emit ONE consolidated assistant message for all collected content
			e.emitConsolidatedAssistant(sessionID, thinkingBlocks, &textOutput, toolUseBlocks,
				e.currentMessageID, e.currentStopReason, e.currentStopSequence, toLoopUsage(e.currentUsage), e.model)

			// AC4: Emit user message wrappers for web search tool results
			for _, result := range pendingWebSearchResults {
				if result.WebSearchResult == nil {
					continue
				}
				var toolResultContent any
				if result.WebSearchResult.IsError {
					toolResultContent = fmt.Sprintf("web search error: %s", result.WebSearchResult.ErrorCode)
				} else {
					// Non-error case: pass through the block's text content if present
					toolResultContent = result.Text
				}
				userContent := []map[string]any{
					{"type": "tool_result", "tool_use_id": result.WebSearchResult.ToolUseID, "content": toolResultContent},
				}
				// Format tool_use_result for user event
				var toolUseResult any
				if result.WebSearchResult.IsError {
					toolUseResult = fmt.Sprintf("web search error: %s", result.WebSearchResult.ErrorCode)
				} else {
					toolUseResult = map[string]any{"stdout": toolResultContent, "stderr": "", "interrupted": false, "isImage": false, "noOutputExpected": false}
				}
				userMsg := StreamMessage{
					Type:            "user",
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Message:         map[string]any{"role": "user", "content": userContent},
					Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
					ToolUseResult:   toolUseResult,
				}
				data, _ := json.Marshal(userMsg)
				fmt.Fprintln(os.Stdout, string(data))
			}
		}

		// Accumulate cost for this turn
		if resp.Model != "" {
			AccumulateUsage(e.costState, resp.Model, resp.Usage)
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

		// Persist assistant message to transcript BEFORE tool execution
		if e.sessionManager != nil && (textOutput.String() != "" || len(toolUseBlocks) > 0) {
			entry := session.TranscriptEntry{
				Type:    "assistant",
				Content: textOutput.String(),
				CWD:     cwd,
			}
			for _, tu := range toolUseBlocks {
				entry.ToolUse = append(entry.ToolUse, session.ToolUse{
					ID:    tu.ID,
					Name:  tu.Name,
					Input: tu.Input,
				})
			}
			if err := e.sessionManager.AppendEntry(sessionID, entry); err != nil {
				return "", fmt.Errorf("persisting assistant message to transcript: %w", err)
			}
		}

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

		// Execute all tools using the parallel executor
		executor := NewToolExecutor(e.tools, cwd)
		execResults, err := executor.Execute(ctx, execBlocks)
		if err != nil {
			return "", fmt.Errorf("executing tools: %w", err)
		}

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
					Type:    "tool_result",
					ToolID:  emitToolUseID,
					Content: emitContent,
					IsError: emitIsError,
					CWD:     cwd,
				}); err != nil {
					return "", fmt.Errorf("persisting tool result to transcript: %w", err)
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
					{"type": "tool_result", "tool_use_id": emitToolUseID, "content": emitContent, "is_error": emitIsError},
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

		// AC3: When synthetic results were generated, detach the loop context
		// from cancellation so the next iteration can deliver them to the model
		// instead of bailing out at the top-of-loop ctx.Err() guard. The model
		// receives the interrupted tool_results and decides whether to retry,
		// summarise, or abort. We keep ctx.Err() honoured everywhere else.
		if hasSynthetic {
			ctx = context.WithoutCancel(ctx)
		}

		// Check session memory threshold after each turn
		if e.sessionMemory != nil {
			turnTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens +
				resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens
			toolCallCount := len(toolUseBlocks)
			shouldAct, action := e.sessionMemory.CheckThreshold(turnTokens, toolCallCount)
			if shouldAct {
				if action == "init" {
					if err := e.sessionMemory.Init(); err != nil {
						log.Warn("Session memory init failed", "error", err)
					}
				} else if action == "update" {
					if err := e.sessionMemory.Update(ctx); err != nil {
						log.Warn("Session memory update failed", "error", err)
					}
				}
			}
		}

		// Handle stop reason
		switch resp.StopReason {
		case api.StopReasonEndTurn:
			// AC3: Enforce structured output at end of turn
			if e.structuredOutputTool != nil && !e.structuredOutputTool.IsEmitted() {
				return "", fmt.Errorf("structured output not emitted")
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
				messages = append(messages, userMsg)
				// end_turn means the model is done - output final result
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
				// AC2: Reset compaction failure counter on successful API response
				e.resetCompactFailCount()
				return finalResult, nil
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
			// AC2: Reset compaction failure counter on successful API response
			e.resetCompactFailCount()

			// Check and run memory extraction before returning
			if e.memExtractor != nil && resp.StopReason != "" {
				e.memExtractor.CheckAndExtract(ctx, TurnContext{
					StopReason: resp.StopReason,

					AssistantMessage: &assistantMsg,
					TotalMessages:    len(messages),
					RecentMessages:   messages,
				})
			}

			return finalResult, nil

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
						IsError:   tr.IsError,
					})
				}
				messages = append(messages, userMsg)
			}
			continue

		case api.StopReasonMaxTokens:
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
					TotalCostCNY:  e.costState.TotalCostCNY,
					ModelUsage:    e.buildModelUsage(),
					// Additional fields for error_max_tokens
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
			return textOutput.String(), fmt.Errorf("max tokens reached: %s", streamResult.MaxTokensErr.Category)

		case api.StopReasonStopSeq:
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
					Type:    "result",
					Subtype: "success",
					Result: func() string {
						if e.secretRedactor != nil && e.secretRedactor.Enabled() {
							return e.secretRedactor.Recover(textOutput.String())
						}
						return textOutput.String()
					}(),
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
					TotalCostCNY:    e.costState.TotalCostCNY,
					ModelUsage:      e.buildModelUsage(),
					FastModeState:   "off",
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			// AC2: Reset compaction failure counter on successful API response
			e.resetCompactFailCount()

			// Check and run memory extraction before returning
			if e.memExtractor != nil {
				e.memExtractor.CheckAndExtract(ctx, TurnContext{
					StopReason: resp.StopReason,

					AssistantMessage: &assistantMsg,
					TotalMessages:    len(messages),
					RecentMessages:   messages,
				})
			}

			return func() string {
				if e.secretRedactor != nil && e.secretRedactor.Enabled() {
					return e.secretRedactor.Recover(textOutput.String())
				}
				return textOutput.String()
			}(), nil
		default:
			// Empty or unrecognized stop_reason: treat as end_turn (terminal).
			// Defensive: if tool_use blocks are present, continue the loop to keep
			// the chain valid (the API requires tool_use to be answered with tool_result).
			if len(toolUseBlocks) > 0 {
				if len(toolResults) > 0 {
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
					messages = append(messages, userMsg)
				}
				continue
			}
			return e.finalizeAsEndTurn(ctx, resp, textOutput, sessionID, &assistantMsg, messages)
		}
	}

	return "", fmt.Errorf("max iterations (%d) exceeded", maxIterations)
}

// seedReadFileCacheFromTranscript seeds the ReadFileCache from transcript entries.
// It extracts completed Read tool_use + tool_result pairs and adds them to the cache.
func seedReadFileCacheFromTranscript(cache *tool.ReadFileCache, sessionManager *session.Manager, sessionID string) error {
	if cache == nil || sessionManager == nil || sessionID == "" {
		return nil
	}

	entries, err := sessionManager.LoadTranscript(sessionID)
	if err != nil {
		return err
	}

	// Build a map of tool_use ID -> tool_use entry for Read tools
	readToolUses := make(map[string]session.TranscriptEntry)
	for _, entry := range entries {
		if entry.Type == "tool_use" && len(entry.ToolUse) > 0 {
			for _, tu := range entry.ToolUse {
				if tu.Name == "Read" {
					readToolUses[tu.ID] = entry
				}
			}
		}
	}

	// Now iterate through tool_result entries and match them to Read tool_use
	for _, entry := range entries {
		if entry.Type == "tool_result" && !entry.IsError {
			if toolUseEntry, ok := readToolUses[entry.ToolID]; ok {
				// Find the specific tool_use that matches entry.ToolID
				var tu *session.ToolUse
				for i := range toolUseEntry.ToolUse {
					if toolUseEntry.ToolUse[i].ID == entry.ToolID {
						tu = &toolUseEntry.ToolUse[i]
						break
					}
				}
				if tu == nil {
					continue
				}
				path, _ := tu.Input["file_path"].(string)
				_, hasOffset := tu.Input["offset"]
				_, hasLimit := tu.Input["limit"]

				// Skip partial reads (offset or limit set means partial read)
				if hasOffset || hasLimit {
					continue
				}

				if path != "" && entry.Content != "" {
					// Use current mtime since transcript doesn't store it precisely
					if info, err := os.Stat(path); err == nil {
						cache.Add(path, entry.Content, info.ModTime(), true, 0, 0)
					}
				}
			}
		}
	}

	return nil
}
