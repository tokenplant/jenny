// Package router provides multi-provider routing with three-layer fallback logic.
// Public API: Init, SelectEndpoint, ReportError, BindSticky, ClearSticky, IsInitialized.
package router

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/ipy/jenny/internal/log"
)

// ErrNoProviders indicates no providers are configured.
var ErrNoProviders = fmt.Errorf("no providers configured")

// ErrAllProvidersExhausted indicates all configured providers have failed.
var ErrAllProvidersExhausted = fmt.Errorf("all providers exhausted")

// ActiveEndpoint represents the currently selected endpoint for a request.
type ActiveEndpoint struct {
	Provider     string
	Model        string
	APIKey       string
	BaseURL      string
	ProtocolType string // openai, anthropic, gemini
	Account      string
	Profile      string
}

// Router handles routing logic with three-layer fallback support.
type Router struct {
	config         *Config
	healthRegistry *HealthRegistry
	sessions       map[string]*SessionState
	mu             sync.RWMutex
	profileName    string
	rrCounter      atomic.Uint64 // monotonically increasing, used by cross-session round-robin
}

// SessionState holds the sticky state for a session.
type SessionState struct {
	Endpoint    *ActiveEndpoint
	TargetIndex int // Current position in target chain
	KeyIndex    int // Current position in keys list (for round-robin)
}

// Global router instance
var (
	globalRouter *Router
	routerOnce   sync.Once
	routerErr    error
)

// Init initializes the global router from a YAML config file.
// Falls back to environment variables if no config file exists.
// This function is idempotent - subsequent calls are no-ops.
func Init(cfgPath string) error {
	routerOnce.Do(func() {
		path := cfgPath
		if path == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				routerErr = fmt.Errorf("failed to get home directory: %w", err)
				return
			}
			path = filepath.Join(home, ".jenny", "routes.yaml")
		}

		cfg, err := LoadConfig(path)
		if err != nil {
			routerErr = fmt.Errorf("failed to load config: %w", err)
			return
		}

		// Fall back to environment variables if no config
		if cfg == nil {
			log.Debug("No router config found, synthesizing from environment variables")
			cfg = SynthesizeConfigFromEnv()
		}

		if cfg != nil && len(cfg.Providers) == 0 {
			cfg = SynthesizeConfigFromEnv()
		}

		if cfg == nil || len(cfg.Providers) == 0 {
			routerErr = ErrNoProviders
			return
		}

		// Ensure default profile exists
		if _, ok := cfg.Profiles["default"]; !ok {
			defaultAllow := true
			cfg.Profiles["default"] = Profile{
				RoutingMode:     "sticky",
				SelectionPolicy: "round_robin",
				RetryPolicy: RetryPolicy{
					MaxRetries: 3,
					Backoff:    "exponential",
				},
				AllowFallback: &defaultAllow,
			}
		}

		globalRouter = &Router{
			config:         cfg,
			healthRegistry: NewHealthRegistry(),
			sessions:       make(map[string]*SessionState),
			profileName:    "default",
		}
		log.Debug("Router initialized", "providers", len(cfg.Providers), "profiles", len(cfg.Profiles))
	})
	return routerErr
}

// NewRouter creates a new Router instance with the given config.
func NewRouter(cfg *Config) *Router {
	if cfg == nil {
		cfg = &Config{
			Profiles: make(map[string]Profile),
		}
	}
	return &Router{
		config:         cfg,
		healthRegistry: NewHealthRegistry(),
		sessions:       make(map[string]*SessionState),
		profileName:    "default",
	}
}

// nextRoundRobinIndex atomically advances and returns the next index, bounded
// by the given length. This is the spec's "Cross-Session" round-robin: each
// SelectEndpoint call gets a distinct position in the candidate pool so a
// single key is never preferred by the selection itself.
func (r *Router) nextRoundRobinIndex(n int) int {
	if n <= 0 {
		return 0
	}
	idx := r.rrCounter.Add(1) - 1
	return int(idx % uint64(n))
}

// GetRouter returns the global router instance.
func GetRouter() *Router {
	return globalRouter
}

// SelectEndpoint selects an endpoint for the given session using three-layer routing.
// L1: Return sticky endpoint if session is already bound (and routing_mode is sticky).
// L2: Round-robin across keys within same account for same model.
// L3: Walk profile's target chain to find matching model.
func (r *Router) SelectEndpoint(sessionID string) (*ActiveEndpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	profile, ok := r.config.Profiles[r.profileName]
	if !ok {
		return nil, fmt.Errorf("profile not found: %s", r.profileName)
	}

	state, hasState := r.sessions[sessionID]

	// L1: Check sticky session binding — only honored in "sticky" routing mode.
	// In "balanced" mode, every call re-evaluates from the candidate pool,
	// intentionally sacrificing prompt-cache continuity for load distribution.
	if hasState && state.Endpoint != nil && profile.RoutingMode == "sticky" {
		return state.Endpoint, nil
	}

	// Find the next target in the chain
	targetIndex := 0
	if hasState {
		targetIndex = state.TargetIndex
	}

	endpoint, err := r.findEndpointForTargetLocked(profile, targetIndex, sessionID)
	if err != nil {
		return nil, err
	}

	// Create or update session state
	if !hasState {
		state = &SessionState{
			Endpoint:    endpoint,
			TargetIndex: targetIndex,
			KeyIndex:    0,
		}
		r.sessions[sessionID] = state
	} else {
		state.Endpoint = endpoint
		state.TargetIndex = targetIndex
	}

	return endpoint, nil
}

