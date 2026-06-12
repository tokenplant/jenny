package mockapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	// The "testing" import below is required by NewTestServer, which provides
	// a test-harness convenience wrapper around NewMockServer + Lookup.
	// It is deliberately kept in this non-test file because NewTestServer is
	// part of the exported mockapi API surface: callers use it in their own
	// _test.go files.
	"testing"
)

// APIRequest is one captured request received by the mock server.
type APIRequest struct {
	Body   map[string]any
	Header http.Header
}

// MockServer is an in-process mock of the Anthropic API.
type MockServer struct {
	Server       *httptest.Server
	CassetteDir  string
	mockBehavior *MockBehavior

	mu       sync.Mutex
	requests []APIRequest

	seqMu  sync.Mutex
	seqDef map[string][]string
	seqIdx map[string]int

	// E1: Inline SSE/JSON content, keyed by cassetteID.
	// Checked before file-based cassette lookup.
	inlineResponses map[string]string

	// E2: Single request inspector callback.
	// Called after body parse + capture, before cassette lookup.
	requestInspector func(r APIRequest) error

	// E3: HTTP status code overrides, keyed by cassetteID.
	// Checked after cassetteID extraction, before reading cassette content.
	// Zero value means no override.
	errorResponses map[string]int

	// E4: Content-Type overrides, keyed by cassetteID.
	// Applied when the default dispatcher serves the response.
	contentTypes map[string]string

	// E5: Custom path handlers, keyed by "METHOD /path" (e.g., "POST /v1/chat/completions").
	// Checked before the default /cassette/<id>/v1/messages dispatcher.
	pathHandlers map[string]http.HandlerFunc

	// extMu protects the5 extension maps above against concurrent access.
	// Setters run in test code; reads happen in handle() from the HTTP server goroutine.
	extMu sync.Mutex
}

// MockBehavior defines custom mock behaviors.
type MockBehavior struct {
	RejectEmptyToolProperties bool
}

// Option configures a MockServer.
type Option func(*MockServer)

// WithCassetteDir sets the base directory from which .sse and .json cassette files are loaded.
// If not set, cassette-based serving is disabled; use SetInlineResponse for in-memory content only.
func WithCassetteDir(dir string) Option {
	return func(m *MockServer) {
		m.CassetteDir = dir
	}
}

// NewMockServer creates a MockServer with the given options.
// The server is pre-configured with:
// - POST /cassette/<id>/v1/messages → serves cassette files or inline content
//   - GET → 405 Method Not Allowed (unchanged)
//   - Content-Type → text/event-stream (default; overridable via SetContentType per cassetteID)
func NewMockServer(opts ...Option) *MockServer {
	m := &MockServer{}
	for _, opt := range opts {
		opt(m)
	}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// SetInlineResponse stores SSE or JSON content to serve for cassetteID.
// When a request arrives, the handler checks the inline map first; if found,
// serves the inline content. If not found, falls back to the file-based
// cassette from WithCassetteDir.
// Empty content clears the entry.
func (m *MockServer) SetInlineResponse(cassetteID string, content string) *MockServer {
	m.extMu.Lock()
	defer m.extMu.Unlock()
	if content == "" {
		delete(m.inlineResponses, cassetteID)
	} else {
		if m.inlineResponses == nil {
			m.inlineResponses = make(map[string]string)
		}
		m.inlineResponses[cassetteID] = content
	}
	return m
}

// SetRequestInspector registers a callback invoked after the request body
// is parsed and captured, before the response is served. If the callback
// returns a non-nil error, the server responds with HTTP 400 and the error
// message as JSON {"error": "<msg>"}. Only one inspector is active at a time.
func (m *MockServer) SetRequestInspector(fn func(r APIRequest) error) *MockServer {
	m.extMu.Lock()
	defer m.extMu.Unlock()
	m.requestInspector = fn
	return m
}

// SetErrorResponse registers a non-200 HTTP status code to return for a
// given cassetteID. The error response is returned before reading any cassette
// content. Zero statusCode clears the override (resets to 200 behavior).
func (m *MockServer) SetErrorResponse(cassetteID string, statusCode int) *MockServer {
	m.extMu.Lock()
	defer m.extMu.Unlock()
	if statusCode == 0 {
		delete(m.errorResponses, cassetteID)
	} else {
		if m.errorResponses == nil {
			m.errorResponses = make(map[string]int)
		}
		m.errorResponses[cassetteID] = statusCode
	}
	return m
}

// SetContentType overrides the Content-Type header for a given cassetteID.
// Without this, the default is "text/event-stream" for SSE cassettes.
// Empty contentType clears the override.
// Applies only to the default /cassette/<id>/v1/messages dispatcher;
// custom path handlers (E5) write their own Content-Type headers directly.
func (m *MockServer) SetContentType(cassetteID string, contentType string) *MockServer {
	m.extMu.Lock()
	defer m.extMu.Unlock()
	if contentType == "" {
		delete(m.contentTypes, cassetteID)
	} else {
		if m.contentTypes == nil {
			m.contentTypes = make(map[string]string)
		}
		m.contentTypes[cassetteID] = contentType
	}
	return m
}

// SetPathHandler registers a custom handler for a specific HTTP path pattern.
// The pathPattern format is "METHOD /path" (e.g., "POST /v1/chat/completions").
// Only the specified method is handled; other methods return 405 unless a handler
// is also registered for them. The path handler takes precedence over the default
// /cassette/<id>/v1/messages dispatcher.
// Multiple calls with the same key replace the previous handler.
func (m *MockServer) SetPathHandler(pathPattern string, handler func(w http.ResponseWriter, r *http.Request)) *MockServer {
	m.extMu.Lock()
	defer m.extMu.Unlock()
	if m.pathHandlers == nil {
		m.pathHandlers = make(map[string]http.HandlerFunc)
	}
	m.pathHandlers[pathPattern] = handler
	return m
}

// URL returns the base URL of the mock server.
func (m *MockServer) URL() string {
	return m.Server.URL
}

// Close stops the mock server.
func (m *MockServer) Close() {
	m.Server.Close()
}

// Requests returns a copy of all captured requests.
func (m *MockServer) Requests() []APIRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]APIRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

