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

func TestLoadedPlugin_SkillsDir_WithSkills(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: &PluginManifest{Name: "test", Skills: "./myskills/"},
	}

	expected := filepath.Join("/tmp/plugin", "./myskills/")
	if got := p.SkillsDir(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestLoadedPlugin_SkillsDir_WithoutSkills(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: &PluginManifest{Name: "test"},
	}

	if got := p.SkillsDir(); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestLoadedPlugin_SkillsDir_NilManifest(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: nil,
	}

	if got := p.SkillsDir(); got != "" {
		t.Errorf("expected empty string for nil manifest, got %q", got)
	}
}

func TestLoadPluginSkills_ValidSkillsDir(t *testing.T) {
	// Create a temp directory with a plugin structure
	tmpDir := t.TempDir()

	// Create .codex-plugin directory and manifest
	pluginDir := filepath.Join(tmpDir, ".codex-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	manifest := `{"name": "test-plugin", "skills": "./skills/"}`
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// Create skills directory with a skill subdirectory containing SKILL.md
	skillsDir := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}

	// Create a skill subdirectory (skills are in subdirectories)
	testSkillDir := filepath.Join(skillsDir, "test-skill")
	if err := os.MkdirAll(testSkillDir, 0755); err != nil {
		t.Fatalf("failed to create test skill dir: %v", err)
	}

	skillContent := `---
description: A test skill for plugin integration
---

# Test Skill

This skill is loaded from a plugin.
`
	skillPath := filepath.Join(testSkillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Load the plugin manifest
	loadedManifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}

	p := &LoadedPlugin{
		RootPath:     tmpDir,
		Manifest:     loadedManifest,
		ManifestPath: manifestPath,
	}

	// Load plugin skills
	skills, err := LoadPluginSkills(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	if skills[0].Name != "test-skill" {
		t.Errorf("expected skill name 'test-skill', got %q", skills[0].Name)
	}

	if skills[0].Content == "" {
		t.Error("expected non-empty skill content")
	}
}

func TestLoadPluginSkills_NoSkillsPath(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: &PluginManifest{Name: "test-plugin"},
	}

	skills, err := LoadPluginSkills(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if skills != nil {
		t.Errorf("expected nil skills, got %v", skills)
	}
}

func TestLoadPluginSkills_NonExistentSkillsDir(t *testing.T) {
	// Create a temp directory with a plugin that points to non-existent skills dir
	tmpDir := t.TempDir()

	pluginDir := filepath.Join(tmpDir, ".codex-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	manifest := `{"name": "test-plugin", "skills": "./nonexistent/"}`
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	loadedManifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}

	p := &LoadedPlugin{
		RootPath:     tmpDir,
		Manifest:     loadedManifest,
		ManifestPath: manifestPath,
	}

	// Load plugin skills - should return error for non-existent dir
	_, err = LoadPluginSkills(p)
	if err == nil {
		t.Error("expected error for non-existent skills directory, got nil")
	}
}

func TestLoadPluginSkills_SkillsPathIsFile(t *testing.T) {
	// Create a temp directory with a plugin that has skills as a file instead of dir
	tmpDir := t.TempDir()

	pluginDir := filepath.Join(tmpDir, ".codex-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	manifest := `{"name": "test-plugin", "skills": "./skills/"}`
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// Create skills as a file (not a directory)
	skillsPath := filepath.Join(tmpDir, "skills")
	if err := os.WriteFile(skillsPath, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to create skills file: %v", err)
	}

	loadedManifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}

	p := &LoadedPlugin{
		RootPath:     tmpDir,
		Manifest:     loadedManifest,
		ManifestPath: manifestPath,
	}

	// Load plugin skills - should return error for non-directory path
	_, err = LoadPluginSkills(p)
	if err == nil {
		t.Error("expected error for skills path that is a file, got nil")
	}
}