// findEndpointForTargetLocked finds an endpoint matching the given target index (must hold lock).
func (r *Router) findEndpointForTargetLocked(profile Profile, targetIndex int, sessionID string) (*ActiveEndpoint, error) {
	target := Target{Match: MatchClause{}}
	if targetIndex < len(profile.Targets) {
		target = profile.Targets[targetIndex]
	}

	candidates := r.findCandidates(target.Match)
	if len(candidates) == 0 {
		return nil, ErrNoProviders
	}

	var endpoint *ActiveEndpoint
	switch profile.SelectionPolicy {
	case "round_robin":
		endpoint = r.selectRoundRobinLocked(candidates, sessionID)
	case "random":
		endpoint = r.selectRandomLocked(candidates, sessionID)
	default:
		endpoint = r.selectRoundRobinLocked(candidates, sessionID)
	}

	return endpoint, nil
}

// CandidateEndpoint represents a potential endpoint for routing.
type CandidateEndpoint struct {
	Provider Provider
	Account  Account
	Model    Model
	APIKey   string
}

// findCandidates finds all provider/model combinations matching the target.
func (r *Router) findCandidates(match MatchClause) []CandidateEndpoint {
	var candidates []CandidateEndpoint

	for _, provider := range r.config.Providers {
		for _, model := range provider.Models {
			if r.modelMatches(model, match) {
				for _, account := range provider.Accounts {
					for _, key := range account.Keys {
						if key != "" && r.healthRegistry.IsHealthy(provider.Name, account.Name, model.Name, key) {
							candidates = append(candidates, CandidateEndpoint{
								Provider: provider,
								Account:  account,
								Model:    model,
								APIKey:   key,
							})
						}
					}
				}
			}
		}
	}

	return candidates
}

// modelMatches checks if a model matches the given match criteria.
func (r *Router) modelMatches(model Model, match MatchClause) bool {
	// Match by model name (provider:model format)
	if slices.Contains(match.Models, model.Name) {
		return true
	}

	// Match by tags
	if len(match.Tags) > 0 {
		for _, tag := range match.Tags {
			if slices.Contains(model.Tags, tag) {
				return true
			}
		}
	}

	// If no explicit match criteria, match all models
	if len(match.Models) == 0 && len(match.Tags) == 0 {
		return true
	}

	return false
}

// selectRoundRobinLocked selects using a monotonic counter so that each call
// returns a different position in the candidate pool (true cross-session
// load distribution). The lock MUST be held by the caller.
func (r *Router) selectRoundRobinLocked(candidates []CandidateEndpoint, _ string) *ActiveEndpoint {
	if len(candidates) == 0 {
		return nil
	}

	idx := r.nextRoundRobinIndex(len(candidates))

	c := candidates[idx]
	return &ActiveEndpoint{
		Provider:     c.Provider.Name,
		Model:        c.Model.Name,
		APIKey:       c.APIKey,
		BaseURL:      c.Provider.BaseURL,
		ProtocolType: c.Provider.Type,
		Account:      c.Account.Name,
		Profile:      r.profileName,
	}
}

// selectRandomLocked selects a candidate randomly based on session ID (must hold lock).
func (r *Router) selectRandomLocked(candidates []CandidateEndpoint, sessionID string) *ActiveEndpoint {
	if len(candidates) == 0 {
		return nil
	}

	h := fnv.New32a()
	h.Write([]byte(sessionID + "-random"))
	idx := int(h.Sum32()) % len(candidates)

	c := candidates[idx]
	return &ActiveEndpoint{
		Provider:     c.Provider.Name,
		Model:        c.Model.Name,
		APIKey:       c.APIKey,
		BaseURL:      c.Provider.BaseURL,
		ProtocolType: c.Provider.Type,
		Account:      c.Account.Name,
		Profile:      r.profileName,
	}
}

// GetStickyEndpoint returns the current sticky endpoint for a session.
func (r *Router) GetStickyEndpoint(sessionID string) *ActiveEndpoint {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if state, ok := r.sessions[sessionID]; ok {
		return state.Endpoint
	}
	return nil
}

// BindSticky binds a session to a specific endpoint.
func (r *Router) BindSticky(sessionID string, endpoint *ActiveEndpoint) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessions[sessionID] = &SessionState{
		Endpoint: endpoint,
	}
}

