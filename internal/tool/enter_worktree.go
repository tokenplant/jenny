// Package tool provides the tool interface and implementations.
package tool

import (
	"context"
	"crypto/rand"
	"fmt"
	"regexp"
	"sync"

	"github.com/ipy/jenny/internal/git"
)

// EnterWorktreeTool provides the ability to create isolated git worktree sessions.
type EnterWorktreeTool struct {
	mu          sync.Mutex
	inWorktree  bool
	worktreeDir string
}

// NewEnterWorktreeTool creates a new EnterWorktreeTool.
func NewEnterWorktreeTool() *EnterWorktreeTool {
	return &EnterWorktreeTool{}
}

// Name returns the tool name.
func (t *EnterWorktreeTool) Name() string {
	return "EnterWorktree"
}

// Description returns a description of the tool.
func (t *EnterWorktreeTool) Description() string {
	return "Create an isolated git worktree session for subagent tasks. Use when you need to work on a branch without affecting the main working directory."
}

// InputSchema returns the JSON schema for tool input.
func (t *EnterWorktreeTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Optional slug for the worktree. If omitted, a random 8-character hex name is generated. Must be alphanumeric segments separated by '/', each segment max 64 chars, total max 128 chars.",
			},
		},
	}
}

// Execute creates a new git worktree session.
func (t *EnterWorktreeTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if already in a worktree session
	if t.inWorktree {
		return &ToolResult{
			Content: "already in a worktree session. Use ExitWorktree first.",
			IsError: true,
		}, nil
	}

	// Get optional name parameter
	var slug string
	if name, ok := input["name"].(string); ok && name != "" {
		slug = name
		// Validate slug
		if err := validateSlug(slug); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("invalid slug: %v", err),
				IsError: true,
			}, nil
		}
	} else {
		// Generate random 8-char hex slug
		slug = generateRandomSlug()
	}

	// Resolve to canonical git root first
	repoRoot, err := git.GetRoot(cwd)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("failed to find git repository root: %v", err),
			IsError: true,
		}, nil
	}

	// Create the worktree
	worktreePath, err := git.CreateWorktree(repoRoot, slug)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("failed to create worktree: %v", err),
			IsError: true,
		}, nil
	}

	// Get branch name
	branch, err := git.GetBranch(worktreePath)
	if err != nil {
		branch = "unknown"
	}

	// Mark as in worktree session
	t.inWorktree = true
	t.worktreeDir = worktreePath

	return &ToolResult{
		Content: fmt.Sprintf(`{"path": %q, "branch": %q}`, worktreePath, branch),
		IsError: false,
	}, nil
}

// validateSlug validates the worktree slug format.
// Must be alphanumeric segments separated by '/', each segment max 64 chars, total max 128 chars.
func validateSlug(slug string) error {
	if len(slug) > 128 {
		return fmt.Errorf("total length exceeds 128 characters")
	}

	// Regex: ^[a-zA-Z0-9][a-zA-Z0-9._-]*(\/[a-zA-Z0-9][a-zA-Z0-9._-]*)*$
	// First char must be alphanumeric, subsequent can be alphanumeric, dot, underscore, hyphen
	// Segments separated by /
	pattern := `^[a-zA-Z0-9][a-zA-Z0-9._-]*(\/[a-zA-Z0-9][a-zA-Z0-9._-]*)*$`
	matched, err := regexp.MatchString(pattern, slug)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("must be alphanumeric segments separated by '/', each segment max 64 chars")
	}

	// Check each segment length
	segments := splitSlug(slug)
	for _, seg := range segments {
		if len(seg) > 64 {
			return fmt.Errorf("segment %q exceeds 64 characters", seg)
		}
	}

	return nil
}

// splitSlug splits a slug by '/'.
func splitSlug(slug string) []string {
	var segments []string
	start := 0
	for i := 0; i < len(slug); i++ {
		if slug[i] == '/' {
			if start < i {
				segments = append(segments, slug[start:i])
			}
			start = i + 1
		}
	}
	if start < len(slug) {
		segments = append(segments, slug[start:])
	}
	return segments
}

// generateRandomSlug generates a random 8-character hex string.
func generateRandomSlug() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
