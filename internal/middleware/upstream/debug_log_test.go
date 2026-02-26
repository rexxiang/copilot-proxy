package upstream

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/monitor"
)

const localResponsesPath = "/v1/responses"

type captureEntryLogger struct {
	entries []monitor.DebugLogEntry
	err     error
}

func (l *captureEntryLogger) Log(entry *monitor.DebugLogEntry) error {
	if entry != nil {
		l.entries = append(l.entries, *entry)
	}
	return l.err
}

func TestBuildLogEntryFromContext_UsesUpstreamRequestSnapshot(t *testing.T) {
	finalBody := `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://upstream.example.com/chat/completions",
		strings.NewReader(finalBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Initiator", "agent")
	req.Header.Set("X-Feature", "upstream")
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(finalBody)), nil
	}

	rc := &middleware.RequestContext{
		ID:      "req-1",
		Start:   time.Now().Add(-20 * time.Millisecond),
		Headers: map[string]string{"X-Initiator": "user", "X-Feature": "client"},
		Body:    []byte(`{"model":"legacy"}`),
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), rc))

	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"upstream failed"}`)),
		Request:    req,
	}

	entry := BuildLogEntryFromContext(resp, req.Context())
	if got := entry.RequestHeaders["X-Initiator"]; got != "agent" {
		t.Fatalf("expected upstream header X-Initiator=agent, got %q", got)
	}
	if got := entry.RequestHeaders["X-Feature"]; got != "upstream" {
		t.Fatalf("expected upstream header X-Feature=upstream, got %q", got)
	}
	if entry.RequestBody != finalBody {
		t.Fatalf("expected upstream request body %q, got %q", finalBody, entry.RequestBody)
	}
	if entry.Path != "/chat/completions" {
		t.Fatalf("expected upstream path /chat/completions, got %q", entry.Path)
	}
	if entry.LocalPath != "" {
		t.Fatalf("expected empty local path, got %q", entry.LocalPath)
	}
	if entry.UpstreamPath != "/chat/completions" {
		t.Fatalf("expected upstream path field /chat/completions, got %q", entry.UpstreamPath)
	}
	if !strings.Contains(entry.ResponseBody, "upstream failed") {
		t.Fatalf("expected upstream response body to be logged, got %q", entry.ResponseBody)
	}
}