// ClearSticky clears the sticky binding for a session.
func (r *Router) ClearSticky(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.sessions, sessionID)
}

// NextEndpoint returns the next endpoint after the current one fails.
func (r *Router) NextEndpoint(sessionID string, current *ActiveEndpoint) (*ActiveEndpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}

	profile, ok := r.config.Profiles[r.profileName]
	if !ok {
		return nil, fmt.Errorf("profile not found: %s", r.profileName)
	}

	// Try to find next key within same model (L2)
	nextKey := r.nextKeyLocked(state, current)
	if nextKey != nil {
		state.Endpoint = nextKey
		state.KeyIndex++
		return nextKey, nil
	}

	// Try to find next target in chain (L3)
	if profile.AllowFallback != nil && *profile.AllowFallback {
		nextTarget := r.nextTargetLocked(state, current)
		if nextTarget != nil {
			state.Endpoint = nextTarget
			state.TargetIndex++
			state.KeyIndex = 0
			return nextTarget, nil
		}
	}

	return nil, ErrAllProvidersExhausted
}

// nextKeyLocked finds the next available key for the same model (must hold lock).
func (r *Router) nextKeyLocked(state *SessionState, current *ActiveEndpoint) *ActiveEndpoint {
	for _, provider := range r.config.Providers {
		if provider.Name != current.Provider {
			continue
		}
		for _, account := range provider.Accounts {
			for i, key := range account.Keys {
				if key != "" && key != current.APIKey && r.healthRegistry.IsHealthy(provider.Name, account.Name, current.Model, key) {
					return &ActiveEndpoint{
						Provider:     provider.Name,
						Model:        current.Model,
						APIKey:       key,
						BaseURL:      provider.BaseURL,
						ProtocolType: provider.Type,
						Account:      account.Name,
						Profile:      r.profileName,
					}
				}
				if i > state.KeyIndex {
					break
				}
			}
		}
	}
	return nil
}

// nextTargetLocked finds the next target in the profile chain (must hold lock).
// The `current` parameter mirrors nextKeyLocked's signature; the implementation
// currently only advances TargetIndex, but keeping the parameter in place lets
// future capability-based filtering (e.g., "current lacks vision → skip ahead
// to a vision-capable model") plug in without changing the call site.
func (r *Router) nextTargetLocked(state *SessionState, _ *ActiveEndpoint) *ActiveEndpoint {
	profile, ok := r.config.Profiles[r.profileName]
	if !ok {
		return nil
	}

	for i := state.TargetIndex + 1; i < len(profile.Targets); i++ {
		target := profile.Targets[i]
		candidates := r.findCandidates(target.Match)
		if len(candidates) > 0 {
			c := candidates[0]
			return &ActiveEndpoint{
				Provider:     c.Provider.Name,
				Model:        c.Model.Name,
				APIKey:       c.APIKey,
				BaseURL:      c.Provider.BaseURL,
				ProtocolType: c.Provider.Type,
				Account:      c.Account.Name,
				Profile:      r.profileName,
			}
		}
	}
	return nil
}

// SetProfile sets the active profile for routing.
func (r *Router) SetProfile(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.config.Profiles[name]; ok {
		r.profileName = name
	}
}

// GetProfile returns the active profile name.
func (r *Router) GetProfile() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.profileName
}

// GetConfig returns the current configuration.
func (r *Router) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.config
}

// getAccountForProvider returns the account name for a provider.
func (r *Router) getAccountForProvider(providerName string) string {
	for _, provider := range r.config.Providers {
		if provider.Name == providerName && len(provider.Accounts) > 0 {
			return provider.Accounts[0].Name
		}
	}
	return "default"
}

// Global convenience functions.

// SelectEndpoint selects an endpoint for the given session using the global router.
func SelectEndpoint(sessionID string) (*ActiveEndpoint, error) {
	if globalRouter == nil {
		return nil, fmt.Errorf("router not initialized")
	}
	return globalRouter.SelectEndpoint(sessionID)
}

// ReportError reports an error for tracking and health management.
func ReportError(sessionID string, err error) {
	if globalRouter == nil {
		return
	}
	if endpoint := globalRouter.GetStickyEndpoint(sessionID); endpoint != nil {
		globalRouter.healthRegistry.RecordFailure(
			endpoint.Provider,
			globalRouter.getAccountForProvider(endpoint.Provider),
			endpoint.Model,
			endpoint.APIKey,
		)
	}
}

// BindSticky binds a session to an endpoint in the global router.
func BindSticky(sessionID string, endpoint *ActiveEndpoint) {
	if globalRouter == nil {
		return
	}
	globalRouter.BindSticky(sessionID, endpoint)
}

// ClearSticky clears the sticky binding for a session in the global router.
func ClearSticky(sessionID string) {
	if globalRouter == nil {
		return
	}
	globalRouter.ClearSticky(sessionID)
}

// IsInitialized returns true if the global router has been initialized.
func IsInitialized() bool {
	return globalRouter != nil
}
