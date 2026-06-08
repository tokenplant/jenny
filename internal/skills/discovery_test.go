package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscover_SingleDirectory(t *testing.T) {
	// Test that Discover finds skills in a single directory
	dir := filepath.Join("testdata", "skills")
	skills, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find readme-writer and deploy-helper (empty-skill has no SKILL.md)
	if len(skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(skills))
	}
}

func TestDiscover_MultipleDirectories(t *testing.T) {
	// Create a second directory with a skill
	tmpDir := t.TempDir()
	skill2Dir := filepath.Join(tmpDir, "skill2")
	if err := os.MkdirAll(skill2Dir, 0755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	skill2Content := `# Skill Two

A test skill.
`
	if err := os.WriteFile(filepath.Join(skill2Dir, "SKILL.md"), []byte(skill2Content), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Discover from both directories
	dir1 := filepath.Join("testdata", "skills")
	skills, err := Discover(dir1, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find 3 skills total
	if len(skills) != 3 {
		t.Errorf("expected 3 skills, got %d", len(skills))
	}
}

func TestDiscover_SkipsDirectoriesWithoutSKILLMD(t *testing.T) {
	// empty-skill directory has no SKILL.md - should be silently skipped
	dir := filepath.Join("testdata", "skills")
	skills, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, s := range skills {
		if s.Name == "empty-skill" {
			t.Error("empty-skill should have been skipped")
		}
	}
}

func TestDiscover_NonExistentDirectory(t *testing.T) {
	// Non-existent directory should be silently skipped
	skills, err := Discover("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills for non-existent dir, got %d", len(skills))
	}
}

func TestDiscover_ExtractsDescriptionFromFrontmatter(t *testing.T) {
	dir := filepath.Join("testdata", "skills")
	skills, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// deploy-helper has frontmatter with description
	for _, s := range skills {
		if s.Name == "deploy-helper" {
			if s.Description != "Assists with CI/CD deployment workflows" {
				t.Errorf("expected frontmatter description, got %q", s.Description)
			}
			return
		}
	}
	t.Error("deploy-helper skill not found")
}

func TestDiscover_ExtractsDescriptionFromFirstLine(t *testing.T) {
	dir := filepath.Join("testdata", "skills")
	skills, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// readme-writer uses first line as description (no frontmatter)
	for _, s := range skills {
		if s.Name == "readme-writer" {
			if s.Description == "" {
				t.Error("expected non-empty description")
			}
			return
		}
	}
	t.Error("readme-writer skill not found")
}

func TestDiscover_ReturnsContent(t *testing.T) {
	dir := filepath.Join("testdata", "skills")
	skills, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, s := range skills {
		if s.Name == "readme-writer" {
			if s.Content == "" {
				t.Error("expected non-empty content")
			}
			if s.RootPath == "" {
				t.Error("expected non-empty root path")
			}
			return
		}
	}
	t.Error("readme-writer skill not found")
}

func TestDiscover_SetsRootPath(t *testing.T) {
	dir := filepath.Join("testdata", "skills")
	skills, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, s := range skills {
		if s.Name == "readme-writer" {
			expected := filepath.Join(dir, "readme-writer")
			absExpected, err := filepath.Abs(expected)
			if err != nil {
				t.Fatalf("failed to get absolute path: %v", err)
			}
			if s.RootPath != absExpected {
				t.Errorf("expected root path %q, got %q", absExpected, s.RootPath)
			}
			return
		}
	}
	t.Error("readme-writer skill not found")
}

func TestFindSkillByName_CaseInsensitive(t *testing.T) {
	testSkills := []Skill{
		{Name: "Readme-Writer", Description: "desc", RootPath: "/path", Content: "content"},
	}

	found := FindSkillByName(testSkills, "readme-writer")
	if found == nil {
		t.Error("expected to find skill case-insensitively")
	}

	found = FindSkillByName(testSkills, "README-WRITER")
	if found == nil {
		t.Error("expected to find skill uppercase")
	}
}

func TestFindSkillByName_NotFound(t *testing.T) {
	testSkills := []Skill{
		{Name: "Readme-Writer", Description: "desc", RootPath: "/path", Content: "content"},
	}

	found := FindSkillByName(testSkills, "nonexistent")
	if found != nil {
		t.Error("expected nil for non-existent skill")
	}
}

func TestSkillsManifest_Empty(t *testing.T) {
	manifest := SkillsManifest([]Skill{})
	if manifest != "" {
		t.Error("expected empty manifest for empty skills")
	}
}

func TestSkillsManifest_NonEmpty(t *testing.T) {
	testSkills := []Skill{
		{Name: "skill1", Description: "Description one"},
		{Name: "skill2", Description: "Description two"},
	}

	manifest := SkillsManifest(testSkills)
	if manifest == "" {
		t.Error("expected non-empty manifest")
	}
	// Should contain both skill names and descriptions
	if !strings.Contains(manifest, "skill1") || !strings.Contains(manifest, "Description one") {
		t.Error("manifest should contain skill name and description")
	}
}

func TestDiscover_DeduplicatesSkills(t *testing.T) {
	// Create a temp directory with a skill
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	skillContent := `# Test Skill

A test skill for deduplication.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Pass the same directory twice
	skills, err := Discover(tmpDir, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find only 1 skill (not 2 from passing same dir twice)
	if len(skills) != 1 {
		t.Errorf("expected 1 skill after dedup, got %d", len(skills))
	}
}

func TestSkill_MatchesPath_WithinRoot(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, ".jenny", "skills", "test-skill")
	skill := Skill{
		Name:     "test-skill",
		RootPath: skillRoot,
	}

	// Path within skill root should match
	if !skill.MatchesPath(filepath.Join(skillRoot, "SKILL.md")) {
		t.Error("expected path within skill root to match")
	}

	// Path outside skill root should not match
	if skill.MatchesPath(filepath.Join(tmpDir, "other", "path.go")) {
		t.Error("expected path outside skill root to not match")
	}
}

func TestSkill_MatchesPath_WithActivationGlob(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, ".jenny", "skills", "go-helper")
	skill := Skill{
		Name:           "go-helper",
		RootPath:       skillRoot,
		ActivationGlob: "**/*.go",
	}

	// Path matching the glob should match even if outside root
	if !skill.MatchesPath(filepath.Join(tmpDir, "other", "path.go")) {
		t.Error("expected path matching activation_glob to match")
	}

	// Path not matching the glob should not match
	if skill.MatchesPath(filepath.Join(tmpDir, "other", "path.md")) {
		t.Error("expected path not matching activation_glob to not match")
	}
}

func TestSkill_MatchesPath_NoActivationGlob(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, ".jenny", "skills", "no-glob-skill")
	skill := Skill{
		Name:     "no-glob-skill",
		RootPath: skillRoot,
		// No ActivationGlob set
	}

	// Path outside root should not match when no activation glob
	if skill.MatchesPath(filepath.Join(tmpDir, "other", "path.go")) {
		t.Error("expected path outside skill root to not match when no activation_glob")
	}
}

func TestDiscover_ExtractsActivationGlob(t *testing.T) {
	// Create a temp directory with a skill that has activation_glob in frontmatter
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "markdown-helper")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	skillContent := `---
description: Assists with Markdown editing
activation_glob: "**/*.md"
---

# Markdown Helper

This skill helps with Markdown files.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	skills, err := Discover(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	if skills[0].ActivationGlob != "**/*.md" {
		t.Errorf("expected activation_glob '**/*.md', got %q", skills[0].ActivationGlob)
	}
}

func TestParseSkillMetadata_WithActivationGlob(t *testing.T) {
	content := []byte(`---
description: Test skill
activation_glob: "**/*.go"
---

# Test
`)
	description, activationGlob := parseSkillMetadata(content)
	if description != "Test skill" {
		t.Errorf("expected description 'Test skill', got %q", description)
	}
	if activationGlob != "**/*.go" {
		t.Errorf("expected activation_glob '**/*.go', got %q", activationGlob)
	}
}

func TestParseSkillMetadata_WithoutActivationGlob(t *testing.T) {
	content := []byte(`# Just a heading

Some content.
`)
	description, activationGlob := parseSkillMetadata(content)
	if description == "" {
		t.Error("expected non-empty description")
	}
	if activationGlob != "" {
		t.Errorf("expected empty activation_glob, got %q", activationGlob)
	}
}
