package skills

import (
	"os"
	"path/filepath"
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
			if s.RootPath != expected {
				t.Errorf("expected root path %q, got %q", expected, s.RootPath)
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
	if !contains(manifest, "skill1") || !contains(manifest, "Description one") {
		t.Error("manifest should contain skill name and description")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