func TestBuildLogEntryFromContext_PrefersLocalPath(t *testing.T) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://upstream.example.com/responses",
		strings.NewReader(`{"model":"gpt-5"}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	rc := &middleware.RequestContext{
		LocalPath: localResponsesPath,
		Start:     time.Now(),
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), rc))

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		Request:    req,
	}

	entry := BuildLogEntryFromContext(resp, req.Context())
	if entry.Path != localResponsesPath {
		t.Fatalf("expected compatibility path %s, got %q", localResponsesPath, entry.Path)
	}
	if entry.LocalPath != localResponsesPath {
		t.Fatalf("expected local path %s, got %q", localResponsesPath, entry.LocalPath)
	}
	if entry.UpstreamPath != "/responses" {
		t.Fatalf("expected upstream path /responses, got %q", entry.UpstreamPath)
	}
}

func TestDebugLogMiddlewareAlwaysCallsLoggerOnce(t *testing.T) {
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://upstream.example.com/chat/completions",
		strings.NewReader(`{"model":"gpt-5-mini"}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"model":"gpt-5-mini"}`)), nil
	}
	req.Header.Set("Content-Type", "application/json")

	rc := &middleware.RequestContext{
		Start: time.Now().Add(-10 * time.Millisecond),
		Info: middleware.RequestInfo{
			Model:       "gpt-5-mini",
			MappedModel: "gpt-5-mini",
		},
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), rc))

	logger := &captureEntryLogger{}
	mw := NewDebugLog(logger)
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if len(logger.entries) != 1 {
		t.Fatalf("expected logger to be called exactly once, got %d", len(logger.entries))
	}
	if logger.entries[0].StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 to be logged, got %d", logger.entries[0].StatusCode)
	}
}

func TestDebugLogMiddlewareCapturesSSEBody(t *testing.T) {
	const streamBody = "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://upstream.example.com/chat/completions",
		strings.NewReader(`{"model":"gpt-5-mini","stream":true}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"model":"gpt-5-mini","stream":true}`)), nil
	}

	rc := &middleware.RequestContext{
		Start: time.Now().Add(-10 * time.Millisecond),
		Info: middleware.RequestInfo{
			Model:       "gpt-5-mini",
			MappedModel: "gpt-5-mini",
		},
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), rc))

	logger := &captureEntryLogger{}
	mw := NewDebugLog(logger)
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(streamBody)),
			Request:    req,
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if len(logger.entries) != 0 {
		t.Fatalf("expected deferred logging for sse before body read, got %d entries", len(logger.entries))
	}

	gotBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read transformed body: %v", readErr)
	}
	if string(gotBody) != streamBody {
		t.Fatalf("expected stream body passthrough, got %q", string(gotBody))
	}
	_ = resp.Body.Close()

	if len(logger.entries) != 1 {
		t.Fatalf("expected exactly one sse log entry, got %d", len(logger.entries))
	}
	if logger.entries[0].ResponseBody != streamBody {
		t.Fatalf("expected sse body in debug log, got %q", logger.entries[0].ResponseBody)
	}
}

func TestDebugLogMiddlewareCapturesFullSSEBodyWithoutTruncation(t *testing.T) {
	chunk := "data: {\"choices\":[{\"delta\":{\"content\":\"" + strings.Repeat("x", 200) + "\"}}]}\n\n"
	streamBody := strings.Repeat(chunk, 40) + "data: [DONE]\n\n"
	if len(streamBody) <= debugResponseBodyLimit {
		t.Fatalf("test stream body should exceed debug limit, got len=%d limit=%d", len(streamBody), debugResponseBodyLimit)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://upstream.example.com/chat/completions",
		strings.NewReader(`{"model":"gpt-5-mini","stream":true}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"model":"gpt-5-mini","stream":true}`)), nil
	}

	rc := &middleware.RequestContext{
		Start: time.Now().Add(-10 * time.Millisecond),
		Info: middleware.RequestInfo{
			Model:       "gpt-5-mini",
			MappedModel: "gpt-5-mini",
		},
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), rc))

	logger := &captureEntryLogger{}
	mw := NewDebugLog(logger)
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(streamBody)),
			Request:    req,
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	gotBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read transformed body: %v", readErr)
	}
	if string(gotBody) != streamBody {
		t.Fatalf("expected stream body passthrough, got %q", string(gotBody))
	}
	_ = resp.Body.Close()

	if len(logger.entries) != 1 {
		t.Fatalf("expected exactly one sse log entry, got %d", len(logger.entries))
	}
	if logger.entries[0].ResponseBody != streamBody {
		t.Fatalf("expected full sse body in debug log, got len=%d want=%d", len(logger.entries[0].ResponseBody), len(streamBody))
	}
}

func TestDebugLogMiddlewareRecordsSSECancelReason(t *testing.T) {
	const streamChunk = "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://upstream.example.com/chat/completions",
		strings.NewReader(`{"model":"gpt-5-mini","stream":true}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(`{"model":"gpt-5-mini","stream":true}`)), nil
	}

	rc := &middleware.RequestContext{
		Start: time.Now().Add(-10 * time.Millisecond),
		Info: middleware.RequestInfo{
			Model:       "gpt-5-mini",
			MappedModel: "gpt-5-mini",
		},
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), rc))

	logger := &captureEntryLogger{}
	mw := NewDebugLog(logger)
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &cancelAfterFirstRead{
				chunk: []byte(streamChunk),
			},
			Request: req,
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	readBuffer := make([]byte, 1024)
	n, readErr := resp.Body.Read(readBuffer)
	if readErr != nil {
		t.Fatalf("expected first read success, got err=%v", readErr)
	}
	if got := string(readBuffer[:n]); got != streamChunk {
		t.Fatalf("expected first stream chunk %q, got %q", streamChunk, got)
	}

	_, readErr = resp.Body.Read(readBuffer)
	if readErr == nil || !strings.Contains(readErr.Error(), "context canceled") {
		t.Fatalf("expected second read context canceled error, got %v", readErr)
	}
	_ = resp.Body.Close()

	if len(logger.entries) != 1 {
		t.Fatalf("expected exactly one sse log entry, got %d", len(logger.entries))
	}
	if logger.entries[0].Error != "stream canceled: context canceled" {
		t.Fatalf("expected cancel reason in debug log, got %q", logger.entries[0].Error)
	}
}

type cancelAfterFirstRead struct {
	chunk []byte
	read  bool
}

func (c *cancelAfterFirstRead) Read(p []byte) (int, error) {
	if !c.read {
		c.read = true
		return copy(p, c.chunk), nil
	}
	return 0, context.Canceled
}

func (c *cancelAfterFirstRead) Close() error {
	return nil
}
