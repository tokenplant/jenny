package tool

import (
	"container/list"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/ipy/jenny/internal/constants"
)

// WebFetch limits.
const (
	webFetchMaxBodyBytes     = 10 * 1024 * 1024 // 10 MB (AC1)
	webFetchMaxMarkdownChars = 100000           // 100K chars (AC2)
	webFetchTimeout          = 60 * time.Second // request timeout
	webFetchMaxRedirects     = 10               // max redirect hops
	webFetchMaxURLLength     = 2000             // max URL length
	webFetchBlocklistTimeout = 10 * time.Second // DNS resolution timeout (AC3)
	webFetchCacheSize        = 50 * 1024 * 1024 // 50 MB LRU cache
	webFetchCacheTTL         = 15 * time.Minute // cache TTL
	webFetchHostnameTTL      = 5 * time.Minute  // hostname blocklist cache TTL
)

// redirectError signals a cross-host redirect — the caller should instruct the
// model to re-fetch at the target URL rather than following the redirect.
type redirectError struct {
	targetURL string
}

func (e *redirectError) Error() string {
	return fmt.Sprintf("cross-host redirect to %s", e.targetURL)
}

// fetchCacheEntry holds a cached response.
type fetchCacheEntry struct {
	key       string
	result    *ToolResult
	size      int
	expiresAt time.Time
	element   *list.Element
}

// fetchCache is an LRU cache with size-based eviction and TTL expiry.
type fetchCache struct {
	mu          sync.Mutex
	ll          *list.List
	entries     map[string]*fetchCacheEntry
	maxSize     int
	currentSize int
	ttl         time.Duration
}

func newFetchCache(maxSize int, ttl time.Duration) *fetchCache {
	return &fetchCache{
		ll:      list.New(),
		entries: make(map[string]*fetchCacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *fetchCache) get(key string) (*ToolResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		// Expired — remove.
		c.removeEntry(entry)
		return nil, false
	}
	// Move to front (most recently used).
	c.ll.MoveToFront(entry.element)
	return entry.result, true
}

func (c *fetchCache) set(key string, result *ToolResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If already exists, remove old entry first.
	if old, ok := c.entries[key]; ok {
		c.removeEntry(old)
	}

	size := len(result.Content)
	entry := &fetchCacheEntry{
		key:       key,
		result:    result,
		size:      size,
		expiresAt: time.Now().Add(c.ttl),
	}
	entry.element = c.ll.PushFront(entry)
	c.entries[key] = entry
	c.currentSize += size

	// Evict until under limit.
	for c.currentSize > c.maxSize {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.removeEntry(back.Value.(*fetchCacheEntry))
	}
}

func (c *fetchCache) removeEntry(entry *fetchCacheEntry) {
	c.ll.Remove(entry.element)
	delete(c.entries, entry.key)
	c.currentSize -= entry.size
}

// hostnameCache caches blocklist results per hostname.
type hostnameCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
}

func newHostnameCache(ttl time.Duration) *hostnameCache {
	return &hostnameCache{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// isBlocked returns true if this hostname is cached as blocked and the entry
// has not expired.
func (c *hostnameCache) isBlocked(hostname string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	expiry, ok := c.entries[hostname]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(c.entries, hostname)
		return false
	}
	return true
}

// markBlocked caches the hostname as blocked until the TTL elapses.
func (c *hostnameCache) markBlocked(hostname string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[hostname] = time.Now().Add(c.ttl)
}

// WebFetchTool fetches URL content and converts HTML to markdown.
type WebFetchTool struct {
	mu            sync.Mutex
	responseCache *fetchCache    // 15 min / 50 MB LRU
	hostnameCache *hostnameCache // 5 min TTL
	skipBlocklist bool           // testing-only: bypass blocklist check
}

// NewWebFetchTool creates a new WebFetchTool.
func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		responseCache: newFetchCache(webFetchCacheSize, webFetchCacheTTL),
		hostnameCache: newHostnameCache(webFetchHostnameTTL),
	}
}

// WithSkipBlocklist disables the blocklist check. Used in tests with httptest servers.
func (t *WebFetchTool) WithSkipBlocklist() *WebFetchTool {
	t.skipBlocklist = true
	return t
}

// Name returns the tool name.
func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

// Description returns a description of the tool.
func (t *WebFetchTool) Description() string {
	return "Fetch content from a URL. Converts HTML to markdown. Caches responses for 15 minutes. " +
		"Supports HTTP and HTTPS URLs only. Binary content is saved to disk and the path is returned. " +
		"Warning: URLs with embedded credentials (user:password@host) are rejected for security reasons."
}

