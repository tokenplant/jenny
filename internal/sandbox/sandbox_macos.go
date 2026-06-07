//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// MacOSSandboxManager implements SandboxManager using sandbox-exec.
type MacOSSandboxManager struct {
	mu        sync.RWMutex
	config    Config
	available bool
	active    bool
	initError error
}

// NewMacOSSandboxManager creates a new macOS sandbox manager.
func NewMacOSSandboxManager() *MacOSSandboxManager {
	return &MacOSSandboxManager{}
}

// Initialize implements SandboxManager.Initialize.
func (m *MacOSSandboxManager) Initialize(ctx context.Context, cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg
	return m.setupLocked(ctx, cfg)
}

// WrapWithSandbox implements SandboxManager.WrapWithSandbox.
func (m *MacOSSandboxManager) WrapWithSandbox(command string) (string, error) {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	// Check if command matches an excluded pattern
	for _, pattern := range cfg.ExcludedCommands {
		if matchGlobPattern(pattern, command) {
			return command, nil
		}
	}

	// Build sandbox profile
	profile, err := m.buildSandboxProfile(cfg, command)
	if err != nil {
		return "", fmt.Errorf("failed to build sandbox profile: %w", err)
	}

	// Wrap command with sandbox-exec
	return fmt.Sprintf("sandbox-exec -p '%s' sh -c %s", profile, escapeShellArg(command)), nil
}

// buildSandboxProfile builds a sandbox-exec profile based on config.
func (m *MacOSSandboxManager) buildSandboxProfile(cfg Config, _ string) (string, error) {
	var builder strings.Builder

	builder.WriteString("(version 1)\n")
	builder.WriteString("(allow default)\n")

	// Build merged allowed domains list for Normal mode
	// Merge AllowedDomains + WebFetchAllowedDomains
	allowedDomains := make([]string, 0, len(cfg.AllowedDomains)+len(cfg.WebFetchAllowedDomains))
	allowedDomains = append(allowedDomains, cfg.AllowedDomains...)
	allowedDomains = append(allowedDomains, cfg.WebFetchAllowedDomains...)

	// Deny network by default unless allowed
	if cfg.NetworkPolicy == NetworkPolicyManagedDomainsOnly {
		builder.WriteString("(deny network*)\n")
		// Allow merged domains (AllowedDomains + WebFetchAllowedDomains)
		for _, domain := range allowedDomains {
			fmt.Fprintf(&builder, "(allow network (remote %s))\n", domain)
		}
	} else if len(allowedDomains) > 0 {
		// Normal mode: allow merged domains
		for _, domain := range allowedDomains {
			fmt.Fprintf(&builder, "(allow network (remote %s))\n", domain)
		}
	}

	// Denied domains are always blocked regardless of policy mode
	for _, domain := range cfg.DeniedDomains {
		fmt.Fprintf(&builder, "(deny network (remote %s))\n", domain)
	}

	// Filesystem restrictions
	for _, dir := range cfg.FilesystemDenyDirs {
		fmt.Fprintf(&builder, "(deny file* (subpath %s))\n", dir)
	}
	for _, dir := range cfg.FilesystemAllowedDirs {
		fmt.Fprintf(&builder, "(allow file* (subpath %s))\n", dir)
	}

	return builder.String(), nil
}

// RefreshConfig implements SandboxManager.RefreshConfig.
func (m *MacOSSandboxManager) RefreshConfig(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-run setup with current config to re-validate all settings
	return m.setupLocked(ctx, m.config)
}

// setupLocked re-validates sandbox dependencies with the given config.
// Caller must hold m.mu.
func (m *MacOSSandboxManager) setupLocked(ctx context.Context, cfg Config) error {
	// Check if sandbox-exec is available
	if err := exec.CommandContext(ctx, "which", "sandbox-exec").Run(); err != nil {
		m.available = false
		if cfg.FailIfUnavailable {
			m.initError = &ErrMissingDependency{
				Backend:     BackendMacOS,
				Dependency:  "sandbox-exec",
				InstallHint: "Install Xcode Command Line Tools or enable Sandbox via System Preferences",
			}
			return m.initError
		}
		return nil
	}

	// Validate sandboxed ripgrep binary if configured
	if cfg.Ripgrep.Command != "" {
		if err := exec.CommandContext(ctx, "which", cfg.Ripgrep.Command).Run(); err != nil {
			m.initError = &ErrMissingDependency{
				Backend:     BackendMacOS,
				Dependency:  "ripgrep: " + cfg.Ripgrep.Command,
				InstallHint: "Install ripgrep or configure correct path in sandbox.ripgrep",
			}
			return m.initError
		}
	}

	m.available = true
	m.active = cfg.Backend == BackendMacOS
	return nil
}

// IsAvailable implements SandboxManager.IsAvailable.
func (m *MacOSSandboxManager) IsAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.available
}

// IsActive implements SandboxManager.IsActive.
func (m *MacOSSandboxManager) IsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// RipgrepConfig implements SandboxManager.RipgrepConfig.
func (m *MacOSSandboxManager) RipgrepConfig() RipgrepConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Ripgrep
}

// escapeShellArg escapes a string for use in a shell argument.
func escapeShellArg(s string) string {
	// Replace ' with '\'' and wrap in single quotes
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// matchGlobPattern checks if a command matches a glob pattern.
// Supports: * (any chars), ? (single char)
func matchGlobPattern(pattern, command string) bool {
	// Convert glob pattern to regex
	regexPattern := globToRegex(pattern)
	matched, _ := regexp.MatchString(regexPattern, command)
	return matched
}

// globToRegex converts a glob pattern to a regex pattern.
func globToRegex(pattern string) string {
	var result strings.Builder
	result.WriteString("^")

	for i, ch := range pattern {
		switch ch {
		case '*':
			result.WriteString(".*")
		case '?':
			result.WriteString(".")
		case '.':
			result.WriteString(`\.`)
		case '\\':
			if i+1 < len(pattern) {
				result.WriteString(regexp.QuoteMeta(string(pattern[i+1])))
			} else {
				result.WriteString(`\\`)
			}
		default:
			result.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}

	result.WriteString("$")
	return result.String()
}
