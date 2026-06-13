// Package skills provides skill discovery and management.
package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a discovered skill with its metadata and content.
type Skill struct {
	Name           string
	Description    string
	RootPath       string
	Content        string
	ActivationGlob string // glob pattern for automatic activation (e.g., "**/*.go")
}

// MatchesPath checks if the given path matches this skill's activation criteria.
// Returns true if:
// - The path is within the skill's RootPath directory, OR
// - The skill has an ActivationGlob that matches the path via glob-style matching
func (s *Skill) MatchesPath(path string) bool {
	// Check if path is within the skill's root directory
	if strings.HasPrefix(path, s.RootPath+string(filepath.Separator)) || path == s.RootPath {
		return true
	}

	// Check activation glob if set
	if s.ActivationGlob == "" {
		return false
	}

	// Use glob-style pattern matching (supports **)
	return matchActivationGlob(filepath.ToSlash(path), filepath.ToSlash(s.ActivationGlob))
}

// matchActivationGlob matches a path against a glob pattern with ** support.
// Handles ** meaning "match any number of directories".
func matchActivationGlob(path, pattern string) bool {
	// Handle ** in pattern
	if strings.Contains(pattern, "**") {
		return matchDoubleStar(path, pattern)
	}
	// Simple glob match using filepath.Match
	matched, _ := filepath.Match(pattern, path)
	return matched
}

// matchDoubleStar handles ** in glob patterns.
func matchDoubleStar(path, pattern string) bool {
	// Handle leading **/
	if after, ok := strings.CutPrefix(pattern, "**/"); ok {
		remaining := after
		// Try matching from any position in the path
		for i := 0; i <= len(path); i++ {
			if i < len(path) && path[i] != '/' {
				continue
			}
			// Strip leading / from suffix
			suffix := strings.TrimPrefix(path[i:], "/")
			if suffix == "" {
				// For paths with no '/' (e.g., "main.go"), try matching the filename
				// This handles ** matching zero directories
				filename := path
				if idx := strings.LastIndex(filename, "/"); idx >= 0 {
					filename = filename[idx+1:]
				}
				matched, _ := filepath.Match(remaining, filename)
				if matched {
					return true
				}
				continue
			}
			if matchDoubleStar(suffix, remaining) {
				return true
			}
		}
		return false
	}

	// Handle trailing /**
	if before, ok := strings.CutSuffix(pattern, "/**"); ok {
		prefix := before
		return strings.HasPrefix(path, prefix)
	}

	// Handle ** in middle or end
	parts := strings.Split(pattern, "**")
	if len(parts) == 2 {
		prefix := parts[0]
		suffix := parts[1]

		// Prefix must match (if non-empty)
		if prefix != "" && !strings.HasPrefix(path, prefix) {
			return false
		}

		// Suffix must match at the end
		if suffix != "" {
			suffix = strings.TrimPrefix(suffix, "/")
			// For suffix matching, use glob matching on the filename
			filename := path
			if idx := strings.LastIndex(filename, "/"); idx >= 0 {
				filename = filename[idx+1:]
			}
			matched, _ := filepath.Match(suffix, filename)
			return matched
		}
		return true
	}

	// No ** in pattern - use regular glob matching on filename
	filename := path
	if idx := strings.LastIndex(filename, "/"); idx >= 0 {
		filename = filename[idx+1:]
	}
	matched, _ := filepath.Match(pattern, filename)
	return matched
}

// Discover scans the given directories for skills.
// A skill is a directory containing a SKILL.md file.
// Returns all discovered skills from all directories.
// Duplicates are removed: if the same skill directory is discovered via multiple
// root directories (e.g., when project and home resolve to the same path), it
// appears only once.
func Discover(dirs ...string) ([]Skill, error) {
	var skills []Skill
	seen := make(map[string]bool)

	for _, dir := range dirs {
		if dir == "" {
			continue
		}

		// Resolve to absolute path for deduplication
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}

		entries, err := os.ReadDir(absDir)
		if err != nil {
			// Skip directories that don't exist
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillPath := filepath.Join(absDir, entry.Name())
			skillFile := filepath.Join(skillPath, "SKILL.md")

			// Deduplicate by absolute path
			if seen[skillPath] {
				continue
			}
			seen[skillPath] = true

			info, err := os.Stat(skillFile)
			if err != nil {
				// Skip if SKILL.md doesn't exist
				if os.IsNotExist(err) {
					continue
				}
				continue
			}

			if info.IsDir() {
				continue
			}

			content, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}

			// Extract name and metadata from the skill
			name := entry.Name()
			description, activationGlob := parseSkillMetadata(content)

			skills = append(skills, Skill{
				Name:           name,
				Description:    description,
				RootPath:       skillPath,
				Content:        string(content),
				ActivationGlob: activationGlob,
			})
		}
	}

	return skills, nil
}

// parseSkillMetadata extracts metadata (description, activation_glob) from SKILL.md content.
// It first looks for YAML frontmatter fields, then falls back to the first line for description.
func parseSkillMetadata(content []byte) (description string, activationGlob string) {
	// Check for YAML frontmatter
	contentStr := string(content)

	// Look for description and activation_glob in frontmatter
	if strings.HasPrefix(contentStr, "---\n") || strings.HasPrefix(contentStr, "---\r\n") {
		parts := strings.SplitN(contentStr[4:], "\n---", 2)
		if len(parts) >= 2 {
			frontmatter := parts[0]
			for line := range strings.SplitSeq(frontmatter, "\n") {
				if desc, ok := strings.CutPrefix(line, "description:"); ok {
					desc = strings.TrimSpace(desc)
					desc = strings.Trim(desc, "\"'")
					if desc != "" {
						description = desc
					}
				}
				if glob, ok := strings.CutPrefix(line, "activation_glob:"); ok {
					glob = strings.TrimSpace(glob)
					glob = strings.Trim(glob, "\"'")
					if glob != "" {
						activationGlob = glob
					}
				}
			}
		}
	}

	// Fall back to first non-empty, non-heading line as description
	for line := range strings.SplitSeq(contentStr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if description == "" {
			if len(line) > 100 {
				line = line[:97] + "..."
			}
			description = line
		}
		break
	}

	if description == "" {
		description = "No description available"
	}
	return description, activationGlob
}

// ReadSkillContent reads the full content of a SKILL.md file.
func ReadSkillContent(rootPath string) (string, error) {
	skillFile := filepath.Join(rootPath, "SKILL.md")
	content, err := os.ReadFile(skillFile)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// NormalizeSkillName normalizes a skill name for case-insensitive lookup.
// It converts to lowercase and trims whitespace.
func NormalizeSkillName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// FindSkillByName finds a skill by name (case-insensitive) from a list of skills.
func FindSkillByName(skills []Skill, name string) *Skill {
	normalized := NormalizeSkillName(name)
	for i := range skills {
		if NormalizeSkillName(skills[i].Name) == normalized {
			return &skills[i]
		}
	}
	return nil
}

// SkillsManifest generates a manifest string for the system prompt.
func SkillsManifest(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var buf bytes.Buffer
	buf.WriteString("\n\nAvailable Skills:\n")

	for _, skill := range skills {
		buf.WriteString("- ")
		buf.WriteString(skill.Name)
		buf.WriteString(": ")
		buf.WriteString(skill.Description)
		buf.WriteString("\n")
	}

	return buf.String()
}