// InputSchema returns the JSON schema for tool input.
func (t *WebFetchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "HTTP(S) URL to fetch (max 2000 characters)",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Optional prompt to apply to fetched content for summarization",
			},
		},
		"required": []string{"url"},
	}
}

// Execute fetches the URL and returns the content.
func (t *WebFetchTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// Extract URL parameter.
	urlStr, ok := input["url"].(string)
	if !ok || urlStr == "" {
		return nil, fmt.Errorf("url is required and must be a string")
	}

	// URL length check.
	if len(urlStr) > webFetchMaxURLLength {
		return &ToolResult{
			Content: fmt.Sprintf("URL exceeds the maximum length of %d characters", webFetchMaxURLLength),
			IsError: true,
		}, nil
	}

	// Parse URL.
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Invalid URL: %v", err),
			IsError: true,
		}, nil
	}

	// Scheme validation.
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &ToolResult{
			Content: fmt.Sprintf("Unsupported URL scheme '%s'. Only http and https are supported.", parsedURL.Scheme),
			IsError: true,
		}, nil
	}

	// AC5: Reject credentials in URL.
	if parsedURL.User != nil {
		return &ToolResult{
			Content: "URL must not contain embedded credentials (user:password@host). This is a security measure.",
			IsError: true,
		}, nil
	}

	hostname := parsedURL.Hostname()

	// AC3: Blocklist preflight — also returns resolved IPs to pin in dialer.
	resolvedAddrs, blErr := t.checkBlocklist(ctx, hostname)
	if blErr != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Access to '%s' is blocked: %v", hostname, blErr),
			IsError: true,
		}, nil
	}

	// Check cache.
	if cached, ok := t.responseCache.get(urlStr); ok {
		return cached, nil
	}

	// Fetch with pinned addresses to prevent DNS rebinding.
	result, err := t.fetch(ctx, parsedURL, urlStr, resolvedAddrs)
	if err != nil {
		// Check if it's a redirectError — return as non-error instruction.
		var redirErr *redirectError
		if errors.As(err, &redirErr) { //nolint:errorsastype // redirectError is a *struct, not an interface target
			result = &ToolResult{
				Content: fmt.Sprintf(
					"The URL redirected to a different host. Re-fetch with `url`: %s",
					redirErr.targetURL,
				),
				IsError: false,
			}
			return result, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error fetching URL: %v", err),
			IsError: true,
		}, nil
	}

	// Cache and return.
	t.responseCache.set(urlStr, result)
	return result, nil
}

// fetch performs the HTTP request and processes the response.
// pinnedAddrs are the pre-resolved IP addresses from the blocklist check;
// using them in a custom dialer prevents DNS rebinding attacks.
func (t *WebFetchTool) fetch(ctx context.Context, parsedURL *url.URL, urlStr string, pinnedAddrs []string) (*ToolResult, error) {
	// Build transport with pinned dialer to prevent DNS rebinding SSRF.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if len(pinnedAddrs) > 0 {
		transport.DialContext = func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			_, port, _ := net.SplitHostPort(addr)
			// Use the first resolved address from blocklist check
			pinnedAddr := net.JoinHostPort(pinnedAddrs[0], port)
			return (&net.Dialer{Timeout: 30 * time.Second}).DialContext(dialCtx, network, pinnedAddr)
		}
	}

	// Create HTTP client with redirect handling.
	client := &http.Client{
		Timeout:   webFetchTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Max redirect hop limit.
			if len(via) >= webFetchMaxRedirects {
				return fmt.Errorf("too many redirects (max %d)", webFetchMaxRedirects)
			}
			// AC4: Cross-host redirect — do not follow.
			if req.URL.Host != parsedURL.Host {
				return &redirectError{targetURL: req.URL.String()}
			}
			return nil // follow same-host redirects
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	// Set a reasonable User-Agent.
	req.Header.Set("User-Agent", "jenny-web-fetch/1.0")

	resp, err := client.Do(req)
	if err != nil {
		// redirectError will be unwrapped and returned as a non-error ToolResult in Execute.
		return nil, err
	}
	defer resp.Body.Close()

	// AC1: Limit response body to 10 MB + 1 byte for overflow detection.
	limitedBody := io.LimitReader(resp.Body, webFetchMaxBodyBytes+1)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if len(body) > webFetchMaxBodyBytes {
		return &ToolResult{
			Content: fmt.Sprintf("Response body exceeds the %d MB limit. Consider a more specific URL.", webFetchMaxBodyBytes/(1024*1024)),
			IsError: true,
		}, nil
	}

	// Determine content type.
	contentType := resp.Header.Get("Content-Type")
	contentType = strings.Split(contentType, ";")[0] // strip charset etc.
	contentType = strings.TrimSpace(contentType)

	// Check if this is HTML content.
	if contentType == "text/html" || contentType == "application/xhtml+xml" || contentType == "" {
		// Treat empty Content-Type as HTML (common for web pages).
		markdown, truncated := t.convertHTMLToMarkdown(string(body))
		result := &ToolResult{
			Content:   markdown,
			IsError:   false,
			Truncated: truncated,
		}
		return result, nil
	}

	// Binary content — save to disk.
	savedPath, err := t.saveBinaryToDisk(body)
	if err != nil {
		return nil, fmt.Errorf("saving binary content: %w", err)
	}

	contentSummary := fmt.Sprintf(
		"Content-Type: %s\nSize: %d bytes\nSaved to: %s",
		contentType, len(body), savedPath,
	)
	return &ToolResult{
		Content: contentSummary,
		IsError: false,
	}, nil
}

