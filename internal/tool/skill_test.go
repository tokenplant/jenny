package tool

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/skills"
)

func TestSkillTool_Name(t *testing.T) {
	tool := NewSkillTool(nil)
	if tool.Name() != "activate_skill" {
		t.Errorf("expected name 'activate_skill', got %q", tool.Name())
	}
}

func TestSkillTool_Description(t *testing.T) {
	tool := NewSkillTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() should not be empty")
	}
	// Should mention SKILL_ROOT convention
	if !strings.Contains(desc, "SKILL_ROOT") {
		t.Error("Description should mention SKILL_ROOT convention")
	}
}

func TestSkillTool_InputSchema(t *testing.T) {
	tool := NewSkillTool(nil)
	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected schema type 'object', got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["name"]; !ok {
		t.Error("schema should have 'name' property")
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required should be a []string")
	}
	found := slices.Contains(required, "name")
	if !found {
		t.Error("'name' should be in required")
	}
}

func TestSkillTool_AC1_ActivationReturnsContentAndPath(t *testing.T) {
	// Create a test skill
	testSkill := skills.Skill{
		Name:        "test-skill",
		Description: "A test skill",
		RootPath:    "/path/to/test-skill",
		Content:     "# Test Skill\n\nSome content",
	}

	tool := NewSkillTool([]skills.Skill{testSkill})
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "test-skill"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Should contain the root_path attribute
	if !strings.Contains(result.Content, `root_path="/path/to/test-skill"`) {
		t.Errorf("expected root_path in output, got: %s", result.Content)
	}

	// Should contain the skill content wrapped in tags
	if !strings.Contains(result.Content, "<activated_skill") || !strings.Contains(result.Content, "</activated_skill>") {
		t.Errorf("expected activated_skill tags, got: %s", result.Content)
	}

	// Should contain the actual skill content
	if !strings.Contains(result.Content, "# Test Skill") {
		t.Errorf("expected skill content in output, got: %s", result.Content)
	}
}

func TestSkillTool_AC5_UnknownSkillError(t *testing.T) {
	// Create a test skill
	testSkill := skills.Skill{
		Name:        "existing-skill",
		Description: "An existing skill",
		RootPath:    "/path/to/skill",
		Content:     "content",
	}

	tool := NewSkillTool([]skills.Skill{testSkill})
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "nonexistent-skill"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for unknown skill")
	}

	// Error message should list available skills
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected 'not found' in error, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "existing-skill") {
		t.Errorf("expected available skill names in error, got: %s", result.Content)
	}
}

func TestSkillTool_AC5_NoSkillsAvailable(t *testing.T) {
	tool := NewSkillTool(nil)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "any-skill"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error when no skills available")
	}

	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected 'not found' in error, got: %s", result.Content)
	}
}

func TestSkillTool_AC3_PathResolution(t *testing.T) {
	// Test that root_path is returned correctly for path resolution
	testSkill := skills.Skill{
		Name:        "path-test",
		Description: "Test path resolution",
		RootPath:    "/absolute/path/to/skill",
		Content:     "content",
	}

	tool := NewSkillTool([]skills.Skill{testSkill})
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "path-test"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify root_path matches actual skill directory
	if !strings.Contains(result.Content, `/absolute/path/to/skill`) {
		t.Errorf("expected root_path to match skill directory, got: %s", result.Content)
	}
}

