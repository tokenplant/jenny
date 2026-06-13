// Package tool provides tool implementations.
package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/grepinproc"
	"github.com/ipy/jenny/internal/sandbox"
)

const (
	// maxResultSizeChars is the maximum output size before truncation.
	maxResultSizeChars = 20000
	// defaultHeadLimit is the default maximum number of matches.
	defaultHeadLimit = 250
	// defaultTimeout is the default timeout in seconds.
	defaultTimeout = 30
)

// GrepTool searches file contents via ripgrep.
type GrepTool struct {
	sandbox sandbox.SandboxManager
}

// NewGrepTool creates a new GrepTool.
func NewGrepTool() *GrepTool {
	return &GrepTool{}
}

// WithSandbox sets the sandbox manager for the GrepTool.
func (t *GrepTool) WithSandbox(sb sandbox.SandboxManager) *GrepTool {
	t.sandbox = sb
	return t
}

// Name returns the tool name.
func (t *GrepTool) Name() string {
	return "Grep"
}

// Description returns a description of the tool.
func (t *GrepTool) Description() string {
	return "Search file contents using regex patterns. Returns matches with context, sorted by relevance."
}

// InputSchema returns the JSON schema for tool input.
func (t *GrepTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regex pattern to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search (default: cwd)",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "File filter glob pattern",
			},
			"output_mode": map[string]any{
				"type":        "string",
				"description": "Output mode: content, files_with_matches, count",
				"enum":        []any{"content", "files_with_matches", "count"},
			},
			"head_limit": map[string]any{
				"type":        "number",
				"description": "Maximum matches (default: 250, 0 = unlimited)",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "Skip first N matches for pagination",
			},
			"i": map[string]any{
				"type":        "boolean",
				"description": "Case insensitive search",
			},
			"n": map[string]any{
				"type":        "boolean",
				"description": "Show line numbers",
			},
			"A": map[string]any{
				"type":        "number",
				"description": "Show N lines after match",
			},
			"B": map[string]any{
				"type":        "number",
				"description": "Show N lines before match",
			},
			"C": map[string]any{
				"type":        "number",
				"description": "Show N lines of context around match",
			},
			"multiline": map[string]any{
				"type":        "boolean",
				"description": "Multiline mode",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Filter by file type (e.g., go, py, js)",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
		},
		"required": []any{"pattern"},
	}
}

