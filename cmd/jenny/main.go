package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/agent"
	"github.com/ipy/jenny/internal/cli"
	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/mcp"
	"github.com/ipy/jenny/internal/plugin"
	"github.com/ipy/jenny/internal/redact"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
	"github.com/joho/godotenv"
)

// AC4: --version and the stream-json claude_code_version field must agree.
// `version` defaults to constants.Version. To override at build time use
// `-ldflags '-X main.version=<value>'`.
var version = constants.Version

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// loadEnvFiles reads .env and .jenny/.env from cwd (whichever exist) and
// applies them to the process environment. godotenv's default Load does NOT
// overwrite variables already set in the environment, so explicit `export`
// in the shell always wins. Missing files are not an error.
func loadEnvFiles(cwd string) {
	if cwd == "" {
		return
	}
	candidates := []string{
		filepath.Join(cwd, ".env"),
		filepath.Join(constants.ProjectJennyDir(cwd), ".env"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		// Best-effort: errors are ignored — a malformed .env should not
		// prevent jenny from starting.
		_ = godotenv.Load(p)
	}
}

func run() error {
	// AC9: load .env files (and .jenny/.env) before parsing flags so any
	// env-driven flag behaviour or pre-flight checks see the merged env.
	// Best-effort: missing or malformed .env is not an error.
	if cwd, err := os.Getwd(); err == nil {
		loadEnvFiles(cwd)
	}

	// Parse command-line flags
	flags, err := cli.Parse()
	if err != nil {
		return err
	}

	// --version / -v: print the version and exit before any session,
	// API client, or MCP setup so no network call is made.
	if flags.Version {
		fmt.Printf("%s (jenny)\n", version)
		return nil
	}

	// --print-system-prompt: print the assembled system prompt and exit.
	if flags.PrintSystemPrompt {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "/"
		}
		tools := buildPrintTools(flags)
		cfg := agent.StreamConfig{
			CustomSystemPrompt: flags.CustomSystemPrompt,
			AppendSystemPrompt: flags.AppendSystemPrompt,
			MemoryContent:      agent.LoadInstructionFile(cwd),
			MaxIterations:      flags.MaxIterations,
		}
		fmt.Print(agent.AssembleSystemPrompt(cfg, tools, cwd))
		return nil
	}

	// Enable verbose/debug mode if JENNY_DEBUG env var is set
	if os.Getenv("JENNY_DEBUG") != "" {
		flags.Verbose = true
	}

	// Set verbose mode in the logger (re-runs resetLogger to enable debug level)
	log.SetVerbose(flags.Verbose)

	// Get working directory
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}

	// Create session manager for transcript persistence
	transcriptDir := os.Getenv("JENNY_TRANSCRIPT_DIR")
	sessionManager, err := session.NewManager(transcriptDir, flags.NoSessionPersistence)
	if err != nil {
		return fmt.Errorf("creating session manager: %w", err)
	}

	// Register shutdown flush hook when persistence is enabled
	if !flags.NoSessionPersistence {
		sessionManager.RegisterShutdownFlush()
	}

	// Handle --continue flag: find most recent session
	if flags.Continue {
		sessions, err := sessionManager.ListSessions()
		if err != nil {
			return fmt.Errorf("listing sessions: %w", err)
		}
		if len(sessions) == 0 {
			return fmt.Errorf("no sessions to continue")
		}
		flags.SessionResume = sessions[0]
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

		// Load transcript for validation (LoadPostBoundaryMessages for history)
		// Use LoadPostBoundaryMessages to skip pre-compaction messages when resuming
		entries, err := sessionManager.LoadPostBoundaryMessages(sessionID)
		if err != nil {
			return fmt.Errorf("loading transcript: %w", err)
		}

		// Reject queue-only/empty transcripts: if no chain participant messages
		// exist after filtering progress types, the session has no conversation to resume.
		if !agent.HasChainMessages(entries) {
			return fmt.Errorf("no conversation found in session %s", sessionID)
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

	// Load MCP configuration
	var mcpConfig map[string]mcp.MCPServerDef
	var mcpTools []tool.Tool

	// Always add ListMcpResourcesTool - it handles the case of no MCP servers connected
	mcpTools = append(mcpTools, mcp.NewListMcpResourcesTool())

	// Phase 1: Load plugin MCP servers (lowest priority) if not bare and not strict
	if !flags.Bare && !flags.StrictMCP {
		homeDir := ""
		if hd, err := os.UserHomeDir(); err == nil {
			homeDir = hd
		}
		mcpConfig = loadPluginMCPServers(cwd, homeDir)
	}

	// Phase 2: Merge CLI MCP config (overrides plugin entries)
	if len(flags.MCPConfig) > 0 {
		cliConfig, err := mcp.LoadConfig(flags.MCPConfig, flags.StrictMCP)
		if err != nil {
			return fmt.Errorf("loading MCP config: %w", err)
		}
		// Merge CLI config into plugin config (CLI wins on collision)
		if mcpConfig == nil {
			mcpConfig = cliConfig
		} else {
			maps.Copy(mcpConfig, cliConfig)
		}
	}

	// Phase 3: Connect and discover tools if we have any MCP servers
	if len(mcpConfig) > 0 {
		if err := mcp.ConnectAll(mcpConfig); err != nil {
			return fmt.Errorf("connecting to MCP servers: %w", err)
		}

		// Get discovered MCP tools
		for _, t := range mcp.GetTools() {
			if mcpTool, ok := t.(*mcp.MCPTool); ok {
				mcpTools = append(mcpTools, mcpTool)
			}
		}

		// Ensure MCP clients are shut down on exit
		defer mcp.ShutdownAll()
	}

	// Build tool registry with skipPermissions flag
	// AC4: ReadFileCache is wired through StreamConfig -> QueryEngine -> tools
	// But Registry.Build() also needs it to create Write/Edit/NotebookEdit tools
	readFileCache := tool.NewReadFileCache()

	// Discover skills from project .jenny/skills/ directory and bundled default directory
	var discoveredSkills []skills.Skill

	// AC4: Bare mode skips all skill discovery
	if !flags.Bare {
		projectSkillsDir := filepath.Join(constants.ProjectJennyDir(cwd), "skills")

		// Bundled default skills directory (user-level)
		bundledSkillsDir := ""
		if _, err := os.UserHomeDir(); err == nil {
			bundledSkillsDir = filepath.Join(constants.JennyHomeDir(), "skills")
		}
		// Discover from both directories (AC6: discovery from multiple directories)
		discoveredSkills, err = skills.Discover(projectSkillsDir, bundledSkillsDir)
		if err != nil {
			return fmt.Errorf("discovering skills: %w", err)
		}

		// Discover plugin skills and merge with discovered skills
		// Plugin skills have lower priority than project/user skills (dedup by name)
		pluginRoots := plugin.FindPluginRoots(cwd)
		homePluginRoots := plugin.FindPluginRoots(constants.JennyHomeDir())
		pluginRoots = append(pluginRoots, homePluginRoots...)

		discoveredSkills = discoverAndMergePluginSkills(discoveredSkills, pluginRoots)
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
		WithStrictMCP(flags.StrictMCP).
		WithSkillsFrameworkEnabled(!flags.Bare, discoveredSkills)
	tools = registry.Build()

	// Get skill activator to wire it to the engine for active skill tracking
	skillActivator := registry.GetSkillActivator()

	// Create subagent runners and AgentTool
	denyRulesMap := make(map[string]bool)
	for _, name := range flags.DeniedTools {
		denyRulesMap[name] = true
	}
	localRunner := agent.NewLocalSubagentRunner(tools, denyRulesMap, nil)
	asyncRunner := agent.NewAsyncSubagentRunner(tools, denyRulesMap, nil)
	agentTool := tool.NewAgentToolWithSwarms(localRunner, asyncRunner, flags.SwarmsEnabled)
	tools = append(tools, agentTool)

	// Create context
	ctx := context.Background()

	// Determine redact mode: CLI flag wins over JENNY_REDACT env var.
	// Default is "recover" if neither is set.
	redactModeStr := os.Getenv("JENNY_REDACT")
	if val, ok := flags.FeatureFlags["redact"]; ok {
		redactModeStr = val
	}
	if redactModeStr == "" {
		redactModeStr = "recover"
	}
	redactMode := redact.ParseRedactMode(redactModeStr)

	// Build stream config
	// AC4: Create ReadFileCache and pass it through StreamConfig for engine-level wiring
	streamCfg := agent.StreamConfig{
		Enabled:            flags.OutputFormat == "stream-json",
		Verbose:            flags.Verbose,
		IncludePartial:     flags.IncludePartialMessages,
		SessionID:          sessionID,
		SessionManager:     sessionManager,
		HistoryMessages:    historyMessages,
		IsResume:           flags.SessionResume != "", // True when resuming an existing session via -r
		MCPConfig:          mcpConfig,
		ReadFileCache:      readFileCache,
		Skills:             discoveredSkills,
		CustomSystemPrompt: flags.CustomSystemPrompt,
		AppendSystemPrompt: flags.AppendSystemPrompt,
		MemoryContent:      agent.LoadInstructionFile(cwd),
		MaxIterations:      flags.MaxIterations,
		MaxTurns:           flags.MaxTurns,
		MaxBudgetUSD:       flags.MaxBudgetUsd,
		RedactMode:         redactMode,
		Effort:             flags.Effort,
	}

	// AC3-streamconfig-inheritance: Set parent config on runner for named agent inheritance
	localRunner.SetParentConfig(streamCfg)
	asyncRunner.SetParentConfig(streamCfg)

	// Run agent
	result, _, err := agent.RunStream(ctx, flags.Prompt, tools, cwd, streamCfg, flags.Model, agent.WithSkillActivator(skillActivator))
	if err != nil {
		return err
	}

	// Print result only when NOT in stream-json mode (result is already emitted as JSON in stream-json mode)
	if flags.OutputFormat != "stream-json" {
		fmt.Println(result)
	}
	return nil
}

// buildPrintTools returns the default set of tools for system prompt printing.
// It skips MCP tool discovery and full skill discovery to remain offline.
func buildPrintTools(flags *cli.Flags) []tool.Tool {
	registry := tool.NewRegistry().
		WithBaseTools().
		WithWebFetchEnabled(true).
		WithWebSearchEnabled(true).
		WithModel(flags.Model).
		WithDenyRules(flags.DeniedTools).
		WithSkipPermissions(flags.SkipPermissions).
		WithStrictMCP(flags.StrictMCP).
		WithSkillsFrameworkEnabled(!flags.Bare, nil)
	tools := registry.Build()

	// Create subagent runners and AgentTool
	denyRulesMap := make(map[string]bool)
	for _, name := range flags.DeniedTools {
		denyRulesMap[name] = true
	}
	localRunner := agent.NewLocalSubagentRunner(tools, denyRulesMap, nil)
	asyncRunner := agent.NewAsyncSubagentRunner(tools, denyRulesMap, nil)
	agentTool := tool.NewAgentToolWithSwarms(localRunner, asyncRunner, flags.SwarmsEnabled)
	tools = append(tools, agentTool)

	return tools
}
