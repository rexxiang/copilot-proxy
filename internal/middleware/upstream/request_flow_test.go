package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/monitor"
)

type upstreamStubAuthStore struct {
	auth    config.AuthConfig
	loadErr error
	saveErr error
	saved   config.AuthConfig
}

func (s *upstreamStubAuthStore) LoadAuth() (config.AuthConfig, error) {
	return s.auth, s.loadErr
}

func (s *upstreamStubAuthStore) SaveAuth(auth config.AuthConfig) error {
	s.saved = auth
	return s.saveErr
}

type upstreamStubTokenProvider struct {
	token string
	err   error
}

//goland:noinspection GoUnusedParameter
func (s upstreamStubTokenProvider) GetToken(ctx context.Context, account config.Account) (string, error) {
	return s.token, s.err
}

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return true }

type deadlineAfterFirstRead struct {
	chunk []byte
	read  bool
}

func (d *deadlineAfterFirstRead) Read(p []byte) (int, error) {
	if !d.read {
		d.read = true
		return copy(p, d.chunk), nil
	}
	return 0, context.DeadlineExceeded
}

func (d *deadlineAfterFirstRead) Close() error {
	return nil
}

type canceledAfterFirstRead struct {
	chunk []byte
	read  bool
}

func (c *canceledAfterFirstRead) Read(p []byte) (int, error) {
	if !c.read {
		c.read = true
		return copy(p, c.chunk), nil
	}
	return 0, context.Canceled
}

func (c *canceledAfterFirstRead) Close() error {
	return nil
}

var (
	errUnexpectedNextCall = errors.New("unexpected next call")
	errTokenBoom          = errors.New("boom")
)

func TestContextInitSetsSourceAndLocalPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	ctx := &middleware.Context{Request: req}
	mw := NewContextInit()

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		rc, ok := middleware.RequestContextFrom(ctx.Request.Context())
		if !ok || rc == nil {
			t.Fatalf("expected request context")
		}
		if rc.SourceLocalPath != localResponsesPath {
			t.Fatalf("expected SourceLocalPath to be set, got %q", rc.SourceLocalPath)
		}
		if rc.LocalPath != localResponsesPath {
			t.Fatalf("expected LocalPath to be set, got %q", rc.LocalPath)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestRequestIDSetsContextAndResponseHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	ctx := &middleware.Context{Request: req}
	mw := NewRequestID()

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		rc, ok := middleware.RequestContextFrom(ctx.Request.Context())
		if !ok || rc == nil || rc.ID == "" {
			t.Fatalf("expected request id in context")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if got := resp.Header.Get("X-Request-Id"); got == "" {
		t.Fatalf("expected X-Request-Id response header")
	}
}

func TestResolveAccountNoAccountsReturnsUnauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	ctx := &middleware.Context{Request: req}
	mw := NewResolveAccount(&upstreamStubAuthStore{auth: config.AuthConfig{}})

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		t.Fatal("next should not be called")
		return nil, errUnexpectedNextCall
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	body := decodeErrorBody(t, resp)
	if body != "no account available" {
		t.Fatalf("error body: got %q, want %q", body, "no account available")
	}
}

func TestTokenTimeoutReturnsGatewayTimeout(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		Account: config.Account{User: "u1", GhToken: "gh"},
	}))
	ctx := &middleware.Context{Request: req}
	mw := NewToken(TokenConfig{Provider: upstreamStubTokenProvider{err: timeoutNetError{}}})

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		t.Fatal("next should not be called")
		return nil, errUnexpectedNextCall
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusGatewayTimeout)
	}
	body := decodeErrorBody(t, resp)
	if body != "request timed out" {
		t.Fatalf("error body: got %q, want %q", body, "request timed out")
	}
}

