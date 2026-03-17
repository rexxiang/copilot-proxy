package execute

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExecuteBufferedFlows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"msg":"ok"}`)
	}))
	defer server.Close()

	var (
		result ExecuteResult
		mu     sync.Mutex
		events []string
	)

	deps := ExecuteDeps{
		DoUpstream: func(ctx context.Context, req *http.Request) (*http.Response, error) {
			return server.Client().Do(req)
		},
	}

	opts := ExecuteOptions{
		Mode: StreamModeBuffered,
		ResultCallback: func(res ExecuteResult) {
			mu.Lock()
			result = res
			mu.Unlock()
		},
		TelemetryCallback: func(event TelemetryEvent) {
			events = append(events, event.Type)
		},
	}

	req := ExecuteRequest{Method: http.MethodGet, Path: server.URL}
	if err := Execute(context.Background(), req, deps, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}

	mu.Lock()
	gotBody := strings.TrimSpace(string(result.Body))
	mu.Unlock()

	if gotBody != `{"msg":"ok"}` {
		t.Fatalf("unexpected body %q", gotBody)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", result.StatusCode)
	}
	if len(events) != 3 || events[0] != "start" || events[1] != "first_byte" || events[2] != "end" {
		t.Fatalf("unexpected telemetry %v", events)
	}
}

func TestExecuteStreamCallbackSequence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flush")
		}
		fmt.Fprint(w, "data: one\n\n")
		flusher.Flush()
		time.Sleep(10 * time.Millisecond)
		fmt.Fprint(w, "data: two\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	var (
		callbacks []ExecuteResult
		events    []string
		mu        sync.Mutex
	)

	opts := ExecuteOptions{
		Mode: StreamModeCallback,
		ResultCallback: func(res ExecuteResult) {
			mu.Lock()
			callbacks = append(callbacks, res)
			mu.Unlock()
		},
		TelemetryCallback: func(event TelemetryEvent) {
			mu.Lock()
			events = append(events, event.Type)
			mu.Unlock()
		},
	}

	deps := ExecuteDeps{
		DoUpstream: func(ctx context.Context, req *http.Request) (*http.Response, error) {
			return server.Client().Do(req)
		},
	}

	if err := Execute(context.Background(), ExecuteRequest{Method: http.MethodGet, Path: server.URL}, deps, opts); err != nil {
		t.Fatalf("execute stream: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(callbacks) < 2 {
		t.Fatalf("expected at least 2 callbacks, got %d", len(callbacks))
	}
	if callbacks[0].StatusCode != http.StatusOK || callbacks[0].Headers["Content-Type"] != "text/event-stream" {
		t.Fatalf("unexpected first callback %+v", callbacks[0])
	}
	if callbacks[1].Body == nil {
		t.Fatalf("expected chunk body, got empty")
	}
	if len(events) != 3 || events[0] != "start" || events[1] != "first_byte" || events[2] != "end" {
		t.Fatalf("unexpected telemetry %v", events)
	}
}

func TestExecuteUpstreamError(t *testing.T) {
	deps := ExecuteDeps{
		DoUpstream: func(ctx context.Context, req *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		},
	}

	var seen []TelemetryEvent
	opts := ExecuteOptions{
		Mode: StreamModeBuffered,
		ResultCallback: func(res ExecuteResult) {
			if res.Error == "" {
				t.Fatalf("expected error in result")
			}
		},
		TelemetryCallback: func(event TelemetryEvent) {
			seen = append(seen, event)
		},
	}

	err := Execute(context.Background(), ExecuteRequest{Method: http.MethodGet, Path: "http://invalid"}, deps, opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(seen) != 2 || seen[0].Type != "start" || seen[1].Type != "error" {
		t.Fatalf("unexpected telemetry %v", seen)
	}
}
