// Package agent provides the core agent loop and query engine.
// File layout: engine.go (constructor, config), engine_loop.go (loop logic),
// engine_stream.go (streaming emission). Total ~1546 lines (within ±30 tolerance).
package agent

import (
	"slices"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/redact"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
)

// QueryEngine orchestrates the agent query lifecycle with structured
// persist-before-API ordering, turn limits, and cost state management.
type QueryEngine struct {
	client         api.Requester
	sessionManager *session.Manager
	costState      *CostState
	tools          []tool.Tool
	toolParams     []ToolParam
	streamCfg      StreamConfig
	model          string
	startTime      time.Time
	turnCount      int
	maxTurns       int
	cwd            string
	mu             sync.Mutex

	// Compaction state
	compactConfig    CompactConfig
	compactFailCount int

	// Session memory
	sessionMemory *SessionMemory

	// Memory extraction
	memExtractor *MemoryExtractor

	// Structured output (AC3)
	structuredOutputTool   *tool.StructuredOutputTool
	structuredOutputResult string // Captured result from StructuredOutput tool call

	// API timing (AC3: duration_api_ms)
	lastAPIStartTime   time.Time
	totalAPIDurationMs int64

	// Stream event state (for assistant event construction)
	currentMessageID    string
	currentStopReason   string
	currentStopSequence string
	currentUsage        api.Usage

	// Secret redaction (AC1: secret redaction)
	secretRedactor *redact.SecretRedactor

	// Skill activator for tracking active skills in system prompt
	skillActivator tool.SkillActivator
}

// QueryEngineOption defines a functional option for QueryEngine.
type QueryEngineOption func(*QueryEngine)

// WithClient sets a custom API client for the QueryEngine.
func WithClient(client api.Requester) QueryEngineOption {
	return func(e *QueryEngine) {
		e.client = client
	}
}

// WithCWD sets the working directory for the QueryEngine.
func WithCWD(cwd string) QueryEngineOption {
	return func(e *QueryEngine) {
		e.cwd = cwd
	}
}

// WithSkillActivator sets the skill activator for tracking active skills.
func WithSkillActivator(activator tool.SkillActivator) QueryEngineOption {
	return func(e *QueryEngine) {
		e.skillActivator = activator
	}
}

