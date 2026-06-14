// Package router provides multi-provider routing with three-layer fallback logic.
package router

import (
	"sync"
	"time"
)

const (
	// DefaultCooldownDuration is the default cooldown period after a failure.
	DefaultCooldownDuration = 30 * time.Second
	// DefaultMaxFailures is the default consecutive failure threshold.
	DefaultMaxFailures = 3
)

// HealthRegistry tracks health status and cooldown periods for endpoints.
type HealthRegistry struct {
	mu          sync.RWMutex
	cooldowns   map[string]time.Time // key -> cooldown expiry
	failures    map[string]int       // key -> consecutive failures
	cooldownDur time.Duration
	maxFailures int
}

// NewHealthRegistry creates a new HealthRegistry with default settings.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{
		cooldowns:   make(map[string]time.Time),
		failures:    make(map[string]int),
		cooldownDur: DefaultCooldownDuration,
		maxFailures: DefaultMaxFailures,
	}
}

// endpointKey generates a unique key for an endpoint.
func endpointKey(provider, account, model, key string) string {
	return provider + ":" + account + ":" + model + ":" + key
}

// IsHealthy checks if an endpoint is healthy (not in cooldown and under failure threshold).
func (h *HealthRegistry) IsHealthy(provider, account, model, apiKey string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	key := endpointKey(provider, account, model, apiKey)

	// Check cooldown
	if expiry, ok := h.cooldowns[key]; ok && time.Now().Before(expiry) {
		return false
	}

	// Check failure count
	if h.failures[key] >= h.maxFailures {
		return false
	}

	return true
}

// RecordFailure records a failure for an endpoint.
func (h *HealthRegistry) RecordFailure(provider, account, model, apiKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := endpointKey(provider, account, model, apiKey)
	h.failures[key]++
	if h.failures[key] >= h.maxFailures {
		h.cooldowns[key] = time.Now().Add(h.cooldownDur)
	}
}

// RecordSuccess records a successful call for an endpoint.
func (h *HealthRegistry) RecordSuccess(provider, account, model, apiKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := endpointKey(provider, account, model, apiKey)
	h.failures[key] = 0
	delete(h.cooldowns, key)
}

// SetCooldown sets a cooldown period for an endpoint (e.g., from Retry-After header).
func (h *HealthRegistry) SetCooldown(provider, account, model, apiKey string, dur time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := endpointKey(provider, account, model, apiKey)
	h.cooldowns[key] = time.Now().Add(dur)
}

// Reset clears all health tracking data.
func (h *HealthRegistry) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.cooldowns = make(map[string]time.Time)
	h.failures = make(map[string]int)
}

// GetFailureCount returns the current failure count for an endpoint.
func (h *HealthRegistry) GetFailureCount(provider, account, model, apiKey string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	key := endpointKey(provider, account, model, apiKey)
	return h.failures[key]
}
