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
	SwarmsEnabled          bool              // When true, enables named agent delegation (swarm mode)
	Version                bool              // --version / -v: print version and exit
	PrintSystemPrompt      bool              // --print-system-prompt: print the assembled system prompt and exit
	CustomSystemPrompt     string            // --system-prompt: replaces default system prompt entirely
	AppendSystemPrompt     string            // --append-system-prompt: appended after assembled system prompt
	MaxIterations          int               // --max-iterations: maximum loop iterations (0 = unlimited)
	MaxTurns               int               // --max-turns: maximum number of turns (0 = unlimited)
	MaxBudgetUsd           float64           // --max-budget-usd: budget limit in USD (0.0 = no limit)
	Effort                 string            // --effort: reasoning effort level (low, medium, high)
	FeatureFlags           map[string]string // --feature-flags / -ff: feature flags in key=value format
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

// FeatureFlagValue implements flag.Value for key=value feature flags.
type FeatureFlagValue map[string]string

func (f *FeatureFlagValue) Set(val string) error {
	if *f == nil {
		*f = make(map[string]string)
	}
	parts := strings.SplitN(val, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid feature flag format %q; expected key=value", val)
	}
	(*f)[parts[0]] = parts[1]
	return nil
}

func (f *FeatureFlagValue) String() string {
	if *f == nil {
		return ""
	}
	var pairs []string
	for k, v := range *f {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(pairs, ",")
}

// Parse parses command-line flags.
// Returns an error if parsing fails or if no prompt is provided.
func Parse() (*Flags, error) {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: %s [-p <prompt>] [--model <model>] [--output-format <format>] [-r <session_id>]\n", os.Args[0])
		flags.PrintDefaults()
	}

	var promptParts StringSlice
	flags.Var(&promptParts, "p", "Prompt to send (can be specified multiple times; values are joined with newlines)")

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

	var version bool
	flags.BoolVar(&version, "version", false, "Print version and exit")
	flags.BoolVar(&version, "v", false, "Print version and exit (alias)")

	var psp bool
	flags.BoolVar(&psp, "print-system-prompt", false, "Print the assembled system prompt and exit")

	var customSys string
	flags.StringVar(&customSys, "system-prompt", "", "Replace the default system prompt")

	var appendSys string
	flags.StringVar(&appendSys, "append-system-prompt", "", "Append text after the system prompt")

	var maxIter int
	flags.IntVar(&maxIter, "max-iterations", 0, "Maximum loop iterations (0 = unlimited)")

	var maxTurns int
	flags.IntVar(&maxTurns, "max-turns", 0, "Maximum number of turns (0 = unlimited)")

	var maxBudget float64
	flags.Float64Var(&maxBudget, "max-budget-usd", 0, "Budget limit in USD (0.0 = no limit)")

	var effort string
	flags.StringVar(&effort, "effort", "", "Reasoning effort level (low, medium, high) for OpenAI o-series and DeepSeek models")

	var featureFlags FeatureFlagValue
	flags.Var(&featureFlags, "feature-flags", "Feature flags in key=value format (can be specified multiple times)")
	flags.Var(&featureFlags, "ff", "Feature flags in key=value format (alias for --feature-flags)")

	// Parse the flags
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			// AC6: Go's flag package already invoked flags.Usage() before
			// returning flag.ErrHelp (see stdlib flag/flag.go:1112). Calling
			// it again would print the usage block twice. Just exit.
			os.Exit(0)
		}
		return nil, err
	}

	// Get remaining non-flag arguments as positional prompt
	args := flags.Args()

	// AC8: -p may be specified multiple times; values are joined with a newline.
	prompt := strings.Join(promptParts, "\n")

	if len(args) > 0 {
		// If positional args exist and -p was not used, use them
		if prompt == "" {
			prompt = strings.Join(args, " ")
		}
	}

	// --version / --print-system-prompt: caller will print and exit before any
	// session or API initialisation, so a prompt is not required.
	if version || psp {
		return &Flags{
			Model:                  model,
			OutputFormat:           outputFormat,
			Verbose:                verbose,
			IncludePartialMessages: includePartial,
			SkipPermissions:        skipPerms,
			NoSessionPersistence:   noSessionPersistence,
			DeniedTools:            deniedTools,
			Bare:                   bare,
			SwarmsEnabled:          swarmsEnabled,
			Version:                version,
			PrintSystemPrompt:      psp,
			CustomSystemPrompt:     customSys,
			AppendSystemPrompt:     appendSys,
			MCPConfig:              mcpPaths,
			MaxIterations:          maxIter,
			MaxTurns:               maxTurns,
			MaxBudgetUsd:           maxBudget,
			Effort:                 effort,
			FeatureFlags:           featureFlags,
		}, nil
	}

	// Validate: require a prompt
	if prompt == "" {
		flags.Usage()
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
		Version:                version,
		PrintSystemPrompt:      psp,
		CustomSystemPrompt:     customSys,
		AppendSystemPrompt:     appendSys,
		MaxIterations:          maxIter,
		MaxTurns:               maxTurns,
		MaxBudgetUsd:           maxBudget,
		Effort:                 effort,
		FeatureFlags:           featureFlags,
	}, nil
}

