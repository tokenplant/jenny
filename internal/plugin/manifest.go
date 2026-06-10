// Package plugin provides plugin manifest types, discovery, and loading.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// AuthorInfo describes a plugin author.
type AuthorInfo struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// PluginManifestInterface controls how install surfaces present the plugin.
type PluginManifestInterface struct {
	DisplayName       string   `json:"displayName,omitempty"`
	ShortDescription  string   `json:"shortDescription,omitempty"`
	LongDescription   string   `json:"longDescription,omitempty"`
	DeveloperName     string   `json:"developerName,omitempty"`
	Category          string   `json:"category,omitempty"`
	Capabilities      []string `json:"capabilities,omitempty"`
	BrandColor        string   `json:"brandColor,omitempty"`
	WebsiteURL        string   `json:"websiteURL,omitempty"`
	PrivacyPolicyURL  string   `json:"privacyPolicyURL,omitempty"`
	TermsOfServiceURL string   `json:"termsOfServiceURL,omitempty"`
	DefaultPrompt     []string `json:"defaultPrompt,omitempty"`
	ComposerIcon      string   `json:"composerIcon,omitempty"`
	Logo              string   `json:"logo,omitempty"`
	Screenshots       []string `json:"screenshots,omitempty"`
}

// PluginManifest represents a plugin.json file inside a plugin marker directory.
type PluginManifest struct {
	Name        string                   `json:"name,omitempty"`
	Version     string                   `json:"version,omitempty"`
	Description string                   `json:"description,omitempty"`
	Author      AuthorInfo               `json:"author"`
	Homepage    string                   `json:"homepage,omitempty"`
	Repository  string                   `json:"repository,omitempty"`
	License     string                   `json:"license,omitempty"`
	Keywords    []string                 `json:"keywords,omitempty"`
	Skills      string                   `json:"skills,omitempty"`
	MCPServers  string                   `json:"mcpServers,omitempty"`
	Hooks       string                   `json:"hooks,omitempty"`
	Apps        string                   `json:"apps,omitempty"`
	Interface   *PluginManifestInterface `json:"interface,omitempty"`
}

// ParseManifest parses raw JSON bytes into a PluginManifest.
// Returns nil, nil for empty input (not an error).
// Returns error for invalid JSON or missing required 'name' field.
func ParseManifest(data []byte) (*PluginManifest, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var m PluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing plugin manifest: %w", err)
	}
	if m.Name == "" {
		return nil, errors.New("plugin manifest requires non-empty 'name'")
	}
	return &m, nil
}

// LoadManifest reads a plugin manifest from a file path.
// Returns error for non-existent file or read failure.
func LoadManifest(path string) (*PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading plugin manifest: %w", err)
	}
	return ParseManifest(data)
}