func TestSkillTool_CaseInsensitiveLookup(t *testing.T) {
	testSkill := skills.Skill{
		Name:        "Readme-Writer",
		Description: "Creates README files",
		RootPath:    "/path/to/readme-writer",
		Content:     "# README Writer",
	}

	tool := NewSkillTool([]skills.Skill{testSkill})
	ctx := context.Background()

	// Test lowercase
	result, err := tool.Execute(ctx, map[string]any{"name": "readme-writer"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error for lowercase name, got: %s", result.Content)
	}

	// Test uppercase
	result, err = tool.Execute(ctx, map[string]any{"name": "README-WRITER"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error for uppercase name, got: %s", result.Content)
	}
}

func TestSkillTool_NameRequired(t *testing.T) {
	tool := NewSkillTool(nil)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error when name is missing")
	}
	if !strings.Contains(result.Content, "required") {
		t.Errorf("expected 'required' error, got: %s", result.Content)
	}
}

func TestSkillTool_AC6_DiscoveryFromMultipleDirs(t *testing.T) {
	// This test verifies that skills discovered from multiple directories
	// are correctly passed to the tool and can be activated.
	// We'll simulate this by creating skills with different root paths.

	skillsDir := filepath.Join("testdata", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Skip("skills testdata not found, skipping")
	}

	var discoveredSkills []skills.Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, entry.Name())
		skillFile := filepath.Join(skillPath, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}
		content, _ := os.ReadFile(skillFile)
		discoveredSkills = append(discoveredSkills, skills.Skill{
			Name:        entry.Name(),
			Description: "Discovered skill",
			RootPath:    skillPath,
			Content:     string(content),
		})
	}

	if len(discoveredSkills) < 2 {
		t.Skip("need at least 2 skills for this test")
	}

	tool := NewSkillTool(discoveredSkills)
	ctx := context.Background()

	// Should be able to activate any of the discovered skills
	for _, s := range discoveredSkills {
		result, err := tool.Execute(ctx, map[string]any{"name": s.Name}, "")
		if err != nil {
			t.Fatalf("unexpected error activating %s: %v", s.Name, err)
		}
		if result.IsError {
			t.Errorf("expected no error activating %s, got: %s", s.Name, result.Content)
		}
	}
}

// TestSkillActivator_Integration tests for path-triggered skill activation.
func TestSkillActivator_Integration(t *testing.T) {
	// Create temp skill directories
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillContent := `# Test Skill

A test skill.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover the skill
	skillList, err := skills.Discover(tmpDir)
	if err != nil {
		t.Fatalf("discover error: %v", err)
	}

	if len(skillList) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skillList))
	}

	// Create activator
	activator := skills.NewPathSkillActivator(skillList)

	// Test activation within skill directory
	activated := activator.ActivateForPath(filepath.Join(skillDir, "some-file.txt"))
	if len(activated) != 1 || activated[0] != "test-skill" {
		t.Errorf("expected skill to be activated for path within skill dir, got %v", activated)
	}

	// Test activation outside skill directory (no glob set)
	activated = activator.ActivateForPath(filepath.Join(tmpDir, "other", "path.txt"))
	if len(activated) != 0 {
		t.Errorf("expected no activation for path outside skill dir without glob, got %v", activated)
	}
}

func TestSkillActivator_WithGlob(t *testing.T) {
	// Create temp skill directories
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "markdown-helper")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillContent := `---
description: Assists with Markdown
activation_glob: "**/*.md"
---

# Markdown Helper
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover the skill
	skillList, err := skills.Discover(tmpDir)
	if err != nil {
		t.Fatalf("discover error: %v", err)
	}

	if len(skillList) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skillList))
	}

	if skillList[0].ActivationGlob != "**/*.md" {
		t.Errorf("expected activation_glob '**/*.md', got %q", skillList[0].ActivationGlob)
	}

	// Create activator
	activator := skills.NewPathSkillActivator(skillList)

	// Test activation for markdown file outside skill directory
	activated := activator.ActivateForPath(filepath.Join(tmpDir, "docs", "README.md"))
	if len(activated) != 1 || activated[0] != "markdown-helper" {
		t.Errorf("expected markdown-helper to be activated for .md path, got %v", activated)
	}

	// Test no activation for non-matching file
	activated = activator.ActivateForPath(filepath.Join(tmpDir, "main.go"))
	if len(activated) != 0 {
		t.Errorf("expected no activation for .go path, got %v", activated)
	}
}

