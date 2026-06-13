package portal

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/session"
)

// SessionMeta represents session metadata returned by the API.
type SessionMeta struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CWD       string `json:"cwd"`
	Model     string `json:"model"`
	StartTime int64  `json:"start_time"`
	PID       int    `json:"pid,omitempty"`
}

// Stats represents the global stats returned by the API.
type Stats struct {
	TotalSessions  int     `json:"total_sessions"`
	ActiveSessions int     `json:"active_sessions"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	TotalTokens    int     `json:"total_tokens"`
}

// setupRoutes sets up the HTTP routes for the portal.
func (p *Portal) setupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", p.withAuth(p.handleHealth))
	mux.HandleFunc("GET /api/sessions", p.withAuth(p.handleListSessions))
	mux.HandleFunc("GET /api/sessions/", p.withAuth(p.handleSessionStream))
	mux.HandleFunc("POST /api/sessions/", p.withAuth(p.handleSessionAction))
	mux.HandleFunc("GET /api/stats", p.withAuth(p.handleStats))
	mux.HandleFunc("GET /", p.handleStatic)
	mux.HandleFunc("/", p.handleStatic)
}

// withAuth wraps a handler with authentication middleware.
func (p *Portal) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.resetIdleTimer()

		token := r.URL.Query().Get("token")
		if token == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(p.authToken)) != 1 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// handleHealth handles GET /api/health.
func (p *Portal) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"pid":    p.pid,
	})
}

// handleListSessions handles GET /api/sessions.
func (p *Portal) handleListSessions(w http.ResponseWriter, r *http.Request) {
	manager, err := session.NewManager(constants.JennyHomeDir(), false)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	sessionIDs, err := manager.ListSessions()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var sessions []SessionMeta
	for _, id := range sessionIDs {
		meta, err := p.getSessionMeta(id)
		if err != nil {
			continue // Skip sessions we can't read
		}
		sessions = append(sessions, *meta)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// getSessionMeta reads session metadata from the filesystem.
func (p *Portal) getSessionMeta(sessionID string) (*SessionMeta, error) {
	sessionDir := constants.SessionDir(sessionID)
	transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")

	// Check if transcript exists
	if _, err := os.Stat(transcriptPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no transcript")
	}

	meta := &SessionMeta{
		ID: sessionID,
	}

	// Read transcript to get metadata
	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		return nil, err
	}

	var lastEntry session.TranscriptEntry
	lines := splitLines(string(data))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry session.TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		lastEntry = entry

		// Capture start time from first entry
		if meta.StartTime == 0 {
			meta.StartTime = entry.Timestamp.UnixMilli()
		}

		// Capture model from assistant entries
		if entry.Type == session.EntryTypeAssistant && entry.ToolUse != nil {
			for _, tu := range entry.ToolUse {
				if tu.Input != nil {
					if model, ok := tu.Input["model"].(string); ok {
						meta.Model = model
					}
				}
			}
		}

		// Capture cwd from state entries
		if entry.Type == session.EntryTypeState && entry.CWD != "" {
			meta.CWD = entry.CWD
		}
	}

	// Use last entry's cwd if no state entry
	if meta.CWD == "" && lastEntry.CWD != "" {
		meta.CWD = lastEntry.CWD
	}

	// Check if session is running
	pidPath := filepath.Join(sessionDir, "pid")
	if pidData, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
			meta.PID = pid
			if proc, err := os.FindProcess(pid); err == nil {
				if err := proc.Signal(syscall.Signal(0)); err == nil {
					meta.Status = "running"
				} else {
					meta.Status = "exited"
				}
			} else {
				meta.Status = "exited"
			}
		}
	}

	if meta.Status == "" {
		meta.Status = "exited"
	}

	return meta, nil
}

// splitLines splits a string into lines (without trailing newlines).
func splitLines(s string) []string {
	var lines []string
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && s[j] != '\n' {
			j++
		}
		lines = append(lines, s[i:j])
		if j < len(s) {
			j++
		}
		i = j
	}
	return lines
}

// handleSessionStream handles GET /api/sessions/{id}/stream (SSE).
func (p *Portal) handleSessionStream(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	path = strings.TrimSuffix(path, "/stream")
	if path == "" {
		http.Error(w, `{"error":"session id required"}`, http.StatusBadRequest)
		return
	}

	sessionDir := constants.SessionDir(path)
	transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")

	// Get initial file size
	initialSize := int64(0)
	if info, err := os.Stat(transcriptPath); err == nil {
		initialSize = info.Size()
	}

	lastSize := initialSize
	flusher := w.(http.Flusher)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	flusher.Flush()

	// Poll for new content
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Check for session exit
	sessionDone := make(chan struct{})
	go func() {
		pollPID(path, sessionDone)
	}()

	var buf bytes.Buffer
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sessionDone:
			return
		case <-ticker.C:
			if info, err := os.Stat(transcriptPath); err == nil {
				if info.Size() > lastSize {
					// Read new content
					f, err := os.Open(transcriptPath)
					if err != nil {
						return
					}
					_, err = f.Seek(lastSize, 0)
					if err != nil {
						f.Close()
						return
					}
					_, err = io.Copy(&buf, f)
					f.Close()
					if err != nil {
						return
					}

					// Parse and emit each new line
					newData := buf.String()
					buf.Reset()
					newLines := splitLines(newData)
					for _, line := range newLines {
						if line == "" {
							continue
						}
						// Validate JSON
						var entry json.RawMessage
						if json.Unmarshal([]byte(line), &entry) == nil {
							fmt.Fprintf(w, "data: %s\n\n", line)
							flusher.Flush()
						}
					}
					lastSize = info.Size()
				}
			}
		}
	}
}

// pollPID polls the session's pid file to detect when the session exits.
func pollPID(sessionID string, done chan<- struct{}) {
	sessionDir := constants.SessionDir(sessionID)
	pidPath := filepath.Join(sessionDir, "pid")

	for {
		pidData, err := os.ReadFile(pidPath)
		if err != nil {
			// No pid file or can't read it - assume session done
			select {
			case done <- struct{}{}:
			default:
			}
			return
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err != nil {
			select {
			case done <- struct{}{}:
			default:
			}
			return
		}

		proc, err := os.FindProcess(pid)
		if err != nil || proc.Signal(syscall.Signal(0)) != nil {
			select {
			case done <- struct{}{}:
			default:
			}
			return
		}

		time.Sleep(2 * time.Second)
	}
}

// handleSessionAction handles POST /api/sessions/{id}/kill.
func (p *Portal) handleSessionAction(w http.ResponseWriter, r *http.Request) {
	// Extract session ID and action from path
	// Path format: /api/sessions/{id}/kill
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, `{"error":"session id and action required"}`, http.StatusBadRequest)
		return
	}

	sessionID := parts[0]
	action := parts[1]

	_ = constants.SessionDir(sessionID) // Ensure session dir path is valid

	switch action {
	case "kill":
		// Get PID from transcript state entry or pid file
		pid, err := p.getSessionPID(sessionID)
		if err != nil || pid == 0 {
			http.Error(w, `{"error":"session not running"}`, http.StatusNotFound)
			return
		}

		proc, err := os.FindProcess(pid)
		if err != nil || proc.Signal(syscall.Signal(0)) != nil {
			http.Error(w, `{"error":"session not running"}`, http.StatusNotFound)
			return
		}

		// Kill the process - use cross-platform approach
		// On Unix: send SIGTERM for graceful shutdown
		// On Windows: use Kill() which calls TerminateProcess
		if runtime.GOOS == "windows" {
			if err := proc.Kill(); err != nil {
				http.Error(w, `{"error":"failed to kill session"}`, http.StatusInternalServerError)
				return
			}
		} else {
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				http.Error(w, `{"error":"failed to kill session"}`, http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "killed"})

	default:
		http.Error(w, fmt.Sprintf(`{"error":"unknown action: %s"}`, action), http.StatusBadRequest)
	}
}

// getSessionPID retrieves the PID from a session's state entry or pid file.
func (p *Portal) getSessionPID(sessionID string) (int, error) {
	sessionDir := constants.SessionDir(sessionID)
	transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")

	// Try pid file first
	pidPath := filepath.Join(sessionDir, "pid")
	if pidData, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
			return pid, nil
		}
	}

	// Try to find from transcript state entry
	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		return 0, err
	}

	lines := splitLines(string(data))
	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Type string `json:"type"`
			PID  int    `json:"pid"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &entry); err == nil {
			if entry.PID > 0 {
				return entry.PID, nil
			}
		}
	}

	return 0, fmt.Errorf("pid not found")
}

