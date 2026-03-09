package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"copilot-proxy/internal/config"
)

// RetryConfig configures retry behavior for upstream requests.
type RetryConfig struct {
	MaxRetries     int           // Maximum number of retry attempts (default: 3)
	InitialBackoff time.Duration // Initial backoff duration (default: 100ms)
	MaxBackoff     time.Duration // Maximum backoff duration (default: 5s)
	BackoffFactor  float64       // Backoff multiplier (default: 2.0)
}

// DefaultRetryConfig returns sensible defaults for retry behavior.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     config.DefaultMaxRetries,
		InitialBackoff: config.DefaultRetryInitialBackoff,
		MaxBackoff:     config.DefaultRetryMaxBackoff,
		BackoffFactor:  config.DefaultRetryBackoffFactor,
	}
}

// RetryTransport wraps an http.RoundTripper to add automatic retry on network errors.
type RetryTransport struct {
	Transport http.RoundTripper
	Config    RetryConfig
}

// DynamicRetryTransport resolves retry config per request.
type DynamicRetryTransport struct {
	Transport      http.RoundTripper
	ConfigProvider func() RetryConfig
}

// NewRetryTransport creates a new RetryTransport with the given config.
func NewRetryTransport(transport http.RoundTripper, cfg RetryConfig) *RetryTransport {
	if transport == nil {
		transport = http.DefaultTransport
	}
	cfg = normalizeRetryConfig(cfg, false)
	return &RetryTransport{
		Transport: transport,
		Config:    cfg,
	}
}

// NewDynamicRetryTransport builds retry transport that can change behavior at runtime.
func NewDynamicRetryTransport(transport http.RoundTripper, provider func() RetryConfig) *DynamicRetryTransport {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &DynamicRetryTransport{
		Transport:      transport,
		ConfigProvider: provider,
	}
}

// RoundTrip implements http.RoundTripper with automatic retry on network errors.
func (rt *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return roundTripWithRetryConfig(rt.Transport, rt.Config, req, false)
}

// RoundTrip implements http.RoundTripper with runtime retry configuration.
func (rt *DynamicRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt == nil {
		return nil, fmt.Errorf("round trip failed: nil transport")
	}
	transport := rt.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	cfg := RetryConfig{}
	if rt.ConfigProvider != nil {
		cfg = rt.ConfigProvider()
	}
	return roundTripWithRetryConfig(transport, cfg, req, true)
}

func roundTripWithRetryConfig(
	transport http.RoundTripper,
	cfg RetryConfig,
	req *http.Request,
	allowDisable bool,
) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("round trip failed: nil request")
	}
	cfg = normalizeRetryConfig(cfg, allowDisable)
	if allowDisable && cfg.MaxRetries <= 0 {
		return transport.RoundTrip(req)
	}

	// For requests without body, we can always retry
	// For requests with body, we need to buffer it for retries
	var bodyBytes []byte
	var originalBody io.ReadCloser

	if req.Body != nil && req.Body != http.NoBody {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if closeErr := req.Body.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		originalBody = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	var lastErr error
	backoff := cfg.InitialBackoff

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		select {
		case <-req.Context().Done():
			return nil, fmt.Errorf("request canceled: %w", req.Context().Err())
		default:
		}

		// Reset body for retry attempts
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		} else if originalBody != nil {
			req.Body = originalBody
		}

		resp, err := transport.RoundTrip(req)

		if err == nil {
			return resp, nil
		}

		// Check if error is retryable
		if !isRetryableError(err) {
			return nil, fmt.Errorf("round trip failed: %w", err)
		}

		lastErr = err

		// Don't sleep after last attempt
		if attempt < cfg.MaxRetries {
			if waitErr := waitBackoff(req.Context(), backoff); waitErr != nil {
				return nil, fmt.Errorf("request canceled: %w", waitErr)
			}
			backoff = time.Duration(float64(backoff) * cfg.BackoffFactor)
			if backoff > cfg.MaxBackoff {
				backoff = cfg.MaxBackoff
			}
		}
	}

	return nil, lastErr
}

func normalizeRetryConfig(cfg RetryConfig, allowDisable bool) RetryConfig {
	if !allowDisable && cfg.MaxRetries <= 0 {
		cfg.MaxRetries = config.DefaultMaxRetries
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = config.DefaultRetryInitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = config.DefaultRetryMaxBackoff
	}
	if cfg.BackoffFactor <= 1 {
		cfg.BackoffFactor = config.DefaultRetryBackoffFactor
	}
	return cfg
}

func waitBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("backoff wait canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// isRetryableError determines if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Context errors are not retryable
	if errors.Is(err, io.EOF) {
		return true
	}

	// Connection reset errors
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}

	// Check for network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		// Timeout errors are retryable
		if netErr.Timeout() {
			return true
		}
	}

	// Check for connection errors in error message
	errStr := err.Error()
	retryableMessages := []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"no such host",
		"network is unreachable",
		"connection timed out",
		"i/o timeout",
		"EOF",
		"unexpected EOF",
	}
	for _, msg := range retryableMessages {
		if strings.Contains(strings.ToLower(errStr), strings.ToLower(msg)) {
			return true
		}
	}

	return false
}
