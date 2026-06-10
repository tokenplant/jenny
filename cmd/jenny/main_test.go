package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/internal/agent"
	"github.com/ipy/jenny/internal/plugin"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/skills"
)

// TestResume_QueueOnlyTranscript_Error tests AC1: queue-only transcript rejected on -r
func TestResume_QueueOnlyTranscript_Error(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_queue_only"

	// Append only progress-type entries (no chain participants)
	entries := []session.TranscriptEntry{
		{Type: "progress", Content: "Thinking..."},
		{Type: "bash_progress", Content: "Running command"},
	}
	for _, e := range entries {
		if err := mgr.AppendEntry(sessionID, e); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Load transcript and verify HasChainMessages returns false
	loaded, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	// These entries are filtered by LoadTranscript (progress types are excluded),
	// so loaded should be empty
	if len(loaded) != 0 {
		t.Errorf("LoadTranscript() returned %d entries, want 0 (progress filtered)", len(loaded))
	}

	// AC1: HasChainMessages returns false for queue-only transcript
	if agent.HasChainMessages(loaded) {
		t.Errorf("HasChainMessages(loaded) = true, want false (queue-only)")
	}
}

// TestResume_EmptyTranscript_Error tests AC2: empty transcript file rejected on -r
func TestResume_EmptyTranscript_Error(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_empty"

	// Create an empty transcript file
	path := filepath.Join(tmpDir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load transcript - should return empty entries
	loaded, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 0 {
		t.Errorf("LoadTranscript() returned %d entries, want 0", len(loaded))
	}

	// AC2: HasChainMessages returns false for empty transcript
	if agent.HasChainMessages(loaded) {
		t.Errorf("HasChainMessages(loaded) = true, want false (empty)")
	}
}

// TestResume_NormalTranscript_NoError tests AC3: normal transcript with user entry works
func TestResume_NormalTranscript_NoError(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_normal"

	// Append a user message (chain participant)
	entries := []session.TranscriptEntry{
		{Type: "user", Content: "Hello"},
	}
	for _, e := range entries {
		if err := mgr.AppendEntry(sessionID, e); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Load transcript
	loaded, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Errorf("LoadTranscript() returned %d entries, want 1", len(loaded))
	}

	if loaded[0].Type != "user" || loaded[0].Content != "Hello" {
		t.Errorf("loaded entry = %+v, want {Type:user, Content:Hello}", loaded[0])
	}

	// AC3: HasChainMessages returns true for normal transcript with user entry
	if !agent.HasChainMessages(loaded) {
		t.Errorf("HasChainMessages(loaded) = false, want true (normal)")
	}
}

// TestResume_ForkSession_NoFileCreated tests AC4: --fork-session with queue-only
// session does not create a new transcript file
func TestResume_ForkSession_NoFileCreated(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_queue_only_fork"

	// Append only progress-type entries
	entries := []session.TranscriptEntry{
		{Type: "progress", Content: "Thinking..."},
	}
	for _, e := range entries {
		if err := mgr.AppendEntry(sessionID, e); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Load transcript
	loaded, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	// Progress types are filtered, so loaded is empty
	if len(loaded) != 0 {
		t.Errorf("LoadTranscript() returned %d entries, want 0", len(loaded))
	}

	// AC4a: HasChainMessages returns false for queue-only transcript
	if agent.HasChainMessages(loaded) {
		t.Errorf("HasChainMessages(loaded) = true, want false (queue-only)")
	}

	// AC4b: Verify no fork transcript file is created (tmpDir has exactly one .jsonl)
	dirEntries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	var jsonlFiles []string
	for _, de := range dirEntries {
		if !de.IsDir() && filepath.Ext(de.Name()) == ".jsonl" {
			jsonlFiles = append(jsonlFiles, de.Name())
		}
	}
	if len(jsonlFiles) != 1 {
		t.Errorf("ReadDir tmpDir returned %d .jsonl files, want 1 (no fork created)", len(jsonlFiles))
	}
	if len(jsonlFiles) == 1 && jsonlFiles[0] != sessionID+".jsonl" {
		t.Errorf("jsonl file = %q, want %q", jsonlFiles[0], sessionID+".jsonl")
	}
}

// TestResume_NormalTranscript_ForkSession_CreatesFile tests AC5: --fork-session with
// normal transcript (has chain participants) creates a new transcript file
func TestResume_NormalTranscript_ForkSession_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_normal_fork"

	// Append a user message (chain participant)
	entries := []session.TranscriptEntry{
		{Type: "user", Content: "Hello"},
	}
	for _, e := range entries {
		if err := mgr.AppendEntry(sessionID, e); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Load transcript
	loaded, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Errorf("LoadTranscript() returned %d entries, want 1", len(loaded))
	}

	// AC5: HasChainMessages returns true for normal transcript
	if !agent.HasChainMessages(loaded) {
		t.Errorf("HasChainMessages(loaded) = false, want true (normal)")
	}

	// Simulate fork: generate new session ID and append entries to it
	newSessionID, err := session.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID() error = %v", err)
	}
	for _, e := range loaded {
		if err := mgr.AppendEntry(newSessionID, e); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// AC5: Verify fork transcript file was created (tmpDir has exactly two .jsonl files)
	dirEntries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	var jsonlFiles []string
	for _, de := range dirEntries {
		if !de.IsDir() && filepath.Ext(de.Name()) == ".jsonl" {
			jsonlFiles = append(jsonlFiles, de.Name())
		}
	}
	if len(jsonlFiles) != 2 {
		t.Errorf("ReadDir tmpDir returned %d .jsonl files, want 2 (original + fork)", len(jsonlFiles))
	}
}

// TestPluginSkillsWiring tests AC7: plugin skills are discoverable at runtime
// and merged with project skills.
func TestPluginSkillsWiring(t *testing.T) {
	tmpDir := t.TempDir()

	// Create project skill
	projectSkillDir := filepath.Join(tmpDir, ".jenny", "skills", "project-skill")
	if err := os.MkdirAll(projectSkillDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	projectSkillContent := `# Project Skill

A project-level skill.
`
	if err := os.WriteFile(filepath.Join(projectSkillDir, "SKILL.md"), []byte(projectSkillContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Create plugin with skills
	pluginRoot := filepath.Join(tmpDir, "my-plugin")
	if err := os.MkdirAll(pluginRoot, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create plugin manifest
	manifestContent := `{
  "name": "my-plugin",
  "skills": "./myskills/"
}`
	manifestDir := filepath.Join(pluginRoot, ".codex-plugin")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "plugin.json"), []byte(manifestContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Create plugin skills directory
	pluginSkillDir := filepath.Join(pluginRoot, "myskills", "plugin-skill")
	if err := os.MkdirAll(pluginSkillDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	pluginSkillContent := `# Plugin Skill

A plugin-level skill.
`
	if err := os.WriteFile(filepath.Join(pluginSkillDir, "SKILL.md"), []byte(pluginSkillContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Discover project skills
	projectSkillsDir := filepath.Join(tmpDir, ".jenny", "skills")
	discoveredSkills, err := skills.Discover(projectSkillsDir)
	if err != nil {
		t.Fatalf("skills.Discover() error = %v", err)
	}

	// Discover plugins
	pluginRoots := plugin.FindPluginRoots(tmpDir)

	// Load plugin skills and merge
	for _, pluginRoot := range pluginRoots {
		manifestPath := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
		manifest, err := plugin.LoadManifest(manifestPath)
		if err != nil {
			continue
		}

		loadedPlugin := &plugin.LoadedPlugin{
			RootPath:     pluginRoot,
			Manifest:     manifest,
			ManifestPath: manifestPath,
		}

		if err := loadedPlugin.Validate(); err != nil {
			continue
		}

		pluginSkills, err := plugin.LoadPluginSkills(loadedPlugin)
		if err != nil {
			continue
		}

		for _, ps := range pluginSkills {
			// Skip if a skill with the same normalized name already exists
			if skills.FindSkillByName(discoveredSkills, ps.Name) != nil {
				continue
			}
			discoveredSkills = append(discoveredSkills, ps)
		}
	}

	// Verify merged list contains both project-skill and plugin-skill
	if len(discoveredSkills) != 2 {
		t.Errorf("expected 2 skills (project-skill + plugin-skill), got %d", len(discoveredSkills))
	}

	hasProjectSkill := false
	hasPluginSkill := false
	for _, s := range discoveredSkills {
		if s.Name == "project-skill" {
			hasProjectSkill = true
		}
		if s.Name == "plugin-skill" {
			hasPluginSkill = true
		}
	}
	if !hasProjectSkill {
		t.Error("expected project-skill to be in merged list")
	}
	if !hasPluginSkill {
		t.Error("expected plugin-skill to be in merged list")
	}
}

// TestPluginSkillsDedup tests AC3: plugin skills with duplicate names are skipped.
func TestPluginSkillsDedup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create project skill
	projectSkillDir := filepath.Join(tmpDir, ".jenny", "skills", "shared-skill")
	if err := os.MkdirAll(projectSkillDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	projectSkillContent := `# Shared Skill

A project-level shared skill.
`
	if err := os.WriteFile(filepath.Join(projectSkillDir, "SKILL.md"), []byte(projectSkillContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Create plugin with same-named skill
	pluginRoot := filepath.Join(tmpDir, "my-plugin")
	if err := os.MkdirAll(pluginRoot, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create plugin manifest
	manifestContent := `{
  "name": "my-plugin",
  "skills": "./myskills/"
}`
	manifestDir := filepath.Join(pluginRoot, ".codex-plugin")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "plugin.json"), []byte(manifestContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Create plugin skill with same name (different casing to test case-insensitive dedup)
	pluginSkillDir := filepath.Join(pluginRoot, "myskills", "SHARED-SKILL")
	if err := os.MkdirAll(pluginSkillDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	pluginSkillContent := `# SHARED-SKILL

A plugin-level shared skill.
`
	if err := os.WriteFile(filepath.Join(pluginSkillDir, "SKILL.md"), []byte(pluginSkillContent), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Discover project skills
	projectSkillsDir := filepath.Join(tmpDir, ".jenny", "skills")
	discoveredSkills, err := skills.Discover(projectSkillsDir)
	if err != nil {
		t.Fatalf("skills.Discover() error = %v", err)
	}

	// Discover plugins
	pluginRoots := plugin.FindPluginRoots(tmpDir)

	// Load plugin skills and merge
	for _, pluginRoot := range pluginRoots {
		manifestPath := filepath.Join(pluginRoot, ".codex-plugin", "plugin.json")
		manifest, err := plugin.LoadManifest(manifestPath)
		if err != nil {
			continue
		}

		loadedPlugin := &plugin.LoadedPlugin{
			RootPath:     pluginRoot,
			Manifest:     manifest,
			ManifestPath: manifestPath,
		}

		if err := loadedPlugin.Validate(); err != nil {
			continue
		}

		pluginSkills, err := plugin.LoadPluginSkills(loadedPlugin)
		if err != nil {
			continue
		}

		for _, ps := range pluginSkills {
			// Skip if a skill with the same normalized name already exists
			if skills.FindSkillByName(discoveredSkills, ps.Name) != nil {
				continue
			}
			discoveredSkills = append(discoveredSkills, ps)
		}
	}

	// Verify only 1 skill (project skill takes priority)
	if len(discoveredSkills) != 1 {
		t.Errorf("expected 1 skill (project shared-skill takes priority), got %d", len(discoveredSkills))
	}

	if discoveredSkills[0].Name != "shared-skill" {
		t.Errorf("expected skill name 'shared-skill', got %q", discoveredSkills[0].Name)
	}
}
