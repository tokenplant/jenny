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
	tmpDir := t.TempDir()

	aDir := filepath.Join(tmpDir, "a")
	bDir := filepath.Join(tmpDir, "b")
	hiddenDir := filepath.Join(tmpDir, ".hidden")

	for _, dir := range []string{aDir, bDir, hiddenDir} {
		if err := os.MkdirAll(filepath.Join(dir, ".jenny-plugin"), 0755); err != nil {
			t.Fatalf("failed to create plugin dir: %v", err)
		}
	}

	manifests := map[string]string{
		aDir:      `{"name": "plugin-a", "version": "1.0.0"}`,
		bDir:      `{"name": "plugin-b", "version": "1.0.0"}`,
		hiddenDir: `{"name": "hidden-plugin", "version": "1.0.0"}`,
	}

	for dir, content := range manifests {
		manifestPath := filepath.Join(dir, ".jenny-plugin", "plugin.json")
		if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}
	}

	roots := FindPluginRoots(tmpDir)

	if len(roots) != 2 {
		t.Errorf("expected 2 plugin roots, got %d: %v", len(roots), roots)
	}

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

func TestFindPluginRoots_FallbackClaudePlugin(t *testing.T) {
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "proj")
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"fb"}`), 0644); err != nil {
		t.Fatal(err)
	}

	roots := FindPluginRoots(tmpDir)
	if len(roots) != 1 || roots[0] != dir {
		t.Errorf("expected .claude-plugin fallback to find %s, got %v", dir, roots)
	}
}

func TestFindPluginRoots_FallbackCodexPlugin(t *testing.T) {
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "proj")
	if err := os.MkdirAll(filepath.Join(dir, ".codex-plugin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".codex-plugin", "plugin.json"), []byte(`{"name":"fb"}`), 0644); err != nil {
		t.Fatal(err)
	}

	roots := FindPluginRoots(tmpDir)
	if len(roots) != 1 || roots[0] != dir {
		t.Errorf("expected .codex-plugin fallback to find %s, got %v", dir, roots)
	}
}

func TestFindPluginRoots_JennyPluginTakesPriority(t *testing.T) {
	tmpDir := t.TempDir()
	dir := filepath.Join(tmpDir, "proj")
	for _, marker := range []string{".jenny-plugin", ".claude-plugin", ".codex-plugin"} {
		if err := os.MkdirAll(filepath.Join(dir, marker), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, marker, "plugin.json"), []byte(`{"name":"multi"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	roots := FindPluginRoots(tmpDir)
	if len(roots) != 1 {
		t.Fatalf("expected exactly 1 root when multiple markers exist, got %d: %v", len(roots), roots)
	}
	if roots[0] != dir {
		t.Errorf("expected root %s, got %s", dir, roots[0])
	}
}

func TestLoadedPlugin_Validate_Valid(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     &PluginManifest{Name: "test-plugin"},
		ManifestPath: "/tmp/plugin/.jenny-plugin/plugin.json",
	}

	if err := p.Validate(); err != nil {
		t.Errorf("expected no error for valid plugin, got %v", err)
	}
}

func TestLoadedPlugin_Validate_NilManifest(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     nil,
		ManifestPath: "/tmp/plugin/.jenny-plugin/plugin.json",
	}

	if err := p.Validate(); err == nil {
		t.Error("expected error for nil manifest, got nil")
	}
}

func TestLoadedPlugin_Validate_EmptyName(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     &PluginManifest{Name: ""},
		ManifestPath: "/tmp/plugin/.jenny-plugin/plugin.json",
	}

	if err := p.Validate(); err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

func TestLoadedPlugin_Validate_InvalidSkillsPath(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     &PluginManifest{Name: "test", Skills: "absolute/path"},
		ManifestPath: "/tmp/plugin/.jenny-plugin/plugin.json",
	}

	if err := p.Validate(); err == nil {
		t.Error("expected error for invalid skills path, got nil")
	}
}

