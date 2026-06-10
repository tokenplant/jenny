// Package api provides the Anthropic API client.
package api

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"time"
)

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxRetries    int           // Maximum number of retries for retryable errors
	Max529Retries int           // Maximum consecutive 529 errors before giving up
	BaseDelay     time.Duration // Initial delay between retries
	MaxDelay      time.Duration // Maximum delay between retries
	Jitter        float64       // Jitter factor (0-1), e.g., 0.25 for ±25%
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    10,
		Max529Retries: 3,
		BaseDelay:     500 * time.Millisecond,
		MaxDelay:      32 * time.Second,
		Jitter:        0.25,
	}
}

// CannotRetryError represents an error that cannot be retried.
type CannotRetryError struct {
	Message    string
	StatusCode int
}

// Error implements the error interface.
func (e *CannotRetryError) Error() string {
	return e.Message
}

// HTTP status codes
const (
	// StatusProxyError is 529 (used by some proxies for Overloaded)
	StatusProxyError = 529
)

// RetryableHTTPError represents an HTTP error that can be retried.
type RetryableHTTPError struct {
	StatusCode  int
	RetryAfter  *time.Duration
	Message     string
	IsPermanent bool // True for errors that should not be retried (e.g., 4xx except 429, 408, 409)
}

// Error implements the error interface.
func (e *RetryableHTTPError) Error() string {
	return e.Message
}

// computeBackoff computes the backoff delay for a given attempt.
func computeBackoff(attempt int, cfg RetryConfig, retryAfter *time.Duration) time.Duration {
	// Exponential backoff: min(baseDelay * 2^attempt, maxDelay)
	delay := min(cfg.BaseDelay*(1<<attempt), cfg.MaxDelay)

	// Apply jitter: delay = delay * (1 - jitter + rand.Float64() * 2 * jitter)
	jitterRange := cfg.Jitter * 2
	jitterOffset := 1 - cfg.Jitter
	delay = time.Duration(float64(delay) * (jitterOffset + rand.Float64()*jitterRange))

	// Override with Retry-After if present and longer
	if retryAfter != nil && *retryAfter > delay {
		delay = *retryAfter
	}

	return delay
}

// isRetryable checks if an HTTP status code or error is retryable.
func isRetryable(statusCode int, err error) bool {
	// Connection errors and context cancellation are retryable
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr); netErr != nil {
			return true
		}
		// Context cancellation is retryable
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return true
		}
	}

	// Retryable status codes
	switch statusCode {
	case http.StatusTooManyRequests: // 429
		return true
	case http.StatusInternalServerError: // 500
		return true
	case http.StatusBadGateway: // 502
		return true
	case http.StatusServiceUnavailable: // 503
		return true
	case http.StatusGatewayTimeout: // 504
		return true
	case StatusProxyError: // 529 (Overloaded)
		return true
	case http.StatusRequestTimeout: // 408
		return true
	case http.StatusConflict: // 409
		return true
	default:
		return false
	}
}

// SetRetryConfig sets the retry configuration on the provider if supported.
func (c *Client) SetRetryConfig(cfg RetryConfig) {
	c.retryConfig = cfg
	if setter, ok := c.provider.(interface{ SetRetryConfig(RetryConfig) }); ok {
		setter.SetRetryConfig(cfg)
	}
}

// SetBackground sets whether this client is used for background classifier calls.
func (c *Client) SetBackground(isBackground bool) {
	if bg, ok := c.provider.(interface{ SetBackground(bool) }); ok {
		bg.SetBackground(isBackground)
	}
}
