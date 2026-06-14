// Package portal provides a sidecar HTTP/SSE server for the Jenny WebUI Portal.
package portal

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// webuiDist is the embedded webui build output.
//
//go:embed webui/dist
var webuiDist embed.FS

// embedFS is the filesystem used for serving static files.
var embedFS fs.FS = webuiDist

// SetEmbedFS sets the filesystem for static file serving (for testing).
func SetEmbedFS(fs fs.FS) {
	embedFS = fs
}

// getSubFS returns a sub-filesystem for webui/dist.
func getSubFS() (fs.FS, error) {
	sub, err := fs.Sub(webuiDist, "webui/dist")
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// Portal represents the WebUI Portal sidecar server.
type Portal struct {
	port        int
	authToken   string
	server      *http.Server
	idleTimer   *time.Timer
	idleTimeout time.Duration
	mu          sync.Mutex
	lastAccess  time.Time
	lockPath    string
	lockFile    *os.File
	pid         int
	exitFunc    func() // injectable exit function for testing
}

// LockfileData represents the lockfile contents.
type LockfileData struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	AuthToken string `json:"auth_token"`
}

// Start creates and starts a new Portal server.
func Start(ctx context.Context, jennyDir string) (*Portal, error) {
	return startWithConfig(ctx, jennyDir, 10*time.Minute)
}

// startWithConfigForTest creates a portal with injectable exit function (for testing).
func startWithConfigForTest(ctx context.Context, jennyDir string, idleTimeout time.Duration, exitFunc func()) (*Portal, error) {
	p, err := startWithConfig(ctx, jennyDir, idleTimeout)
	if err != nil {
		return nil, err
	}
	p.exitFunc = exitFunc
	return p, nil
}

// startWithConfig creates and starts a portal with a custom idle timeout (for testing).
func startWithConfig(ctx context.Context, jennyDir string, idleTimeout time.Duration) (*Portal, error) {
	lockPath := filepath.Join(jennyDir, "portal.lock")

	// Generate auth token (64 hex chars from 32 random bytes)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generating auth token: %w", err)
	}
	authToken := hex.EncodeToString(tokenBytes)

	// Find a random high port
	port, err := findAvailablePort(33669)
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	p := &Portal{
		port:        port,
		authToken:   authToken,
		idleTimeout: idleTimeout,
		lockPath:    lockPath,
		pid:         os.Getpid(),
		lastAccess:  time.Now(),
	}

	// Set up HTTP server
	mux := http.NewServeMux()
	p.setupRoutes(mux)

	p.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	// Start the server
	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("portal server error: %v", err)
		}
	}()

	// Wait for server to be ready
	for i := 0; i < 50; i++ {
		conn, err := net.DialTimeout("tcp", p.server.Addr, 10*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Write lockfile with flock to prevent TOCTOU race
	if err := p.writeLockfileWithLock(); err != nil {
		p.server.Shutdown(context.Background())
		return nil, err
	}

	// Start idle timer
	p.resetIdleTimer()
	go p.runIdleMonitor(ctx)

	return p, nil
}

// writeLockfileWithLock writes the lockfile and acquires an exclusive flock.
// This prevents the TOCTOU race between checking and writing the lockfile.
// If another portal is running, it will be detected here.
func (p *Portal) writeLockfileWithLock() error {
	lf := LockfileData{
		PID:       p.pid,
		Port:      p.port,
		AuthToken: p.authToken,
	}

	data, err := json.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling lockfile: %w", err)
	}

	// Try to acquire exclusive lock on the lockfile path
	// This will fail if another process has it locked
	lockFile, err := flock(p.lockPath)
	if err != nil {
		// Another process has the lockfile locked - portal is running
		// Read existing lockfile to get port for error message
		if existingData, readErr := os.ReadFile(p.lockPath); readErr == nil {
			var existingLF LockfileData
			if json.Unmarshal(existingData, &existingLF) == nil {
				return fmt.Errorf("portal already running on port %d", existingLF.Port)
			}
		}
		return fmt.Errorf("portal already running")
	}

	// We have the lock - check if it's stale or fresh
	// Use the ALREADY OPEN lockFile to read data, avoiding the "sharing violation" on Windows.
	var existingLF LockfileData
	decoder := json.NewDecoder(lockFile)
	if err := decoder.Decode(&existingLF); err == nil {
		// Check if the existing PID is alive
		if isProcessAlive(existingLF.PID) {
			lockFile.Close()
			return fmt.Errorf("portal already running on port %d", existingLF.Port)
		}
	}

	// Write directly to the locked file.
	// We don't use os.Rename here because it fails on Windows when the destination is open.
	if err := lockFile.Truncate(0); err != nil {
		lockFile.Close()
		return fmt.Errorf("truncating lockfile: %w", err)
	}
	if _, err := lockFile.Seek(0, 0); err != nil {
		lockFile.Close()
		return fmt.Errorf("seeking lockfile: %w", err)
	}
	if _, err := lockFile.Write(data); err != nil {
		lockFile.Close()
		return fmt.Errorf("writing lockfile: %w", err)
	}

	// Store the lock handle so it's held for the duration of the portal process.
	// It will be closed in p.Shutdown().
	p.lockFile = lockFile
	return nil
}

