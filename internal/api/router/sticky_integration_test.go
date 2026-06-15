package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
)

// directHandler is an api.Requester that forwards directly to an http.Handler,
// completely bypassing api.Client so the test controls retry behavior.
type directHandler struct {
	h http.Handler
}

func (d *directHandler) SendMessage(ctx context.Context, _ []api.Message, _ []api.ToolParam, _ []api.ToolResult, _ []string, _ string) (*api.Response, error) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	d.h.ServeHTTP(w, req)
	if w.Code >= 400 {
		return &api.Response{}, &api.HTTPError{StatusCode: w.Code}
	}
	return &api.Response{
		Content:    []api.ContentBlock{{Type: "text", Text: "ok"}},
		StopReason: api.StopReasonEndTurn,
	}, nil
}

func (d *directHandler) SendMessageStream(_ context.Context, _ []api.Message, _ []api.ToolParam, _ []api.ToolResult, _ []string, _ string, _ time.Duration, _ time.Duration, _ func(context.Context) (*api.Response, error)) (<-chan api.StreamContentBlock, *api.StreamResult) {
	ch := make(chan api.StreamContentBlock)
	close(ch)
	return ch, &api.StreamResult{}
}

func (d *directHandler) SetMaxTokensOverride(int)               {}
func (d *directHandler) SetRetryConfig(api.RetryConfig)         {}
func (d *directHandler) SetBackground(bool)                    {}
func (d *directHandler) SetThinkingConfig(api.ThinkingConfig)   {}

// stickyClientWithHandler builds a StickyClient that talks to a real
// httptest server, bypassing api.Client entirely.
func stickyClientWithHandler(t *testing.T, sessionID string, r *Router, h http.Handler) *StickyClient {
	t.Helper()
	ep, err := r.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("SelectEndpoint: %v", err)
	}
	sc := NewStickyClient(sessionID, r)
	sc.endpoint = ep
	sc.client = &directHandler{h: h}
	return sc
}

// Test L2 failover: k1 returns 401 (permanent), L2 tries k2 which also returns 401 → L3 exhausted.
func TestSticky_L2KeyFailoverOn401_viaHTTPHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := twoKeyConfig()
	cfg.Providers[0].BaseURL = srv.URL
	r := NewRouter(cfg)
	sc := stickyClientWithHandler(t, "s1", r, srv.Config.Handler)

	_, err := sc.SendMessage(context.Background(), nil, nil, nil, nil, "")
	if err == nil {
		t.Error("expected error after all layers exhausted")
	}
}

// Test L3 target fallback: verify that after L1+L2 exhaustion, the router
// advances TargetIndex and the session is bound to the fallback model.
func TestSticky_L3TargetFallback_viaHTTPHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_BASE_URL", srv.URL)

	cfg := twoProviderConfig()
	cfg.Providers[0].BaseURL = srv.URL
	cfg.Providers[1].BaseURL = srv.URL

	// Verify config has AllowFallback=true before NewRouter
	af := cfg.Profiles["default"].AllowFallback
	if af == nil || !*af {
		t.Fatalf("config AllowFallback is not true: af=%v", af)
	}

	r := NewRouter(cfg)

	// Verify router's config has AllowFallback=true
	af2 := r.GetConfig().Profiles["default"].AllowFallback
	if af2 == nil || !*af2 {
		t.Fatalf("router AllowFallback is not true: af2=%v", af2)
	}

	epA := &ActiveEndpoint{Provider: "provider-a", Model: "model-a", APIKey: "k1", BaseURL: srv.URL}

	// BindSticky then advance TargetIndex to 0 so the NEXT call to NextEndpoint
	// will increment it to 1 and find target[1].
	r.BindSticky("s1", epA)
	r.mu.Lock()
	r.sessions["s1"].TargetIndex = 0
	r.mu.Unlock()

	next, err := r.NextEndpoint("s1", epA)
	if err != nil {
		t.Fatalf("NextEndpoint: %v", err)
	}
	if next.Provider != "provider-b" {
		t.Errorf("expected provider-b, got %s", next.Provider)
	}
	if next.Model != "model-b" {
		t.Errorf("expected model-b, got %s", next.Model)
	}
}

