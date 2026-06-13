package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// isSedInPlace checks if command is a sed in-place edit
func isSedInPlace(command string) bool {
	return strings.Contains(command, "sed -i") || strings.Contains(command, "sed -i ")
}

// executeSed simulates sed -i edit
func (t *BashTool) executeSed(command string, cwd string) (*ToolResult, error) {
	// Parse: sed -i 's/pattern/replacement/flags' file
	// or: sed -i "s/pattern/replacement/flags" file

	// Extract the sed expression and file
	parts := strings.Fields(command)
	if len(parts) < 4 {
		return &ToolResult{
			Content: "sed: invalid syntax. Expected: sed -i 's/pattern/replacement/flags' file",
			IsError: true,
		}, nil
	}

	// Find the expression (between -i and the file)
	// Simple approach: find -i, then next token is expression, then remaining is file
	var expr string
	var filePath string
	afterExpr := false

	for i := 1; i < len(parts); i++ {
		if parts[i] == "-i" {
			afterExpr = false
			continue
		}
		if !afterExpr && expr == "" {
			// This should be the expression - remove surrounding quotes
			expr = strings.Trim(parts[i], "'\"")
			afterExpr = true
			continue
		}
		if afterExpr {
			// After expression, the first remaining token is the file path
			filePath = parts[i]
			break
		}
	}

	if expr == "" || filePath == "" {
		return &ToolResult{
			Content: "sed: could not parse expression or file path",
			IsError: true,
		}, nil
	}

	// Remove surrounding quotes from expression
	expr = strings.Trim(expr, "'\"")

	// Parse sed expression
	parsed, err := parseSedExpression(expr)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("sed: %v", err),
			IsError: true,
		}, nil
	}

	// Resolve file path
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}
	filePath = filepath.Clean(filePath)

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("sed: cannot read file: %v", err),
			IsError: true,
		}, nil
	}

	// Apply replacement using regex (matching real sed behavior)
	content := string(data)
	re, reErr := regexp.Compile(parsed.pattern)
	if reErr != nil {
		// If pattern is not valid regex, fall back to literal replacement
		if parsed.global {
			content = strings.ReplaceAll(content, parsed.pattern, parsed.replacement)
		} else {
			content = strings.Replace(content, parsed.pattern, parsed.replacement, 1)
		}
	} else {
		if parsed.global {
			content = re.ReplaceAllString(content, parsed.replacement)
		} else {
			found := false
			content = re.ReplaceAllStringFunc(content, func(match string) string {
				if found {
					return match
				}
				found = true
				return re.ReplaceAllString(match, parsed.replacement)
			})
		}
	}

	// Write back
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("sed: cannot write file: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf("sed: edited %s in place", filePath),
		IsError: false,
	}, nil
}

type sedParsed struct {
	pattern     string
	replacement string
	global      bool
}

// parseSedExpression parses a sed s/// expression
func parseSedExpression(expr string) (*sedParsed, error) {
	// Support different delimiters: s/// s### s,,,
	// Find the delimiter (first char after 's')
	if len(expr) < 4 || expr[0] != 's' {
		return nil, fmt.Errorf("invalid sed expression format")
	}

	delimiter := expr[1]
	rest := expr[2:]

	// Find the three parts separated by delimiter
	parts := strings.SplitN(rest, string(delimiter), 4)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid sed expression: could not parse pattern/replacement")
	}

	pattern := parts[0]
	replacement := parts[1]
	flags := ""
	if len(parts) >= 4 {
		flags = parts[3]
	}

	// Check for 'g' flag (global)
	global := strings.Contains(flags, "g")

	return &sedParsed{
		pattern:     pattern,
		replacement: replacement,
		global:      global,
	}, nil
}