func TestParseRequestBodyStoresInfoAndPreservesBody(t *testing.T) {
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(body))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{}))
	ctx := &middleware.Context{Request: req}
	mw := NewParseRequestBody()

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		rc, ok := middleware.RequestContextFrom(ctx.Request.Context())
		if !ok || rc == nil {
			t.Fatalf("missing request context")
		}
		if rc.Info.Model != "gpt-4o" {
			t.Fatalf("model: got %q, want %q", rc.Info.Model, "gpt-4o")
		}
		data, readErr := io.ReadAll(ctx.Request.Body)
		if readErr != nil {
			t.Fatalf("read body: %v", readErr)
		}
		if string(data) != body {
			t.Fatalf("body: got %q, want %q", string(data), body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestParseRequestBodyMessagesInitSequenceDefaultsToUser(t *testing.T) {
	body := `{"model":"claude-3","messages":[{"role":"user","content":"system prompt"},{"role":"user","content":"question"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/messages", bytes.NewBufferString(body))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{}))
	ctx := &middleware.Context{Request: req}
	mw := NewParseRequestBody()

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		rc, ok := middleware.RequestContextFrom(ctx.Request.Context())
		if !ok || rc == nil {
			t.Fatalf("missing request context")
		}
		if rc.Info.IsAgent {
			t.Fatalf("expected IsAgent=false by default for messages init sequence")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestParseRequestBodyMessagesInitSequenceCanBeEnabled(t *testing.T) {
	body := `{"model":"claude-3","messages":[{"role":"user","content":"system prompt"},{"role":"user","content":"question"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/messages", bytes.NewBufferString(body))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{}))
	ctx := &middleware.Context{Request: req}
	mw := NewParseRequestBodyWithOptions(middleware.ParseOptions{
		MessagesInitSeqAgent: true,
	})

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		rc, ok := middleware.RequestContextFrom(ctx.Request.Context())
		if !ok || rc == nil {
			t.Fatalf("missing request context")
		}
		if !rc.Info.IsAgent {
			t.Fatalf("expected IsAgent=true when messages init sequence option is enabled")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestCaptureDebugStoresHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Test", "one")
	req.Header.Add("X-Test", "two")
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{}))
	ctx := &middleware.Context{Request: req}
	mw := NewCaptureDebug()

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		rc, ok := middleware.RequestContextFrom(ctx.Request.Context())
		if !ok || rc == nil {
			t.Fatalf("missing request context")
		}
		if rc.Headers["X-Test"] != "one" {
			t.Fatalf("header: got %q, want %q", rc.Headers["X-Test"], "one")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestStripXHeadersRemovesClientXHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Client-Id", "abc")
	req.Header.Set("X-Trace-Id", "trace")
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	ctx := &middleware.Context{Request: req}
	mw := NewStripXHeaders()

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		if got := ctx.Request.Header.Get("X-Client-Id"); got != "" {
			t.Fatalf("expected X-Client-Id removed, got %q", got)
		}
		if got := ctx.Request.Header.Get("X-Trace-Id"); got != "" {
			t.Fatalf("expected X-Trace-Id removed, got %q", got)
		}
		if got := ctx.Request.Header.Get("Authorization"); got == "" {
			t.Fatalf("expected Authorization kept")
		}
		if got := ctx.Request.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected Content-Type preserved, got %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestRequestTimeoutDisabledWhenZero(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	ctx := &middleware.Context{Request: req}
	mw := NewRequestTimeout(0)

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		if _, ok := ctx.Request.Context().Deadline(); ok {
			t.Fatalf("expected no deadline when timeout is disabled")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestRequestTimeoutSetsDeadline(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	ctx := &middleware.Context{Request: req}
	mw := NewRequestTimeout(2 * time.Second)

	start := time.Now()
	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		deadline, ok := ctx.Request.Context().Deadline()
		if !ok {
			t.Fatalf("expected deadline")
		}
		if deadline.Before(start.Add(1500*time.Millisecond)) || deadline.After(start.Add(2500*time.Millisecond)) {
			t.Fatalf("deadline out of expected range: %v", deadline)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestRequestTimeoutKeepsShorterDeadline(t *testing.T) {
	baseCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`)).WithContext(baseCtx)
	ctx := &middleware.Context{Request: req}
	mw := NewRequestTimeout(5 * time.Second)

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		deadline, ok := ctx.Request.Context().Deadline()
		if !ok {
			t.Fatalf("expected deadline")
		}
		if deadline.After(time.Now().Add(500 * time.Millisecond)) {
			t.Fatalf("expected existing shorter deadline to be kept, got %v", deadline)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	closeResponse(resp)
}

func TestTokenReturnsBadGatewayForGenericError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		Account: config.Account{User: "u1", GhToken: "gh"},
	}))
	ctx := &middleware.Context{Request: req}
	mw := NewToken(TokenConfig{Provider: upstreamStubTokenProvider{err: errTokenBoom}})

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		t.Fatal("next should not be called")
		return nil, errUnexpectedNextCall
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	body := decodeErrorBody(t, resp)
	if body != "failed to get token" {
		t.Fatalf("error body: got %q, want %q", body, "failed to get token")
	}
}

type metricsCapture struct {
	started           int
	completed         int
	firstResponses    int
	lastFirstID       string
	lastFirstCode     int
	lastFirstPath     string
	lastFirstDelay    time.Duration
	lastFirstIsStream bool
	lastCompleteID    string
	lastCompleteCode  int
	lastCompletePath  string
	lastCompleteDelay time.Duration
}

func (m *metricsCapture) RecordStart(_ *monitor.RequestRecord) {
	m.started++
}

func (m *metricsCapture) RecordFirstResponse(requestID string, statusCode int, duration time.Duration, upstreamPath string, isStream bool) {
	m.firstResponses++
	m.lastFirstID = requestID
	m.lastFirstCode = statusCode
	m.lastFirstPath = upstreamPath
	m.lastFirstDelay = duration
	m.lastFirstIsStream = isStream
}

func (m *metricsCapture) RecordComplete(requestID string, statusCode int, duration time.Duration, upstreamPath string) {
	m.completed++
	m.lastCompleteID = requestID
	m.lastCompleteCode = statusCode
	m.lastCompletePath = upstreamPath
	m.lastCompleteDelay = duration
}

func (m *metricsCapture) Record(_ *monitor.RequestRecord) {}

func TestMetricsRecordsContextCanceledAs499(t *testing.T) {
	metrics := &metricsCapture{}
	mw := NewMetrics(metrics)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		ID:        "req-1",
		Start:     time.Now().Add(-20 * time.Millisecond),
		LocalPath: localResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
		Account: config.Account{User: "u1"},
	}))
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return nil, context.Canceled
	})
	if resp != nil {
		defer closeResponse(resp)
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response on canceled request")
	}
	if metrics.started != 1 {
		t.Fatalf("expected one RecordStart call, got %d", metrics.started)
	}
	if metrics.completed != 1 {
		t.Fatalf("expected one RecordComplete call, got %d", metrics.completed)
	}
	if metrics.lastCompleteCode != monitor.StatusClientCanceled {
		t.Fatalf("expected canceled status 499, got %d", metrics.lastCompleteCode)
	}
	if metrics.lastCompletePath != localResponsesPath {
		t.Fatalf("expected path %s, got %q", localResponsesPath, metrics.lastCompletePath)
	}
	if metrics.lastCompleteDelay <= 0 {
		t.Fatalf("expected positive duration, got %v", metrics.lastCompleteDelay)
	}
}

func TestMetricsRecordsDeadlineExceededAs504(t *testing.T) {
	metrics := &metricsCapture{}
	mw := NewMetrics(metrics)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		ID:        "req-2",
		Start:     time.Now().Add(-15 * time.Millisecond),
		LocalPath: localResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
		Account: config.Account{User: "u1"},
	}))
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})
	if resp != nil {
		defer closeResponse(resp)
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response on timeout request")
	}
	if metrics.lastCompleteCode != http.StatusGatewayTimeout {
		t.Fatalf("expected timeout status 504, got %d", metrics.lastCompleteCode)
	}
	if metrics.lastCompletePath != localResponsesPath {
		t.Fatalf("expected path %s, got %q", localResponsesPath, metrics.lastCompletePath)
	}
}

func TestMetricsRecordsTimeoutNetErrorAs504(t *testing.T) {
	metrics := &metricsCapture{}
	mw := NewMetrics(metrics)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		ID:        "req-3",
		Start:     time.Now().Add(-15 * time.Millisecond),
		LocalPath: localResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
		Account: config.Account{User: "u1"},
	}))
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return nil, timeoutNetError{}
	})
	if resp != nil {
		defer closeResponse(resp)
	}

	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected timeout net.Error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response on timeout request")
	}
	if metrics.lastCompleteCode != http.StatusGatewayTimeout {
		t.Fatalf("expected timeout status 504, got %d", metrics.lastCompleteCode)
	}
	if metrics.lastCompletePath != localResponsesPath {
		t.Fatalf("expected path %s, got %q", localResponsesPath, metrics.lastCompletePath)
	}
}

func TestMetricsDefersSSECompletionUntilEOF(t *testing.T) {
	metrics := &metricsCapture{}
	mw := NewMetrics(metrics)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		ID:        "req-sse-eof",
		Start:     time.Now().Add(-30 * time.Millisecond),
		LocalPath: localResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
		Account: config.Account{User: "u1"},
	}))
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: hello\n\ndata: [DONE]\n\n")),
			Request: &http.Request{
				URL: &url.URL{Path: "/responses"},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)

	if metrics.started != 1 {
		t.Fatalf("expected one RecordStart call, got %d", metrics.started)
	}
	if metrics.firstResponses != 1 {
		t.Fatalf("expected one RecordFirstResponse call, got %d", metrics.firstResponses)
	}
	if metrics.lastFirstCode != http.StatusOK {
		t.Fatalf("expected first response code 200, got %d", metrics.lastFirstCode)
	}
	if metrics.lastFirstPath != "/responses" {
		t.Fatalf("expected first response path /responses, got %q", metrics.lastFirstPath)
	}
	if !metrics.lastFirstIsStream {
		t.Fatalf("expected first response to be marked as stream")
	}
	if metrics.lastFirstDelay <= 0 {
		t.Fatalf("expected positive first response duration, got %v", metrics.lastFirstDelay)
	}
	if metrics.completed != 0 {
		t.Fatalf("expected no completion before stream read, got %d", metrics.completed)
	}

	if _, readErr := io.ReadAll(resp.Body); readErr != nil {
		t.Fatalf("read stream body: %v", readErr)
	}
	_ = resp.Body.Close()

	if metrics.completed != 1 {
		t.Fatalf("expected one completion after EOF, got %d", metrics.completed)
	}
	if metrics.lastCompleteCode != http.StatusOK {
		t.Fatalf("expected status 200 on EOF, got %d", metrics.lastCompleteCode)
	}
	if metrics.lastCompletePath != "/responses" {
		t.Fatalf("expected path /responses, got %q", metrics.lastCompletePath)
	}
}

func TestMetricsRecordsSSEContextCanceledAs499(t *testing.T) {
	metrics := &metricsCapture{}
	mw := NewMetrics(metrics)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		ID:        "req-sse-cancel",
		Start:     time.Now().Add(-30 * time.Millisecond),
		LocalPath: localResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
		Account: config.Account{User: "u1"},
	}))
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &canceledAfterFirstRead{
				chunk: []byte("data: partial\n\n"),
			},
			Request: &http.Request{
				URL: &url.URL{Path: "/responses"},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)

	buf := make([]byte, 64)
	if _, readErr := resp.Body.Read(buf); readErr != nil {
		t.Fatalf("expected first read to succeed, got %v", readErr)
	}
	if metrics.firstResponses != 1 {
		t.Fatalf("expected one RecordFirstResponse call, got %d", metrics.firstResponses)
	}
	if metrics.completed != 0 {
		t.Fatalf("expected no completion after first chunk, got %d", metrics.completed)
	}

	_, readErr := resp.Body.Read(buf)
	if !errors.Is(readErr, context.Canceled) {
		t.Fatalf("expected context.Canceled on second read, got %v", readErr)
	}
	_ = resp.Body.Close()

	if metrics.completed != 1 {
		t.Fatalf("expected one completion after cancel, got %d", metrics.completed)
	}
	if metrics.lastCompleteCode != monitor.StatusClientCanceled {
		t.Fatalf("expected status 499 after cancel, got %d", metrics.lastCompleteCode)
	}
}

func TestMetricsRecordsSSEDeadlineExceededAs504(t *testing.T) {
	metrics := &metricsCapture{}
	mw := NewMetrics(metrics)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		ID:        "req-sse-deadline",
		Start:     time.Now().Add(-30 * time.Millisecond),
		LocalPath: localResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
		Account: config.Account{User: "u1"},
	}))
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &deadlineAfterFirstRead{
				chunk: []byte("data: partial\n\n"),
			},
			Request: &http.Request{
				URL: &url.URL{Path: "/responses"},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)

	buf := make([]byte, 64)
	if _, readErr := resp.Body.Read(buf); readErr != nil {
		t.Fatalf("expected first read to succeed, got %v", readErr)
	}
	if metrics.firstResponses != 1 {
		t.Fatalf("expected one RecordFirstResponse call, got %d", metrics.firstResponses)
	}
	_, readErr := resp.Body.Read(buf)
	if !errors.Is(readErr, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded on second read, got %v", readErr)
	}
	_ = resp.Body.Close()

	if metrics.completed != 1 {
		t.Fatalf("expected one completion after deadline exceeded, got %d", metrics.completed)
	}
	if metrics.lastCompleteCode != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504 after deadline exceeded, got %d", metrics.lastCompleteCode)
	}
}

func TestMetricsRecordsSSECloseBeforeEOFAs499(t *testing.T) {
	metrics := &metricsCapture{}
	mw := NewMetrics(metrics)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		ID:        "req-sse-close",
		Start:     time.Now().Add(-30 * time.Millisecond),
		LocalPath: localResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
		Account: config.Account{User: "u1"},
	}))
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: partial\n\n")),
			Request: &http.Request{
				URL: &url.URL{Path: "/responses"},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)

	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("close stream body: %v", closeErr)
	}
	if metrics.firstResponses != 1 {
		t.Fatalf("expected one RecordFirstResponse call, got %d", metrics.firstResponses)
	}

	if metrics.completed != 1 {
		t.Fatalf("expected one completion after close-before-eof, got %d", metrics.completed)
	}
	if metrics.lastCompleteCode != monitor.StatusClientCanceled {
		t.Fatalf("expected status 499 for close-before-eof, got %d", metrics.lastCompleteCode)
	}
}

func TestMetricsRecordsSSECloseAfterDoneAs200(t *testing.T) {
	metrics := &metricsCapture{}
	mw := NewMetrics(metrics)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		ID:        "req-sse-done-close",
		Start:     time.Now().Add(-30 * time.Millisecond),
		LocalPath: localResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
		Account: config.Account{User: "u1"},
	}))
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: hello\n\ndata: [DONE]\n\n",
			)),
			Request: &http.Request{
				URL: &url.URL{Path: "/responses"},
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)

	readBuf := make([]byte, 1024)
	if _, readErr := resp.Body.Read(readBuf); readErr != nil {
		t.Fatalf("expected first read without EOF, got %v", readErr)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("close stream body: %v", closeErr)
	}

	if metrics.firstResponses != 1 {
		t.Fatalf("expected one RecordFirstResponse call, got %d", metrics.firstResponses)
	}
	if metrics.completed != 1 {
		t.Fatalf("expected one completion after close with done marker, got %d", metrics.completed)
	}
	if metrics.lastCompleteCode != http.StatusOK {
		t.Fatalf("expected status 200 for close-after-done, got %d", metrics.lastCompleteCode)
	}
}

func decodeErrorBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp == nil {
		t.Fatalf("nil response")
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return payload["error"]
}

func closeResponse(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

var _ net.Error = timeoutNetError{}
