package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"copilot-proxy/internal/config"
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
	settings := config.DefaultSettings()
	srv := New(&settings, http.NewServeMux())
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

func TestIsShutdownSignal(t *testing.T) {
	tests := []struct {
		name string
		sig  os.Signal
		want bool
	}{
		{name: "interrupt", sig: os.Interrupt, want: true},
		{name: "sigterm", sig: syscall.SIGTERM, want: true},
		{name: "sigusr1", sig: syscall.SIGUSR1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isShutdownSignal(tt.sig)
			if got != tt.want {
				t.Fatalf("isShutdownSignal(%v) = %v, want %v", tt.sig, got, tt.want)
			}
		})
	}
}
