package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ipy/jenny/internal/agent"
	"github.com/ipy/jenny/internal/cli"
	"github.com/ipy/jenny/internal/mcp"
	"github.com/ipy/jenny/internal/session"
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
	}

	// Create tools
	tools := []tool.Tool{
		tool.NewBashTool(),
		tool.NewReadTool(),
	}

	// Load MCP configuration if paths are provided
	var mcpConfig map[string]mcp.MCPServerDef
	if len(flags.MCPConfig) > 0 {
		mcpConfig, err = mcp.LoadConfig(flags.MCPConfig, flags.StrictMCP)
		if err != nil {
			return fmt.Errorf("loading MCP config: %w", err)
		}
	}

	// Create context
	ctx := context.Background()

	// Build stream config
	streamCfg := agent.StreamConfig{
		Enabled:         flags.OutputFormat == "stream-json",
		Verbose:         flags.Verbose,
		IncludePartial:  flags.IncludePartialMessages,
		SessionID:       sessionID,
		SessionManager:  sessionManager,
		HistoryMessages: historyMessages,
		IsResume:        flags.SessionResume != "", // True when resuming an existing session via -r
		MCPConfig:       mcpConfig,
	}

	// Run agent
	result, _, err := agent.RunStream(ctx, flags.Prompt, tools, cwd, streamCfg, flags.Model)
	if err != nil {
		return err
	}

	// Print result
	fmt.Print(result)
	return nil
}
