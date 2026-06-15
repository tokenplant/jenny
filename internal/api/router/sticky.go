// Package router provides multi-provider routing with three-layer fallback logic.
package router

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/api"
)

// StickyClient implements api.Requester and wraps routing logic with sticky sessions.
type StickyClient struct {
	router      *Router
	sessionID   string
	endpoint    *ActiveEndpoint
	client      api.Requester
	maxRetries  int
	backoffType string
	mu          sync.Mutex
}

// NewStickyClient creates a new StickyClient wrapping the router.
func NewStickyClient(sessionID string, router *Router) *StickyClient {
	return &StickyClient{
		router:      router,
		sessionID:   sessionID,
		maxRetries:  5,
		backoffType: "exponential",
	}
}

// SendMessage implements api.Requester.
// It selects an endpoint, delegates to the underlying client, and handles
// three-layer fallback on errors: retry (L1) -> key failover (L2) -> model fallback (L3).
func (s *StickyClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string) (*api.Response, error) {
	if err := s.selectEndpoint(); err != nil {
		return nil, fmt.Errorf("failed to select endpoint: %w", err)
	}

	if err := s.ensureClient(); err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// L1: Retry with backoff on same key+model
	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		resp, err := s.client.SendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
		if err == nil {
			s.router.healthRegistry.RecordSuccess(
				s.endpoint.Provider,
				s.endpoint.Account,
				s.endpoint.Model,
				s.endpoint.APIKey,
			)
			return resp, nil
		}

		// Check error type
		var retryableErr *api.RetryableHTTPError
		if errors.As(err, &retryableErr) {
			if retryableErr.IsPermanent {
				return nil, err
			}
			if attempt < s.maxRetries {
				delay := s.computeBackoff(attempt, retryableErr.RetryAfter)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
				continue
			}
		}

		var httpErr *api.HTTPError
		if errors.As(err, &httpErr) {
			code := httpErr.StatusCode
			if code >= 400 && code < 500 && code != 429 && code != 408 && code != 409 {
				return nil, err
			}
			if code == http.StatusTooManyRequests || (code >= 500 && code < 600) {
				if attempt < s.maxRetries {
					delay := s.computeBackoff(attempt, nil)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(delay):
					}
					continue
				}
			}
		}

		s.router.healthRegistry.RecordFailure(
			s.endpoint.Provider,
			s.endpoint.Account,
			s.endpoint.Model,
			s.endpoint.APIKey,
		)
	}

	// L2: Key failover
	if nextKey := s.tryNextKey(); nextKey != nil {
		s.endpoint = nextKey
		if err := s.ensureClient(); err == nil {
			resp, err := s.client.SendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
			if err == nil {
				s.router.BindSticky(s.sessionID, nextKey)
				return resp, nil
			}
		}
	}

	// L3: Model fallback
	// L3: Model fallback
	if nextTarget := s.tryNextTarget(); nextTarget != nil {
		s.endpoint = nextTarget
		if err := s.ensureClient(); err == nil {
			resp, err := s.client.SendMessage(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix)
			if err == nil {
				s.router.BindSticky(s.sessionID, nextTarget)
				return resp, nil
			}
		}
	}

	// L3 exhausted: record failure so HealthRegistry is aware before returning
	s.router.healthRegistry.RecordFailure(
		s.endpoint.Provider,
		s.endpoint.Account,
		s.endpoint.Model,
		s.endpoint.APIKey,
	)

	return nil, fmt.Errorf("all routing layers exhausted")
}

// SendMessageStream implements api.Requester streaming.
func (s *StickyClient) SendMessageStream(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt []string, systemPromptSuffix string, idleTimeout time.Duration, fallbackTimeout time.Duration, onStreamingFallback func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
	if err := s.selectEndpoint(); err != nil {
		blocks := make(chan api.StreamContentBlock)
		result := &api.StreamResult{Error: err.Error()}
		close(blocks)
		return blocks, result
	}

	if err := s.ensureClient(); err != nil {
		blocks := make(chan api.StreamContentBlock)
		result := &api.StreamResult{Error: err.Error()}
		close(blocks)
		return blocks, result
	}

	return s.client.SendMessageStream(ctx, messages, tools, toolResults, systemPrompt, systemPromptSuffix, idleTimeout, fallbackTimeout, onStreamingFallback)
}

// SetMaxTokensOverride implements api.Requester.
func (s *StickyClient) SetMaxTokensOverride(maxTokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.SetMaxTokensOverride(maxTokens)
	}
}

// SetRetryConfig implements api.Requester.
func (s *StickyClient) SetRetryConfig(cfg api.RetryConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxRetries = cfg.MaxRetries
	if s.client != nil {
		s.client.SetRetryConfig(cfg)
	}
}

// SetBackground implements api.Requester.
func (s *StickyClient) SetBackground(isBackground bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bg, ok := s.client.(interface{ SetBackground(bool) }); ok {
		bg.SetBackground(isBackground)
	}
}

// SetThinkingConfig implements api.Requester.
func (s *StickyClient) SetThinkingConfig(cfg api.ThinkingConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.SetThinkingConfig(cfg)
	}
}

func (s *StickyClient) selectEndpoint() error {
	if s.endpoint != nil {
		return nil
	}

	endpoint, err := s.router.SelectEndpoint(s.sessionID)
	if err != nil {
		return err
	}

	s.endpoint = endpoint
	return nil
}

func (s *StickyClient) ensureClient() error {
	if s.client != nil {
		return nil
	}

	client, err := api.NewClientWithModel(s.endpoint.Model)
	if err != nil {
		return err
	}
	s.client = client
	return nil
}

func (s *StickyClient) computeBackoff(attempt int, retryAfter *time.Duration) time.Duration {
	baseDelay := 500 * time.Millisecond
	maxDelay := 32 * time.Second

	delay := baseDelay * time.Duration(1<<uint(attempt))
	delay = min(delay, maxDelay)

	jitter := time.Duration(rand.Float64() * float64(delay) * 0.25)
	delay = delay + jitter

	if retryAfter != nil && *retryAfter > delay {
		delay = *retryAfter
	}

	return delay
}

func (s *StickyClient) tryNextKey() *ActiveEndpoint {
	if s.endpoint == nil {
		return nil
	}

	next, err := s.router.NextEndpoint(s.sessionID, s.endpoint)
	if err != nil {
		return nil
	}

	if next.Model == s.endpoint.Model {
		return next
	}
	return nil
}

func (s *StickyClient) tryNextTarget() *ActiveEndpoint {
	if s.endpoint == nil {
		return nil
	}

	// Call nextTargetLocked directly (not NextEndpoint) so we don't
	// advance TargetIndex again. NextEndpoint already advanced it when
	// tryNextKey returned nil (L2 exhausted). We just need to read the
	// result that was stored in state.Endpoint.
	state := s.router.GetStickyEndpoint(s.sessionID)
	if state == nil {
		return nil
	}
	// After L2 exhaustion, NextEndpoint stored the next target in
	// state.Endpoint. Return it directly.
	return state
}