// StreamMessage represents a message in the stream-json output.
// Field order matches the headless-agent reference format: type, then payload,
// then session_id, parent_tool_use_id, uuid, then remaining fields.
type StreamMessage struct {
	Type              string   `json:"type"`
	Kind              string   `json:"kind,omitempty"`
	Subtype           string   `json:"subtype,omitempty"`
	Content           string   `json:"content,omitempty"`
	SessionID         string   `json:"session_id,omitempty"`
	ParentToolUseID   *string  `json:"parent_tool_use_id"`
	Uuid              string   `json:"uuid,omitempty"`
	Result            string   `json:"result,omitempty"`
	Model             string   `json:"model,omitempty"`
	CWD               string   `json:"cwd,omitempty"`
	Tools             []string `json:"tools,omitempty"`
	ToolName          string   `json:"tool_name,omitempty"`
	ToolInput         any      `json:"input,omitempty"`
	IsError           bool     `json:"is_error,omitempty"`
	IsPartial         bool     `json:"is_partial,omitempty"`
	ClaudeCodeVersion string   `json:"claude_code_version,omitempty"`
	PermissionMode    string   `json:"permissionMode,omitempty"`
	FastModeState     string   `json:"fast_mode_state,omitempty"`
	OutputStyle       string   `json:"output_style,omitempty"`
	MCPServers        []string `json:"mcp_servers,omitempty"`
}

// MarshalJSON implements custom marshaling for StreamMessage to:
// - Maintain correct field ordering per reference format
// - Include 'kind' field for compatibility with Claude Code parsers
func (s StreamMessage) MarshalJSON() ([]byte, error) {
	kind := s.Kind
	if kind == "" {
		switch s.Type {
		case "assistant", "user", "result", "system":
			kind = "message"
		case "tool_call", "tool_use":
			kind = "tool_call"
		default:
			kind = s.Type
		}
	}

	var fields []string
	fields = append(fields, `"type":`+encodeString(s.Type))
	fields = append(fields, `"kind":`+encodeString(kind))
	if s.Subtype != "" {
		fields = append(fields, `"subtype":`+encodeString(s.Subtype))
	}
	if s.Content != "" {
		fields = append(fields, `"content":`+encodeString(s.Content))
	}
	if s.SessionID != "" {
		fields = append(fields, `"session_id":`+encodeString(s.SessionID))
	}
	if s.ParentToolUseID != nil {
		fields = append(fields, `"parent_tool_use_id":`+encodeString(*s.ParentToolUseID))
	}
	if s.Uuid != "" {
		fields = append(fields, `"uuid":`+encodeString(s.Uuid))
	}
	if s.Result != "" {
		fields = append(fields, `"result":`+encodeString(s.Result))
	}
	if s.Model != "" {
		fields = append(fields, `"model":`+encodeString(s.Model))
	}
	if s.CWD != "" {
		fields = append(fields, `"cwd":`+encodeString(s.CWD))
	}
	if len(s.Tools) > 0 {
		toolsBytes, _ := json.Marshal(s.Tools)
		fields = append(fields, `"tools":`+string(toolsBytes))
	}
	if s.ToolName != "" {
		fields = append(fields, `"tool_name":`+encodeString(s.ToolName))
	}
	if s.ToolInput != nil {
		inputBytes, _ := json.Marshal(s.ToolInput)
		fields = append(fields, `"input":`+string(inputBytes))
	}
	if s.IsError {
		fields = append(fields, `"is_error":true`)
	}
	if s.IsPartial {
		fields = append(fields, `"is_partial":true`)
	}
	if s.ClaudeCodeVersion != "" {
		fields = append(fields, `"claude_code_version":`+encodeString(s.ClaudeCodeVersion))
	}
	if s.PermissionMode != "" {
		fields = append(fields, `"permissionMode":`+encodeString(s.PermissionMode))
	}
	if s.FastModeState != "" {
		fields = append(fields, `"fast_mode_state":`+encodeString(s.FastModeState))
	}
	if s.OutputStyle != "" {
		fields = append(fields, `"output_style":`+encodeString(s.OutputStyle))
	}
	// Always emit mcp_servers as array (even if empty) for init events compatibility
	if s.MCPServers != nil {
		mcpBytes, _ := json.Marshal(s.MCPServers)
		fields = append(fields, `"mcp_servers":`+string(mcpBytes))
	} else {
		fields = append(fields, `"mcp_servers":[]`)
	}

	return []byte("{" + strings.Join(fields, ",") + "}"), nil
}

func encodeString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
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
