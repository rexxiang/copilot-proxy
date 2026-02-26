package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"copilot-proxy/internal/config"
)

type Server struct {
	*http.Server
	listenFn func(network, address string) (net.Listener, error)
}

const (
	readHeaderTimeout  = 5 * time.Second
	retryBackoffStart  = 200 * time.Millisecond
	retryBackoffMax    = 2 * time.Second
	retryBackoffFactor = 2
)

func New(settings *config.Settings, handler http.Handler) *Server {
	if settings == nil {
		settings = &config.Settings{
			ListenAddr:      "",
			UpstreamBase:    "",
			RequiredHeaders: nil,
			UpstreamTimeout: config.NewDuration(0),
			MaxRetries:      0,
			RetryBackoff:    config.NewDuration(0),
		}
	}
	mux := http.NewServeMux()
	for _, path := range config.AllowedPaths {
		mux.Handle(path, handler)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	})

	srv := &http.Server{
		Addr:              settings.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	return &Server{Server: srv, listenFn: net.Listen}
}

func (s *Server) Start(ctx context.Context) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.serveWithRetry(cancelCtx)
	}()

	for {
		select {
		case <-cancelCtx.Done():
			return s.shutdownWithTimeout(cancelCtx)
		case sig := <-sigCh:
			if isShutdownSignal(sig) {
				cancel()
				return s.shutdownWithTimeout(cancelCtx)
			}
		case err := <-errCh:
			return err
		}
	}
}

func isShutdownSignal(sig os.Signal) bool {
	return sig == os.Interrupt || sig == syscall.SIGTERM
}

func (s *Server) shutdownWithTimeout(ctx context.Context) error {
	ctxShutdown, cancel := context.WithTimeout(ctx, config.ShutdownTimeout)
	defer cancel()
	if err := s.Shutdown(ctxShutdown); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}
	return nil
}

func (s *Server) serveWithRetry(ctx context.Context) error {
	backoff := retryBackoffStart
	for {
		select {
		case <-ctx.Done():
			return http.ErrServerClosed
		default:
		}

		listenFn := s.listenFn
		if listenFn == nil {
			listenFn = net.Listen
		}
		ln, err := listenFn("tcp", s.Addr)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("server context done: %w", ctx.Err())
			}
			slog.Warn("listen failed, retrying", "err", err, "addr", s.Addr)
			time.Sleep(backoff)
			backoff = minDuration(backoff*retryBackoffFactor, retryBackoffMax)
			continue
		}

		err = s.Serve(ln)
		_ = ln.Close()
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return http.ErrServerClosed
		}
		if ctx.Err() != nil {
			return fmt.Errorf("server context done: %w", ctx.Err())
		}
		slog.Warn("server stopped unexpectedly, retrying", "err", err)
		time.Sleep(backoff)
		backoff = minDuration(backoff*retryBackoffFactor, retryBackoffMax)
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