// convertHTMLToMarkdown converts HTML to markdown and caps the output at
// webFetchMaxMarkdownChars characters (AC2).
func (t *WebFetchTool) convertHTMLToMarkdown(htmlInput string) (string, bool) {
	markdown, err := htmltomarkdown.ConvertString(htmlInput)
	if err != nil {
		// Fallback: return the raw HTML if conversion fails.
		markdown = htmlInput
	}

	if len(markdown) > webFetchMaxMarkdownChars {
		// AC2: Cap at 100K chars (rune-safe truncation).
		note := fmt.Sprintf("\n\n[Content truncated at %d characters]", webFetchMaxMarkdownChars)
		runes := []rune(markdown)
		maxRunes := webFetchMaxMarkdownChars - len(note)
		if maxRunes > len(runes) {
			maxRunes = len(runes)
		}
		truncated := string(runes[:maxRunes])
		return truncated + note, true
	}
	return markdown, false
}

// saveBinaryToDisk saves binary data to ~/.jenny/fetched/ and returns the path.
func (t *WebFetchTool) saveBinaryToDisk(data []byte) (string, error) {
	// Generate unique filename.
	var randSuffix [8]byte
	if _, err := rand.Read(randSuffix[:]); err != nil {
		return "", fmt.Errorf("generating random suffix: %w", err)
	}
	filename := fmt.Sprintf("fetch-%d-%016x.bin", time.Now().UnixNano(), randSuffix)

	fetchDir := filepath.Join(constants.JennyHomeDir(), "fetched")
	if err := os.MkdirAll(fetchDir, 0755); err != nil {
		return "", fmt.Errorf("creating fetch directory: %w", err)
	}

	filePath := filepath.Join(fetchDir, filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("writing binary to disk: %w", err)
	}
	return filePath, nil
}

// checkBlocklist verifies that a hostname is not on the blocklist (AC3).
// It checks hardcoded SSRF targets and resolves DNS to detect private/loopback ranges.
// Returns the resolved IP addresses (if any) for use in a pinned dialer to prevent DNS rebinding.
func (t *WebFetchTool) checkBlocklist(ctx context.Context, hostname string) ([]string, error) {
	if t.skipBlocklist {
		return nil, nil
	}

	// Strip IPv6 brackets.
	hostname = strings.Trim(hostname, "[]")

	// Fast-path: check hostname cache.
	if t.hostnameCache.isBlocked(hostname) {
		return nil, fmt.Errorf("hostname is blocked (cached)")
	}

	// Hardcoded blocklist for common SSRF targets.
	lower := strings.ToLower(hostname)
	hardcodedBlocked := map[string]bool{
		"localhost":                true,
		"127.0.0.1":                true,
		"127.0.1.1":                true,
		"0.0.0.0":                  true,
		"::1":                      true,
		"metadata.google.internal": true,
		"metadata.internal":        true,
	}
	if hardcodedBlocked[lower] {
		t.hostnameCache.markBlocked(hostname)
		return nil, fmt.Errorf("access to loopback/localhost addresses is not allowed")
	}

	// Check if it's an IP literal.
	if ip := net.ParseIP(hostname); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			t.hostnameCache.markBlocked(hostname)
			return nil, fmt.Errorf("access to loopback, private, or link-local addresses is not allowed")
		}
		// Public IP — allowed.
		return []string{hostname}, nil
	}

	// Resolve DNS with a 10s timeout context.
	resolveCtx, cancel := context.WithTimeout(ctx, webFetchBlocklistTimeout)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupHost(resolveCtx, hostname)
	if err != nil {
		// DNS resolution failure — allow the fetch; the HTTP request will fail
		// naturally if the host is unreachable.
		return nil, nil
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			t.hostnameCache.markBlocked(hostname)
			return nil, fmt.Errorf("access to addresses resolving to loopback, private, or link-local ranges is not allowed")
		}
	}

	return addrs, nil
}
