package proxy

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"copilot-proxy/internal/middleware"
)

var errRateLimitedHandlerClosed = errors.New("rate limited handler closed")

type RateLimitedHandler struct {
	next             http.Handler
	cooldownProvider func() time.Duration

	mu           sync.Mutex
	notify       chan struct{}
	inFlight     bool
	lastComplete time.Time
	closed       bool
}

func NewRateLimitedHandlerWithProvider(next http.Handler, provider func() time.Duration) *RateLimitedHandler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return &RateLimitedHandler{
		next:             next,
		cooldownProvider: provider,
		notify:           make(chan struct{}),
	}
}

func (h *RateLimitedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.next == nil {
		return
	}

	req := attachRequestStart(r)
	cooldown := h.currentCooldown()
	if cooldown <= 0 {
		h.next.ServeHTTP(w, req)
		return
	}

	if err := h.waitTurn(req.Context(), cooldown); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		middleware.WriteError(w, http.StatusServiceUnavailable, "server shutting down")
		return
	}

	defer h.finishTurn()
	h.next.ServeHTTP(w, req)
}

func (h *RateLimitedHandler) Close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	h.broadcastLocked()
	return nil
}

func (h *RateLimitedHandler) waitTurn(ctx context.Context, cooldown time.Duration) error {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return errRateLimitedHandlerClosed
		}
		if !h.inFlight {
			wait := h.cooldownRemainingLocked(time.Now(), cooldown)
			if wait <= 0 {
				h.inFlight = true
				h.mu.Unlock()
				return nil
			}
			notify := h.notify
			h.mu.Unlock()

			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-notify:
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
			}
			continue
		}

		notify := h.notify
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-notify:
		}
	}
}

func (h *RateLimitedHandler) finishTurn() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.inFlight {
		return
	}
	h.inFlight = false
	h.lastComplete = time.Now()
	h.broadcastLocked()
}

func (h *RateLimitedHandler) cooldownRemainingLocked(now time.Time, cooldown time.Duration) time.Duration {
	if h.lastComplete.IsZero() {
		return 0
	}
	nextAllowed := h.lastComplete.Add(cooldown)
	if !now.Before(nextAllowed) {
		return 0
	}
	return nextAllowed.Sub(now)
}

func (h *RateLimitedHandler) currentCooldown() time.Duration {
	if h == nil {
		return 0
	}
	cooldown := time.Duration(0)
	if h.cooldownProvider != nil {
		cooldown = h.cooldownProvider()
	}
	if cooldown < 0 {
		return 0
	}
	return cooldown
}

func (h *RateLimitedHandler) broadcastLocked() {
	close(h.notify)
	h.notify = make(chan struct{})
}

func attachRequestStart(req *http.Request) *http.Request {
	if req == nil {
		return nil
	}
	rc, ok := middleware.RequestContextFrom(req.Context())
	if !ok || rc == nil {
		rc = &middleware.RequestContext{
			LocalPath:       req.URL.Path,
			SourceLocalPath: req.URL.Path,
			Start:           time.Now(),
		}
		return req.WithContext(middleware.WithRequestContext(req.Context(), rc))
	}
	if rc.Start.IsZero() {
		rc.Start = time.Now()
	}
	if rc.LocalPath == "" {
		rc.LocalPath = req.URL.Path
	}
	if rc.SourceLocalPath == "" {
		rc.SourceLocalPath = rc.LocalPath
	}
	return req.WithContext(middleware.WithRequestContext(req.Context(), rc))
}
