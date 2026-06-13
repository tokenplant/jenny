// Package grepinproc provides a small, in-process text-search backend for
// the GrepTool. It is used as a fallback when ripgrep is not available on
// the host. The implementation uses filepath.WalkDir and regexp.FindAllIndex
// from the Go standard library.
//
// The output of Run is a slice of Result records. Rendering to
// ripgrep-style text is the caller's responsibility, so the same renderer
// can be used for both the rg-shell path and the in-process path.
package grepinproc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// File-type extension map used when opts.FileType is non-empty.
// Mirrors ripgrep's built-in --type support for the common cases.
var fileTypeExt = map[string][]string{
	"go":   {".go"},
	"cc":   {".c", ".h", ".xs"},
	"cpp":  {".cpp", ".cc", ".cxx", ".m", ".hpp", ".hh", ".h", ".hxx"},
	"html": {".htm", ".html", ".shtml", ".xhtml"},
	"java": {".java", ".properties"},
	"js":   {".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx"},
	"py":   {".py", ".pyw", ".pyx"},
	"rb":   {".rb", ".erb", ".rake"},
	"rs":   {".rs"},
	"sh":   {".sh", ".bash", ".zsh"},
	"xml":  {".xml"},
}

// Options configures a single search.
type Options struct {
	// Pattern is the regex to search for. Required.
	Pattern string
	// Path is the file or directory to search. Required.
	// If relative, it is interpreted relative to Cwd.
	Path string
	// Cwd is the base directory for relative Path values.
	Cwd string
	// Glob, if non-empty, restricts results to filenames matching
	// the pattern (filepath.Match syntax: e.g. "*.txt").
	Glob string
	// OutputMode selects rendering at the call site: "content",
	// "files_with_matches", or "count". Run() returns the raw
	// matches; the caller decides how to format them.
	OutputMode string
	// IgnoreCase makes the pattern case-insensitive.
	IgnoreCase bool
	// Multiline treats the whole file as a single string.
	Multiline bool
	// FileType, if non-empty, restricts to files of that built-in
	// type (see fileTypeExt).
	FileType string
	// Hidden searches dotfiles. Default: false.
	Hidden bool
	// IgnoreDirs is a list of directory basenames to skip during
	// recursive walk. Defaults to {".git", ".svn"} when empty.
	IgnoreDirs []string
	// ContextBefore is the number of context lines before each match.
	ContextBefore int
	// ContextAfter is the number of context lines after each match.
	ContextAfter int
}

// Match is a single regex hit within a file.
type Match struct {
	// Line is the 1-based line number of the match start.
	Line int64
	// Content is the matched line (without trailing newline).
	Content string
	// Before is up to ContextBefore context lines preceding the match.
	Before []string
	// After is up to ContextAfter context lines following the match.
	After []string
}

// Result holds all matches for a single file.
type Result struct {
	// Target is the absolute path of the file that was searched.
	Target string
	// Matches is the list of matches in the file, in source order.
	Matches []Match
}

// Run performs a single search and returns the collected matches.
// The context controls cancellation only — there is no built-in
// timeout here. Callers that need a timeout should derive one from
// the context.
func Run(ctx context.Context, opts Options) ([]Result, error) {
	if opts.Pattern == "" {
		return nil, fmt.Errorf("grepinproc: pattern is required")
	}
	if opts.Path == "" {
		return nil, fmt.Errorf("grepinproc: path is required")
	}

	// Build the regex.
	pattern := opts.Pattern
	if opts.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	if opts.Multiline {
		pattern = "(?s)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("grepinproc: invalid pattern: %w", err)
	}

	// Resolve the search path.
	searchPath := opts.Path
	if !filepath.IsAbs(searchPath) {
		searchPath = filepath.Join(opts.Cwd, searchPath)
	}

	// Default ignore list.
	ignoreDirs := opts.IgnoreDirs
	if len(ignoreDirs) == 0 {
		ignoreDirs = []string{".git", ".svn"}
	}

	// File-type filter set (lower-case, with leading dot).
	var typeExt map[string]struct{}
	if opts.FileType != "" {
		exts, ok := fileTypeExt[strings.ToLower(opts.FileType)]
		if !ok {
			return nil, fmt.Errorf("grepinproc: unknown file type %q", opts.FileType)
		}
		typeExt = make(map[string]struct{}, len(exts))
		for _, e := range exts {
			typeExt[strings.ToLower(e)] = struct{}{}
		}
	}

	// Glob matcher (compiled once).
	var globMatch func(name string) bool
	if opts.Glob != "" {
		globMatch = func(name string) bool {
			matched, err := filepath.Match(opts.Glob, name)
			if err != nil {
				return false
			}
			return matched
		}
	}

	// Enumerate candidate files.
	var files []string
	stat, err := os.Stat(searchPath)
	if err != nil {
		return nil, fmt.Errorf("grepinproc: %w", err)
	}
	if !stat.IsDir() {
		files = []string{searchPath}
	} else {
		walkErr := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Skip unreadable entries; do not abort the walk.
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				base := d.Name()
				if slices.Contains(ignoreDirs, base) {
					return filepath.SkipDir
				}
				if !opts.Hidden && strings.HasPrefix(base, ".") && path != searchPath {
					return filepath.SkipDir
				}
				return nil
			}
			// Regular file (or symlink; we follow the dir walk's policy).
			name := d.Name()
			if !opts.Hidden && strings.HasPrefix(name, ".") {
				return nil
			}
			if globMatch != nil && !globMatch(name) {
				return nil
			}
			if typeExt != nil {
				ext := strings.ToLower(filepath.Ext(name))
				if _, ok := typeExt[ext]; !ok {
					return nil
				}
			}
			files = append(files, path)
			return nil
		})
		if walkErr != nil {
			if walkErr == context.Canceled || walkErr == context.DeadlineExceeded {
				return nil, walkErr
			}
			return nil, fmt.Errorf("grepinproc: walk %s: %w", searchPath, walkErr)
		}
	}

	if len(files) == 0 {
		return nil, nil
	}

	// Process files in parallel.
	results := make([]Result, len(files))
	workers := max(min(runtime.NumCPU(), 8, len(files)), 1)

	type job struct {
		idx  int
		path string
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for range workers {
		wg.Go(func() {
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				matches, err := searchFile(ctx, j.path, re, opts)
				if err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					continue
				}
				results[j.idx] = Result{Target: j.path, Matches: matches}
			}
		})
	}
	for i, f := range files {
		select {
		case jobs <- job{idx: i, path: f}:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		// A worker observed a context error mid-search. Surface it so
		// the caller can distinguish a partial result from a clean run.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, firstErr
	}

	// Compact: drop empty results and preserve source order.
	out := make([]Result, 0, len(results))
	for _, r := range results {
		if len(r.Matches) > 0 {
			out = append(out, r)
		}
	}
	return out, nil
}

