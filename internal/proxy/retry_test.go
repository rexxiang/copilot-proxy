package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

var (
	errBrokenPipe            = errors.New("write: broken pipe")
	errConnectionRefused     = errors.New("connection refused")
	errConnectionResetByPeer = errors.New("connection reset by peer")
	errIOTimeout             = errors.New("i/o timeout")
	errNoSuchHost            = errors.New("no such host")
	errPermanent             = errors.New("some permanent error")
	errPermissionDenied      = errors.New("permission denied")
	errReadConnectionReset   = errors.New("read: connection reset by peer")
	errUnexpectedEOF         = errors.New("unexpected EOF")
)

func TestRetryTransport_NoRetryOnSuccess(t *testing.T) {
	callCount := int32(0)
	transport := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		},
	}

	rt := NewRetryTransport(transport, DefaultRetryConfig())
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestRetryTransport_RetryOnConnectionReset(t *testing.T) {
	callCount := int32(0)
	transport := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			count := atomic.AddInt32(&callCount, 1)
			if count < 3 {
				return nil, &net.OpError{Op: "read", Err: syscall.ECONNRESET}
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		},
	}

	cfg := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		BackoffFactor:  2.0,
	}
	rt := NewRetryTransport(transport, cfg)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.com", strings.NewReader("body"))

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestRetryTransport_MaxRetriesExhausted(t *testing.T) {
	callCount := int32(0)
	transport := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, &net.OpError{Op: "dial", Err: errConnectionRefused}
		},
	}

	cfg := RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		BackoffFactor:  2.0,
	}
	rt := NewRetryTransport(transport, cfg)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)

	resp, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	// 1 initial + 2 retries = 3 total
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestRetryTransport_NoRetryOnNonRetryableError(t *testing.T) {
	callCount := int32(0)
	transport := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, errPermanent
		},
	}

	cfg := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		BackoffFactor:  2.0,
	}
	rt := NewRetryTransport(transport, cfg)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)

	resp, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error")
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 call (no retry), got %d", callCount)
	}
}

func TestRetryTransport_ContextCancellation(t *testing.T) {
	callCount := int32(0)
	transport := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
		},
	}

	cfg := RetryConfig{
		MaxRetries:     10,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
	}
	rt := NewRetryTransport(transport, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", http.NoBody)

	// Cancel after short delay
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	resp, err := rt.RoundTrip(req)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func TestRetryTransport_CancelDuringBackoffStopsWithoutExtraAttempt(t *testing.T) {
	callCount := int32(0)
	firstAttemptDone := make(chan struct{})
	transport := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			count := atomic.AddInt32(&callCount, 1)
			if count == 1 {
				close(firstAttemptDone)
			}
			return nil, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
		},
	}

	cfg := RetryConfig{
		MaxRetries:     5,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     500 * time.Millisecond,
		BackoffFactor:  2.0,
	}
	rt := NewRetryTransport(transport, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", http.NoBody)

	errCh := make(chan error, 1)
	go func() {
		resp, err := rt.RoundTrip(req)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		errCh <- err
	}()

	select {
	case <-firstAttemptDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first attempt did not run in time")
	}

	cancelAt := time.Now()
	cancel()

	var err error
	select {
	case err = <-errCh:
	case <-time.After(400 * time.Millisecond):
		t.Fatal("round trip did not return promptly after cancel")
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("expected no extra retry after cancel, got %d attempts", got)
	}
	if elapsed := time.Since(cancelAt); elapsed > 350*time.Millisecond {
		t.Fatalf("expected fast cancel response during backoff, elapsed=%v", elapsed)
	}
}

func TestRetryTransport_BodyPreservedOnRetry(t *testing.T) {
	bodies := make([]string, 0, 3)
	callCount := int32(0)
	transport := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			count := atomic.AddInt32(&callCount, 1)
			body, _ := io.ReadAll(req.Body)
			bodies = append(bodies, string(body))
			if count < 3 {
				return nil, errConnectionResetByPeer
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		},
	}

	cfg := RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
		BackoffFactor:  2.0,
	}
	rt := NewRetryTransport(transport, cfg)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.com", bytes.NewReader([]byte("test body")))

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// All attempts should have received the same body
	for i, body := range bodies {
		if body != "test body" {
			t.Errorf("attempt %d: expected 'test body', got %q", i+1, body)
		}
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil", nil, false},
		{"EOF", io.EOF, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"connection refused", syscall.ECONNREFUSED, true},
		{"connection reset message", errReadConnectionReset, true},
		{"broken pipe message", errBrokenPipe, true},
		{"timeout message", errIOTimeout, true},
		{"unexpected EOF message", errUnexpectedEOF, true},
		{"permanent error", errPermissionDenied, false},
		{"dns error", errNoSuchHost, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isRetryableError(tc.err)
			if result != tc.retryable {
				t.Errorf("isRetryableError(%v) = %v, want %v", tc.err, result, tc.retryable)
			}
		})
	}
}

func TestDynamicRetryTransportUsesLatestConfigPerRequest(t *testing.T) {
	var maxRetries atomic.Int32
	var remainingFailures atomic.Int32
	var calls atomic.Int32
	transport := &mockTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			if remainingFailures.Load() > 0 {
				remainingFailures.Add(-1)
				return nil, &net.OpError{Op: "dial", Err: syscall.ECONNRESET}
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		},
	}
	retryTransport := NewDynamicRetryTransport(transport, func() RetryConfig {
		return RetryConfig{
			MaxRetries:     int(maxRetries.Load()),
			InitialBackoff: 1 * time.Millisecond,
			MaxBackoff:     5 * time.Millisecond,
			BackoffFactor:  2,
		}
	})

	maxRetries.Store(0)
	remainingFailures.Store(1)
	req1, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
	resp, err := retryTransport.RoundTrip(req1)
	if err == nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("expected first request to fail without retries")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected one attempt with retries disabled, got %d", got)
	}

	maxRetries.Store(2)
	remainingFailures.Store(2)
	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", http.NoBody)
	resp, err = retryTransport.RoundTrip(req2)
	if err != nil {
		t.Fatalf("expected second request to succeed with retries, got %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("expected total 4 attempts (1 + 3), got %d", got)
	}
}

type mockTransport struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTrip(req)
}
