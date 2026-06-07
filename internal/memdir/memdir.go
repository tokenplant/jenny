// Package memdir provides project-scoped auto-memory with MEMORY.md index
// and topic files (user/feedback/project/reference).
package memdir

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// MemoryIndexFile is the name of the MEMORY.md index file.
	MemoryIndexFile = "MEMORY.md"

	// MaxIndexLines is the maximum number of lines in MEMORY.md.
	MaxIndexLines = 200

	// MaxIndexBytes is the maximum byte size of MEMORY.md.
	MaxIndexBytes = 25 * 1024

	// FreshnessThreshold is the duration after which a memory file is considered stale.
	FreshnessThreshold = 24 * time.Hour
)

// MemoryType represents the type of memory entry.
type MemoryType string

const (
	MemoryTypeUser     MemoryType = "user"
	MemoryTypeFeedback MemoryType = "feedback"
	MemoryTypeProject  MemoryType = "project"
	MemoryTypeRef      MemoryType = "reference"
)

// Config holds configuration for the Memdir.
type Config struct {
	// ProjectRoot is the git repository root path.
	ProjectRoot string

	// BareMode indicates whether --bare mode is active.
	BareMode bool

	// AutoMemoryEnabled indicates whether auto-memory is enabled in settings.
	AutoMemoryEnabled bool

	// IsRemote indicates whether this is a remote session.
	IsRemote bool

	// MemoryDirExists is a function that checks if a memory directory exists
	// for a remote session. If nil, defaults to false.
	MemoryDirExists func(projectRoot string) bool
}

// Memdir provides project-scoped auto-memory management.
type Memdir struct {
	config     Config
	memoryPath string
}

// New creates a new Memdir with the given configuration.
// It does not create any directories - use Create() for that.
func New(cfg Config) (*Memdir, error) {
	if cfg.ProjectRoot == "" {
		return nil, fmt.Errorf("project root is required")
	}

	// Determine memory path: <config-home>/projects/<sanitized-git-root>/memory/
	configHome, err := os.UserConfigDir()
	if err != nil {
		// Fallback to home directory
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine config home: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}

	sanitizedRoot := strings.ReplaceAll(cfg.ProjectRoot, "/", "-")
	// Remove leading dashes from sanitized root (e.g., /Users/sin -> Users-sin)
	sanitizedRoot = strings.TrimPrefix(sanitizedRoot, "-")

	memoryPath := filepath.Join(configHome, "projects", sanitizedRoot, "memory")

	return &Memdir{
		config:     cfg,
		memoryPath: memoryPath,
	}, nil
}

// MemoryPathFromProjectRoot returns the memory directory path for a given project root.
// This is a convenience function that computes the path without creating a Memdir instance.
func MemoryPathFromProjectRoot(projectRoot string) string {
	configHome, err := os.UserConfigDir()
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return "." // Fallback
		}
		configHome = filepath.Join(home, ".config")
	}

	sanitizedRoot := strings.ReplaceAll(projectRoot, "/", "-")
	sanitizedRoot = strings.TrimPrefix(sanitizedRoot, "-")
	return filepath.Join(configHome, "projects", sanitizedRoot, "memory")
}

// IsDisabled returns true if memdir is disabled based on the disable chain:
// - DISABLE_AUTO_MEMORY env var
// - --bare mode flag
// - remote session without memory directory
// - settings autoMemoryEnabled: false
func (m *Memdir) IsDisabled() bool {
	// Check DISABLE_AUTO_MEMORY env var first
	if os.Getenv("DISABLE_AUTO_MEMORY") != "" {
		return true
	}

	// Check bare mode
	if m.config.BareMode {
		return true
	}

	// Check remote session without memory dir
	if m.config.IsRemote {
		if m.config.MemoryDirExists != nil {
			if !m.config.MemoryDirExists(m.config.ProjectRoot) {
				return true
			}
		} else {
			return true
		}
	}

	// Check settings autoMemoryEnabled
	if !m.config.AutoMemoryEnabled {
		return true
	}

	return false
}

