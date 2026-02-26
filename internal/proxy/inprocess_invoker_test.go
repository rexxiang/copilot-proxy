package proxy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"copilot-proxy/internal/middleware"
)

func TestInProcessInvokerDoMarksInternalCall(t *testing.T) {
	invoker := NewInProcessInvoker(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !middleware.IsInternalCall(r.Context()) {
			http.Error(w, "missing internal call marker", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(r.URL.Path))
	}))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://in-process/custom/path", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := invoker.Do(req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if strings.TrimSpace(string(body)) != "/custom/path" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestInProcessInvokerDoRejectsNilRequest(t *testing.T) {
	invoker := NewInProcessInvoker(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	resp, err := invoker.Do(nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatalf("expected nil request error")
	}
}

func TestInProcessInvokerDoRejectsNilHandler(t *testing.T) {
	invoker := NewInProcessInvoker(nil)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://in-process/any", http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := invoker.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatalf("expected nil handler error")
	}
}