// ClearRequests resets the recorded requests list.
func (m *MockServer) ClearRequests() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = nil
}

// SetMockBehavior sets custom mock API behaviors.
func (m *MockServer) SetMockBehavior(mb *MockBehavior) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mockBehavior = mb
}

// SetSequence registers an ordered list of cassette file names to serve.
func (m *MockServer) SetSequence(cassetteID string, cassettes []string) {
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	if m.seqDef == nil {
		m.seqDef = make(map[string][]string)
	}
	if m.seqIdx == nil {
		m.seqIdx = make(map[string]int)
	}
	m.seqDef[cassetteID] = cassettes
	m.seqIdx[cassetteID] = 0
}

func (m *MockServer) handle(w http.ResponseWriter, r *http.Request) {
	// E5: Check custom path handlers first (before cassette extraction)
	m.extMu.Lock()
	pathHandlerKey := r.Method + " " + r.URL.Path
	pathHandler, pathHandlerOK := m.pathHandlers[pathHandlerKey]
	// Also check if the path matches but method doesn't (for 405 response)
	_, pathExistsForOtherMethod := m.pathHandlers[r.URL.Path]
	m.extMu.Unlock()

	if pathHandlerOK {
		pathHandler(w, r)
		return
	}
	if pathExistsForOtherMethod {
		// Path is registered but not for this method → 405
		m.writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	// Default: only POST is allowed for /cassette/... paths
	if r.Method != http.MethodPost {
		m.writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		m.writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		m.writeError(w, http.StatusBadRequest, "decode body: "+err.Error())
		return
	}

	// Capture request
	m.mu.Lock()
	m.requests = append(m.requests, APIRequest{Body: decoded, Header: r.Header.Clone()})
	mb := m.mockBehavior
	m.mu.Unlock()

	// Apply custom mock behaviors if any
	if mb != nil && mb.RejectEmptyToolProperties {
		if tools, ok := decoded["tools"].([]any); ok {
			for _, toolEntry := range tools {
				tool, ok := toolEntry.(map[string]any)
				if !ok {
					continue
				}
				// Skip web_search tools as they don't have properties in some dialects
				if toolType, ok := tool["type"].(string); ok && (toolType == "web_search" || toolType == "web_search_20250305") {
					continue
				}
				inputSchema, ok := tool["input_schema"].(map[string]any)
				if !ok || inputSchema == nil {
					m.writeError(w, http.StatusBadRequest, "function name or parameters is empty (2013)")
					return
				}
				props, ok := inputSchema["properties"]
				if !ok || props == nil {
					m.writeError(w, http.StatusBadRequest, "function name or parameters is empty (2013)")
					return
				}
				propsMap, ok := props.(map[string]any)
				if !ok || len(propsMap) == 0 {
					m.writeError(w, http.StatusBadRequest, "function name or parameters is empty (2013)")
					return
				}
			}
		} else {
			// No tools array at all
			m.writeError(w, http.StatusBadRequest, "function name or parameters is empty (2013)")
			return
		}
	}

	// E2: Call request inspector
	m.extMu.Lock()
	inspector := m.requestInspector
	m.extMu.Unlock()
	if inspector != nil {
		req := APIRequest{Body: decoded, Header: r.Header.Clone()}
		if err := inspector(req); err != nil {
			m.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	cassetteID, ok := extractCassetteID(r.URL.Path)
	if !ok {
		m.writeError(w, http.StatusBadRequest, fmt.Sprintf("no cassette id in path %q", r.URL.Path))
		return
	}

	// E3: Check error response override before reading any content
	m.extMu.Lock()
	errorStatus := m.errorResponses[cassetteID]
	m.extMu.Unlock()
	if errorStatus != 0 {
		m.writeError(w, errorStatus, fmt.Sprintf("error response for cassette %q", cassetteID))
		return
	}

	// Sequence lookup (existing)
	effectiveID := cassetteID
	m.seqMu.Lock()
	if seq, hasSeq := m.seqDef[cassetteID]; hasSeq {
		idx := m.seqIdx[cassetteID]
		if idx >= len(seq) {
			m.seqMu.Unlock()
			m.writeError(w, http.StatusBadRequest,
				fmt.Sprintf("cassette sequence %q exhausted after %d requests", cassetteID, len(seq)))
			return
		}
		effectiveID = seq[idx]
		m.seqIdx[cassetteID]++
	}
	m.seqMu.Unlock()

	// E1: Check inline response before file-based cassette
	m.extMu.Lock()
	inlineContent, inlineOK := m.inlineResponses[cassetteID]
	m.extMu.Unlock()
	if inlineOK {
		// Determine Content-Type (E4 applies here too)
		m.extMu.Lock()
		contentType := "text/event-stream"
		if ct, ok := m.contentTypes[cassetteID]; ok && ct != "" {
			contentType = ct
		}
		m.extMu.Unlock()

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write([]byte(inlineContent))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}

	// File-based cassette read
	cassettePath := filepath.Join(m.CassetteDir, effectiveID+".sse")
	cassetteData, err := os.ReadFile(cassettePath)
	if err != nil {
		m.writeError(w, http.StatusBadRequest, fmt.Sprintf("cassette not found: %s: %v", cassettePath, err))
		return
	}

	// E4: Content-Type for file-based cassette
	m.extMu.Lock()
	contentType := "text/event-stream"
	if ct, ok := m.contentTypes[cassetteID]; ok && ct != "" {
		contentType = ct
	}
	m.extMu.Unlock()

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	_, _ = w.Write(cassetteData)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (m *MockServer) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// extractCassetteID extracts the cassette id from a path containing
// /cassette/<id>/v1/messages.
func extractCassetteID(path string) (string, bool) {
	const prefix = "/cassette/"
	const suffix = "/v1/messages"

	_, after, ok := strings.Cut(path, prefix)
	if !ok {
		return "", false
	}

	rest := after
	id, _, ok := strings.Cut(rest, suffix)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}

// NewTestServer creates a test server for the given cassetteID.
// It performs the following steps:
//  1. Calls Lookup(cassetteID) to resolve the file path; panics if not found.
//  2. Calls NewMockServer(WithCassetteDir(dir)) where dir is the parent directory of the cassette.
//  3. Calls t.Setenv("ANTHROPIC_BASE_URL", ms.URL()) and t.Setenv("OPENAI_BASE_URL", ms.URL()).
//  4. Calls t.Cleanup(ms.Close).
//  5. Applies all opts in order.
//  6. Returns ms.
func NewTestServer(t *testing.T, cassetteID string, opts ...Option) *MockServer {
	// Step 1: Lookup cassette
	path, err := Lookup(cassetteID)
	if err != nil {
		panic("NewTestServer: " + err.Error())
	}
	// Step 2: Extract the testdata base directory.
	// path is ".../testdata/{provider}/{name}.{sse,json}".
	// cassetteID is "{provider}/{name}".
	// We need cassetteDir to be the testdata dir so that
	// filepath.Join(cassetteDir, cassetteID+".sse") resolves correctly.
	cassetteDir := filepath.Dir(filepath.Dir(path)) // go up from file to provider dir, then up to testdata
	// Step 3+4: Create server with t.Setenv and t.Cleanup
	ms := NewMockServer(WithCassetteDir(cassetteDir))
	t.Setenv("ANTHROPIC_BASE_URL", ms.URL())
	t.Setenv("OPENAI_BASE_URL", ms.URL())
	t.Cleanup(ms.Close)
	// Step 5: Apply options
	for _, opt := range opts {
		opt(ms)
	}
	// Step 6: Return
	return ms
}