// NewQueryEngine creates a new QueryEngine with the given configuration.
func NewQueryEngine(cfg StreamConfig, tools []tool.Tool, model string, opts ...QueryEngineOption) *QueryEngine {
	// AC1/AC4: Inject StructuredOutputTool for non-interactive sessions with schema
	var structuredTool *tool.StructuredOutputTool
	if cfg.StructuredSchema != nil && cfg.Enabled {
		// AC1: Check deny rules - if StructuredOutput is explicitly denied, panic (unrecoverable config)
		if slices.Contains(cfg.StructuredDenyRules, "StructuredOutput") {
			panic("StructuredOutput tool denied but schema is configured")
		}
		// Create the structured output tool
		structuredTool = tool.NewStructuredOutputTool(cfg.StructuredSchema)
		tools = append(tools, structuredTool)
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
		} else if reqAny, ok := schema["required"].([]any); ok {
			for _, r := range reqAny {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}

		// Extract extra fields ($defs, $schema, etc.) for third-party API compatibility (AC1, AC3)
		extraFields := make(map[string]any)
		for k, v := range schema {
			if k != "type" && k != "properties" && k != "required" {
				extraFields[k] = v
			}
		}

		tp := ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: ToolInputSchema{
				Type:        "object",
				Properties:  props,
				Required:    required,
				ExtraFields: extraFields,
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
		// AC3: Restore frozen system prompt from transcript for prompt caching
		if cfg.SessionManager != nil {
			if sp, err := cfg.SessionManager.LoadSystemPrompt(sessionID); err == nil && sp != "" {
				cfg.CachedSystemPrompt = sp
				log.Debug("System prompt restored from transcript", "sessionID", sessionID, "len", len(sp))
			}
		}
		// AC2: Seed readFileState from transcript for read-before-write optimization on resume
		if cfg.ReadFileCache != nil {
			if err := seedReadFileCacheFromTranscript(cfg.ReadFileCache, cfg.SessionManager, sessionID); err != nil {
				log.Debug("Failed to seed readFileCache from transcript", "sessionID", sessionID, "error", err)
			}
		}
	}

	e := &QueryEngine{
		sessionManager:       cfg.SessionManager,
		costState:            costState,
		tools:                tools,
		toolParams:           toolParams,
		streamCfg:            cfg,
		turnCount:            0,
		maxTurns:             cfg.MaxTurns,
		compactFailCount:     compactFailCount,
		structuredOutputTool: structuredTool,
		secretRedactor:       redact.NewSecretRedactor(cfg.RedactMode),
		startTime:            time.Now(),
	}

	// Apply options
	for _, opt := range opts {
		opt(e)
	}

	// Default client if none provided
	if e.client == nil {
		client, err := api.NewClientWithModel(model)
		if err != nil {
			// Client creation error will be reported on first API call
			log.Debug("QueryEngine: API client creation warning", "error", err)
		}
		e.client = client
	}

	// Wire thinking config (Effort) from StreamConfig to client/provider
	if cfg.Effort != "" && e.client != nil {
		e.client.SetThinkingConfig(api.ThinkingConfig{
			Effort: cfg.Effort,
		})
	}

	// Finalize model and compact config
	if e.model == "" {
		if model != "" {
			e.model = model
		} else if c, ok := e.client.(interface{ GetModel() string }); ok {
			e.model = c.GetModel()
		}
	}
	e.compactConfig = newCompactConfigForModel(e.model)

	// Wire context into tools
	e.WireTools()

	// Initialize session memory
	e.sessionMemory = NewSessionMemory(sessionID, e.client, e.compactConfig)

	return e
}

// WireTools injects context (ReadFileCache, SessionID) from StreamConfig into tools.
func (e *QueryEngine) WireTools() {
	cache := e.streamCfg.ReadFileCache
	sessionID := e.streamCfg.SessionID

	for _, t := range e.tools {
		// Wire ReadFileCache
		if cache != nil {
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

		// Wire SessionID
		if sessionID != "" {
			switch t := t.(type) {
			case *tool.BashTool:
				t.WithSessionID(sessionID)
			case *tool.ReadTool:
				t.WithSessionID(sessionID)
			case *tool.WriteTool:
				t.WithSessionID(sessionID)
			case *tool.EditTool:
				t.WithSessionID(sessionID)
			case *tool.NotebookEditTool:
				t.WithSessionID(sessionID)
			case *tool.ReadMcpResourceTool:
				t.WithSessionID(sessionID)
			}
		}
	}
}

// SetMaxTurns sets the maximum number of turns for this engine.
func (e *QueryEngine) SetMaxTurns(maxTurns int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.maxTurns = maxTurns
}

// SetMaxBudgetUsd sets the budget limit in USD for cost enforcement.
// When set, the engine checks accumulated cost against this budget before each API call.
// If exceeded, the loop terminates with error type "error_budget_exceeded".
// A value of 0 means no budget limit (skips cost checking for performance).
func (e *QueryEngine) SetMaxBudgetUsd(amount float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.streamCfg.MaxBudgetUSD = amount
}

// setMemExtractorForTest sets the memExtractor field for testing purposes.
// This is only for use in test files within the agent package.
func (e *QueryEngine) setMemExtractorForTest(memExtractor *MemoryExtractor) {
	e.memExtractor = memExtractor
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
			CWD:              e.cwd,
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

// getTaskManager returns the TaskManager from the BashTool if available.
func (e *QueryEngine) getTaskManager() *tool.TaskManager {
	for _, t := range e.tools {
		if bt, ok := t.(*tool.BashTool); ok {
			return bt.GetTaskManager()
		}
	}
	return nil
}

// persistCompactBoundary persists a compaction boundary entry to the transcript.
// This is called after successful context compaction to record the boundary for
// future session resume filtering. Returns an error if the write fails.
func (e *QueryEngine) persistCompactBoundary(preTokens int, preservedCount int, trigger string) error {
	if e.sessionManager == nil || e.streamCfg.SessionID == "" {
		return nil
	}
	entry := session.TranscriptEntry{
		Type:    "system",
		Subtype: "compact_boundary",
		CompactMetadata: &session.CompactMetadata{
			Trigger:          trigger,
			PreTokens:        preTokens,
			PreservedSegment: preservedCount,
		},
		CWD: e.cwd,
	}
	if err := e.sessionManager.AppendEntry(e.streamCfg.SessionID, entry); err != nil {
		log.Error("Failed to persist compaction boundary", "error", err)
		return err
	}
	return nil
}

// syncActiveSkills syncs the activated skills from the skill activator to StreamConfig.
// This should be called after each tool execution to keep the system prompt in sync.
func (e *QueryEngine) syncActiveSkills() {
	if e.skillActivator == nil {
		return
	}

	// Check if the activator supports GetActivatedSkills
	type activatorWithSkills interface {
		GetActivatedSkills() []skills.ActivatedSkill
	}
	if activator, ok := e.skillActivator.(activatorWithSkills); ok {
		// Convert from skills.ActivatedSkill to agent.ActivatedSkill
		skillsList := activator.GetActivatedSkills()
		activated := make([]ActivatedSkill, len(skillsList))
		for i, s := range skillsList {
			activated[i] = ActivatedSkill{Name: s.Name, RootPath: s.RootPath}
		}
		e.streamCfg.SetActiveSkills(activated)
	}
}