// Execute searches for the pattern using ripgrep.
func (t *GrepTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	pattern, ok := input["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required and must be a string")
	}

	// Determine ripgrep command - use sandboxed ripgrep if available.
	// If ripgrep is not available on the host, fall back to the
	// in-process search backend in internal/grepinproc. The fallback
	// produces the same text format as ripgrep, so the rest of the
	// pipeline (head_limit, offset, truncation) works unchanged.
	rgCommand := "rg"
	rgAvailable := true
	if t.sandbox != nil && t.sandbox.IsActive() {
		// Use sandboxed ripgrep config if provided
		ripgrepCfg := t.sandbox.RipgrepConfig()
		if ripgrepCfg.Command != "" {
			rgCommand = ripgrepCfg.Command
		} else {
			if _, err := exec.LookPath("rg"); err != nil {
				rgAvailable = false
			}
		}
	} else {
		if _, err := exec.LookPath("rg"); err != nil {
			rgAvailable = false
		}
	}

	if !rgAvailable {
		return t.executeInProcess(ctx, input, pattern, cwd)
	}

	// Build ripgrep arguments
	args := []string{}

	// Always enable hidden files, set max columns, and explicitly exclude VCS dirs
	// (ripgrep should auto-exclude via .gitignore, but we add explicit exclusion for reliability)
	args = append(args, "--hidden", "--max-columns", "500", "--glob", "!.git", "--glob", "!.svn")

	// Pattern handling: use -e if pattern starts with -
	if strings.HasPrefix(pattern, "-") {
		args = append(args, "-e", pattern)
	} else {
		args = append(args, pattern)
	}

	// Path - use relative path "." when searching cwd to get relative output
	path := "."
	if pathVal, ok := input["path"].(string); ok && pathVal != "" {
		path = pathVal
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
	}
	args = append(args, path)

	// Glob
	if globVal, ok := input["glob"].(string); ok && globVal != "" {
		args = append(args, "-g", globVal)
	}

	// Output mode
	outputMode := "files_with_matches"
	if modeVal, ok := input["output_mode"].(string); ok && modeVal != "" {
		outputMode = modeVal
	}

	switch outputMode {
	case "content":
		// No extra flag needed
	case "files_with_matches":
		args = append(args, "-l")
	case "count":
		args = append(args, "-c")
	}

	// Flags
	if boolVal(input, "i") {
		args = append(args, "-i")
	}
	if boolVal(input, "n") {
		args = append(args, "-n")
	}
	if intVal, ok := input["A"].(float64); ok {
		args = append(args, "-A", fmt.Sprintf("%d", int(intVal)))
	}
	if intVal, ok := input["B"].(float64); ok {
		args = append(args, "-B", fmt.Sprintf("%d", int(intVal)))
	}
	if intVal, ok := input["C"].(float64); ok {
		args = append(args, "-C", fmt.Sprintf("%d", int(intVal)))
	}
	if boolVal(input, "multiline") {
		args = append(args, "-U", "--multiline-dotall")
	}
	if typeVal, ok := input["type"].(string); ok && typeVal != "" {
		args = append(args, "--type", typeVal)
	}

	// Timeout
	timeout := defaultTimeout
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	// First derive a context that inherits cancellation from the passed context.
	derivedCtx, derivedCancel := context.WithCancel(ctx)
	defer derivedCancel()

	// Then apply timeout on top of the cancellation-inheriting context.
	cmdCtx, cmdCancel := context.WithTimeout(derivedCtx, time.Duration(timeout)*time.Second)
	defer cmdCancel()

	cmd := exec.CommandContext(cmdCtx, rgCommand, args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if cmdCtx.Err() == context.DeadlineExceeded {
		return &ToolResult{
			Content: fmt.Sprintf("Grep timed out after %d seconds", timeout),
			IsError: true,
		}, nil
	}

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			exitCode := exitErr.ExitCode()
			// Exit code 1 means no matches (not an error)
			if exitCode == 1 {
				return &ToolResult{
					Content: "No matches found",
					IsError: false,
				}, nil
			}
			// Surface ripgrep errors
			errMsg := stderr.String()
			if errMsg == "" {
				errMsg = stdout.String()
			}
			return &ToolResult{
				Content: fmt.Sprintf("ripgrep error (exit code %d): %s", exitCode, errMsg),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error executing ripgrep: %v", err),
			IsError: true,
		}, nil
	}

	output := stdout.String()

	// Apply head_limit and offset to output
	headLimit := defaultHeadLimit
	if limitVal, ok := input["head_limit"].(float64); ok {
		headLimit = int(limitVal)
	} else if limitVal, ok := input["head_limit"].(int); ok {
		headLimit = limitVal
	}
	offset := 0
	if offsetVal, ok := input["offset"].(float64); ok {
		offset = int(offsetVal)
	} else if offsetVal, ok := input["offset"].(int); ok {
		offset = offsetVal
	}

	// Process output by lines (applies to all modes)
	lines := strings.Split(output, "\n")
	if offset > 0 && offset < len(lines) {
		lines = lines[offset:]
	} else if offset >= len(lines) {
		lines = []string{}
	}
	if headLimit > 0 && len(lines) > headLimit {
		lines = lines[:headLimit]
		output = strings.Join(lines, "\n")
	} else {
		output = strings.Join(lines, "\n")
	}

	// Truncate if needed (20K char cap, rune-safe)
	truncated := false
	if len(output) > maxResultSizeChars {
		runes := []rune(output)
		if len(runes) > maxResultSizeChars {
			output = string(runes[:maxResultSizeChars])
		}
		truncated = true
		output += "\n[output truncated]"
	}

	return &ToolResult{
		Content:   output,
		IsError:   false,
		Truncated: truncated,
	}, nil
}

// boolVal safely extracts a boolean from a map.
func boolVal(input map[string]any, key string) bool {
	if val, ok := input[key].(bool); ok {
		return val
	}
	return false
}

