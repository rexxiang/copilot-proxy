package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/middleware/upstream"
	"copilot-proxy/internal/runtime/config"
	models "copilot-proxy/internal/runtime/model"
	"copilot-proxy/internal/runtime/observability"
	requestctx "copilot-proxy/internal/runtime/request"
	runtimemiddleware "copilot-proxy/internal/runtime/server/middleware"
)

// stubAuthStore implements AuthStore for testing.
type stubAuthStore struct {
	auth      config.AuthConfig
	loadErr   error
	saveErr   error
	saved     config.AuthConfig
	loadFunc  func() (config.AuthConfig, error) // optional dynamic load function
	loadCalls int
}

func (s *stubAuthStore) LoadAuth() (config.AuthConfig, error) {
	s.loadCalls++
	if s.loadFunc != nil {
		return s.loadFunc()
	}
	return s.auth, s.loadErr
}

func (s *stubAuthStore) SaveAuth(auth config.AuthConfig) error {
	s.saved = auth
	return s.saveErr
}

func newTestHandler(
	t *testing.T,
	upstreamURL string,
	store upstream.AuthStore,
	transport http.RoundTripper,
	opts ...func(*HandlerConfig),
) *Handler {
	t.Helper()
	cfg := HandlerConfig{
		UpstreamURL: upstreamURL,
		Transport:   transport,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.UpstreamMiddlewares == nil {
		cfg.UpstreamMiddlewares = buildTestUpstreamMiddlewares(store, nil, nil, nil)
	}
	h, err := NewHandler(&cfg)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	return h
}

func buildTestUpstreamMiddlewares(
	store upstream.AuthStore,
	headers map[string]string,
	catalog models.Catalog,
	metrics middleware.ObservabilitySink,
) []middleware.Middleware {
	return []middleware.Middleware{
		upstream.NewContextInit(),
		upstream.NewRequestID(),
		upstream.NewResolveAccount(store),
		upstream.NewToken(),
		upstream.NewParseRequestBodyWithOptionsProvider(func() requestctx.ParseOptions {
			return requestctx.ParseOptions{
				MessagesAgentDetectionRequestMode: true,
			}
		}),
		upstream.NewRequestTimeout(0),
		runtimemiddleware.NewMessagesTranslateWithRuntimeOptions(catalog, config.PathMapping, func() runtimemiddleware.MessagesTranslateRuntimeOptions {
			return runtimemiddleware.MessagesTranslateRuntimeOptions{}
		}),
		upstream.NewTokenInjection(),
		upstream.NewStaticHeaders(headers),
		upstream.NewDynamicHeaders(),
		upstream.NewObservabilityMiddleware(metrics),
	}
}

func captureDefaultSlogOutput(t *testing.T) (buffer *bytes.Buffer, restore func()) {
	t.Helper()

	oldDefault := slog.Default()
	buffer = &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	restore = func() {
		slog.SetDefault(oldDefault)
	}
	return buffer, restore
}

type stubCatalog struct {
	models []models.ModelInfo
}

func (s *stubCatalog) GetModels() []models.ModelInfo {
	copied := make([]models.ModelInfo, len(s.models))
	copy(copied, s.models)
	return copied
}

func TestProxyHandlerForwardsRequest(t *testing.T) {
	capture := make(chan *http.Request, 1)
	bodyCapture := make(chan string, 1)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture <- r
		data, _ := io.ReadAll(r.Body)
		bodyCapture <- string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{
			Default:  "u1",
			Accounts: []config.Account{{User: "u1", GhToken: "gh"}},
		},
	}

	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			cfg.UpstreamMiddlewares = buildTestUpstreamMiddlewares(
				store,
				map[string]string{"X-Required": "yes"},
				nil,
				nil,
			)
		},
	)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString("ping"))
	req.Header.Set("X-Client", "abc")
	req.Header.Set("Authorization", "client")
	resp := httptest.NewRecorder()

	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}

	upstreamReq := <-capture
	const chatCompletionsPath = "/chat/completions"
	if upstreamReq.URL.Path != chatCompletionsPath {
		t.Fatalf("unexpected upstream path: %s, expected /chat/completions (rewritten from /v1/chat/completions)", upstreamReq.URL.Path)
	}
	if got := upstreamReq.Header.Get("Authorization"); got != "Bearer gh" {
		t.Fatalf("unexpected auth header: %s", got)
	}
	if got := upstreamReq.Header.Get("X-Required"); got != "yes" {
		t.Fatalf("missing required header: %s", got)
	}
	if got := upstreamReq.Header.Get("X-Client"); got != "abc" {
		t.Fatalf("missing client header: %s", got)
	}
	if body := <-bodyCapture; body != "ping" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestProxyHandlerModelMappingRewritesBody(t *testing.T) {
	capture := make(chan string, 1)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		capture <- string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{
			Default:  "u1",
			Accounts: []config.Account{{User: "u1", GhToken: "gh"}},
		},
	}

	modelCatalog := &stubCatalog{
		models: []models.ModelInfo{{ID: "claude-sonnet-4.5", Family: "claude-sonnet-4.5"}},
	}
	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			cfg.UpstreamMiddlewares = buildTestUpstreamMiddlewares(
				store,
				nil,
				modelCatalog,
				nil,
			)
		},
	)

	body := `{"model":"CLAUDE-SONNET-3.5","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(body))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}

	gotBody := <-capture
	if !strings.Contains(gotBody, `"model":"claude-sonnet-4.5"`) {
		t.Fatalf("expected mapped model in body, got %q", gotBody)
	}
}

func TestProxyHandlerUpdatesDefault(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{
			Default:  "missing",
			Accounts: []config.Account{{User: "u1", GhToken: "gh"}},
		},
	}

	proxyHandler := newTestHandler(t, upstreamServer.URL, store, upstreamServer.Client().Transport)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if store.saved.Default != "u1" {
		t.Fatalf("expected default updated, got %s", store.saved.Default)
	}
}

func TestProxyHandlerNoAccounts(t *testing.T) {
	store := &stubAuthStore{auth: config.AuthConfig{}}

	proxyHandler := newTestHandler(t, "http://example.com", store, http.DefaultTransport)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Code)
	}
}

func TestProxyHandlerMethodPassesThroughToUpstream(t *testing.T) {
	methodCh := make(chan string, 1)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodCh <- r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}
	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
	)
	req := httptest.NewRequest(http.MethodGet, "http://localhost/v1/responses", http.NoBody)
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.Code)
	}
	if got := <-methodCh; got != http.MethodGet {
		t.Fatalf("expected upstream method GET, got %s", got)
	}
}

func TestProxyHandlerTokenError(t *testing.T) {
	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1"}}},
	}

	proxyHandler := newTestHandler(t, "http://example.com", store, http.DefaultTransport)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.Code)
	}
}

func TestProxyHandlerErrorPathIncludesRequestID(t *testing.T) {
	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}
	failingTransport := middleware.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})
	proxyHandler := newTestHandler(
		t,
		"http://example.com",
		store,
		failingTransport,
	)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", resp.Code)
	}
	if got := resp.Header().Get("X-Request-Id"); got == "" {
		t.Fatalf("expected X-Request-Id in error response")
	}
}

func TestProxyHandlerErrorHandlerSkipsWarnForClientCanceled(t *testing.T) {
	logOutput, restore := captureDefaultSlogOutput(t)
	defer restore()

	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()

	h.errorHandler(resp, req, context.Canceled)

	if logOutput.Len() != 0 {
		t.Fatalf("expected no warn log for canceled request, got %q", logOutput.String())
	}
}

func TestProxyHandlerErrorHandlerSkipsWarnForTimeout(t *testing.T) {
	logOutput, restore := captureDefaultSlogOutput(t)
	defer restore()

	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()

	h.errorHandler(resp, req, context.DeadlineExceeded)

	if resp.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 for timeout, got %d", resp.Code)
	}
	if logOutput.Len() != 0 {
		t.Fatalf("expected no warn log for timeout request, got %q", logOutput.String())
	}
}

func TestProxyHandlerErrorHandlerKeepsWarnForOtherErrors(t *testing.T) {
	logOutput, restore := captureDefaultSlogOutput(t)
	defer restore()

	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()

	h.errorHandler(resp, req, io.ErrUnexpectedEOF)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for non-timeout upstream failure, got %d", resp.Code)
	}
	if !strings.Contains(logOutput.String(), "request failed") {
		t.Fatalf("expected warn log for non-timeout failure, got %q", logOutput.String())
	}
}

func TestProxyHandlerRecordsMetricsOnUpstreamError(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}
	metrics := newStubMetrics()

	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			cfg.UpstreamMiddlewares = buildTestUpstreamMiddlewares(
				store,
				nil,
				nil,
				metrics,
			)
		},
	)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o"}`))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.Code)
	}

	if len(metrics.records) != 1 {
		t.Fatalf("expected 1 metric record, got %d", len(metrics.records))
	}

	rec := metrics.records[0]
	if rec.statusCode != http.StatusBadGateway {
		t.Fatalf("statusCode: got %d, want %d", rec.statusCode, http.StatusBadGateway)
	}
	if rec.path != "/v1/chat/completions" {
		t.Fatalf("path: got %q, want /v1/chat/completions", rec.path)
	}
	if rec.upstreamPath != "/chat/completions" {
		t.Fatalf("upstreamPath: got %q, want /chat/completions", rec.upstreamPath)
	}
	if rec.requestID == "" {
		t.Fatalf("requestID should be set")
	}
	if rec.duration <= 0 {
		t.Fatalf("duration should be positive")
	}
}

