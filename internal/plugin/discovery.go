// Package plugin provides plugin manifest types, discovery, and loading.
package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ipy/jenny/internal/mcp"
	"github.com/ipy/jenny/internal/skills"
)

const (
	// manifestFileName is the name of the plugin manifest file.
	manifestFileName = "plugin.json"

	// maxDiscoveryDepth is the maximum directory depth for plugin discovery.
	maxDiscoveryDepth = 5
)

// pluginDirNames lists marker directories in priority order.
var pluginDirNames = []string{".jenny-plugin", ".claude-plugin", ".codex-plugin"}

// PluginDirNames returns the marker directory names in priority order.
func PluginDirNames() []string { return pluginDirNames }

// LoadedPlugin represents a plugin loaded from disk with its manifest.
type LoadedPlugin struct {
	RootPath     string
	Manifest     *PluginManifest
	ManifestPath string
}

// Validate checks that the plugin has a valid manifest and proper paths.
func (p *LoadedPlugin) Validate() error {
	if p.Manifest == nil {
		return errors.New("plugin manifest is nil")
	}
	if p.Manifest.Name == "" {
		return errors.New("plugin manifest requires non-empty 'name'")
	}

	// Validate path fields are empty or relative starting with ./
	if err := validateRelativePath(p.Manifest.Skills); err != nil {
		return err
	}
	if err := validateRelativePath(p.Manifest.MCPServers); err != nil {
		return err
	}
	if err := validateRelativePath(p.Manifest.Hooks); err != nil {
		return err
	}
	if err := validateRelativePath(p.Manifest.Apps); err != nil {
		return err
	}

	// Validate interface URLs if set
	if p.Manifest.Interface != nil {
		if err := validateURL(p.Manifest.Interface.WebsiteURL); err != nil {
			return err
		}
		if err := validateURL(p.Manifest.Interface.PrivacyPolicyURL); err != nil {
			return err
		}
		if err := validateURL(p.Manifest.Interface.TermsOfServiceURL); err != nil {
			return err
		}
	}

	return nil
}

// validateRelativePath checks that a path is empty or starts with ./
func validateRelativePath(path string) error {
	if path == "" {
		return nil
	}
	if !strings.HasPrefix(path, "./") {
		return errors.New("path must start with './': " + path)
	}
	return nil
}

// validateURL checks that a URL starts with https://
func validateURL(url string) error {
	if url == "" {
		return nil
	}
	if !strings.HasPrefix(url, "https://") {
		return errors.New("URL must start with 'https://': " + url)
	}
	return nil
}

func isPluginMarker(name string) bool {
	for _, m := range pluginDirNames {
		if name == m {
			return true
		}
	}
	return false
}

// hasPluginMarker checks whether dir contains any marker/plugin.json,
// testing markers in priority order.
func hasPluginMarker(dir string) bool {
	for _, marker := range pluginDirNames {
		if _, err := os.Stat(filepath.Join(dir, marker, manifestFileName)); err == nil {
			return true
		}
	}
	return false
}

// FindPluginRoots walks rootDir looking for plugin marker directories.
// Markers are checked in priority order: .jenny-plugin, .claude-plugin, .codex-plugin.
// Returns paths to plugin root directories (the parent of the marker dir).
// Skips hidden directories except plugin markers. Maximum depth of 5 levels.
func FindPluginRoots(rootDir string) []string {
	var roots []string

	info, err := os.Stat(rootDir)
	if err != nil || !info.IsDir() {
		return roots
	}

	seen := make(map[string]bool)

	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			return nil
		}
		depth := 0
		if rel != "." {
			depth = len(strings.Split(rel, string(filepath.Separator)))
		}
		if depth > maxDiscoveryDepth {
			return filepath.SkipDir
		}

		name := d.Name()

		// Skip marker directories themselves — we probe for them from the parent.
		if isPluginMarker(name) {
			return filepath.SkipDir
		}

		// Skip other hidden directories (but not rootDir).
		if path != rootDir && strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}

		// Probe this directory for any plugin marker.
		if !seen[path] && hasPluginMarker(path) {
			seen[path] = true
			roots = append(roots, path)
		}

		return nil
	})

	return roots
}

// SkillsDir returns the absolute path to the plugin's skills directory.
// Returns "" if no skills path is configured.
func (p *LoadedPlugin) SkillsDir() string {
	if p.Manifest == nil || p.Manifest.Skills == "" {
		return ""
	}
	return filepath.Join(p.RootPath, p.Manifest.Skills)
}

// MCPServersDir returns the absolute path to the plugin's MCP server config file.
// Returns "" if no mcpServers path is configured.
func (p *LoadedPlugin) MCPServersDir() string {
	if p.Manifest == nil || p.Manifest.MCPServers == "" {
		return ""
	}
	return filepath.Join(p.RootPath, p.Manifest.MCPServers)
}

// LoadPluginMCPServers loads MCP server definitions from a plugin's MCP config file.
// Returns nil, nil if no mcpServers path is configured.
func LoadPluginMCPServers(p *LoadedPlugin) (map[string]mcp.MCPServerDef, error) {
	if p.Manifest == nil || p.Manifest.MCPServers == "" {
		return nil, nil
	}
	configPath := p.MCPServersDir()
	return mcp.LoadConfig([]string{configPath}, false)
}

// LoadPluginSkills loads skills from a plugin's skills directory.
// Returns nil, nil if no skills path is configured.
func LoadPluginSkills(p *LoadedPlugin) ([]skills.Skill, error) {
	if p.Manifest == nil || p.Manifest.Skills == "" {
		return nil, nil
	}
	skillsDir := p.SkillsDir()
	if skillsDir == "" {
		return nil, nil
	}
	info, err := os.Stat(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("plugin skills dir %q: %w", skillsDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("plugin skills path %q is not a directory", skillsDir)
	}
	return skills.Discover(skillsDir)
}
