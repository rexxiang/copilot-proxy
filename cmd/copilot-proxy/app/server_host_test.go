package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	runtimeconfig "copilot-proxy/internal/runtime/config"
)

var (
	errAcceptFailed = errors.New("accept")
	errListenFailed = errors.New("listen failed")
)

type stubListener struct {
	closed atomic.Bool
}

func (s *stubListener) Accept() (net.Conn, error) { return nil, errAcceptFailed }
func (s *stubListener) Close() error              { s.closed.Store(true); return nil }
func (s *stubListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0} }

func TestServeWithRetryRetriesOnListenFailure(t *testing.T) {
	srv := newServerHost(runtimeconfig.Default().ListenAddr, http.NewServeMux())
	t.Cleanup(func() {
		_ = srv.Close()
	})
	var calls int32
	srv.listenFn = func(network, address string) (net.Listener, error) {
		count := atomic.AddInt32(&calls, 1)
		if count < 2 {
			return nil, errListenFailed
		}
		return &stubListener{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(250 * time.Millisecond)
		cancel()
	}()

	_ = srv.serveWithRetry(ctx)
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected retry after listen failure, got %d attempts", calls)
	}
}

func TestServeWithRetryReturnsImmediatelyOnAddressInUse(t *testing.T) {
	srv := newServerHost(runtimeconfig.Default().ListenAddr, http.NewServeMux())
	t.Cleanup(func() {
		_ = srv.Close()
	})

	var calls int32
	srv.listenFn = func(network, address string) (net.Listener, error) {
		atomic.AddInt32(&calls, 1)
		return nil, &net.OpError{Op: "listen", Net: "tcp", Err: syscall.EADDRINUSE}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.serveWithRetry(ctx)
	}()

	select {
	case err := <-done:
		if !errors.Is(err, syscall.EADDRINUSE) {
			t.Fatalf("expected EADDRINUSE error, got %v", err)
		}
	case <-time.After(150 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("expected serveWithRetry to return immediately on EADDRINUSE")
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 listen attempt, got %d", got)
	}
}

func TestStartStopsOnContextCancel(t *testing.T) {
	srv := newServerHost(runtimeconfig.Default().ListenAddr, http.NewServeMux())
	t.Cleanup(func() {
		_ = srv.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	srv.listenFn = func(network, address string) (net.Listener, error) {
		<-ctx.Done()
		return nil, context.Canceled
	}

	done := make(chan error, 1)
	go func() {
		done <- srv.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !isExpectedShutdownError(err) {
			t.Fatalf("expected shutdown error semantics, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected Start to return after context cancellation")
	}
}