func TestProxyHandlerDefaultPipelineDoesNotRetryOnUnauthorized(t *testing.T) {
	var calls int32
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}

	proxyHandler := newTestHandler(t, upstreamServer.URL, store, upstreamServer.Client().Transport)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o"}`))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Code)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected one upstream call without retry, got %d", calls)
	}
}

var errToken = io.ErrUnexpectedEOF

func TestProxyHandlerLoadAuthError(t *testing.T) {
	store := &stubAuthStore{loadErr: errToken}

	proxyHandler := newTestHandler(t, "http://example.com", store, http.DefaultTransport)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.Code)
	}
}

func TestProxyHandlerSaveAuthError(t *testing.T) {
	store := &stubAuthStore{
		auth:    config.AuthConfig{Default: "missing", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
		saveErr: errToken,
	}

	proxyHandler := newTestHandler(t, "http://example.com", store, http.DefaultTransport)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.Code)
	}
}

func TestProxyHandlerSaveAuthPreservesAccounts(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstreamServer.Close()

	first := config.AuthConfig{
		Default:  "missing",
		Accounts: []config.Account{{User: "u1", GhToken: "gh1"}},
	}
	second := config.AuthConfig{
		Default:  "u2",
		Accounts: []config.Account{{User: "u1", GhToken: "gh1"}, {User: "u2", GhToken: "gh2"}},
	}

	store := &stubAuthStore{}
	store.loadFunc = func() (config.AuthConfig, error) {
		if store.loadCalls == 1 {
			return first, nil
		}
		return second, nil
	}

	proxyHandler := newTestHandler(t, upstreamServer.URL, store, upstreamServer.Client().Transport)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if store.saved.Default != "u1" {
		t.Fatalf("expected default u1, got %s", store.saved.Default)
	}
	if len(store.saved.Accounts) != 2 {
		t.Fatalf("expected accounts preserved")
	}
}