// Test L3 fallback on 5xx: verify the router selects target[1] after L1+L2 exhaustion.
func TestSticky_L3FallbackOn5xx_viaHTTPHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_BASE_URL", srv.URL)

	cfg := twoProviderConfig()
	cfg.Providers[0].BaseURL = srv.URL
	cfg.Providers[1].BaseURL = srv.URL
	r := NewRouter(cfg)

	epA := &ActiveEndpoint{Provider: "provider-a", Model: "model-a", APIKey: "k1", BaseURL: srv.URL}
	r.BindSticky("s1", epA)
	r.mu.Lock()
	r.sessions["s1"].TargetIndex = 0
	r.mu.Unlock()

	next, err := r.NextEndpoint("s1", epA)
	if err != nil {
		t.Fatalf("NextEndpoint: %v", err)
	}
	if next.Provider != "provider-b" || next.Model != "model-b" {
		t.Errorf("expected provider-b model-b, got %s %s", next.Provider, next.Model)
	}
}

// Test that context cancellation during L1 backoff stops immediately.
func TestSticky_L1BackoffRespectsContextCancel_viaHTTPHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	cfg := minimalConfig()
	cfg.Providers[0].BaseURL = srv.URL
	cfg.Profiles["fast"] = Profile{
		Targets:     []Target{{Match: MatchClause{Models: []string{"model-a"}}}},
		RoutingMode: "sticky",
		RetryPolicy: RetryPolicy{MaxRetries: 5, Backoff: "exponential"},
		AllowFallback: new(true),
	}
	r := NewRouter(cfg)
	r.SetProfile("fast")
	sc := stickyClientWithHandler(t, "s2", r, srv.Config.Handler)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := sc.SendMessage(ctx, nil, nil, nil, nil, "")
	if err == nil {
		t.Error("expected context cancelled error")
	}
}

// Test that non-retryable 400 returns immediately without L1 retries.
func TestSticky_NonRetryable400_viaHTTPHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	cfg := minimalConfig()
	cfg.Providers[0].BaseURL = srv.URL
	r := NewRouter(cfg)
	sc := stickyClientWithHandler(t, "s1", r, srv.Config.Handler)

	_, err := sc.SendMessage(context.Background(), nil, nil, nil, nil, "")
	if err == nil {
		t.Error("expected bad request error")
	}
}

// --- config builders ---

func minimalConfig() *Config {
	return &Config{
		Providers: []Provider{
			{
				Name:    "test",
				Type:    "openai",
				BaseURL: "http://unused",
				Accounts: []Account{
					{Name: "default", Keys: []string{"k1"}, Priority: 1},
				},
				Models: []Model{
					{Name: "model-a", Tags: []string{}, Priority: 1},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets:     []Target{{Match: MatchClause{Models: []string{"model-a"}}}},
				RoutingMode: "sticky",
				RetryPolicy: RetryPolicy{MaxRetries: 5, Backoff: "exponential"},
				AllowFallback: new(true),
			},
		},
	}
}

func twoKeyConfig() *Config {
	return &Config{
		Providers: []Provider{
			{
				Name:    "test",
				Type:    "openai",
				BaseURL: "http://unused",
				Accounts: []Account{
					{Name: "default", Keys: []string{"k1", "k2"}, Priority: 1},
				},
				Models: []Model{
					{Name: "model-a", Tags: []string{}, Priority: 1},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets:     []Target{{Match: MatchClause{Models: []string{"model-a"}}}},
				RoutingMode: "sticky",
				RetryPolicy: RetryPolicy{MaxRetries: 5, Backoff: "exponential"},
				AllowFallback: new(true),
			},
		},
	}
}

func twoProviderConfig() *Config {
	return &Config{
		Providers: []Provider{
			{
				Name:    "provider-a",
				Type:    "openai",
				BaseURL: "http://unused",
				Accounts: []Account{
					{Name: "default", Keys: []string{"k1"}, Priority: 1},
				},
				Models: []Model{
					{Name: "model-a", Tags: []string{"expensive"}, Priority: 1},
				},
			},
			{
				Name:    "provider-b",
				Type:    "openai",
				BaseURL: "http://unused",
				Accounts: []Account{
					{Name: "default", Keys: []string{"k2"}, Priority: 2},
				},
				Models: []Model{
					{Name: "model-b", Tags: []string{"cheap"}, Priority: 2},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets: []Target{
					{Match: MatchClause{Tags: []string{"expensive"}}},
					{Match: MatchClause{Tags: []string{"cheap"}}},
				},
				RoutingMode:     "sticky",
				SelectionPolicy: "round_robin",
				RetryPolicy:     RetryPolicy{MaxRetries: 5, Backoff: "exponential"},
				AllowFallback:   new(true),
			},
		},
	}
}