// Create creates the memory directory and MEMORY.md index if not disabled.
// Returns nil if memdir is disabled or creation succeeds.
func (m *Memdir) Create() error {
	if m.IsDisabled() {
		return nil
	}

	// Create memory directory
	if err := os.MkdirAll(m.memoryPath, 0755); err != nil {
		return fmt.Errorf("creating memory directory: %w", err)
	}

	// Create subdirectories for memory types
	for _, memType := range []MemoryType{MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeRef} {
		subdir := filepath.Join(m.memoryPath, string(memType))
		if err := os.MkdirAll(subdir, 0755); err != nil {
			return fmt.Errorf("creating memory subdirectory %s: %w", memType, err)
		}
	}

	// Create MEMORY.md index if it doesn't exist
	indexPath := m.IndexPath()
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		file, err := os.Create(indexPath)
		if err != nil {
			return fmt.Errorf("creating memory index: %w", err)
		}
		file.Close()
	}

	return nil
}

// IndexPath returns the path to MEMORY.md.
func (m *Memdir) IndexPath() string {
	return filepath.Join(m.memoryPath, MemoryIndexFile)
}

// MemoryPath returns the memory directory path.
func (m *Memdir) MemoryPath() string {
	return m.memoryPath
}

// TopicPath returns the path to a topic file for the given memory type and name.
// The name is sanitized to prevent path traversal.
func (m *Memdir) TopicPath(memType MemoryType, name string) string {
	// Sanitize name to prevent path traversal
	sanitized := strings.ReplaceAll(name, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "..", "")
	sanitized = strings.Trim(sanitized, " ")

	return filepath.Join(m.memoryPath, string(memType), sanitized+".md")
}

// ReadIndex reads MEMORY.md with cap enforcement.
// Returns the content (possibly truncated) and any error.
// If truncated, the content will include a warning identifying which cap fired.
func (m *Memdir) ReadIndex() (string, error) {
	indexPath := m.IndexPath()
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading memory index: %w", err)
	}

	content := string(data)

	// Check line cap
	lines := strings.Split(content, "\n")
	lineCapFired := len(lines) > MaxIndexLines

	// Check byte cap
	byteCapFired := len(data) > MaxIndexBytes

	// Build warning string first so we can account for its size in truncation
	lineWarning := fmt.Sprintf("MEMORY.md truncated: %d-line cap\n\n", MaxIndexLines)
	byteWarning := fmt.Sprintf("MEMORY.md truncated: %d KB cap\n\n", MaxIndexBytes/1024)

	// When both caps fire, use byte warning (more specific to content size)
	warning := lineWarning
	if byteCapFired {
		warning = byteWarning
	}

	// If neither cap fires, return content as-is (no warning)
	if !lineCapFired && !byteCapFired {
		return content, nil
	}

	// Truncate content to leave headroom for warning, satisfying both caps.
	content = truncateForWarning(content, lines, lineCapFired, warning)
	return warning + content, nil
}

// EnsureFresh checks if the file at path has an mtime older than 24 hours.
// Returns true if the file is stale (mtime > 24h), false otherwise.
func (m *Memdir) EnsureFresh(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return time.Since(info.ModTime()) > FreshnessThreshold
}

// ReadTopicFile reads a topic file and applies freshness prefix if stale.
// Returns the content with <system-reminder> prefix if stale, or empty string if not found.
func (m *Memdir) ReadTopicFile(memType MemoryType, name string) (string, error) {
	path := m.TopicPath(memType, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading topic file: %w", err)
	}

	content := string(data)

	// Check freshness and prepend reminder if stale
	if m.EnsureFresh(path) {
		content = "<system-reminder>\n" + content
	}

	return content, nil
}

// ValidatePath validates that a path is within the memory directory.
// Returns an error if the path escapes the memory directory via path traversal.
func (m *Memdir) ValidatePath(path string) error {
	absMemoryPath, err := filepath.Abs(m.memoryPath)
	if err != nil {
		return fmt.Errorf("resolving memory path: %w", err)
	}

	absTargetPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving target path: %w", err)
	}

	// Check for path traversal
	if !strings.HasPrefix(absTargetPath, absMemoryPath) {
		return fmt.Errorf("path traversal rejected: %s", path)
	}

	return nil
}

// sanitizeMemoryType validates and returns the memory type.
func sanitizeMemoryType(memType MemoryType) error {
	switch memType {
	case MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeRef:
		return nil
	default:
		return fmt.Errorf("invalid memory type: %s", memType)
	}
}

