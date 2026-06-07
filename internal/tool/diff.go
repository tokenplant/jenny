// Package tool provides tool implementations.
package tool

import (
	"fmt"
	"strings"
)

const (
	// maxDiffSize caps diff output to prevent bloating tool results
	maxDiffSize = 50 * 1024
)

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Type    string // "context", "add", "delete"
	Content string
}

// GenerateUnifiedDiff generates a unified diff string between old and new content.
func GenerateUnifiedDiff(oldContent, newContent, path string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Remove trailing empty line from split if present
	if len(oldLines) > 0 && oldLines[len(oldLines)-1] == "" {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}

	// Compute LCS-based diff
	diff := computeLCS(oldLines, newLines)

	// Build unified diff output
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n", path))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", path))

	// Format hunks
	var hunks []string
	var currentHunk []DiffLine

	for i := 0; i < len(diff); i++ {
		diffLine := diff[i]
		switch diffLine.Type {
		case "context":
			if len(currentHunk) == 0 {
				currentHunk = append(currentHunk, DiffLine{Type: "context", Content: " " + diffLine.Content})
			} else {
				currentHunk = append(currentHunk, DiffLine{Type: "context", Content: " " + diffLine.Content})
			}
		case "delete":
			currentHunk = append(currentHunk, DiffLine{Type: "delete", Content: "-" + diffLine.Content})
		case "add":
			currentHunk = append(currentHunk, DiffLine{Type: "add", Content: "+" + diffLine.Content})
		}

		// Flush hunk when we have a good chunk
		if len(currentHunk) >= 4 && i < len(diff)-1 {
			hunks = append(hunks, formatHunk(currentHunk, diff, i))
			currentHunk = nil
		}
	}

	// Flush remaining hunk
	if len(currentHunk) > 0 {
		hunks = append(hunks, formatHunk(currentHunk, diff, len(diff)-1))
	}

	// Write hunks
	for _, hunk := range hunks {
		if sb.Len()+len(hunk) > maxDiffSize {
			break
		}
		sb.WriteString(hunk)
	}

	result := sb.String()
	if len(result) > maxDiffSize {
		result = result[:maxDiffSize] + "\n... (diff truncated)"
	}
	return result
}

// diffEntry represents a line in the computed diff
type diffEntry struct {
	Type    string
	OldIdx  int
	NewIdx  int
	Content string
}

func computeLCS(oldLines, newLines []string) []diffEntry {
	m := len(oldLines)
	n := len(newLines)

	// Build LCS table
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else {
				if lcs[i-1][j] > lcs[i][j-1] {
					lcs[i][j] = lcs[i-1][j]
				} else {
					lcs[i][j] = lcs[i][j-1]
				}
			}
		}
	}

	// Backtrack to find diff
	var result []diffEntry
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			result = append([]diffEntry{{Type: "context", OldIdx: i - 1, NewIdx: j - 1, Content: oldLines[i-1]}}, result...)
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			result = append([]diffEntry{{Type: "add", OldIdx: i, NewIdx: j - 1, Content: newLines[j-1]}}, result...)
			j--
		} else if i > 0 {
			result = append([]diffEntry{{Type: "delete", OldIdx: i - 1, NewIdx: j, Content: oldLines[i-1]}}, result...)
			i--
		}
	}

	return result
}

func formatHunk(lines []DiffLine, fullDiff []diffEntry, endIdx int) string {
	if len(lines) == 0 {
		return ""
	}

	// Find hunk start positions by scanning from beginning to find first non-context line
	oldStart := 1
	newStart := 1
	for _, line := range lines {
		if line.Type != "context" {
			break
		}
		oldStart++
		newStart++
	}

	// Adjust start to be the line number of the first line in this hunk
	for i := endIdx - len(lines) + 1; i <= endIdx; i++ {
		if i < 0 || i >= len(fullDiff) {
			continue
		}
		entry := fullDiff[i]
		if entry.Type == "context" {
			// Adjust start to the actual line numbers from the diff entries
			oldStart = entry.OldIdx + 1
			newStart = entry.NewIdx + 1
			break
		}
	}

	oldCount := 0
	newCount := 0
	for _, line := range lines {
		if line.Type == "delete" {
			oldCount++
		} else if line.Type == "add" {
			newCount++
		} else {
			oldCount++
			newCount++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount))
	for _, line := range lines {
		sb.WriteString(line.Content + "\n")
	}
	return sb.String()
}
