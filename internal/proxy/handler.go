package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"

	"copilot-proxy/internal/middleware"
)

var (
	errUpstreamMiddlewaresRequired = errors.New("upstream middlewares are required")
	errUpstreamURLRequired         = errors.New("upstream URL is required")
	errInvalidUpstreamURL          = errors.New("invalid upstream URL")
)

// HandlerConfig contains all configuration for creating a Handler.
type HandlerConfig struct {
	// Required fields
	UpstreamURL string // Upstream Copilot API URL (required)

	// Optional fields (zero values use defaults)
	Transport           http.RoundTripper       // HTTP transport (default: http.DefaultTransport)
	UpstreamMiddlewares []middleware.Middleware // Upstream middleware chain (required)
}

// Handler proxies requests to the Copilot API with token injection.
type Handler struct {
	proxy    *httputil.ReverseProxy
	proxyErr error
}

// NewHandler creates a new Handler from the provided configuration.
// Returns an error if required fields are missing.
func NewHandler(cfg *HandlerConfig) (*Handler, error) {
	if cfg == nil {
		return nil, errUpstreamURLRequired
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if len(cfg.UpstreamMiddlewares) == 0 {
		return nil, errUpstreamMiddlewaresRequired
	}

	transport := cfg.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	proxy, proxyErr := buildReverseProxy(cfg.UpstreamURL, transport, cfg.UpstreamMiddlewares)

	h := &Handler{
		proxy:    proxy,
		proxyErr: proxyErr,
	}

	if proxy != nil {
		proxy.ErrorHandler = h.errorHandler
	}

	return h, nil
}

// validate checks that required fields are present.
func (cfg *HandlerConfig) validate() error {
	if cfg.UpstreamURL == "" {
		return errUpstreamURLRequired
	}
	return nil
}

// buildReverseProxy creates the reverse proxy with custom director.
func buildReverseProxy(
	upstreamURL string,
	transport http.RoundTripper,
	middlewares []middleware.Middleware,
) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(upstreamURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		if err == nil {
			err = errInvalidUpstreamURL
		}
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}

	proxy.Transport = middleware.NewPipeline(transport, middlewares...)

	return proxy, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.proxyErr != nil || h.proxy == nil {
		middleware.WriteError(w, http.StatusBadGateway, "invalid upstream base")
		return
	}
	h.proxy.ServeHTTP(w, r)
}

func (h *Handler) errorHandler(rw http.ResponseWriter, req *http.Request, err error) {
	ctx := req.Context()
	var requestID string
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil {
		requestID = rc.ID
	}
	if requestID == "" {
		requestID = req.Header.Get("X-Request-Id")
	}
	if requestID != "" {
		rw.Header().Set("X-Request-Id", requestID)
	}
	canceled := errors.Is(err, context.Canceled)
	timeout := isTimeoutError(err)
	if !canceled && !timeout {
		slog.Warn("request failed",
			"request_id", requestID,
			"error", err.Error(),
			"path", req.URL.Path)
	}

	if canceled {
		return
	}
	if timeout {
		middleware.WriteError(rw, http.StatusGatewayTimeout, "upstream request timed out")
		return
	}
	middleware.WriteError(rw, http.StatusBadGateway, "upstream request failed")
}

// isTimeoutError checks if an error represents a timeout condition.
func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