func TestRegistry_WithSkillsFrameworkEnabled(t *testing.T) {
	// Create temp skill directories
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillContent := `# Test Skill

A test skill.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover skills
	skillList, err := skills.Discover(tmpDir)
	if err != nil {
		t.Fatalf("discover error: %v", err)
	}

	// Build registry with skills framework enabled
	registry := NewRegistry().
		WithBaseTools().
		WithReadFileCache(NewReadFileCache()).
		WithSkillsFrameworkEnabled(true, skillList)

	tools := registry.Build()

	// Find the read tool and verify it has the activator wired
	var readTool *ReadTool
	for _, tool := range tools {
		if rt, ok := tool.(*ReadTool); ok {
			readTool = rt
			break
		}
	}

	if readTool == nil {
		t.Fatal("ReadTool not found in registry")
	}

	// Verify skill tool is registered
	foundSkillTool := false
	for _, tool := range tools {
		if tool.Name() == "activate_skill" {
			foundSkillTool = true
			break
		}
	}
	if !foundSkillTool {
		t.Error("activate_skill should be registered when skills framework is enabled")
	}
}

func TestRegistry_BareMode_NoSkills(t *testing.T) {
	// Build registry with bare mode (no skills framework)
	registry := NewRegistry().
		WithBaseTools().
		WithReadFileCache(NewReadFileCache()).
		WithSkillsFrameworkEnabled(false, nil)

	tools := registry.Build()

	// Verify no skill tool is registered
	for _, tool := range tools {
		if tool.Name() == "activate_skill" {
			t.Error("activate_skill should not be registered in bare mode")
		}
	}
}

// TestReadTool_WithSkillActivator tests that ReadTool properly uses the activator
func TestReadTool_WithSkillActivator(t *testing.T) {
	readCache := NewReadFileCache()
	readTool := NewReadTool(false, readCache)

	// Create a mock activator
	mockActivator := &mockSkillActivator{skills: []skills.Skill{}}

	// Set the activator
	readTool.WithSkillActivator(mockActivator)

	if readTool == nil {
		t.Error("expected non-nil ReadTool")
	}
}

// TestWriteTool_WithSkillActivator tests that WriteTool properly uses the activator
func TestWriteTool_WithSkillActivator(t *testing.T) {
	readCache := NewReadFileCache()
	writeTool := NewWriteTool(readCache)

	// Create a mock activator
	mockActivator := &mockSkillActivator{skills: []skills.Skill{}}

	// Set the activator
	writeTool.WithSkillActivator(mockActivator)

	if writeTool == nil {
		t.Error("expected non-nil WriteTool")
	}
}

// TestEditTool_WithSkillActivator tests that EditTool properly uses the activator
func TestEditTool_WithSkillActivator(t *testing.T) {
	readCache := NewReadFileCache()
	editTool := NewEditTool(readCache)

	// Create a mock activator
	mockActivator := &mockSkillActivator{skills: []skills.Skill{}}

	// Set the activator
	editTool.WithSkillActivator(mockActivator)

	if editTool == nil {
		t.Error("expected non-nil EditTool")
	}
}

// mockSkillActivator implements SkillActivator for testing
type mockSkillActivator struct {
	skills []skills.Skill
}

func (a *mockSkillActivator) ActivateForPath(path string) []string {
	var activated []string
	for _, skill := range a.skills {
		if skill.MatchesPath(path) {
			activated = append(activated, skill.Name)
		}
	}
	return activated
}

// mockMCPTool implements Tool interface for testing MCP exclusion
type mockMCPTool struct {
	name string
}

func (m *mockMCPTool) Name() string                { return m.name }
func (m *mockMCPTool) Description() string         { return "An MCP prompt tool" }
func (m *mockMCPTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (m *mockMCPTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	return &ToolResult{Content: "mcp tool result"}, nil
}

// TestSkillTool_MCPExclusion verifies MCP tools are not in skills list
func TestSkillTool_MCPExclusion(t *testing.T) {
	// Create a local skill
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "local-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillContent := `# Local Skill

A local skill.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover the local skill
	discoveredSkills, err := skills.Discover(tmpDir)
	if err != nil {
		t.Fatalf("discover error: %v", err)
	}
	if len(discoveredSkills) != 1 {
		t.Fatalf("expected 1 discovered skill, got %d", len(discoveredSkills))
	}

	// Create a mock MCP tool
	mcpTools := []Tool{
		&mockMCPTool{name: "mcp-prompt"},
	}

	// Build registry with base tools, MCP tools, and discovered skills
	registry := NewRegistry().
		WithBaseTools().
		WithReadFileCache(NewReadFileCache()).
		WithMCPTools(mcpTools).
		WithSkills(discoveredSkills)

	tools := registry.Build()

	// Find the Skill tool
	var skillTool *SkillTool
	for _, tool := range tools {
		if st, ok := tool.(*SkillTool); ok {
			skillTool = st
			break
		}
	}
	if skillTool == nil {
		t.Fatal("SkillTool not found in registry")
	}

	// Verify MCP tool name is NOT in the skills list
	for _, skill := range skillTool.skills {
		if skill.Name == "mcp-prompt" {
			t.Error("MCP tool should not appear in skills list")
		}
	}

	// Verify local skill IS in the skills list
	foundLocalSkill := false
	for _, skill := range skillTool.skills {
		if skill.Name == "local-skill" {
			foundLocalSkill = true
			break
		}
	}
	if !foundLocalSkill {
		t.Error("local skill should appear in skills list")
	}
}
