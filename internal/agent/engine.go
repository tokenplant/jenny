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
	"github.com/ipy/jenny/internal/git"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/memdir"
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

	// Session memory
	sessionMemory *SessionMemory

	// Memory extraction
	memExtractor *MemoryExtractor
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
		tp := ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: ToolInputSchema{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		}
		// Set MaxUses for web_search to enforce max results at definition level
		if t.Name() == "web_search" {
			maxUses := int64(tool.WebSearchMaxResults)
			tp.MaxUses = &maxUses
		}
		toolParams = append(toolParams, tp)
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
		// AC2: Seed readFileState from transcript for read-before-write optimization on resume
		if cfg.ReadFileCache != nil {
			if err := seedReadFileCacheFromTranscript(cfg.ReadFileCache, cfg.SessionManager, sessionID); err != nil {
				log.Debug("Failed to seed readFileCache from transcript", "sessionID", sessionID, "error", err)
			}
		}
	}

	engine := &QueryEngine{
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

	// Wire ReadFileCache from StreamConfig into tools that support it
	engine.WireReadFileCache()

	// Initialize session memory
	engine.sessionMemory = NewSessionMemory(sessionID, client, engine.compactConfig)

	return engine
}

// WireReadFileCache injects the ReadFileCache from StreamConfig into tools
// that support read-before-write enforcement (Read, Write, Edit, NotebookEdit).
// This enables the engine to own the cache lifecycle.
func (e *QueryEngine) WireReadFileCache() {
	if e.streamCfg.ReadFileCache == nil {
		return
	}
	cache := e.streamCfg.ReadFileCache
	for _, t := range e.tools {
		switch t := t.(type) {
		case *tool.ReadTool:
			t.WithReadFileCache(cache)
		case *tool.WriteTool:
			t.WithReadFileCache(cache)
		case *tool.EditTool:
			t.WithReadFileCache(cache)
		case *tool.NotebookEditTool:
			t.WithReadFileCache(cache)
		}
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

	// AC3: When stream-json mode is active, redirect debug logs to stderr
	// to prevent interleaving with NDJSON output on stdout
	if e.streamCfg.Enabled {
		log.SetOutput(os.Stderr)
	}

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
		budgetCNY := e.streamCfg.MaxBudgetCNY
		e.mu.Unlock()

		// AC2: Budget enforcement - check before each API call
		// Use CNY budget if currency is CNY, otherwise use USD budget
		currency := e.costState.Currency
		if currency == "CNY" && budgetCNY > 0 {
			if exceeded, _ := CheckBudgetExceeded(e.costState, budgetCNY, "CNY"); exceeded {
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
							TotalCostCNY:             e.costState.TotalCostCNY,
						},
						IsError: true,
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
						TotalCostUSD:             e.costState.TotalCostUSD,
					}
					if e.costState.Currency == "CNY" {
						usage.TotalCostCNY = e.costState.TotalCostCNY
					}
					msg := StreamMessage{
						Type:      "result",
						Result:    textOutput.String(),
						SessionID: sessionID,
						Model:     resp.Model,
						Usage:     usage,
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
				usage := &Usage{
					InputTokens:              resp.Usage.InputTokens,
					OutputTokens:             resp.Usage.OutputTokens,
					CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
					CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
					TotalCostUSD:             e.costState.TotalCostUSD,
				}
				if e.costState.Currency == "CNY" {
					usage.TotalCostCNY = e.costState.TotalCostCNY
				}
				msg := StreamMessage{
					Type:      "result",
					Result:    textOutput.String(),
					SessionID: sessionID,
					Model:     resp.Model,
					Usage:     usage,
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			// AC2: Reset compaction failure counter on successful API response
			e.resetCompactFailCount()

			// Check and run memory extraction before returning
			if e.memExtractor != nil && resp.StopReason != "" {
				e.memExtractor.CheckAndExtract(ctx, TurnContext{
					StopReason:       resp.StopReason,
					AssistantMessage: &assistantMsg,
					TotalMessages:    len(messages),
					RecentMessages:   messages,
				})
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
				usage := &Usage{
					InputTokens:              resp.Usage.InputTokens,
					OutputTokens:             resp.Usage.OutputTokens,
					CacheReadInputTokens:     resp.Usage.CacheReadInputTokens,
					CacheCreationInputTokens: resp.Usage.CacheCreationInputTokens,
					TotalCostUSD:             e.costState.TotalCostUSD,
				}
				if e.costState.Currency == "CNY" {
					usage.TotalCostCNY = e.costState.TotalCostCNY
				}
				msg := StreamMessage{
					Type:      "result",
					Result:    textOutput.String(),
					SessionID: sessionID,
					Model:     resp.Model,
					Usage:     usage,
				}
				data, _ := json.Marshal(msg)
				fmt.Fprintln(os.Stdout, string(data))
			}
			// AC2: Reset compaction failure counter on successful API response
			e.resetCompactFailCount()

			// Check and run memory extraction before returning
			if e.memExtractor != nil {
				e.memExtractor.CheckAndExtract(ctx, TurnContext{
					StopReason:       resp.StopReason,
					AssistantMessage: &assistantMsg,
					TotalMessages:    len(messages),
					RecentMessages:   messages,
				})
			}

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

// initMemoryExtractor initializes the memory extractor with the project root.
// It is called lazily when auto-memory is enabled and the project root is derived.
func (e *QueryEngine) initMemoryExtractor(projectRoot string) {
	if e.memExtractor != nil {
		return // Already initialized
	}
	e.memExtractor = NewMemoryExtractor(e.client, ExtractorConfig{
		IsSubAgent:         false,
		ExtractEveryNTurns: 1,
		AutoMemoryEnabled:  e.streamCfg.AutoMemoryEnabled,
		ProjectRoot:        projectRoot,
		SessionID:          e.streamCfg.SessionID,
	})
}

// Drain waits for any in-progress memory extraction to complete.
// Used during shutdown to ensure clean termination.
func (e *QueryEngine) Drain(ctx context.Context) {
	if e.memExtractor == nil {
		return
	}
	e.memExtractor.Drain(ctx)
}

// getTaskManager returns the TaskManager from the BashTool if available.
func (e *QueryEngine) getTaskManager() *tool.TaskManager {
	for _, t := range e.tools {
		if bt, ok := t.(*tool.BashTool); ok {
			return bt.GetTaskManager()
		}
	}
	return nil
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
						cache.Add(path, entry.Content, info.ModTime(), true)
					}
				}
			}
		}
	}

	return nil
}