func TestLoadedPlugin_Validate_ValidPaths(t *testing.T) {
	p := &LoadedPlugin{
		RootPath:     "/tmp/plugin",
		Manifest:     &PluginManifest{Name: "test", Skills: "./skills/", MCPServers: "./.mcp.json", Hooks: "./hooks.json", Apps: "./.app.json"},
		ManifestPath: "/tmp/plugin/.jenny-plugin/plugin.json",
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
		ManifestPath: "/tmp/plugin/.jenny-plugin/plugin.json",
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
				WebsiteURL: "http://example.com",
			},
		},
		ManifestPath: "/tmp/plugin/.jenny-plugin/plugin.json",
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

	pluginDir := filepath.Join(tmpDir, ".jenny-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	manifest := `{"name": "test-plugin", "skills": "./skills/"}`
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	skillsDir := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}

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
	tmpDir := t.TempDir()

	pluginDir := filepath.Join(tmpDir, ".jenny-plugin")
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
	tmpDir := t.TempDir()

	pluginDir := filepath.Join(tmpDir, ".jenny-plugin")
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

func TestLoadedPlugin_MCPServersDir_WithServers(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: &PluginManifest{Name: "test", MCPServers: "./.mcp.json"},
	}

	expected := filepath.Join("/tmp/plugin", "./.mcp.json")
	if got := p.MCPServersDir(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestLoadedPlugin_MCPServersDir_WithoutServers(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: &PluginManifest{Name: "test"},
	}

	if got := p.MCPServersDir(); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestLoadedPlugin_MCPServersDir_NilManifest(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: nil,
	}

	if got := p.MCPServersDir(); got != "" {
		t.Errorf("expected empty string for nil manifest, got %q", got)
	}
}

func TestLoadPluginMCPServers_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()

	pluginDir := filepath.Join(tmpDir, ".jenny-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	manifest := `{"name": "test-plugin", "mcpServers": "./.mcp.json"}`
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// Create MCP config file
	mcpConfig := `{
		"mcpServers": {
			"test-server": {
				"command": "npx",
				"args": ["-y", "@test/mcp-server"]
			}
		}
	}`
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	if err := os.WriteFile(mcpPath, []byte(mcpConfig), 0644); err != nil {
		t.Fatalf("failed to write mcp config: %v", err)
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

	// Load plugin MCP servers
	serverDefs, err := LoadPluginMCPServers(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(serverDefs) != 1 {
		t.Fatalf("expected 1 server def, got %d", len(serverDefs))
	}

	def, ok := serverDefs["test-server"]
	if !ok {
		t.Fatal("expected 'test-server' in server defs")
	}

	if def.Command != "npx" {
		t.Errorf("expected command 'npx', got %q", def.Command)
	}

	if len(def.Args) != 2 || def.Args[0] != "-y" || def.Args[1] != "@test/mcp-server" {
		t.Errorf("unexpected args: %v", def.Args)
	}
}

func TestLoadPluginMCPServers_NoMCPServersPath(t *testing.T) {
	p := &LoadedPlugin{
		RootPath: "/tmp/plugin",
		Manifest: &PluginManifest{Name: "test-plugin"},
	}

	serverDefs, err := LoadPluginMCPServers(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if serverDefs != nil {
		t.Errorf("expected nil server defs, got %v", serverDefs)
	}
}

func TestLoadPluginMCPServers_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	pluginDir := filepath.Join(tmpDir, ".jenny-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	manifest := `{"name": "test-plugin", "mcpServers": "./.mcp.json"}`
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

	// Load plugin MCP servers - should return error for non-existent file
	_, err = LoadPluginMCPServers(p)
	if err == nil {
		t.Error("expected error for non-existent MCP config file, got nil")
	}
}

func TestLoadPluginMCPServers_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	pluginDir := filepath.Join(tmpDir, ".jenny-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	manifest := `{"name": "test-plugin", "mcpServers": "./.mcp.json"}`
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("failed to write manifest: %v", err)
	}

	// Create malformed MCP config file
	mcpPath := filepath.Join(tmpDir, ".mcp.json")
	if err := os.WriteFile(mcpPath, []byte("{ invalid json"), 0644); err != nil {
		t.Fatalf("failed to write malformed mcp config: %v", err)
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

	// Load plugin MCP servers - should return error for invalid JSON
	_, err = LoadPluginMCPServers(p)
	if err == nil {
		t.Error("expected error for malformed MCP config file, got nil")
	}
}
