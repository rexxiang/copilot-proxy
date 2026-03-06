package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimitedHandlerBypassesWhenCooldownDisabled(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})

	handler := NewRateLimitedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}), 0)
	t.Cleanup(func() {
		_ = handler.Close()
	})

	done := make(chan struct{}, 2)
	resp1 := httptest.NewRecorder()
	resp2 := httptest.NewRecorder()

	go func() {
		handler.ServeHTTP(resp1, httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil))
		done <- struct{}{}
	}()
	go func() {
		handler.ServeHTTP(resp2, httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil))
		done <- struct{}{}
	}()

	waitForSignal(t, entered, 200*time.Millisecond, "first request to enter underlying handler")
	waitForSignal(t, entered, 200*time.Millisecond, "second request to enter underlying handler")

	close(release)
	waitForSignal(t, done, 200*time.Millisecond, "first request to finish")
	waitForSignal(t, done, 200*time.Millisecond, "second request to finish")

	if resp1.Code != http.StatusNoContent {
		t.Fatalf("expected first response status 204, got %d", resp1.Code)
	}
	if resp2.Code != http.StatusNoContent {
		t.Fatalf("expected second response status 204, got %d", resp2.Code)
	}
}

func TestRateLimitedHandlerWaitsForCompletionAndCooldownBeforeNextRequest(t *testing.T) {
	const cooldown = 80 * time.Millisecond

	var (
		callCount int32
		startsMu  sync.Mutex
		starts    []time.Time
	)
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondStarted := make(chan struct{})
	firstDoneAt := make(chan time.Time, 1)

	handler := NewRateLimitedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&callCount, 1)
		startsMu.Lock()
		starts = append(starts, time.Now())
		startsMu.Unlock()

		if call == 1 {
			close(firstStarted)
			<-firstRelease
		}
		if call == 2 {
			close(secondStarted)
		}

		w.WriteHeader(http.StatusNoContent)
	}), cooldown)
	t.Cleanup(func() {
		_ = handler.Close()
	})

	go func() {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil))
		firstDoneAt <- time.Now()
	}()

	waitForSignal(t, firstStarted, 200*time.Millisecond, "first request to enter underlying handler")

	secondDone := make(chan struct{})
	go func() {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil))
		close(secondDone)
	}()

	ensureNoSignal(t, secondStarted, 40*time.Millisecond, "second request before first completion")

	close(firstRelease)
	firstCompleted := waitForTime(t, firstDoneAt, 200*time.Millisecond, "first request completion")

	ensureNoSignal(t, secondStarted, cooldown/2, "second request before cooldown elapsed")
	waitForSignal(t, secondStarted, 300*time.Millisecond, "second request after cooldown")
	waitForSignal(t, secondDone, 200*time.Millisecond, "second request completion")

	startsMu.Lock()
	defer startsMu.Unlock()
	if len(starts) != 2 {
		t.Fatalf("expected two handler start timestamps, got %d", len(starts))
	}
	if delta := starts[1].Sub(firstCompleted); delta < cooldown-20*time.Millisecond {
		t.Fatalf("expected second request to start after cooldown, got %s", delta)
	}
}

func TestRateLimitedHandlerStopsWaitingWhenRequestContextIsCanceled(t *testing.T) {
	var callCount int32
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})

	handler := NewRateLimitedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&callCount, 1)
		if call == 1 {
			close(firstStarted)
			<-firstRelease
		}
		w.WriteHeader(http.StatusNoContent)
	}), time.Second)
	t.Cleanup(func() {
		_ = handler.Close()
	})

	firstDone := make(chan struct{})
	go func() {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil))
		close(firstDone)
	}()

	waitForSignal(t, firstStarted, 200*time.Millisecond, "first request to enter underlying handler")

	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan struct{})
	go func() {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil).WithContext(ctx)
		handler.ServeHTTP(resp, req)
		close(secondDone)
	}()

	cancel()
	waitForSignal(t, secondDone, 200*time.Millisecond, "canceled waiting request to return")
	close(firstRelease)
	waitForSignal(t, firstDone, 200*time.Millisecond, "first request completion")

	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("expected only the first request to reach the underlying handler, got %d calls", got)
	}
}

func TestRateLimitedHandlerCloseUnblocksWaitingRequestsWith503(t *testing.T) {
	var callCount int32
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})

	handler := NewRateLimitedHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&callCount, 1)
		if call == 1 {
			close(firstStarted)
			<-firstRelease
		}
		w.WriteHeader(http.StatusNoContent)
	}), time.Second)

	firstDone := make(chan struct{})
	go func() {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil))
		close(firstDone)
	}()

	waitForSignal(t, firstStarted, 200*time.Millisecond, "first request to enter underlying handler")

	secondResp := httptest.NewRecorder()
	secondDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(secondResp, httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", nil))
		close(secondDone)
	}()

	time.Sleep(40 * time.Millisecond)
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("expected waiting request not to reach underlying handler before close, got %d calls", got)
	}

	if err := handler.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	waitForSignal(t, secondDone, 200*time.Millisecond, "waiting request to unblock after close")
	if secondResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected waiting request to receive 503 after close, got %d", secondResp.Code)
	}

	close(firstRelease)
	waitForSignal(t, firstDone, 200*time.Millisecond, "first request completion")
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("expected close to keep waiting request out of underlying handler, got %d calls", got)
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, timeout time.Duration, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func ensureNoSignal(t *testing.T, ch <-chan struct{}, timeout time.Duration, label string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("unexpected signal for %s", label)
	case <-time.After(timeout):
	}
}

func waitForTime(t *testing.T, ch <-chan time.Time, timeout time.Duration, label string) time.Time {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", label)
		return time.Time{}
	}
}
