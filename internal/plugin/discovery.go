// Package plugin provides plugin manifest types, discovery, and loading.
package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ipy/jenny/internal/skills"
)

const (
	// manifestFileName is the name of the plugin manifest file.
	manifestFileName = "plugin.json"

	// pluginDirName is the directory name containing the plugin manifest.
	pluginDirName = ".codex-plugin"

	// maxDiscoveryDepth is the maximum directory depth for plugin discovery.
	maxDiscoveryDepth = 5
)

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

// FindPluginRoots walks rootDir looking for .codex-plugin/plugin.json directories.
// Returns paths to plugin root directories (the parent of .codex-plugin/).
// Skips hidden directories (starting with .) but not .codex-plugin.
// Maximum depth of 5 levels.
func FindPluginRoots(rootDir string) []string {
	var roots []string

	// Ensure root directory exists
	info, err := os.Stat(rootDir)
	if err != nil || !info.IsDir() {
		return roots
	}

	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}

		if d.IsDir() {
			// Calculate depth from root by counting path separators
			rel, err := filepath.Rel(rootDir, path)
			if err != nil {
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

			// Check if this directory is a .codex-plugin directory (the plugin marker)
			if name == pluginDirName {
				// Get the parent directory (plugin root)
				root := filepath.Dir(path)
				roots = append(roots, root)
				return filepath.SkipDir // Don't recurse into plugin
			}

			// Skip other hidden directories (but not root and not .codex-plugin)
			if path != rootDir && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
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
