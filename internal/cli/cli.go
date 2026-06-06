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
	MCPConfig              []string
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

	var mcpPaths = []string{}

	flags.Var((*StringSlice)(&mcpPaths), "mcp-config", "MCP configuration file path(s) (can be specified multiple times)")

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

	return &Flags{
		Prompt:                 prompt,
		Model:                  model,
		OutputFormat:           outputFormat,
		Verbose:                verbose,
		IncludePartialMessages: includePartial,
		SkipPermissions:        skipPerms,
		SessionResume:          sessionResume,
		MCPConfig:              mcpPaths,
	}, nil
}

// StreamMessage represents a message in the stream-json output.
type StreamMessage struct {
	Type       string `json:"type"`
	Content    string `json:"content,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Result     string `json:"result,omitempty"`
	Model      string `json:"model,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolInput  any    `json:"tool_input,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	IsPartial  bool   `json:"is_partial,omitempty"`
	MessageIdx int    `json:"message_idx,omitempty"`
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