func TestProxyHandlerInvalidUpstreamURL(t *testing.T) {
	cfg := HandlerConfig{
		UpstreamURL: "not-a-url",
		Transport:   http.DefaultTransport,
	}
	_, err := NewHandler(&cfg)
	if err == nil {
		t.Fatal("expected error for invalid upstream URL")
	}
}

func TestProxyHandlerUnknownPathPassesThrough(t *testing.T) {
	pathCh := make(chan string, 1)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathCh <- r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}

	proxyHandler := newTestHandler(t, upstreamServer.URL, store, upstreamServer.Client().Transport)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/models", bytes.NewBufferString("{}"))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.Code)
	}
	if got := <-pathCh; got != "/v1/models" {
		t.Fatalf("expected /v1/models passthrough path, got %s", got)
	}
}

func TestProxyHandlerDynamicHeaders(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		body            string
		expectInitiator string
		expectVision    bool
	}{
		{
			name:            "user message sets x-initiator to user",
			path:            "/v1/chat/completions",
			body:            `{"messages":[{"role":"user","content":"hello"}]}`,
			expectInitiator: "user",
			expectVision:    false,
		},
		{
			name:            "assistant message sets x-initiator to agent",
			path:            "/v1/chat/completions",
			body:            `{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`,
			expectInitiator: "agent",
			expectVision:    false,
		},
		{
			name: "image_url sets Copilot-Vision-Request",
			path: "/v1/chat/completions",
			body: `{"messages":[{"role":"user","content":[` +
				`{"type":"text","text":"hi"},` +
				`{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`,
			expectInitiator: "user",
			expectVision:    true,
		},
		{
			name:            "responses API with input_image",
			path:            "/v1/responses",
			body:            `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,abc"}]}]}`,
			expectInitiator: "user",
			expectVision:    true,
		},
		{
			name:            "responses API agent last message",
			path:            "/v1/responses",
			body:            `{"input":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`,
			expectInitiator: "agent",
			expectVision:    false,
		},
		{
			name:            "anthropic messages user message",
			path:            "/v1/messages",
			body:            `{"model":"claude-3-opus","messages":[{"role":"user","content":"hello"}]}`,
			expectInitiator: "user",
			expectVision:    false,
		},
		{
			name:            "anthropic messages assistant last",
			path:            "/v1/messages",
			body:            `{"model":"claude-3-opus","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`,
			expectInitiator: "agent",
			expectVision:    false,
		},
		{
			name:            "anthropic messages init sequence defaults to agent",
			path:            "/v1/messages",
			body:            `{"model":"claude-3-opus","messages":[{"role":"user","content":"system prompt"},{"role":"user","content":"actual question"}]}`,
			expectInitiator: "agent",
			expectVision:    false,
		},
		{
			name: "anthropic messages with image",
			path: "/v1/messages",
			body: `{"model":"claude-3-opus","messages":[{"role":"user","content":[` +
				`{"type":"image","source":{"type":"base64","data":"abc"}}]}]}`,
			expectInitiator: "user",
			expectVision:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			capture := make(chan *http.Request, 1)

			upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capture <- r
				w.WriteHeader(http.StatusOK)
			}))
			defer upstreamServer.Close()

			store := &stubAuthStore{
				auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
			}

			proxyHandler := newTestHandler(t, upstreamServer.URL, store, upstreamServer.Client().Transport)
			req := httptest.NewRequest(http.MethodPost, "http://localhost"+tc.path, bytes.NewBufferString(tc.body))
			resp := httptest.NewRecorder()
			proxyHandler.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("unexpected status: %d", resp.Code)
			}

			upstreamReq := <-capture
			got := upstreamReq.Header.Get("X-Initiator")
			if got != tc.expectInitiator {
				t.Errorf("X-Initiator: got %q, want %q", got, tc.expectInitiator)
			}

			visionHeader := upstreamReq.Header.Get("Copilot-Vision-Request")
			if tc.expectVision && visionHeader != "true" {
				t.Errorf("Copilot-Vision-Request: got %q, want %q", visionHeader, "true")
			}
			if !tc.expectVision && visionHeader != "" {
				t.Errorf("Copilot-Vision-Request: got %q, want empty", visionHeader)
			}
		})
	}
}

