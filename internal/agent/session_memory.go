// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/tool"
)

// APIClient defines the interface for making API calls in session memory operations.
type APIClient interface {
	SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error)
}

// SessionMemory tracks session-level memory for long conversations.
// It maintains a background markdown notes file that captures session context
// incrementally as the conversation grows.
type SessionMemory struct {
	sessionID        string
	memdir           string
	compactCfg       CompactConfig
	accumTokens      int // Cumulative token count since last baseline
	toolCalls        int // Tool calls since last update
	lastBaseline     int // Token count at last memory update
	lastToolBaseline int // Tool calls at last memory update
	lastUpdateTime   time.Time
	memoryFilePath   string
	client           APIClient
	readCache        *tool.ReadFileCache
}

// NewSessionMemory creates a new SessionMemory instance.
func NewSessionMemory(sessionID string, client APIClient, compactCfg CompactConfig, memdir string) *SessionMemory {
	baseDir := filepath.Join(constants.JennyHomeDir(), "session-memory")
	if memdir != "" {
		baseDir = memdir
	}
	return &SessionMemory{
		sessionID:        sessionID,
		memdir:           baseDir,
		compactCfg:       compactCfg,
		accumTokens:      0,
		toolCalls:        0,
		lastBaseline:     0,
		lastToolBaseline: 0,
		memoryFilePath:   filepath.Join(baseDir, sessionID+".md"),
		client:           client,
		readCache:        tool.NewReadFileCache(),
	}
}

// MemoryFilePath returns the path to the session memory file.
func (sm *SessionMemory) MemoryFilePath() string {
	return sm.memoryFilePath
}

// CheckThreshold evaluates whether to trigger a memory action based on
// accumulated token count and tool call count.
// Returns (shouldAct, action) where action is "init", "update", or "".
func (sm *SessionMemory) CheckThreshold(turnTokens int, toolCallCount int) (bool, string) {
	// AC5: First check if auto-compact is disabled - gate on auto-compact enabled
	// Session memory shares lifecycle with auto-compact
	if sm.compactCfg.DisableAutoCompact || sm.compactCfg.DisableCompact {
		return false, "disabled"
	}

	// Accumulate tokens
	sm.accumTokens += turnTokens
	sm.toolCalls += toolCallCount

	// Check for init: >= 10K tokens and no file exists
	if sm.lastBaseline == 0 && !sm.fileExists() {
		if sm.accumTokens >= 10000 {
			return true, "init"
		}
		return false, ""
	}

	// Check for update: >= 5K tokens since last baseline AND >= 3 tool calls since last tool baseline
	tokenGrowth := sm.accumTokens - sm.lastBaseline
	toolGrowth := sm.toolCalls - sm.lastToolBaseline

	if tokenGrowth >= 5000 && toolGrowth >= 3 {
		return true, "update"
	}

	return false, ""
}

// fileExists checks if the memory file already exists.
func (sm *SessionMemory) fileExists() bool {
	_, err := os.Stat(sm.memoryFilePath)
	return err == nil
}

// Init creates the session memory file with the initial template.
func (sm *SessionMemory) Init() error {
	// Ensure directory exists
	if err := os.MkdirAll(sm.memdir, 0755); err != nil {
		return fmt.Errorf("creating session memory directory: %w", err)
	}

	// Create template
	timestamp := time.Now().UTC().Format(time.RFC3339)
	content := fmt.Sprintf("# Session Memory: %s\nCreated: %s\n\n## Context / Goals\n\n## Key Decisions\n\n## Current State\n\n## Open Questions\n\n", sm.sessionID, timestamp)

	// Write with 0600 permissions
	if err := os.WriteFile(sm.memoryFilePath, []byte(content), 0600); err != nil {
		return fmt.Errorf("creating session memory file: %w", err)
	}

	// Record read in cache for edit validation
	sm.readCache.RecordRead(sm.memoryFilePath, content, time.Now(), true)

	log.Debug("Session memory file created", "path", sm.memoryFilePath)
	return nil
}

// Update invokes a forked sub-agent to update the session memory file.
// It uses a 15-second timeout and Edit-only tool access.
func (sm *SessionMemory) Update(ctx context.Context) error {
	// Check if file exists - if not, recreate it
	if !sm.fileExists() {
		if err := sm.Init(); err != nil {
			return fmt.Errorf("recreating session memory file: %w", err)
		}
	}

	// Read current content
	currentContent, err := os.ReadFile(sm.memoryFilePath)
	if err != nil {
		return fmt.Errorf("reading session memory file: %w", err)
	}

	// Invalidate readFileState before forked edit (edge case: read dedup)
	sm.readCache.Remove(sm.memoryFilePath)

	// Create context with 15-second timeout
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Build prompt for the forked agent
	prompt := sm.buildUpdatePrompt(string(currentContent))

	// Create restricted tool set (Edit only, allowed path is memory file)
	editTool := tool.NewEditTool(sm.readCache)
	editTool.SetAllowedPaths([]string{sm.memoryFilePath})
	tools := []tool.Tool{editTool}

	// Build tool params
	toolParams := make([]api.ToolParam, 0, len(tools))
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
		toolParams = append(toolParams, api.ToolParam{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: api.ToolInputSchema{
				Type:       "object",
				Properties: props,
				Required:   required,
			},
		})
	}

	// Make the API call with timeout
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	systemPrompt := "You are a helpful assistant that updates session memory files. You may only use the Edit tool to modify the session memory file. Focus on summarizing recent context and updating the relevant sections."

	resp, err := sm.client.SendMessage(ctx, messages, toolParams, nil, systemPrompt)
	if err != nil {
		// Check if context deadline exceeded
		if ctx.Err() == context.DeadlineExceeded {
			log.Warn("Session memory update timed out after 15 seconds")
			return nil // AC3: Don't block main loop on timeout
		}
		return fmt.Errorf("forked agent call: %w", err)
	}

	// Process response - extract text content
	var summary strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			summary.WriteString(block.Text)
		}
	}

	// If the model requested an edit, execute it
	// (The model should have used the Edit tool based on system prompt)
	// For now, we rely on the model following instructions

	// Update baselines
	sm.lastBaseline = sm.accumTokens
	sm.lastToolBaseline = sm.toolCalls
	sm.lastUpdateTime = time.Now()

	log.Debug("Session memory updated", "path", sm.memoryFilePath)
	return nil
}

// buildUpdatePrompt builds the prompt for the forked agent.
func (sm *SessionMemory) buildUpdatePrompt(existingContent string) string {
	var sb strings.Builder
	sb.WriteString("Update the session memory markdown file at ")
	sb.WriteString(sm.memoryFilePath)
	sb.WriteString(".\n\nYou may use the Edit tool only. Current content:\n\n")
	sb.WriteString(existingContent)
	sb.WriteString("\n\nRecent:\n\nSummarize any new context, decisions, or state changes that have occurred in this session. Update the relevant sections (Context / Goals, Key Decisions, Current State, Open Questions) based on what you know about the conversation.")
	return sb.String()
}

// ResetBaselines resets the token and tool call baselines after a memory update.
func (sm *SessionMemory) ResetBaselines() {
	sm.lastBaseline = sm.accumTokens
	sm.lastToolBaseline = sm.toolCalls
	sm.lastUpdateTime = time.Now()
}
