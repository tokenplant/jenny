// Black-box validation tests for GrepTool
package tool_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/tool"
)

func TestBlackBox_AC1_HeadLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 310 files with matching content
	for i := 0; i < 310; i++ {
		p := filepath.Join(tmpDir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(p, []byte("match content"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	tools := tool.NewGrepTool()

	// Verify default head_limit is 250
	result, err := tools.Execute(context.Background(), map[string]any{
		"pattern": "match",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error result: %s", result.Content)
	}

	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	if len(lines) != 250 {
		t.Errorf("expected 250 results with default head_limit, got %d", len(lines))
	}
	if result.Truncated {
		t.Errorf("expected Truncated=false (250 lines < 20K chars), got true")
	}

	// Verify head_limit=0 returns all matches
	result, err = tools.Execute(context.Background(), map[string]any{
		"pattern":    "match",
		"head_limit": 0,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error result: %s", result.Content)
	}

	lines = strings.Split(strings.TrimSpace(result.Content), "\n")
	if len(lines) <= 250 {
		t.Errorf("expected >250 results with head_limit=0, got %d", len(lines))
	}
}

func TestBlackBox_AC2_DashPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with literal "-foo"
	if err := os.WriteFile(filepath.Join(tmpDir, "matches.txt"), []byte("-foo"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create file with "bar" content that should NOT match "-foo"
	if err := os.WriteFile(filepath.Join(tmpDir, "nomatch.txt"), []byte("bar"), 0644); err != nil {
		t.Fatal(err)
	}

	tools := tool.NewGrepTool()

	// Search for "-foo"
	result, err := tools.Execute(context.Background(), map[string]any{
		"pattern":     "-foo",
		"output_mode": "content",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error result: %s", result.Content)
	}

	// Should find matches.txt but NOT nomatch.txt
	if !strings.Contains(result.Content, "matches.txt") {
		t.Errorf("expected to find matches.txt for pattern '-foo', got: %s", result.Content)
	}
	if strings.Contains(result.Content, "nomatch.txt") {
		t.Errorf("-foo should NOT match file containing 'bar', got: %s", result.Content)
	}
}

func TestBlackBox_AC3_Timeout(t *testing.T) {
	tools := tool.NewGrepTool()

	// Test 1: timeout=0 (as float64) with a search that requires scanning many files.
	// The timeout value flows through as float64 (matching JSON decode behavior).
	tmpDir := t.TempDir()
	for i := 0; i < 200; i++ {
		dir := filepath.Join(tmpDir, fmt.Sprintf("d%d", i))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 100; j++ {
			p := filepath.Join(dir, fmt.Sprintf("f%d.txt", j))
			if err := os.WriteFile(p, []byte("content data for search pattern here\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}

	result, err := tools.Execute(context.Background(), map[string]any{
		"pattern": "content",
		"timeout": float64(0), // must be float64 (JSON schema "number" type)
	}, tmpDir)
	if err != nil {
		return // expected: error returned
	}
	if result != nil && result.IsError {
		return // expected: error result (timeout)
	}
	t.Errorf("expected timeout error with timeout=0, got success result")
}

func TestBlackBox_AC4_OutputCap(t *testing.T) {
	tmpDir := t.TempDir()

	// Single file with many lines that match, exceeding 20K chars
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("This is a very long line of text for testing grep output truncation. ")
		sb.WriteString("We need to exceed twenty thousand characters easily. ")
		sb.WriteString(string('A' + rune(i%26)))
		sb.WriteString("\n")
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "big.txt"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}

	tools := tool.NewGrepTool()

	result, err := tools.Execute(context.Background(), map[string]any{
		"pattern":     "very",
		"output_mode": "content",
		"head_limit":  0,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error result: %s", result.Content)
	}

	if !result.Truncated {
		t.Errorf("expected Truncated=true for >20K output")
	}
	// Content should be reasonably bounded
	if len(result.Content) > 21000 {
		t.Errorf("output too long: %d chars (should be capped ~20K)", len(result.Content))
	}
}

func TestBlackBox_AC5_VCSExcluded(t *testing.T) {
	tmpDir := t.TempDir()

	// .git/objects tree with matching content
	gitObjs := filepath.Join(tmpDir, ".git", "objects")
	if err := os.MkdirAll(gitObjs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitObjs, "pack"), []byte("secret-data"), 0644); err != nil {
		t.Fatal(err)
	}

	// .svn dir with matching content
	svnDir := filepath.Join(tmpDir, ".svn")
	if err := os.MkdirAll(svnDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svnDir, "entries"), []byte("secret-data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Working tree file with same content
	if err := os.WriteFile(filepath.Join(tmpDir, "workfile.txt"), []byte("secret-data"), 0644); err != nil {
		t.Fatal(err)
	}

	tools := tool.NewGrepTool()

	result, err := tools.Execute(context.Background(), map[string]any{
		"pattern": "secret-data",
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("error result: %s", result.Content)
	}

	if !strings.Contains(result.Content, "workfile.txt") {
		t.Errorf("expected to find workfile.txt, got: %s", result.Content)
	}
	if strings.Contains(result.Content, ".git") {
		t.Errorf(".git should be excluded, got: %s", result.Content)
	}
	if strings.Contains(result.Content, ".svn") {
		t.Errorf(".svn should be excluded, got: %s", result.Content)
	}
}
