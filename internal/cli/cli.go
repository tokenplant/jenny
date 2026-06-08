// Package cli provides command-line interface support for jenny.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// Flags holds the parsed command-line flags.
type Flags struct {
	Prompt                 string
	Model                  string
	OutputFormat           string
	Verbose                bool
	IncludePartialMessages bool
	SkipPermissions        bool
	SessionResume          string
	NoSessionPersistence   bool
	ForkSession            bool
	Continue               bool
	MCPConfig              []string
	StrictMCP              bool
	DeniedTools            []string
	Bare                   bool
	SwarmsEnabled          bool // When true, enables named agent delegation (swarm mode)
}

// StringSlice implements flag.Value for multiple string values.
type StringSlice []string

func (s *StringSlice) Set(val string) error {
	*s = append(*s, val)
	return nil
}

func (s *StringSlice) String() string {
	return strings.Join(*s, ",")
}

// Usage outputs usage information to stderr.
func Usage() {
	fmt.Fprintln(flag.CommandLine.Output(), "Usage: jenny [-p <prompt>] [--model <model>] [--output-format <format>] [-r <session_id>]")
}

// Parse parses command-line flags.
// Returns an error if parsing fails or if no prompt is provided.
func Parse() (*Flags, error) {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {} // We handle usage ourselves

	var prompt string
	flags.StringVar(&prompt, "p", "", "Prompt to send")

	var model string
	flags.StringVar(&model, "model", "", "Model to use")

	var outputFormat string
	flags.StringVar(&outputFormat, "output-format", "text", "Output format (text, stream-json)")

	var verbose bool
	flags.BoolVar(&verbose, "verbose", false, "Enable verbose output")

	var includePartial bool
	flags.BoolVar(&includePartial, "include-partial-messages", false, "Include partial messages")

	var skipPerms bool
	flags.BoolVar(&skipPerms, "dangerously-skip-permissions", false, "Skip permissions")

	var sessionResume string
	flags.StringVar(&sessionResume, "r", "", "Session ID to resume")

	var noSessionPersistence bool
	flags.BoolVar(&noSessionPersistence, "no-session-persistence", false, "Disable session persistence")

	var forkSession bool
	flags.BoolVar(&forkSession, "fork-session", false, "Fork resumed session to new ID")

	var continueFlag bool
	flags.BoolVar(&continueFlag, "continue", false, "Resume most recent session")

	var mcpPaths = []string{}

	flags.Var((*StringSlice)(&mcpPaths), "mcp-config", "MCP configuration file path(s) (can be specified multiple times)")

	var strictMCP bool
	flags.BoolVar(&strictMCP, "strict-mcp-config", false, "Only load MCP servers from --mcp-config files")

	var deniedTools = []string{}
	flags.Var((*StringSlice)(&deniedTools), "deny-tool", "Tool name to deny (can be specified multiple times)")

	var bare bool
	flags.BoolVar(&bare, "bare", false, "Disable skill discovery for minimal environments")

	var swarmsEnabled bool
	flags.BoolVar(&swarmsEnabled, "swarm", false, "Enable swarm mode for named agent delegation")

	// Parse the flags
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			Usage()
			os.Exit(0)
		}
		return nil, err
	}

	// Get remaining non-flag arguments as positional prompt
	args := flags.Args()

	if len(args) > 0 {
		// If positional args exist and -p was not used, use the first positional arg
		if prompt == "" {
			prompt = strings.Join(args, " ")
		}
	}

	// Validate: require a prompt
	if prompt == "" {
		Usage()
		return nil, fmt.Errorf("no prompt provided")
	}

	// Validate: --fork-session requires -r/--resume
	if forkSession && sessionResume == "" {
		return nil, fmt.Errorf("--fork-session requires -r/--resume")
	}

	// Validate: --continue is mutually exclusive with -r/--resume
	if continueFlag && sessionResume != "" {
		return nil, fmt.Errorf("--continue is mutually exclusive with -r/--resume")
	}

	// Validate: --continue requires session persistence
	if continueFlag && noSessionPersistence {
		return nil, fmt.Errorf("--continue requires session persistence")
	}

	return &Flags{
		Prompt:                 prompt,
		Model:                  model,
		OutputFormat:           outputFormat,
		Verbose:                verbose,
		IncludePartialMessages: includePartial,
		SkipPermissions:        skipPerms,
		SessionResume:          sessionResume,
		NoSessionPersistence:   noSessionPersistence,
		ForkSession:            forkSession,
		Continue:               continueFlag,
		MCPConfig:              mcpPaths,
		StrictMCP:              strictMCP,
		DeniedTools:            deniedTools,
		Bare:                   bare,
		SwarmsEnabled:          swarmsEnabled,
	}, nil
}

// StreamMessage represents a message in the stream-json output.
type StreamMessage struct {
	Type       string   `json:"type"`
	Subtype    string   `json:"subtype,omitempty"`
	Content    string   `json:"content,omitempty"`
	SessionID  string   `json:"session_id,omitempty"`
	Result     string   `json:"result,omitempty"`
	Model      string   `json:"model,omitempty"`
	CWD        string   `json:"cwd,omitempty"`
	Tools      []string `json:"tools,omitempty"`
	ToolName   string   `json:"tool_name,omitempty"`
	ToolInput  any      `json:"parameters,omitempty"`
	IsError    bool     `json:"is_error,omitempty"`
	IsPartial  bool     `json:"is_partial,omitempty"`
	MessageIdx int      `json:"message_idx,omitempty"`
}

// WriteStreamJSON writes a message as NDJSON line to stdout.
func WriteStreamJSON(msg StreamMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}

// WriteStreamJSONRaw writes raw JSON as NDJSON line to stdout.
func WriteStreamJSONRaw(data []byte) error {
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}