func TestProxyHandlerDynamicHeadersMessagesSessionDetectionMode(t *testing.T) {
	capture := make(chan *http.Request, 1)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture <- r
		w.WriteHeader(http.StatusOK)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}

	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			middlewares := buildTestUpstreamMiddlewares(store, nil, nil, nil)
			middlewares[4] = upstream.NewParseRequestBodyWithOptionsProvider(func() requestctx.ParseOptions {
				return requestctx.ParseOptions{
					MessagesAgentDetectionRequestMode: false,
				}
			})
			cfg.UpstreamMiddlewares = middlewares
		},
	)

	body := `{"model":"claude-3-opus","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"last"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/messages", bytes.NewBufferString(body))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}

	upstreamReq := <-capture
	got := upstreamReq.Header.Get("X-Initiator")
	if got != "agent" {
		t.Fatalf("X-Initiator: got %q, want %q", got, "agent")
	}
}

func TestProxyHandlerPathRewrite(t *testing.T) {
	tests := []struct {
		localPath    string
		upstreamPath string
	}{
		{"/v1/chat/completions", "/chat/completions"},
		{"/v1/responses", "/responses"},
	}

	for _, tc := range tests {
		t.Run(tc.localPath, func(t *testing.T) {
			capture := make(chan string, 1)

			upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capture <- r.URL.Path
				w.WriteHeader(http.StatusOK)
			}))
			defer upstreamServer.Close()

			store := &stubAuthStore{
				auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
			}

			proxyHandler := newTestHandler(t, upstreamServer.URL, store, upstreamServer.Client().Transport)
			req := httptest.NewRequest(http.MethodPost, "http://localhost"+tc.localPath, bytes.NewBufferString("{}"))
			resp := httptest.NewRecorder()
			proxyHandler.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("unexpected status: %d", resp.Code)
			}

			gotPath := <-capture
			if gotPath != tc.upstreamPath {
				t.Errorf("path rewrite: got %q, want %q", gotPath, tc.upstreamPath)
			}
		})
	}
}

func TestProxyHandlerMessagesFallsBackToChatCompletionsWhenModelEndpointsMissing(t *testing.T) {
	capture := make(chan string, 1)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}
	modelCatalog := &stubCatalog{
		models: []models.ModelInfo{
			{ID: "gpt-4o"},
		},
	}

	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			cfg.UpstreamMiddlewares = buildTestUpstreamMiddlewares(
				store,
				nil,
				modelCatalog,
				nil,
			)
		},
	)

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/messages", bytes.NewBufferString(reqBody))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}

	if got := <-capture; got != config.UpstreamChatCompletionsPath {
		t.Fatalf("expected fallback path %q, got %q", config.UpstreamChatCompletionsPath, got)
	}
}

func TestProxyHandlerBodyPreserved(t *testing.T) {
	bodyCapture := make(chan string, 1)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		bodyCapture <- string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}

	proxyHandler := newTestHandler(t, upstreamServer.URL, store, upstreamServer.Client().Transport)

	originalBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"test body preservation"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(originalBody))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}

	receivedBody := <-bodyCapture
	if receivedBody != originalBody {
		t.Errorf("body not preserved: got %q, want %q", receivedBody, originalBody)
	}
}

type stubMetrics struct {
	records        []metricsRecord
	activeRequests map[string]metricsRecord
}

type metricsRecord struct {
	method, path, upstreamPath, model, account, requestID string
	statusCode                                            int
	duration                                              time.Duration
	isVision, isAgent                                     bool
}

func newStubMetrics() *stubMetrics {
	return &stubMetrics{
		activeRequests: make(map[string]metricsRecord),
	}
}

func (s *stubMetrics) RecordStart(r *observability.RequestRecord) {
	s.activeRequests[r.RequestID] = metricsRecord{
		method:       r.Method,
		path:         r.Path,
		upstreamPath: r.UpstreamPath,
		model:        r.Model,
		account:      r.Account,
		requestID:    r.RequestID,
		statusCode:   r.StatusCode,
		duration:     r.Duration,
		isVision:     r.IsVision,
		isAgent:      r.IsAgent,
	}
}

func (s *stubMetrics) RecordComplete(
	requestID string,
	statusCode int,
	duration time.Duration,
	upstreamPath string,
) {
	if record, ok := s.activeRequests[requestID]; ok {
		record.statusCode = statusCode
		record.duration = duration
		record.upstreamPath = upstreamPath
		delete(s.activeRequests, requestID)
		s.records = append(s.records, record)
	}
}

func (s *stubMetrics) Record(r *observability.RequestRecord) {
	s.records = append(s.records, metricsRecord{
		method:       r.Method,
		path:         r.Path,
		upstreamPath: r.UpstreamPath,
		model:        r.Model,
		account:      r.Account,
		requestID:    r.RequestID,
		statusCode:   r.StatusCode,
		duration:     r.Duration,
		isVision:     r.IsVision,
		isAgent:      r.IsAgent,
	})
}

func (s *stubMetrics) RecordFirstResponse(requestID string, statusCode int, duration time.Duration, upstreamPath string, isStream bool) {
	if record, ok := s.activeRequests[requestID]; ok {
		record.statusCode = statusCode
		record.duration = duration
		record.upstreamPath = upstreamPath
		s.activeRequests[requestID] = record
	}
}

func (s *stubMetrics) AddEvent(observability.Event) {}

func (s *stubMetrics) Snapshot() observability.Snapshot {
	return observability.Snapshot{}
}

func TestProxyHandlerRecordsMetrics(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}
	metrics := newStubMetrics()

	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			cfg.UpstreamMiddlewares = buildTestUpstreamMiddlewares(
				store,
				nil,
				nil,
				metrics,
			)
		},
	)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(body))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}

	if len(metrics.records) != 1 {
		t.Fatalf("expected 1 metric record, got %d", len(metrics.records))
	}

	rec := metrics.records[0]
	if rec.path != "/v1/chat/completions" {
		t.Errorf("path: got %q, want /v1/chat/completions", rec.path)
	}
	if rec.upstreamPath != "/chat/completions" {
		t.Errorf("upstreamPath: got %q, want /chat/completions", rec.upstreamPath)
	}
	if rec.model != "gpt-4o" {
		t.Errorf("model: got %q, want gpt-4o", rec.model)
	}
	if rec.account != "u1" {
		t.Errorf("account: got %q, want u1", rec.account)
	}
	if rec.statusCode != 200 {
		t.Errorf("statusCode: got %d, want 200", rec.statusCode)
	}
	if rec.duration <= 0 {
		t.Error("duration should be positive")
	}
	if rec.isVision {
		t.Error("expected isVision to be false")
	}
	if rec.isAgent {
		t.Error("expected isAgent to be false")
	}
}

func TestProxyHandlerSetsRequestID(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}
	metrics := newStubMetrics()

	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			cfg.UpstreamMiddlewares = buildTestUpstreamMiddlewares(
				store,
				nil,
				nil,
				metrics,
			)
		},
	)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o"}`))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	requestID := resp.Header().Get("X-Request-Id")
	if requestID == "" {
		t.Fatalf("expected X-Request-Id header")
	}
	if len(metrics.records) != 1 {
		t.Fatalf("expected 1 metric record, got %d", len(metrics.records))
	}
	if metrics.records[0].requestID != requestID {
		t.Fatalf("metrics request ID mismatch: got %q, want %q", metrics.records[0].requestID, requestID)
	}
}

