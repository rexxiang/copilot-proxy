package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"copilot-proxy/internal/middleware"
	requestctx "copilot-proxy/internal/runtime/request"
)

var (
	errUpstreamMiddlewaresRequired = errors.New("upstream middlewares are required")
	errUpstreamURLRequired         = errors.New("upstream URL is required")
	errInvalidUpstreamURL          = errors.New("invalid upstream URL")
)

// HandlerConfig contains all configuration for creating a Handler.
type HandlerConfig struct {
	// Required fields
	UpstreamURL         string        // Upstream Copilot API URL (required unless provider is set)
	UpstreamURLProvider func() string // Dynamic upstream provider

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

	proxy, proxyErr := buildReverseProxy(cfg.UpstreamURL, cfg.UpstreamURLProvider, transport, cfg.UpstreamMiddlewares)

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
	if cfg.UpstreamURL == "" && cfg.UpstreamURLProvider == nil {
		return errUpstreamURLRequired
	}
	if cfg.UpstreamURLProvider == nil {
		if _, err := parseTargetURL(cfg.UpstreamURL); err != nil {
			return err
		}
	}
	return nil
}

// buildReverseProxy creates the reverse proxy with custom director.
func buildReverseProxy(
	upstreamURL string,
	upstreamURLProvider func() string,
	transport http.RoundTripper,
	middlewares []middleware.Middleware,
) (*httputil.ReverseProxy, error) {
	targetProvider := func() (*url.URL, error) {
		return parseTargetURL(upstreamURL)
	}
	if upstreamURLProvider != nil {
		targetProvider = func() (*url.URL, error) {
			return parseTargetURL(upstreamURLProvider())
		}
	}

	pipeline := middleware.NewPipeline(transport, middlewares...)
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.RequestURI = ""
		},
		Transport: &dynamicTargetTransport{
			base:           pipeline,
			targetProvider: targetProvider,
		},
	}
	return proxy, nil
}

type dynamicTargetTransport struct {
	base           http.RoundTripper
	targetProvider func() (*url.URL, error)
}

func (t *dynamicTargetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || t.base == nil {
		return nil, errInvalidUpstreamURL
	}
	target, err := t.targetProvider()
	if err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	rewriteRequestURL(out, target)
	return t.base.RoundTrip(out)
}

func parseTargetURL(raw string) (*url.URL, error) {
	target, err := url.Parse(raw)
	if err != nil || target == nil || target.Scheme == "" || target.Host == "" {
		if err == nil {
			err = errInvalidUpstreamURL
		}
		return nil, err
	}
	return target, nil
}

func rewriteRequestURL(req *http.Request, target *url.URL) {
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path, req.URL.RawPath = joinURLPath(target, req.URL)
	req.Host = target.Host
	if target.RawQuery == "" || req.URL.RawQuery == "" {
		req.URL.RawQuery = target.RawQuery + req.URL.RawQuery
	} else {
		req.URL.RawQuery = target.RawQuery + "&" + req.URL.RawQuery
	}
	req.RequestURI = ""
}

func joinURLPath(a, b *url.URL) (path, rawPath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}
	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")
	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	default:
		return a.Path + b.Path, apath + bpath
	}
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
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
	if rc, ok := requestctx.RequestContextFrom(ctx); ok && rc != nil {
		requestID = rc.ID
	}
	if requestID == "" {
		requestID = req.Header.Get("X-Request-Id")
	}
	if requestID == "" {
		now := time.Now().UTC()
		requestID = fmt.Sprintf("%x-%x", now.UnixNano(), time.Now().UnixNano())
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
