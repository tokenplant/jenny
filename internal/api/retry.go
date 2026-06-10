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

// sendWithRetry executes a function with retry logic.
// isBackground indicates whether this is a background classifier call.
func (c *Client) sendWithRetry(ctx context.Context, fn func(context.Context) (*Response, error), isBackground bool) (*Response, error) {
	cfg := c.retryConfig
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 10 // Ensure default if not set
	}

	var lastErr error
	consecutive529 := 0

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		resp, err := fn(ctx)

		// Check for HTTP response with retryable error
		if err != nil {
			// Check if it's a retryable HTTP error wrapped in our response
			var retryableErr *RetryableHTTPError
			if errors.As(err, &retryableErr) && retryableErr != nil {
				statusCode := retryableErr.StatusCode

				// Background classifier: skip 529 retry
				if isBackground && statusCode == StatusProxyError { // 529
					return nil, &CannotRetryError{
						Message:    "Background request rejected with 529 Overloaded",
						StatusCode: statusCode,
					}
				}

				// Check 529 cap
				if statusCode == StatusProxyError { // 529
					consecutive529++
					if consecutive529 > cfg.Max529Retries {
						return nil, &CannotRetryError{
							Message:    "Repeated 529 Overloaded errors",
							StatusCode: statusCode,
						}
					}
				} else {
					consecutive529 = 0 // Reset on non-529 response
				}

				// Check if we should retry
				if retryableErr.IsPermanent || !isRetryable(statusCode, nil) {
					return nil, err
				}

				lastErr = err
			} else {
				// Non-HTTP error (connection error, etc.)
				if !isRetryable(0, err) {
					return nil, err
				}
				lastErr = err
			}
		} else if resp != nil && resp.Error != "" {
			// Check for error in response (e.g., from streaming fallback)
			// This is not retryable as we already got a response
			return resp, nil
		} else {
			// Success
			return resp, nil
		}

		// Don't sleep on last attempt
		if attempt < cfg.MaxRetries {
			var retryAfter *time.Duration
			if retryableErr, ok := lastErr.(*RetryableHTTPError); ok {
				retryAfter = retryableErr.RetryAfter
			}

			delay := computeBackoff(attempt, cfg, retryAfter)

			// Wait before retry, respecting context cancellation
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				// Continue to next retry
			}
		}
	}

	// Exhausted retries
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("max retries exhausted")
}

// SetRetryConfig sets the retry configuration.
func (c *Client) SetRetryConfig(cfg RetryConfig) {
	c.retryConfig = cfg
}

// SetBackground sets whether this client is used for background classifier calls.
func (c *Client) SetBackground(isBackground bool) {
	if bg, ok := c.provider.(interface{ SetBackground(bool) }); ok {
		bg.SetBackground(isBackground)
	}
}