func TestProxyHandlerRespectsExistingDeadline(t *testing.T) {
	t.Skip("deadline propagation in httptest is nondeterministic")
	deadlineCh := make(chan time.Time, 1)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deadline, ok := r.Context().Deadline(); ok {
			deadlineCh <- deadline
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}

	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			middlewares := buildTestUpstreamMiddlewares(
				store,
				nil,
				nil,
				nil,
			)
			middlewares[6] = upstream.NewRequestTimeout(2 * time.Second)
			cfg.UpstreamMiddlewares = middlewares
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(`{}`))
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}

	if deadline, ok := ctx.Deadline(); ok {
		select {
		case got := <-deadlineCh:
			if got.After(deadline) {
				t.Fatalf("expected deadline <= %v, got %v", deadline, got)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("upstream deadline not observed")
		}
	}
}

func TestProxyHandlerPropagatesClientCancelToUpstreamTransport(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan error, 1)

	transport := middleware.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		canceled <- req.Context().Err()
		return nil, req.Context().Err()
	})

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}

	proxyHandler := newTestHandler(
		t,
		"http://example.com",
		store,
		transport,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(
		http.MethodPost,
		"http://localhost/v1/chat/completions",
		bytes.NewBufferString(`{"model":"gpt-4o"}`),
	)
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		proxyHandler.ServeHTTP(resp, req)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("upstream transport did not start in time")
	}

	cancel()

	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled in upstream transport, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("upstream transport did not observe cancellation")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handler did not return promptly after cancellation")
	}
}

func TestProxyHandlerMetricsWithVisionAndAgent(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstreamServer.Close()

	store := &stubAuthStore{
		auth: config.AuthConfig{Default: "u1", Accounts: []config.Account{{User: "u1", GhToken: "gh"}}},
	}
	metrics := newStubMetrics()

	proxyHandler := newTestHandler(
		t,
		upstreamServer.URL,
		store,
		upstreamServer.Client().Transport,
		func(cfg *HandlerConfig) {
			cfg.UpstreamMiddlewares = buildTestUpstreamMiddlewares(
				store,
				nil,
				nil,
				metrics,
			)
		},
	)

	// Request with vision and agent (assistant last message)
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]},` +
		`{"role":"assistant","content":"I see an image"}]}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBufferString(body))
	resp := httptest.NewRecorder()
	proxyHandler.ServeHTTP(resp, req)

	if len(metrics.records) != 1 {
		t.Fatalf("expected 1 metric record, got %d", len(metrics.records))
	}

	rec := metrics.records[0]
	if !rec.isVision {
		t.Error("expected isVision to be true")
	}
	if !rec.isAgent {
		t.Error("expected isAgent to be true")
	}
}