// searchFile runs the regex against a single file and returns its matches.
func searchFile(ctx context.Context, path string, re *regexp.Regexp, opts Options) ([]Match, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if opts.Multiline {
		return searchFileMultiline(ctx, f, re, opts)
	}
	return searchFileLines(ctx, f, re, opts)
}

// searchFileLines reads the file line-by-line and matches each line.
func searchFileLines(ctx context.Context, f *os.File, re *regexp.Regexp, opts Options) ([]Match, error) {
	scanner := bufio.NewScanner(f)
	// Allow long lines; default 64K is too small for log files.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	// For context, we keep a sliding window of recent lines.
	before := opts.ContextBefore
	after := opts.ContextAfter
	var beforeBuf []string // ring buffer of the last `before` lines
	if before > 0 {
		beforeBuf = make([]string, 0, before)
	}
	afterRemaining := 0
	var afterBuf []string

	var matches []Match
	var lineNum int64
	for scanner.Scan() {
		line := scanner.Text()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineNum++
		if re.MatchString(line) {
			m := Match{Line: lineNum, Content: line}
			if before > 0 && len(beforeBuf) > 0 {
				m.Before = append([]string(nil), beforeBuf...)
			}
			matches = append(matches, m)
			afterRemaining = after
			afterBuf = nil
		} else if afterRemaining > 0 {
			afterBuf = append(afterBuf, line)
			// Attach to the most recent match.
			if len(matches) > 0 {
				matches[len(matches)-1].After = append(matches[len(matches)-1].After, line)
			}
			afterRemaining--
		}
		// Maintain the before ring buffer.
		if before > 0 {
			beforeBuf = append(beforeBuf, line)
			if len(beforeBuf) > before {
				beforeBuf = beforeBuf[len(beforeBuf)-before:]
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	_ = afterBuf // reserved for future per-match separation
	return matches, nil
}

// searchFileMultiline reads the entire file and runs the regex once.
func searchFileMultiline(ctx context.Context, f *os.File, re *regexp.Regexp, opts Options) ([]Match, error) {
	data, err := readAllContext(ctx, f)
	if err != nil {
		return nil, err
	}
	// Split into lines for line-number computation and context.
	lines := splitLines(data)
	lineStart := make([]int64, len(lines))
	var off int64
	for i, l := range lines {
		lineStart[i] = off
		off += int64(len(l)) + 1 // +1 for the newline
	}

	loc := re.FindAllIndex(data, -1)
	if len(loc) == 0 {
		return nil, nil
	}
	before := opts.ContextBefore
	after := opts.ContextAfter
	matches := make([]Match, 0, len(loc))
	for _, l := range loc {
		start, end := int64(l[0]), int64(l[1])
		// Find the line number by binary search.
		lineIdx := findLine(lineStart, start)
		if lineIdx < 0 {
			continue
		}
		m := Match{
			Line:    int64(lineIdx) + 1,
			Content: strings.TrimRight(lines[lineIdx], "\r"),
		}
		if before > 0 {
			from := max(lineIdx-before, 0)
			m.Before = append(m.Before, lines[from:lineIdx]...)
		}
		if after > 0 {
			to := min(lineIdx+1+after, len(lines))
			m.After = append(m.After, lines[lineIdx+1:to]...)
		}
		matches = append(matches, m)
		_ = end
	}
	return matches, nil
}

// readAllContext reads the whole file, respecting ctx cancellation.
func readAllContext(ctx context.Context, f *os.File) ([]byte, error) {
	const chunk = 64 * 1024
	var buf []byte
	rb := make([]byte, chunk)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := f.Read(rb)
		if n > 0 {
			buf = append(buf, rb[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				return buf, nil
			}
			return buf, err
		}
	}
}

// splitLines splits on \n and trims trailing \r for Windows line endings.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := string(data)
	if s == "" {
		return nil
	}
	var lines []string
	// Fast path: no trailing newline.
	if !strings.HasSuffix(s, "\n") {
		lines = strings.Split(s, "\n")
	} else {
		lines = strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}
	for i, l := range lines {
		if strings.HasSuffix(l, "\r") {
			lines[i] = l[:len(l)-1]
		}
	}
	return lines
}

// findLine returns the index of the line containing the byte offset.
func findLine(lineStart []int64, off int64) int {
	lo, hi := 0, len(lineStart)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if lineStart[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi
}
