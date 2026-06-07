// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// QueryEngine orchestrates the agent query lifecycle with structured
// persist-before-API ordering, turn limits, and cost state management.
type QueryEngine struct {
	client         *api.Client
	sessionManager *session.Manager
	costState      *CostState
	tools          []tool.Tool
	toolParams     []ToolParam
	streamCfg      StreamConfig
	model          string
	turnCount      int
	maxTurns       int
	mu             sync.Mutex

	// Compaction state
	compactConfig    CompactConfig
	compactFailCount int
}

// NewQueryEngine creates a new QueryEngine with the given configuration.
func NewQueryEngine(cfg StreamConfig, tools []tool.Tool, model string) *QueryEngine {
	client, err := api.NewClientWithModel(model)
	if err != nil {
		// Client creation error will be reported on first API call
		log.Debug("QueryEngine: API client creation warning", "error", err)
	}

	// Derive tool params from tool list
	toolParams := make([]ToolParam, 0, len(tools))
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
		toolParams = append(toolParams, ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		})
	}

	// Initialize cost state (restore from disk if resuming)
	costState := &CostState{}
	sessionID := cfg.SessionID
	compactFailCount := 0
	if cfg.IsResume && sessionID != "" {
		if restored, ok, err := RestoreCostState(sessionID); err == nil && ok {
			costState = restored
			log.Debug("Cost state restored", "sessionID", sessionID, "totalCostUSD", costState.TotalCostUSD)
		}
		// Restore compactFailCount from transcript
		if cfg.SessionManager != nil {
			if count, err := cfg.SessionManager.LoadCompactFailCount(sessionID); err == nil {
				compactFailCount = count
				log.Debug("Compact fail count restored", "sessionID", sessionID, "count", count)
			}
		}
	}

	return &QueryEngine{
		client:           client,
		sessionManager:   cfg.SessionManager,
		costState:        costState,
		tools:            tools,
		toolParams:       toolParams,
		streamCfg:        cfg,
		model:            model,
		turnCount:        0,
		maxTurns:         0, // 0 means unlimited
		compactConfig:    newCompactConfig(),
		compactFailCount: compactFailCount,
	}
}

// SetMaxTurns sets the maximum number of turns for this engine.
func (e *QueryEngine) SetMaxTurns(maxTurns int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.maxTurns = maxTurns
}

