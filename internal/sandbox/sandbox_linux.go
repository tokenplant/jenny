//go:build linux && !jenny_no_sandbox

package sandbox

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// LinuxSandboxManager implements SandboxManager using bubblewrap (bwrap).
type LinuxSandboxManager struct {
	mu        sync.RWMutex
	config    Config
	available bool
	active    bool
	initError error
}

// NewLinuxSandboxManager creates a new Linux sandbox manager.
func NewLinuxSandboxManager() *LinuxSandboxManager {
	return &LinuxSandboxManager{}
}

// Initialize implements SandboxManager.Initialize.
func (m *LinuxSandboxManager) Initialize(ctx context.Context, cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = cfg
	return m.setupLocked(ctx, cfg)
}

// setupLocked re-validates sandbox dependencies with the given config.
// Caller must hold m.mu.
func (m *LinuxSandboxManager) setupLocked(ctx context.Context, cfg Config) error {
	// Check if bwrap is available
	if err := exec.CommandContext(ctx, "which", "bwrap").Run(); err != nil {
		m.available = false
		if cfg.FailIfUnavailable {
			m.initError = &ErrMissingDependency{
				Backend:     BackendLinux,
				Dependency:  "bwrap",
				InstallHint: "Install bubblewrap: sudo apt install bubblewrap (Debian/Ubuntu) or sudo dnf install bubblewrap (Fedora)",
			}
			return m.initError
		}
		return nil
	}

	// Validate sandboxed ripgrep binary if configured
	if cfg.Ripgrep.Command != "" {
		if err := exec.CommandContext(ctx, "which", cfg.Ripgrep.Command).Run(); err != nil {
			m.initError = &ErrMissingDependency{
				Backend:     BackendLinux,
				Dependency:  "ripgrep: " + cfg.Ripgrep.Command,
				InstallHint: "Install ripgrep or configure correct path in sandbox.ripgrep",
			}
			return m.initError
		}
	}

	m.available = true
	m.active = cfg.Backend == BackendLinux
	return nil
}

// WrapWithSandbox implements SandboxManager.WrapWithSandbox.
func (m *LinuxSandboxManager) WrapWithSandbox(command string) (string, error) {
	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()

	// Check if command matches an excluded pattern
	for _, pattern := range cfg.ExcludedCommands {
		if matchGlobPattern(pattern, command) {
			return command, nil
		}
	}

	// Build bwrap arguments
	args := m.buildBwrapArgs(cfg)

	// Build the wrapped command
	var wrapped strings.Builder
	wrapped.WriteString("bwrap")

	// Add bwrap arguments
	for _, arg := range args {
		wrapped.WriteString(" ")
		wrapped.WriteString(arg)
	}

	// Add shell command to execute
	wrapped.WriteString(" -- sh -c ")
	wrapped.WriteString(escapeShellArg(command))

	return wrapped.String(), nil
}

// buildBwrapArgs builds the bwrap command arguments based on config.
func (m *LinuxSandboxManager) buildBwrapArgs(cfg Config) []string {
	var args []string

	// Basic sandbox options
	args = append(args, "--unshare-ipc", "--unshare-pid", "--unshare-net")

	// Network restrictions
	if cfg.NetworkPolicy == NetworkPolicyManagedDomainsOnly {
		// Note: bwrap doesn't have fine-grained network allowlists like sandbox-exec
		// For managed-domains-only, network is already disabled via --unshare-net above
		// No additional flags needed; --unshare-net already added above
	}

	// Filesystem: readonly home and tmp
	args = append(args, "--ro-bind", "/usr", "/usr")
	args = append(args, "--ro-bind", "/lib", "/lib")
	args = append(args, "--ro-bind", "/lib64", "/lib64")
	args = append(args, "--tmpfs", "/tmp")
	args = append(args, "--tmpfs", "/var/tmp")

	// Deny specific directories
	for _, dir := range cfg.FilesystemDenyDirs {
		args = append(args, "--ro-bind", dir, dir)
	}

	// Allow specific directories (read-write)
	for _, dir := range cfg.FilesystemAllowedDirs {
		args = append(args, "--bind", dir, dir)
	}

	// Dev null access
	args = append(args, "--dev", "/dev")
	args = append(args, "--proc", "/proc")

	return args
}

// RefreshConfig implements SandboxManager.RefreshConfig.
func (m *LinuxSandboxManager) RefreshConfig(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Re-run setup with current config to re-validate all settings
	return m.setupLocked(ctx, m.config)
}

// IsAvailable implements SandboxManager.IsAvailable.
func (m *LinuxSandboxManager) IsAvailable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.available
}

// IsActive implements SandboxManager.IsActive.
func (m *LinuxSandboxManager) IsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// RipgrepConfig implements SandboxManager.RipgrepConfig.
func (m *LinuxSandboxManager) RipgrepConfig() RipgrepConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.Ripgrep
}

// escapeShellArg escapes a string for use in a shell argument.
func escapeShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// matchGlobPattern checks if a command matches a glob pattern.
// Supports: * (any chars), ? (single char)
func matchGlobPattern(pattern, command string) bool {
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