// handleStats handles GET /api/stats.
func (p *Portal) handleStats(w http.ResponseWriter, r *http.Request) {
	manager, err := session.NewManager(constants.JennyHomeDir(), false)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	sessionIDs, err := manager.ListSessions()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	stats := Stats{
		TotalSessions: len(sessionIDs),
	}

	for _, id := range sessionIDs {
		meta, err := p.getSessionMeta(id)
		if err != nil {
			continue
		}

		if meta.Status == "running" {
			stats.ActiveSessions++
		}

		// Read cost from cost_state.json if available
		costPath := filepath.Join(constants.SessionDir(id), "cost_state.json")
		if costData, err := os.ReadFile(costPath); err == nil {
			var cost struct {
				TotalCost float64 `json:"total_cost_usd"`
			}
			if json.Unmarshal(costData, &cost) == nil {
				stats.TotalCostUSD += cost.TotalCost
			}
		}
	}

	// Read tokens from transcript for sorting (for consistent output)
	tokens := make([]int, 0)
	for _, id := range sessionIDs {
		transcriptPath := filepath.Join(constants.SessionDir(id), "transcript.jsonl")
		if data, err := os.ReadFile(transcriptPath); err == nil {
			lines := splitLines(string(data))
			for _, line := range lines {
				if line == "" {
					continue
				}
				var entry struct {
					Type  string `json:"type"`
					Token int    `json:"token_count,omitempty"`
				}
				if json.Unmarshal([]byte(line), &entry) == nil {
					if entry.Token > 0 {
						tokens = append(tokens, entry.Token)
					}
				}
			}
		}
	}
	sort.Ints(tokens)
	for _, t := range tokens {
		stats.TotalTokens += t
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleStatic serves the embedded webui dist.
func (p *Portal) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	// fs.FS paths must not have a leading slash.
	cleanPath := strings.TrimPrefix(path, "/")

	// Get sub-fs for webui/dist
	subFS, err := getSubFS()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Try to serve from embedded dist
	content, err := subFS.Open(cleanPath)
	if err != nil {
		// Fallback to index.html for SPA routing
		content, err = subFS.Open("index.html")
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
	}
	defer content.Close()

	stat, err := content.Stat()
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if stat.IsDir() {
		indexContent, err := subFS.Open("index.html")
		if err != nil {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		defer indexContent.Close()
		content = indexContent
	}

	data, err := io.ReadAll(content)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	// Set content type based on extension
	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css")
	case strings.HasSuffix(path, ".json"):
		w.Header().Set("Content-Type", "application/json")
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	w.Write(data)
}