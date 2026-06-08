// Package tool provides tool implementations.
package tool

import (
	"sync"
	"time"
)

// ReadFileEntry represents a cached read state for a file.
type ReadFileEntry struct {
	Path       string
	Content    string
	Mtime      time.Time
	IsFullRead bool
	Offset     int
	Limit      int
}

// ReadFileCache tracks file read state for the read-before-write contract.
type ReadFileCache struct {
	mu      sync.Mutex
	entries map[string]*ReadFileEntry
}

// NewReadFileCache creates a new ReadFileCache.
func NewReadFileCache() *ReadFileCache {
	return &ReadFileCache{
		entries: make(map[string]*ReadFileEntry),
	}
}

// RecordRead records a file read operation.
func (c *ReadFileCache) RecordRead(path, content string, mtime time.Time, isFullRead bool, offset, limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[path] = &ReadFileEntry{
		Path:       path,
		Content:    content,
		Mtime:      mtime,
		IsFullRead: isFullRead,
		Offset:     offset,
		Limit:      limit,
	}
}

// GetRead returns the cached read entry for a path and whether it exists.
func (c *ReadFileCache) GetRead(path string) (*ReadFileEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[path]
	return entry, ok
}

// Remove removes the cached entry for a path.
func (c *ReadFileCache) Remove(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, path)
}

// UpdateAfterWrite updates the cache after a successful write.
func (c *ReadFileCache) UpdateAfterWrite(path, content string, mtime time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[path] = &ReadFileEntry{
		Path:       path,
		Content:    content,
		Mtime:      mtime,
		IsFullRead: true,
		Offset:     0,
		Limit:      0,
	}
}

// Add adds a pre-seeded entry to the cache (used for resume seeding from transcript).
func (c *ReadFileCache) Add(path, content string, mtime time.Time, isFullRead bool, offset, limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[path] = &ReadFileEntry{
		Path:       path,
		Content:    content,
		Mtime:      mtime,
		IsFullRead: isFullRead,
		Offset:     offset,
		Limit:      limit,
	}
}