// WriteTopicFile writes content to a topic file.
// Validates path and rejects path traversal attempts.
func (m *Memdir) WriteTopicFile(memType MemoryType, name string, content string) error {
	if err := sanitizeMemoryType(memType); err != nil {
		return err
	}

	path := m.TopicPath(memType, name)

	// Validate path is within memory directory
	if err := m.ValidatePath(path); err != nil {
		return err
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing topic file: %w", err)
	}

	return nil
}

// ListTopicFiles returns a list of topic file names for the given memory type.
func (m *Memdir) ListTopicFiles(memType MemoryType) ([]string, error) {
	if err := sanitizeMemoryType(memType); err != nil {
		return nil, err
	}

	subdir := filepath.Join(m.memoryPath, string(memType))
	entries, err := os.ReadDir(subdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading topic directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			name := strings.TrimSuffix(entry.Name(), ".md")
			names = append(names, name)
		}
	}

	return names, nil
}

// UpdateIndex updates MEMORY.md with a line-by-line index of topic files.
// Deduplicates by name (same-name write replaces, no duplicate entries).
func (m *Memdir) UpdateIndex(entries []string) error {
	indexPath := m.IndexPath()

	// Read existing entries
	existing := make(map[string]bool)
	var lines []string

	file, err := os.Open(indexPath)
	if err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			// Skip empty lines and comment lines
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Skip truncation warnings
			if strings.HasPrefix(line, "MEMORY.md truncated:") {
				continue
			}
			lines = append(lines, line)
			existing[line] = true
		}
		if err := scanner.Err(); err != nil {
			file.Close()
			return fmt.Errorf("scanning memory index: %w", err)
		}
		file.Close()
	}

	// Add new entries (skip duplicates)
	for _, entry := range entries {
		if !existing[entry] {
			lines = append(lines, entry)
			existing[entry] = true
		}
	}

	// Build content with header
	var content strings.Builder
	content.WriteString("# Auto-Memory Index\n\n")

	// Write all entries
	for _, line := range lines {
		content.WriteString(line)
		content.WriteString("\n")
	}

	// Apply caps and get final content
	finalContent := content.String()
	data := []byte(finalContent)

	// Check line cap
	allLines := strings.Split(finalContent, "\n")
	lineCapFired := len(allLines) > MaxIndexLines

	// Check byte cap
	byteCapFired := len(data) > MaxIndexBytes

	// Build warning strings so we can account for their size in truncation
	lineWarning := fmt.Sprintf("MEMORY.md truncated: %d-line cap\n\n", MaxIndexLines)
	byteWarning := fmt.Sprintf("MEMORY.md truncated: %d KB cap\n\n", MaxIndexBytes/1024)

	// When both caps fire, use byte warning (more specific to content size)
	warning := lineWarning
	if byteCapFired {
		warning = byteWarning
	}

	if lineCapFired || byteCapFired {
		// Truncate content to leave headroom for warning, satisfying both caps.
		finalContent = truncateForWarning(finalContent, allLines, lineCapFired, warning)
		finalContent = warning + finalContent
	}

	if err := os.WriteFile(indexPath, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("writing memory index: %w", err)
	}

	return nil
}

// Exists returns true if the memory directory exists.
func (m *Memdir) Exists() bool {
	_, err := os.Stat(m.memoryPath)
	return err == nil
}

// truncateForWarning shrinks content so the (warning + content) result satisfies
// MaxIndexLines and MaxIndexBytes. The caller has already computed lines and
// lineCapFired from the original content. When lineCapFired is true, the line
// cap is enforced; otherwise the byte cap is enforced (and may reduce lines too
// if a single line exceeds the byte budget). Warning headroom is reserved
// before truncation so the final result respects both caps.
func truncateForWarning(content string, lines []string, lineCapFired bool, warning string) string {
	warningLines := strings.Count(warning, "\n")
	warningBytes := len(warning)

	maxContentLines := max(MaxIndexLines-warningLines, 0)
	maxContentBytes := max(MaxIndexBytes-warningBytes, 0)

	// Apply line cap first (per spec: line-first).
	if lineCapFired && len(lines) > maxContentLines {
		lines = lines[:maxContentLines]
		content = strings.Join(lines, "\n")
	}

	// Apply byte cap, trimming at the last newline so we don't leave a partial line.
	if len(content) > maxContentBytes {
		truncated := content[:maxContentBytes]
		if lastNewline := strings.LastIndex(truncated, "\n"); lastNewline > 0 {
			content = truncated[:lastNewline]
		} else {
			content = truncated
		}
	}

	return content
}
