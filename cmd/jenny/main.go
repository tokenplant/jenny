package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/agent"
	"github.com/ipy/jenny/internal/cli"
	"github.com/ipy/jenny/internal/mcp"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse command-line flags
	flags, err := cli.Parse()
	if err != nil {
		return err
	}

	// Enable verbose mode if JENNY_DEBUG env var is set
	if os.Getenv("JENNY_DEBUG") != "" {
		flags.Verbose = true
	}

	// Get working directory
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}

	// Create session manager for transcript persistence
	sessionManager, err := session.NewManager("", flags.NoSessionPersistence)
	if err != nil {
		return fmt.Errorf("creating session manager: %w", err)
	}

	// Register shutdown flush hook when persistence is enabled
	if !flags.NoSessionPersistence {
		sessionManager.RegisterShutdownFlush()
	}

	// Determine session ID: use -r flag value if provided, otherwise generate new
	sessionID := flags.SessionResume
	var historyMessages []agent.Message

	if sessionID == "" {
		sessionID, err = session.NewSessionID()
		if err != nil {
			return fmt.Errorf("generating session ID: %w", err)
		}
	} else {
		// When persistence is disabled, resume is not supported
		if flags.NoSessionPersistence {
			return fmt.Errorf("session persistence is disabled, cannot resume session %s", sessionID)
		}

		// Validate session ID doesn't contain path traversal before using it
		if err := session.ValidateSessionID(sessionID); err != nil {
			return err
		}

		// Check if the session exists when resuming
		if !sessionManager.SessionExists(sessionID) {
			return fmt.Errorf("session not found: %s", sessionID)
		}

		// Load transcript and rebuild message history
		entries, err := sessionManager.LoadTranscript(sessionID)
		if err != nil {
			return fmt.Errorf("loading transcript: %w", err)
		}

		historyMessages = agent.RebuildMessages(entries)

		// Fork session if --fork-session flag is set
		if flags.ForkSession {
			newSessionID, err := session.NewSessionID()
			if err != nil {
				return fmt.Errorf("generating new session ID for fork: %w", err)
			}

			// Write all loaded entries to the new session
			for _, entry := range entries {
				if err := sessionManager.AppendEntry(newSessionID, entry); err != nil {
					return fmt.Errorf("forking transcript entry: %w", err)
				}
			}

			// Use the new session ID for the remainder of the run
			sessionID = newSessionID
		}
	}

	// Load MCP configuration if paths are provided
	var mcpConfig map[string]mcp.MCPServerDef
	var mcpTools []tool.Tool

	// Always add ListMcpResourcesTool - it handles the case of no MCP servers connected
	mcpTools = append(mcpTools, mcp.NewListMcpResourcesTool())

	if len(flags.MCPConfig) > 0 {
		mcpConfig, err = mcp.LoadConfig(flags.MCPConfig, flags.StrictMCP)
		if err != nil {
			return fmt.Errorf("loading MCP config: %w", err)
		}

		// Connect to MCP servers and discover their tools
		if err := mcp.ConnectAll(mcpConfig); err != nil {
			return fmt.Errorf("connecting to MCP servers: %w", err)
		}

		// Get discovered MCP tools
		for _, t := range mcp.GetTools() {
			if mcpTool, ok := t.(*mcp.MCPTool); ok {
				mcpTools = append(mcpTools, mcpTool)
			}
		}
	}

	// Build tool registry with skipPermissions flag
	// AC4: ReadFileCache is wired through StreamConfig -> QueryEngine -> tools
	// But Registry.Build() also needs it to create Write/Edit/NotebookEdit tools
	readFileCache := tool.NewReadFileCache()

	// Discover skills from project .jenny/skills/ directory and bundled default directory
	var discoveredSkills []skills.Skill

	// AC4: Bare mode skips all skill discovery
	if !flags.Bare {
		projectSkillsDir := filepath.Join(cwd, ".jenny", "skills")

		// Bundled default skills directory (user-level)
		bundledSkillsDir := ""
		if homeDir, err := os.UserHomeDir(); err == nil {
			bundledSkillsDir = filepath.Join(homeDir, ".jenny", "skills")
		}

		// Discover from both directories (AC6: discovery from multiple directories)
		discoveredSkills, err = skills.Discover(projectSkillsDir, bundledSkillsDir)
		if err != nil {
			return fmt.Errorf("discovering skills: %w", err)
		}
	}

	var tools []tool.Tool
	registry := tool.NewRegistry().
		WithBaseTools().
		WithWebFetchEnabled(true).
		WithWebSearchEnabled(true).
		WithModel(flags.Model).
		WithReadFileCache(readFileCache).
		WithMCPTools(mcpTools).
		WithDenyRules(flags.DeniedTools).
		WithSkipPermissions(flags.SkipPermissions).
		WithSkillsFrameworkEnabled(!flags.Bare, discoveredSkills)
	tools = registry.Build()

	// Create subagent runners and AgentTool
	denyRulesMap := make(map[string]bool)
	for _, name := range flags.DeniedTools {
		denyRulesMap[name] = true
	}
	localRunner := agent.NewLocalSubagentRunner(tools, denyRulesMap)
	asyncRunner := agent.NewAsyncSubagentRunner(tools, denyRulesMap)
	agentTool := tool.NewAgentToolWithSwarms(localRunner, asyncRunner, flags.SwarmsEnabled)
	tools = append(tools, agentTool)

	// Ensure MCP clients are shut down on exit
	if len(flags.MCPConfig) > 0 {
		defer mcp.ShutdownAll()
	}

	// Create context
	ctx := context.Background()

	// Build stream config
	// AC4: Create ReadFileCache and pass it through StreamConfig for engine-level wiring
	streamCfg := agent.StreamConfig{
		Enabled:         flags.OutputFormat == "stream-json",
		Verbose:         flags.Verbose,
		IncludePartial:  flags.IncludePartialMessages,
		SessionID:       sessionID,
		SessionManager:  sessionManager,
		HistoryMessages: historyMessages,
		IsResume:        flags.SessionResume != "", // True when resuming an existing session via -r
		MCPConfig:       mcpConfig,
		ReadFileCache:   readFileCache,
		Skills:          discoveredSkills,
	}

	// AC3-streamconfig-inheritance: Set parent config on runner for named agent inheritance
	localRunner.SetParentConfig(streamCfg)
	asyncRunner.SetParentConfig(streamCfg)

	// Run agent
	result, _, err := agent.RunStream(ctx, flags.Prompt, tools, cwd, streamCfg, flags.Model)
	if err != nil {
		return err
	}

	// Print result
	fmt.Print(result)
	return nil
}
