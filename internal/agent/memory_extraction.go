// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/memdir"
	"github.com/ipy/jenny/internal/tool"
)

// ExtractorConfig holds configuration for the memory extractor.
type ExtractorConfig struct {
	// IsSubAgent indicates whether this engine is running as a sub-agent.
	// Extraction only runs for the main agent.
	IsSubAgent bool

	// ExtractEveryNTurns is the throttle setting (default 1).
	ExtractEveryNTurns int

	// AutoMemoryEnabled indicates whether auto-memory is enabled.
	AutoMemoryEnabled bool

	// ProjectRoot is the git repository root path.
	ProjectRoot string

	// SessionID is the current session ID.
	SessionID string
}

// MemoryExtractor manages end-of-turn memory extraction to auto-mem directory.
type MemoryExtractor struct {
	memdir    string
	client    APIClient
	config    ExtractorConfig
	readCache *tool.ReadFileCache

	// Cursor tracking
	lastMemoryMessageUuid  string
	lastMemoryMessageCount int

	// Throttle
	turnsSinceLastExtract int

	// Coalescing
	inProgress bool
	pendingCtx context.Context
	mu         sync.Mutex

	// Timeout for extraction
	timeout time.Duration
}

// NewMemoryExtractor creates a new MemoryExtractor.
func NewMemoryExtractor(client APIClient, config ExtractorConfig) *MemoryExtractor {
	// Use memdir package to compute the path
	memdirPath := memdir.MemoryPathFromProjectRoot(config.ProjectRoot)

	return &MemoryExtractor{
		memdir:                memdirPath,
		client:                client,
		config:                config,
		readCache:             tool.NewReadFileCache(),
		turnsSinceLastExtract: 0,
		timeout:               15 * time.Second,
	}
}

// WithMemdir sets a custom memory directory, overriding the default path.
// This is primarily for test isolation.
func (me *MemoryExtractor) WithMemdir(dir string) *MemoryExtractor {
	me.memdir = dir
	return me
}

// WithTimeout sets a custom timeout for extraction.
// This is primarily for testing.
func (me *MemoryExtractor) WithTimeout(timeout time.Duration) *MemoryExtractor {
	me.timeout = timeout
	return me
}

// CheckAndExtract determines whether to run memory extraction after a turn.
// It returns immediately if:
// - Sub-agent (AC4: main agent only)
// - Auto-memory disabled
// - Stop reason was tool_use (AC1: only run on end_turn or stop_sequence)
// - Throttle not yet exceeded
// - Extraction already in progress (AC5: coalescing)
func (me *MemoryExtractor) CheckAndExtract(ctx context.Context, turnCtx TurnContext) {
	// AC4: Skip for sub-agents
	if me.config.IsSubAgent {
		return
	}

	// Skip if auto-memory disabled
	if !me.config.AutoMemoryEnabled {
		return
	}

	// AC1: Only run on end_turn or stop_sequence, not max_tokens, tool_use, etc.
	if turnCtx.StopReason != api.StopReasonEndTurn && turnCtx.StopReason != api.StopReasonStopSeq {
		return
	}

	// Skip if no assistant message (nothing to extract from)
	if turnCtx.AssistantMessage == nil {
		return
	}

	// Check throttle
	me.turnsSinceLastExtract++
	if me.turnsSinceLastExtract < me.extractEvery() {
		return
	}

	// AC2: Check if main agent already wrote to auto-mem paths
	if me.mainAgentWroteToAutoMem(turnCtx) {
		me.advanceCursor(turnCtx)
		return
	}

	// AC5: Coalescing - if in progress, stash context for trailing run
	me.mu.Lock()
	if me.inProgress {
		me.pendingCtx = ctx
		me.mu.Unlock()
		log.Debug("Memory extraction coalescing: in progress, stashing context")
		return
	}
	me.inProgress = true
	me.mu.Unlock()

	// Run extraction synchronously
	go func() {
		defer me.finalizeExtraction()

		extractCtx, cancel := context.WithTimeout(context.Background(), me.timeout)
		defer cancel()

		if err := me.extract(extractCtx, turnCtx); err != nil {
			log.Warn("Memory extraction failed", "error", err)
			return
		}

		me.turnsSinceLastExtract = 0
		me.advanceCursor(turnCtx)
	}()
}

// extractEvery returns the throttle interval.
func (me *MemoryExtractor) extractEvery() int {
	if me.config.ExtractEveryNTurns <= 0 {
		return 1
	}
	return me.config.ExtractEveryNTurns
}