// SubmitMessage runs a single query turn: persist message, run agent loop,
// flush state on completion. Returns the text result and error.
func (e *QueryEngine) SubmitMessage(ctx context.Context, prompt string) (string, error) {
	e.mu.Lock()
	// Reset turn counter for this submit
	e.turnCount = 0
	sessionID := e.streamCfg.SessionID
	isResume := e.streamCfg.IsResume
	sessionManager := e.sessionManager
	historyMessages := e.streamCfg.HistoryMessages
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
			}); err != nil {
				return "", fmt.Errorf("persisting user message to transcript: %w", err)
			}
		}
	}

	// Get working directory
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
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

	for range MaxIterations {
		e.mu.Lock()
		// AC2: maxTurns enforcement - check before each API call
		if e.maxTurns > 0 && e.turnCount >= e.maxTurns {
			e.mu.Unlock()
			// Emit error result if streaming enabled
			if e.streamCfg.Enabled {
				msg := StreamMessage{
					Type:      "result",
					SessionID: sessionID,
					Model:     e.model,
					Usage: &Usage{
						InputTokens:              0,
						OutputTokens:             0,
						CacheReadInputTokens:     0,
						CacheCreationInputTokens: 0,
						TotalCostUSD:             e.costState.TotalCostUSD,
					},
					IsError: true,
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			return "", fmt.Errorf("error_max_turns: limit reached at turn %d", e.turnCount)
		}
		// Increment turn counter at start of each API iteration
		e.turnCount++
		currentTurn := e.turnCount
		budgetUSD := e.streamCfg.MaxBudgetUSD
		e.mu.Unlock()

		// AC2: Budget enforcement - check before each API call
		if budgetUSD > 0 {
			if exceeded, _ := CheckBudgetExceeded(e.costState, budgetUSD); exceeded {
				if e.streamCfg.Enabled {
					msg := StreamMessage{
						Type:      "result",
						SessionID: sessionID,
						Model:     e.model,
						Usage: &Usage{
							InputTokens:              0,
							OutputTokens:             0,
							CacheReadInputTokens:     0,
							CacheCreationInputTokens: 0,
							TotalCostUSD:             e.costState.TotalCostUSD,
						},
						IsError: true,
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
				Type: "stream_request_start",
			}
			data, _ := json.Marshal(msg)
			fmt.Fprintln(os.Stdout, string(data))
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
				// Attempt compaction
				compacted, err := e.compactMessages(ctx, messages, e.compactConfig, systemPrompt)
				if err == nil {
					// Compaction succeeded - normalize the compacted chain
					messages = normalizeCompactedChain(compacted)
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

		// Normalize messages before API request (strip internal fields, enforce tool pairing, etc.)
		messages = normalizeMessages(messages)

		// Create fallback function for streaming failures (AC3)
		fallbackFn := func(fallbackCtx context.Context) (*api.Response, error) {
			return e.client.SendMessage(fallbackCtx, messages, e.toolParams, nil, systemPrompt)
		}

		// Use streaming API (AC1)
		blocksChan, streamResult := e.client.SendMessageStream(
			ctx,
			messages,
			e.toolParams,
			nil,
			systemPrompt,
			api.DefaultIdleTimeout,
			api.DefaultFallbackTimeout,
			fallbackFn,
		)

		// Process streaming blocks
		var textOutput strings.Builder
		var toolResults []api.ToolResult
		var toolUseBlocks []api.ToolUseBlock

		// Process blocks as they arrive
		for block := range blocksChan {
			switch block.Block.Type {
			case "text":
				textOutput.WriteString(block.Block.Text)
				if e.streamCfg.Enabled && e.streamCfg.IncludePartial {
					// Output partial text as we receive it
					msg := StreamMessage{
						Type:       "message",
						Content:    block.Block.Text,
						SessionID:  sessionID,
						IsPartial:  true,
						MessageIdx: currentTurn,
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

				if e.streamCfg.Enabled {
					// Output tool use event
					msg := StreamMessage{
						Type:       "tool_use",
						SessionID:  sessionID,
						ToolName:   block.Block.ToolName,
						ToolInput:  block.Block.ToolInput,
						MessageIdx: currentTurn,
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
			}
		}

		// Check if streaming completed with error
		if streamResult.Error != "" && len(streamResult.Blocks) == 0 {
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

		// Execute all tools using the parallel executor
		executor := NewToolExecutor(e.tools, cwd)
		execResults, err := executor.Execute(ctx, execBlocks)
		if err != nil {
			return "", fmt.Errorf("executing tools: %w", err)
		}

		// Process results and collect for API response
		for _, res := range execResults {
			toolResults = append(toolResults, api.ToolResult{
				ToolUseID: res.ToolUseID,
				Content:   res.Content,
				IsError:   res.IsError,
			})

			// Persist tool result to transcript AFTER assistant message
			if e.sessionManager != nil {
				if err := e.sessionManager.AppendEntry(sessionID, session.TranscriptEntry{
					Type:    "tool_result",
					ToolID:  res.ToolUseID,
					Content: res.Content,
					IsError: res.IsError,
				}); err != nil {
					return "", fmt.Errorf("persisting tool result to transcript: %w", err)
				}
			}

			if e.streamCfg.Enabled {
				// Output tool result event
				msg := StreamMessage{
					Type:       "tool_result",
					SessionID:  sessionID,
					Content:    res.Content,
					IsError:    res.IsError,
					MessageIdx: currentTurn,
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
						IsError:   tr.IsError,
					})
				}
				messages = append(messages, userMsg)
				// end_turn means the model is done - output final result
				if e.streamCfg.Enabled {
					msg := StreamMessage{
						Type:      "result",
						Result:    textOutput.String(),
						SessionID: sessionID,
						Model:     resp.Model,
						Usage: &Usage{
							InputTokens:              resp.Usage.InputTokens,
							OutputTokens:             resp.Usage.OutputTokens,
							CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
							CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
							TotalCostUSD:             e.costState.TotalCostUSD,
						},
					}
					data, _ := json.Marshal(msg)
					fmt.Fprintln(os.Stdout, string(data))
				}
				// AC2: Reset compaction failure counter on successful API response
				e.resetCompactFailCount()
				return textOutput.String(), nil
			}
			// Output final result
			if e.streamCfg.Enabled {
				msg := StreamMessage{
					Type:      "result",
					Result:    textOutput.String(),
					SessionID: sessionID,
					Model:     resp.Model,
					Usage: &Usage{
						InputTokens:              resp.Usage.InputTokens,
						OutputTokens:             resp.Usage.OutputTokens,
						CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
						CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
						TotalCostUSD:             e.costState.TotalCostUSD,
					},
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			// AC2: Reset compaction failure counter on successful API response
			e.resetCompactFailCount()
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
						IsError:   tr.IsError,
					})
				}
				messages = append(messages, userMsg)
			}
			continue

		case api.StopReasonMaxTokens:
			return textOutput.String(), fmt.Errorf("max tokens reached")

		case api.StopReasonStopSeq:
			if e.streamCfg.Enabled {
				msg := StreamMessage{
					Type:      "result",
					Result:    textOutput.String(),
					SessionID: sessionID,
					Model:     resp.Model,
					Usage: &Usage{
						InputTokens:              resp.Usage.InputTokens,
						OutputTokens:             resp.Usage.OutputTokens,
						CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
						CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
						TotalCostUSD:             e.costState.TotalCostUSD,
					},
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			// AC2: Reset compaction failure counter on successful API response
			e.resetCompactFailCount()
			return textOutput.String(), nil
		}

		// If we get here without text output and without tool results, something is wrong
		if textOutput.String() == "" && len(toolResults) == 0 && len(toolUseBlocks) == 0 {
			return "", fmt.Errorf("unexpected empty response")
		}
	}

	return "", fmt.Errorf("max iterations (%d) exceeded", MaxIterations)
}

// TurnCount returns the current turn count for diagnostics.
func (e *QueryEngine) TurnCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.turnCount
}

// resetCompactFailCount resets the compaction failure counter on successful API response.
// AC2: Circuit breaker resets on success.
func (e *QueryEngine) resetCompactFailCount() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.compactFailCount = 0
	// Persist to transcript
	e.persistCompactFailCount()
}

// incrementCompactFailCount increments the compaction failure counter.
// AC2: Circuit breaker tracks consecutive failures.
func (e *QueryEngine) incrementCompactFailCount() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.compactFailCount++
	// Persist to transcript
	e.persistCompactFailCount()
}

// persistCompactFailCount saves the current compactFailCount to the transcript.
func (e *QueryEngine) persistCompactFailCount() {
	if e.sessionManager != nil && e.streamCfg.SessionID != "" {
		_ = e.sessionManager.AppendEntry(e.streamCfg.SessionID, session.TranscriptEntry{
			Type:             "state",
			CompactFailCount: e.compactFailCount,
		})
	}
}

// CompactFailCount returns the current compaction failure count for diagnostics.
func (e *QueryEngine) CompactFailCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.compactFailCount
}
