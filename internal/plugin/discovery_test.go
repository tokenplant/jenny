package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindPluginRoots_NoneFound(t *testing.T) {
	// Create a temp directory with no plugins
	tmpDir := t.TempDir()

	roots := FindPluginRoots(tmpDir)
	if len(roots) != 0 {
		t.Errorf("expected no plugin roots, got %v", roots)
	}
}

func TestFindPluginRoots_FindsPlugins(t *testing.T) {
	// Create a temp directory structure:
	// root/
	//   a/.codex-plugin/plugin.json
	//   b/.codex-plugin/plugin.json
	//   .hidden/.codex-plugin/plugin.json (should be skipped)
	tmpDir := t.TempDir()

	// Create plugin directories
	aDir := filepath.Join(tmpDir, "a")
	bDir := filepath.Join(tmpDir, "b")
	hiddenDir := filepath.Join(tmpDir, ".hidden")

	for _, dir := range []string{aDir, bDir, hiddenDir} {
		if err := os.MkdirAll(filepath.Join(dir, ".codex-plugin"), 0755); err != nil {
			t.Fatalf("failed to create plugin dir: %v", err)
		}
	}

	// Create manifest files
	manifests := map[string]string{
		aDir:      `{"name": "plugin-a", "version": "1.0.0"}`,
		bDir:      `{"name": "plugin-b", "version": "1.0.0"}`,
		hiddenDir: `{"name": "hidden-plugin", "version": "1.0.0"}`,
	}

	for dir, content := range manifests {
		manifestPath := filepath.Join(dir, ".codex-plugin", "plugin.json")
		if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}
	}

	roots := FindPluginRoots(tmpDir)

	// Should find a and b, but not .hidden
	if len(roots) != 2 {
		t.Errorf("expected 2 plugin roots, got %d: %v", len(roots), roots)
	}

	// Check that both a and b are found
	found := make(map[string]bool)
	for _, r := range roots {
		found[r] = true
	}

	if !found[aDir] {
		t.Errorf("expected to find plugin a at %s", aDir)
	}
	if !found[bDir] {
		t.Errorf("expected to find plugin b at %s", bDir)
	}
	if found[hiddenDir] {
		t.Errorf("expected NOT to find hidden plugin at %s", hiddenDir)
	}
}

func TestLoadedPlugin_Validate_Valid(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     &PluginManifest{Name: "test-plugin"},
		ManifestPath: "/tmp/plugin/.codex-plugin/plugin.json",
	}

	if err := p.Validate(); err != nil {
		t.Errorf("expected no error for valid plugin, got %v", err)
	}
}

func TestLoadedPlugin_Validate_NilManifest(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     nil,
		ManifestPath: "/tmp/plugin/.codex-plugin/plugin.json",
	}

	if err := p.Validate(); err == nil {
		t.Error("expected error for nil manifest, got nil")
	}
}

func TestLoadedPlugin_Validate_EmptyName(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     &PluginManifest{Name: ""},
		ManifestPath: "/tmp/plugin/.codex-plugin/plugin.json",
	}

	if err := p.Validate(); err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestLoadedPlugin_Validate_InvalidSkillsPath(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     &PluginManifest{Name: "test", Skills: "absolute/path"},
		ManifestPath: "/tmp/plugin/.codex-plugin/plugin.json",
	}

	if err := p.Validate(); err == nil {
		t.Error("expected error for invalid skills path, got nil")
	}
}

func TestLoadedPlugin_Validate_ValidPaths(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     &PluginManifest{Name: "test", Skills: "./skills/", MCPServers: "./.mcp.json", Hooks: "./hooks.json", Apps: "./.app.json"},
		ManifestPath: "/tmp/plugin/.codex-plugin/plugin.json",
	}

	if err := p.Validate(); err != nil {
		t.Errorf("expected no error for valid paths, got %v", err)
	}
}

func TestLoadedPlugin_Validate_ValidInterfaceURLs(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: &PluginManifest{
			Name: "test",
			Interface: &PluginManifestInterface{
				WebsiteURL:        "https://example.com",
				PrivacyPolicyURL:  "https://example.com/privacy",
				TermsOfServiceURL: "https://example.com/tos",
			},
		},
		ManifestPath: "/tmp/plugin/.codex-plugin/plugin.json",
	}

	if err := p.Validate(); err != nil {
		t.Errorf("expected no error for valid URLs, got %v", err)
	}
}

func TestLoadedPlugin_Validate_InvalidInterfaceURL(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: &PluginManifest{
			Name: "test",
			Interface: &PluginManifestInterface{
				WebsiteURL: "http://example.com", // Should be https://
			},
		},
		ManifestPath: "/tmp/plugin/.codex-plugin/plugin.json",
	}

	if err := p.Validate(); err == nil {
		t.Error("expected error for http:// URL, got nil")
	}
}