// mainAgentWroteToAutoMem checks if the main agent wrote to auto-mem paths.
// AC2: Skip extraction if main agent already updated memory.
func (me *MemoryExtractor) mainAgentWroteToAutoMem(turnCtx TurnContext) bool {
	if turnCtx.AssistantMessage == nil {
		return false
	}

	// Check tool_use blocks for paths under auto-mem directory
	for _, tu := range turnCtx.AssistantMessage.ToolUse {
		if tu.Name == "edit" || tu.Name == "write" || tu.Name == "Edit" || tu.Name == "Write" {
			if filePath, ok := tu.Input["file_path"].(string); ok {
				if me.isUnderAutoMem(filePath) {
					log.Debug("Main agent wrote to auto-mem, skipping extraction", "path", filePath)
					return true
				}
			}
		}
	}
	return false
}

// isUnderAutoMem checks if a path is under the auto-mem directory.
func (me *MemoryExtractor) isUnderAutoMem(filePath string) bool {
	absMemdir, err := filepath.Abs(me.memdir)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}
	// The path itself is not "under" the memdir
	if absPath == absMemdir {
		return false
	}
	return strings.HasPrefix(absPath, absMemdir)
}

// advanceCursor advances the extraction cursor after successful extraction or skip.
func (me *MemoryExtractor) advanceCursor(turnCtx TurnContext) {
	if turnCtx.AssistantMessage == nil {
		return
	}

	// Store the last message UUID
	if turnCtx.AssistantMessage.ID != "" {
		me.lastMemoryMessageUuid = turnCtx.AssistantMessage.ID
	} else {
		// AC3: UUID missing after compaction - fall back to count
		me.lastMemoryMessageCount = turnCtx.TotalMessages
		me.lastMemoryMessageUuid = ""
	}
}

// extract runs the forked extraction agent.
func (me *MemoryExtractor) extract(ctx context.Context, turnCtx TurnContext) error {
	// Ensure memdir exists
	if err := os.MkdirAll(me.memdir, 0755); err != nil {
		return err
	}

	// Build restricted tool set for the forked agent
	tools := me.buildExtractionTools()

	// Build tool params
	toolParams := me.buildToolParams(tools)

	// Pre-inject memory manifest to avoid extra ls turn
	manifest := me.buildMemoryManifest()

	// Build the extraction prompt
	prompt := me.buildExtractionPrompt(turnCtx, manifest)

	// Make the API call with timeout
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	systemPrompt := "You are a memory extraction assistant. Your task is to identify durable facts, preferences, and context from the conversation and write them to the appropriate memory files in the auto-mem directory. You may use Read to examine existing memory files, and Edit/Write to update them. Focus on extracting: user preferences (user/), feedback about behavior (feedback/), project state and decisions (project/), and external references (reference/)."

	resp, err := me.client.SendMessage(ctx, messages, toolParams, nil, systemPrompt)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Warn("Memory extraction timed out")
			return ctx.Err()
		}
		return err
	}

	// Process response - handle tool_use blocks
	me.processExtractionResponse(ctx, resp, tools)

	return nil
}

// buildExtractionTools creates the restricted tool set for extraction.
func (me *MemoryExtractor) buildExtractionTools() []tool.Tool {
	// Read tool - unrestricted
	readTool := tool.NewReadTool(true, me.readCache)

	// Grep tool - unrestricted
	grepTool := tool.NewGrepTool()

	// Glob tool - unrestricted
	globTool := tool.NewGlobTool()

	// Edit tool - scoped to auto-mem dir
	editTool := tool.NewEditTool(me.readCache)
	editTool.SetAllowedPaths([]string{me.memdir})

	// Write tool - scoped to auto-mem dir
	writeTool := tool.NewWriteTool(me.readCache)
	writeTool.SetAllowedPaths([]string{me.memdir})

	return []tool.Tool{
		readTool,
		grepTool,
		globTool,
		editTool,
		writeTool,
	}
}

// buildToolParams converts tools to API tool params.
func (me *MemoryExtractor) buildToolParams(tools []tool.Tool) []api.ToolParam {
	params := make([]api.ToolParam, 0, len(tools))
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
		params = append(params, api.ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: api.ToolInputSchema{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		})
	}
	return params
}