// Port returns the port the server is listening on.
func (p *Portal) Port() int {
	return p.port
}

// AuthToken returns the auth token.
func (p *Portal) AuthToken() string {
	return p.authToken
}

// Shutdown gracefully shuts down the portal server.
func (p *Portal) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop idle timer
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}

	// Remove URL file first
	urlPath := p.lockPath[:len(p.lockPath)-len("portal.lock")] + "portal.url"
	os.Remove(urlPath)

	// Close and remove lockfile
	if p.lockFile != nil {
		p.lockFile.Close()
	}
	os.Remove(p.lockPath)

	// Shutdown server
	return p.server.Shutdown(ctx)
}

// PortalURLFile returns the path to the portal URL file.
func (p *Portal) PortalURLFile() string {
	return p.lockPath[:len(p.lockPath)-len("portal.lock")] + "portal.url"
}

// WritePortalURLFile writes the portal URL to the URL file for non-interactive mode.
// This is used by cmd/jenny/portal.go when !isInteractive().
func (p *Portal) WritePortalURLFile() error {
	url := fmt.Sprintf("http://127.0.0.1:%d?token=%s", p.port, p.authToken)
	return os.WriteFile(p.PortalURLFile(), []byte(url+"\n"), 0644)
}

// resetIdleTimer resets the idle timeout timer.
func (p *Portal) resetIdleTimer() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastAccess = time.Now()
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}
	p.idleTimer = time.AfterFunc(p.idleTimeout, func() {
		// Timer expired - exit
		// Clean up URL file first, then lockfile
		urlPath := p.lockPath[:len(p.lockPath)-len("portal.lock")] + "portal.url"
		os.Remove(urlPath)
		os.Remove(p.lockPath)
		if p.exitFunc != nil {
			p.exitFunc()
		} else {
			os.Exit(0)
		}
	})
}

// runIdleMonitor monitors the last access time and triggers shutdown.
func (p *Portal) runIdleMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			idle := time.Since(p.lastAccess)
			p.mu.Unlock()
			if idle >= p.idleTimeout {
				// Clean up URL file first, then lockfile
				urlPath := p.lockPath[:len(p.lockPath)-len("portal.lock")] + "portal.url"
				os.Remove(urlPath)
				os.Remove(p.lockPath)
				if p.exitFunc != nil {
					p.exitFunc()
				} else {
					os.Exit(0)
				}
				return
			}
		}
	}
}

	// findAvailablePort finds an available port starting from the given port.
	func findAvailablePort(start int) (int, error) {
		for port := start; port < 65535; port++ {
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				ln.Close()
				return port, nil
			}
		}
		return 0, fmt.Errorf("no available port found")
	}
