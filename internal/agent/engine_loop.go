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
				Type:    session.EntryTypeUser,
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
		messages = historyMessages

		// Inject system reminders for environment changes detected on resume.
		// These go as virtual user messages before the new user message so the
		// model sees them in context without polluting the system prompt prefix.
		if isResume {
			reminders := e.detectResumeChanges(cwd)
			for _, r := range reminders {
				msg := api.Message{
					Role:      api.RoleUser,
					Content:   "[system]: " + r,
					IsVirtual: true,
				}
				messages = append(messages, msg)
				e.persistSystemReminder(sessionID, r)
			}
		}

		messages = append(messages, api.Message{
			Role:    api.RoleUser,
			Content: prompt,
		})
	} else {
		messages = []api.Message{
			{
				Role:    api.RoleUser,
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
	systemPromptBlocks := AssembleSystemPrompt(e.streamCfg, e.tools, cwd)
	// Freeze the system prompt after first assembly so that subsequent turns
	// within the same session receive an identical string, protecting prompt caching.
	if len(e.streamCfg.CachedSystemPrompt) == 0 {
		e.streamCfg.CachedSystemPrompt = systemPromptBlocks
		// Persist frozen system prompt to transcript for cross-process resume
		if e.sessionManager != nil && sessionID != "" {
			// Join blocks for persistence (backward compatible or just for storage)
			fullPrompt := strings.Join(systemPromptBlocks, "\n\n")
			_ = e.sessionManager.AppendSystemPrompt(sessionID, fullPrompt)
		}
	}

	// For providers that don't support multi-block system prompt, we join them
	// but currently providers handle the slice.
	systemPrompt := e.streamCfg.CachedSystemPrompt

	// AC3: When stream-json mode is active, redirect debug logs to stderr
	// to prevent interleaving with NDJSON output on stdout
	if e.streamCfg.Enabled {
		log.SetOutput(os.Stderr)
	}

	maxIterations := e.streamCfg.MaxIterations

	for i := 0; maxIterations <= 0 || i < maxIterations; i++ {
		// Record turn start time for time_to_request_ms calculation
		e.turnStartTime = time.Now()
		// Reset firstStreamTime and firstTokenTime for this turn
		e.firstStreamTime = time.Time{}
		e.firstTokenTime = time.Time{}

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
				errStr := fmt.Sprintf("Maximum number of turns (%d) reached. stopping.", e.maxTurns)
				msg := StreamMessage{
					Type:            "result",
					Subtype:         "error",
					Result:          errStr,
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Model:           e.model,
					IsError:         true,
					StopReason:      "max_turns",
					TTFTMs:          0,
					TerminalReason:  "",
					APIErrorStatus:  &errStr,
					DurationMs:      time.Since(e.startTime).Milliseconds(),
					DurationAPIMs:   e.totalAPIDurationMs,
					TotalCostUSD:    e.costState.TotalCostUSD,
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
		e.mu.Unlock()

		// AC3: Reset structured output tool at start of each turn
		if e.structuredOutputTool != nil {
			e.structuredOutputTool.Reset()
		}

		// AC2: Budget enforcement - check before each API call
		if budgetUSD > 0 {
			if exceeded, _ := CheckBudgetExceeded(e.costState, budgetUSD); exceeded {
				if e.streamCfg.Enabled {
					errStr := fmt.Sprintf("budget exceeded: %.4f USD > %.4f USD limit", e.costState.TotalCostUSD, budgetUSD)
					msg := StreamMessage{
						Type:            "result",
						Subtype:         "error",
						Result:          errStr,
						SessionID:       sessionID,
						ParentToolUseID: nil,
						Uuid:            GenerateUUID(),
						Model:           e.model,
						IsError:         true,
						StopReason:      "budget_exceeded",
						TTFTMs:          0,
						TerminalReason:  "",
						APIErrorStatus:  &errStr,
						DurationMs:      time.Since(e.startTime).Milliseconds(),
						DurationAPIMs:   e.totalAPIDurationMs,
						TotalCostUSD:    e.costState.TotalCostUSD,
						ModelUsage:      e.buildModelUsage(),
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
				return "", fmt.Errorf("error_budget_exceeded: %.4f USD > %.4f USD limit", e.costState.TotalCostUSD, budgetUSD)
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
				Role:        api.RoleUser,
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
					summaryText := extractCompactSummary(compacted)
					messages = normalizeCompactedChain(compacted)
					preservedCount := len(compacted) - 1
					if err := e.persistCompactBoundary(preCompactTokens, preservedCount, "auto", summaryText); err != nil {
						log.Error("Failed to persist compaction boundary", "error", err)
					}
					// Re-inject active skills after compaction since the activation
					// tool results may have been summarized away.
					if reminder := activeSkillsSection(e.streamCfg.ActiveSkills); reminder != "" {
						messages = append(messages, api.Message{
							Role:      api.RoleUser,
							Content:   "[system]: " + reminder,
							IsVirtual: true,
						})
						e.persistSystemReminder(sessionID, reminder)
					}
					log.Debug("Context compaction succeeded", "newMessageCount", len(messages))
				} else {
					// Compaction failed - increment failure counter with persistence
					e.incrementCompactFailCount()
					e.mu.Lock()
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
		// Reset firstTokenTime and lastAPIStartTime for TTFT calculation per API call
		e.firstTokenTime = time.Time{}
		e.lastAPIStartTime = apiStartTime
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
				if msgStart, ok := block.RawEvent.(api.AnthropicStreamEvent); ok && msgStart.Type == api.EventMessageStart && msgStart.Message != nil {
					e.currentMessageID = msgStart.Message.ID
					e.currentUsage = api.Usage{
						InputTokens:              msgStart.Message.Usage.InputTokens,
						CacheReadInputTokens:     msgStart.Message.Usage.CacheReadInputTokens,
						CacheCreationInputTokens: msgStart.Message.Usage.CacheCreationInputTokens,
					}
				}
				// Capture stop_reason and usage from MessageDeltaEvent
				if msgDelta, ok := block.RawEvent.(api.AnthropicStreamEvent); ok && msgDelta.Type == api.EventMessageDelta && msgDelta.Delta != nil {
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
			case api.BlockTypeText:
				// Track firstStreamTime and TTFT: record time when first content block arrives
				if e.firstStreamTime.IsZero() {
					e.firstStreamTime = time.Now()
				}
				if e.firstTokenTime.IsZero() {
					e.firstTokenTime = time.Now()
				}
				// AC1: Redact secrets from text output before writing
				if e.secretRedactor != nil && e.secretRedactor.Enabled() {
					textOutput.WriteString(e.secretRedactor.Redact(block.Block.Text))
				} else {
					textOutput.WriteString(block.Block.Text)
				}
			case api.BlockTypeThinking:
				// Track firstStreamTime and TTFT: record time when first content block arrives
				if e.firstStreamTime.IsZero() {
					e.firstStreamTime = time.Now()
				}
				if e.firstTokenTime.IsZero() {
					e.firstTokenTime = time.Now()
				}
				thinkingBlocks = append(thinkingBlocks, thinkingBlock{Text: block.Block.Thinking, Signature: block.Block.Signature})
			case api.BlockTypeToolUse:
				// Track firstStreamTime and TTFT: record time when first content block arrives
				if e.firstStreamTime.IsZero() {
					e.firstStreamTime = time.Now()
				}
				if e.firstTokenTime.IsZero() {
					e.firstTokenTime = time.Now()
				}
				// Collect tool_use blocks for the assistant message
				toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
					ID:    block.Block.ToolID,
					Name:  block.Block.ToolName,
					Input: block.Block.ToolInput,
				})
			case api.BlockTypeWebSearchResult:
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
		// Only emit if streaming actually produced content; fallback path emits separately.
		if textOutput.Len() > 0 || len(toolUseBlocks) > 0 || len(thinkingBlocks) > 0 {
			e.emitConsolidatedAssistant(sessionID, thinkingBlocks, &textOutput, toolUseBlocks,
				e.currentMessageID, e.currentStopReason, e.currentStopSequence, toLoopUsage(e.currentUsage), e.model)
		}

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
				errStr := fmt.Sprintf("streaming error: %v", streamResult.Error)
				msg := StreamMessage{
					Type:            "result",
					Subtype:         "error",
					Result:          errStr,
					SessionID:       sessionID,
					ParentToolUseID: nil,
					Uuid:            GenerateUUID(),
					Model:           e.model,
					IsError:         true,
					StopReason:      "error",
					TTFTMs:          0,
					TerminalReason:  "",
					APIErrorStatus:  &errStr,
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
				case api.BlockTypeText:
					// AC1: Redact secrets from text output in fallback path
					if e.secretRedactor != nil && e.secretRedactor.Enabled() {
						textOutput.WriteString(e.secretRedactor.Redact(block.Text))
					} else {
						textOutput.WriteString(block.Text)
					}
				case api.BlockTypeThinking:
					thinkingBlocks = append(thinkingBlocks, thinkingBlock{Text: block.Thinking, Signature: block.Signature})
				case api.BlockTypeToolUse:
					// Collect tool_use blocks for the assistant message
					toolUseBlocks = append(toolUseBlocks, api.ToolUseBlock{
						ID:    block.ToolID,
						Name:  block.ToolName,
						Input: block.ToolInput,
					})
				case api.BlockTypeWebSearchResult:
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
					{"type": api.BlockTypeToolResult, "tool_use_id": result.WebSearchResult.ToolUseID, "content": toolResultContent},
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

		// Build and append assistant message with text, tool_use, and thinking blocks
		assistantMsg := api.Message{
			Role:    api.RoleAssistant,
			Content: textOutput.String(),
		}
		if len(thinkingBlocks) > 0 {
			var thinkingText strings.Builder
			for _, tb := range thinkingBlocks {
				thinkingText.WriteString(tb.Text)
			}
			assistantMsg.Thinking = thinkingText.String()
			// Use signature from last thinking block if present
			if thinkingBlocks[len(thinkingBlocks)-1].Signature != "" {
				assistantMsg.Signature = thinkingBlocks[len(thinkingBlocks)-1].Signature
			}
		}
		if len(toolUseBlocks) > 0 {
			assistantMsg.ToolUse = toolUseBlocks
		}
		if textOutput.String() != "" || len(toolUseBlocks) > 0 || len(thinkingBlocks) > 0 {
			messages = append(messages, assistantMsg)
		}

		// Persist assistant message to transcript BEFORE tool execution
		if e.sessionManager != nil && (textOutput.String() != "" || len(toolUseBlocks) > 0 || len(thinkingBlocks) > 0) {
			entry := session.TranscriptEntry{
				Type:    session.EntryTypeAssistant,
				Content: textOutput.String(),
				CWD:     cwd,
			}
			// Persist thinking blocks (concatenated text with signature from last block)
			if len(thinkingBlocks) > 0 {
				var thinkingText strings.Builder
				for _, tb := range thinkingBlocks {
					thinkingText.WriteString(tb.Text)
				}
				entry.Thinking = thinkingText.String()
				// Use signature from last thinking block if present
				if thinkingBlocks[len(thinkingBlocks)-1].Signature != "" {
					entry.Signature = thinkingBlocks[len(thinkingBlocks)-1].Signature
				}
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

		// Execute all tools using the parallel executor with cross-turn state support
		execToolResults, hasSynthetic, execErr := e.executeAndProcessTools(ctx, toolUseBlocks, sessionID, cwd)
		if execErr != nil {
			return "", fmt.Errorf("executing tools: %w", execErr)
		}
		toolResults = append(toolResults, execToolResults...)

		// Sync active skills and inject a reminder message if they changed.
		// (The executeAndProcessTools function also calls syncActiveSkills, but this
		// is the additional cross-turn reminder injection that must happen at loop level)
		prevSkillCount := len(e.streamCfg.ActiveSkills)
		e.syncActiveSkills()
		if len(e.streamCfg.ActiveSkills) != prevSkillCount {
			if reminder := activeSkillsSection(e.streamCfg.ActiveSkills); reminder != "" {
				reminderMsg := api.Message{
					Role:      api.RoleUser,
					Content:   "[system]: " + reminder,
					IsVirtual: true,
				}
				messages = append(messages, reminderMsg)
				e.persistSystemReminder(sessionID, reminder)
			}
		}

		// AC3: When synthetic results were generated, detach the loop context
		// from cancellation so the next iteration can deliver them to the model
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

		// Delegate to extracted handler
		result, err, shouldContinue := e.handleStopReason(ctx, resp, *streamResult, textOutput, sessionID, toolResults, toolUseBlocks, thinkingBlocks, &assistantMsg, &messages)
		if shouldContinue {
			continue
		}
		if err != nil {
			return result, err
		}
		return result, nil
	}

	return "", fmt.Errorf("max iterations (%d) exceeded", maxIterations)
}