// buildMemoryManifest creates a manifest of existing memory files.
func (me *MemoryExtractor) buildMemoryManifest() string {
	var sb strings.Builder

	sb.WriteString("Auto-mem directory: ")
	sb.WriteString(me.memdir)
	sb.WriteString("\n\nExisting memory files:\n")

	// List existing files in each subdirectory
	for _, memType := range []memdir.MemoryType{
		memdir.MemoryTypeUser,
		memdir.MemoryTypeFeedback,
		memdir.MemoryTypeProject,
		memdir.MemoryTypeRef,
	} {
		subdir := filepath.Join(me.memdir, string(memType))
		entries, err := os.ReadDir(subdir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				sb.WriteString("- ")
				sb.WriteString(string(memType))
				sb.WriteString("/")
				sb.WriteString(entry.Name())
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// buildExtractionPrompt builds the prompt for the extraction agent.
func (me *MemoryExtractor) buildExtractionPrompt(turnCtx TurnContext, manifest string) string {
	var sb strings.Builder

	sb.WriteString("Extract durable memories from the recent conversation to the auto-mem directory.\n\n")
	sb.WriteString(manifest)
	sb.WriteString("\n\nInstructions:\n")
	sb.WriteString("1. Use Read to examine existing memory files that may need updating\n")
	sb.WriteString("2. Use Edit to update existing files or Write to create new ones\n")
	sb.WriteString("3. Write files only under: ")
	sb.WriteString(me.memdir)
	sb.WriteString("\n")
	sb.WriteString("4. Focus on extracting:\n")
	sb.WriteString("   - User preferences and characteristics → user/\n")
	sb.WriteString("   - Feedback about agent behavior → feedback/\n")
	sb.WriteString("   - Project state and decisions → project/\n")
	sb.WriteString("   - External references and resources → reference/\n")
	sb.WriteString("\nRecent conversation:\n")

	// Include recent messages (up to message limit)
	// AC3: If cursor UUID is missing (after compaction), fall back to counting
	messages := turnCtx.RecentMessages
	if me.lastMemoryMessageUuid == "" && me.lastMemoryMessageCount > 0 {
		if me.lastMemoryMessageCount < len(messages) {
			messages = messages[me.lastMemoryMessageCount:]
		} else {
			messages = nil
		}
	}

	if messages != nil {
		for _, msg := range messages {
			sb.WriteString("\n[")
			sb.WriteString(msg.Role)
			sb.WriteString("]: ")
			sb.WriteString(msg.Content)
		}
	}

	return sb.String()
}

// processExtractionResponse executes tool_use blocks from the extraction agent.
func (me *MemoryExtractor) processExtractionResponse(ctx context.Context, resp *api.Response, tools []tool.Tool) {
	toolMap := make(map[string]tool.Tool)
	for _, t := range tools {
		toolMap[t.Name()] = t
	}

	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.ToolUse != nil {
			t, ok := toolMap[block.ToolUse.Name]
			if !ok {
				continue
			}

			// AC4: Explicit path guard - reject Edit/Write outside auto-mem
			if t.Name() == "edit" || t.Name() == "write" {
				if filePath, ok := block.ToolUse.Args["file_path"].(string); ok {
					if !me.isUnderAutoMem(filePath) {
						log.Warn("Extraction tool rejected: path outside auto-mem", "tool", t.Name(), "path", filePath)
						continue
					}
				}
			}

			// Use auto-mem dir as cwd for path validation
			result, err := t.Execute(ctx, block.ToolUse.Args, me.memdir)
			if err != nil {
				log.Warn("Extraction tool failed", "tool", block.ToolUse.Name, "error", err)
				continue
			}
			_ = result // Tool already modified files in place
		}
	}
}

// finalizeExtraction completes the extraction and triggers a trailing run if needed.
func (me *MemoryExtractor) finalizeExtraction() {
	me.mu.Lock()
	pendingCtx := me.pendingCtx
	me.pendingCtx = nil

	// If there was a stashed context, trigger a trailing extraction
	if pendingCtx != nil {
		log.Debug("Memory extraction: running trailing extraction")
		me.inProgress = true // Keep inProgress true for trailing extraction

		// Unlock BEFORE spawning goroutine so it can acquire the lock independently
		me.mu.Unlock()
		go func() {
			defer me.finalizeExtraction()

			extractCtx, cancel := context.WithTimeout(context.Background(), me.timeout)
			defer cancel()

			// Create a minimal turn context for the trailing extraction
			trailingCtx := TurnContext{
				StopReason: api.StopReasonEndTurn,
			}
			if err := me.extract(extractCtx, trailingCtx); err != nil {
				log.Warn("Trailing memory extraction failed", "error", err)
			}
		}()
		return // Goroutine will call finalizeExtraction again when it completes
	}

	me.inProgress = false
	me.mu.Unlock()
}

// Drain waits for any in-progress extraction to complete during shutdown.
// Has a 60-second timeout.
func (me *MemoryExtractor) Drain(ctx context.Context) {
	me.mu.Lock()
	if !me.inProgress {
		me.mu.Unlock()
		return
	}
	// Create a channel to wait on
	done := make(chan struct{})
	go func() {
		for {
			me.mu.Lock()
			if !me.inProgress {
				me.mu.Unlock()
				close(done)
				return
			}
			me.mu.Unlock()
			time.Sleep(100 * time.Millisecond)
		}
	}()
	me.mu.Unlock()

	select {
	case <-done:
		return
	case <-ctx.Done():
		log.Warn("Memory extraction drain timed out")
		return
	}
}

// TurnContext holds context about the current turn for extraction decisions.
type TurnContext struct {
	// StopReason indicates why the turn ended.
	StopReason api.StopReason

	// AssistantMessage is the assistant's message for this turn.
	AssistantMessage *api.Message

	// TotalMessages is the total count of model-visible messages.
	TotalMessages int

	// RecentMessages are the recent messages to extract context from.
	RecentMessages []api.Message
}
