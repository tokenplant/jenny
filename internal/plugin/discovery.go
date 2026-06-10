// Package plugin provides plugin manifest types, discovery, and loading.
package plugin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

	// Track depth per directory to implement max depth of 5
	dirDepth := map[string]int{rootDir: 0}

	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}

		if d.IsDir() {
			// Get depth for this directory
			depth := 0
			if savedDepth, ok := dirDepth[path]; ok {
				depth = savedDepth
			} else {
				// Calculate depth from root by counting path separators
				rel, err := filepath.Rel(rootDir, path)
				if err != nil {
					return nil
				}
				if rel == "." {
					depth = 0
				} else {
					depth = len(strings.Split(rel, string(filepath.Separator)))
				}
				dirDepth[path] = depth
			}

			// Skip if we've exceeded max depth
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