// executeInProcess is the fallback path used when ripgrep is not
// available on the host. It calls the in-process grepinproc search
// backend, renders the result in ripgrep's text format, and runs the
// same post-processing (head_limit, offset, 20K char truncation) as
// the rg path.
func (t *GrepTool) executeInProcess(ctx context.Context, input map[string]any, pattern, cwd string) (*ToolResult, error) {
	// Build a context with the requested timeout.
	timeout := defaultTimeout
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}
	derivedCtx, derivedCancel := context.WithCancel(ctx)
	defer derivedCancel()
	cmdCtx, cmdCancel := context.WithTimeout(derivedCtx, time.Duration(timeout)*time.Second)
	defer cmdCancel()

	// Resolve the search path the same way the rg path does.
	path := "."
	if pathVal, ok := input["path"].(string); ok && pathVal != "" {
		path = pathVal
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}

	opts := grepinproc.Options{
		Pattern:       pattern,
		Path:          path,
		Cwd:           cwd,
		Glob:          stringVal(input, "glob"),
		OutputMode:    stringVal(input, "output_mode"),
		IgnoreCase:    boolVal(input, "i"),
		Multiline:     boolVal(input, "multiline"),
		Hidden:        true, // matches the rg path's --hidden
		FileType:      stringVal(input, "type"),
		ContextBefore: intValOrZero(input, "B"),
		ContextAfter:  intValOrZero(input, "A"),
	}
	if c, ok := input["C"].(float64); ok && c > 0 {
		// -C overrides -A/-B if both set
		opts.ContextBefore = int(c)
		opts.ContextAfter = int(c)
	}

	results, err := grepinproc.Run(cmdCtx, opts)
	if err != nil {
		// Distinguish a real error from a context cancellation.
		if cmdCtx.Err() == context.DeadlineExceeded {
			return &ToolResult{
				Content: fmt.Sprintf("Grep timed out after %d seconds", timeout),
				IsError: true,
			}, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return &ToolResult{
				Content: "search cancelled",
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("grepinproc error: %v", err),
			IsError: true,
		}, nil
	}

	outputMode := opts.OutputMode
	if outputMode == "" {
		outputMode = "files_with_matches"
	}
	// Relativize paths against the search root so the output matches
	// ripgrep's behavior (relative paths when searching "." from cwd).
	searchRoot := path
	if !strings.HasSuffix(searchRoot, string(filepath.Separator)) {
		searchRoot += string(filepath.Separator)
	}
	for i := range results {
		results[i].Target = strings.TrimPrefix(results[i].Target, searchRoot)
	}
	output := renderGrepResults(results, outputMode)

	// No matches: emulate the rg exit-code-1 behavior.
	if output == "" && !anyMatches(results) {
		return &ToolResult{Content: "No matches found", IsError: false}, nil
	}

	// Post-process: head_limit, offset, 20K char cap. Identical to
	// the rg path's tail of Execute.
	headLimit := defaultHeadLimit
	if limitVal, ok := input["head_limit"].(float64); ok {
		headLimit = int(limitVal)
	} else if limitVal, ok := input["head_limit"].(int); ok {
		headLimit = limitVal
	}
	offset := 0
	if offsetVal, ok := input["offset"].(float64); ok {
		offset = int(offsetVal)
	} else if offsetVal, ok := input["offset"].(int); ok {
		offset = offsetVal
	}

	lines := strings.Split(output, "\n")
	if offset > 0 && offset < len(lines) {
		lines = lines[offset:]
	} else if offset >= len(lines) {
		lines = []string{}
	}
	if headLimit > 0 && len(lines) > headLimit {
		lines = lines[:headLimit]
	}
	output = strings.Join(lines, "\n")

	truncated := false
	if len(output) > maxResultSizeChars {
		output = output[:maxResultSizeChars]
		truncated = true
		output += "\n[output truncated]"
	}

	return &ToolResult{
		Content:   output,
		IsError:   false,
		Truncated: truncated,
	}, nil
}

// renderGrepResults converts grepinproc results into ripgrep-style text.
// The format matches what ripgrep emits for the same mode so callers
// that already understand rg output do not need to know which backend
// produced the result.
func renderGrepResults(results []grepinproc.Result, mode string) string {
	var b strings.Builder
	switch mode {
	case "files_with_matches":
		for _, r := range results {
			b.WriteString(r.Target)
			b.WriteByte('\n')
		}
	case "count":
		for _, r := range results {
			fmt.Fprintf(&b, "%s:%d\n", r.Target, len(r.Matches))
		}
	default: // "content" or unset
		for _, r := range results {
			for _, m := range r.Matches {
				// Render context-before lines as "filename-lineno-content".
				// Render the match line as "filename:lineno:content".
				// Render context-after lines the same way.
				for i, before := range m.Before {
					fmt.Fprintf(&b, "%s:%d-%s\n", r.Target,
						m.Line-int64(len(m.Before))+int64(i), before)
				}
				fmt.Fprintf(&b, "%s:%d:%s\n", r.Target, m.Line, m.Content)
				for i, after := range m.After {
					fmt.Fprintf(&b, "%s:%d-%s\n", r.Target, m.Line+int64(i+1), after)
				}
			}
		}
	}
	return b.String()
}

// anyMatches reports whether any result has at least one match.
func anyMatches(results []grepinproc.Result) bool {
	for _, r := range results {
		if len(r.Matches) > 0 {
			return true
		}
	}
	return false
}

// stringVal safely extracts a string from a map.
func stringVal(input map[string]any, key string) string {
	if val, ok := input[key].(string); ok {
		return val
	}
	return ""
}

// intValOrZero safely extracts a non-negative int from a map.
func intValOrZero(input map[string]any, key string) int {
	if v, ok := input[key].(float64); ok && v > 0 {
		return int(v)
	}
	if v, ok := input[key].(int); ok && v > 0 {
		return v
	}
	return 0
}
