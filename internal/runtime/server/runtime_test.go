package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/middleware/upstream"
	"copilot-proxy/internal/proxy"
	runtimeconfig "copilot-proxy/internal/runtime/config"
	models "copilot-proxy/internal/runtime/model"
	requestctx "copilot-proxy/internal/runtime/request"
	core "copilot-proxy/internal/runtime/types"
)

type testLoader struct{}

func (testLoader) Load(ctx context.Context) ([]models.ModelInfo, error) {
	return []models.ModelInfo{{ID: "test", Name: "test"}}, nil
}

type testObservabilitySink struct{}

func (testObservabilitySink) RecordStart(_ *core.RequestRecord)                            {}
func (testObservabilitySink) RecordFirstResponse(string, int, time.Duration, string, bool) {}
func (testObservabilitySink) RecordComplete(string, int, time.Duration, string)            {}
func (testObservabilitySink) AddEvent(core.Event)                                          {}
func (testObservabilitySink) Snapshot() core.Snapshot                                      { return core.Snapshot{} }

type runtimeTestCatalog struct {
	models []models.ModelInfo
}

type capturingObservabilitySink struct {
	mu      sync.Mutex
	started []core.RequestRecord
}

func (s *capturingObservabilitySink) RecordStart(record *core.RequestRecord) {
	if record == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned := *record
	s.started = append(s.started, cloned)
}

func (s *capturingObservabilitySink) RecordFirstResponse(string, int, time.Duration, string, bool) {}
func (s *capturingObservabilitySink) RecordComplete(string, int, time.Duration, string)            {}
func (s *capturingObservabilitySink) AddEvent(core.Event)                                          {}
func (s *capturingObservabilitySink) Snapshot() core.Snapshot                                      { return core.Snapshot{} }

func (s *capturingObservabilitySink) startedRecords() []core.RequestRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.RequestRecord, len(s.started))
	copy(out, s.started)
	return out
}

func (c *runtimeTestCatalog) GetModels() []models.ModelInfo {
	out := make([]models.ModelInfo, len(c.models))
	copy(out, c.models)
	return out
}

func (c *runtimeTestCatalog) SetModels(items []models.ModelInfo) {
	c.models = make([]models.ModelInfo, len(items))
	copy(c.models, items)
}

func TestRuntimeStoresProvidedObservabilitySink(t *testing.T) {
	ctx := context.Background()
	sink := &testObservabilitySink{}
	deps := RuntimeDeps{
		SettingsFunc: func() (runtimeconfig.RuntimeSettings, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			return settings, nil
		},
		AuthFunc: func() (runtimeconfig.AuthConfig, error) {
			return runtimeconfig.AuthConfig{
				Default:  "user",
				Accounts: []runtimeconfig.Account{{User: "user", GhToken: "token"}},
			}, nil
		},
		Observability: sink,
		ModelCatalog:  &runtimeTestCatalog{},
		ModelLoader:   testLoader{},
	}

	rt, err := NewRuntimeWithContext(ctx, deps)
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	if rt.Observability != sink {
		t.Fatalf("expected runtime to store sink, got %T", rt.Observability)
	}
}

func TestRuntimeBuildSucceedsWithoutAccounts(t *testing.T) {
	ctx := context.Background()
	catalog := &runtimeTestCatalog{}
	deps := RuntimeDeps{
		SettingsFunc: func() (runtimeconfig.RuntimeSettings, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			return settings, nil
		},
		AuthFunc: func() (runtimeconfig.AuthConfig, error) {
			return runtimeconfig.AuthConfig{}, nil
		},
		ModelCatalog: catalog,
	}

	rt, err := NewRuntimeWithContext(ctx, deps)
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	if rt == nil || rt.Handler == nil {
		t.Fatalf("expected runtime handler to be initialized")
	}
	if got := catalog.GetModels(); len(got) != 0 {
		t.Fatalf("expected empty model catalog without accounts, got %v", got)
	}
}

func TestRuntimeBuildRequiresExplicitModelCatalog(t *testing.T) {
	ctx := context.Background()
	deps := RuntimeDeps{
		SettingsFunc: func() (runtimeconfig.RuntimeSettings, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			return settings, nil
		},
		AuthFunc: func() (runtimeconfig.AuthConfig, error) {
			return runtimeconfig.AuthConfig{}, nil
		},
	}

	if _, err := NewRuntimeWithContext(ctx, deps); err == nil {
		t.Fatalf("expected runtime build to fail without explicit model catalog")
	}
}

func TestRuntimeUsesUpdatedExternalAuthAndSettingsState(t *testing.T) {
	type capturedRequest struct {
		authHeader string
		testHeader string
		path       string
	}

	var (
		mu          sync.Mutex
		settings    = runtimeconfig.Default()
		authConfig  = runtimeconfig.AuthConfig{}
		server1Hits []capturedRequest
		server2Hits []capturedRequest
	)

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		server1Hits = append(server1Hits, capturedRequest{
			authHeader: r.Header.Get("Authorization"),
			testHeader: r.Header.Get("X-Test-Header"),
			path:       r.URL.Path,
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok-1"}`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		server2Hits = append(server2Hits, capturedRequest{
			authHeader: r.Header.Get("Authorization"),
			testHeader: r.Header.Get("X-Test-Header"),
			path:       r.URL.Path,
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok-2"}`))
	}))
	defer server2.Close()

	settings.ListenAddr = "127.0.0.1:0"
	settings.UpstreamBase = server1.URL
	settings.RequiredHeaders = map[string]string{"X-Test-Header": "one"}
	authConfig = runtimeconfig.AuthConfig{
		Default: "user-a",
		Accounts: []runtimeconfig.Account{
			{User: "user-a", GhToken: "token-a"},
			{User: "user-b", GhToken: "token-b"},
		},
	}

	rt, err := NewRuntimeWithContext(context.Background(), RuntimeDeps{
		SettingsFunc: func() (runtimeconfig.RuntimeSettings, error) {
			mu.Lock()
			defer mu.Unlock()
			return settings, nil
		},
		AuthFunc: func() (runtimeconfig.AuthConfig, error) {
			mu.Lock()
			defer mu.Unlock()
			return authConfig, nil
		},
		ModelCatalog: &runtimeTestCatalog{},
		ModelLoader:  testLoader{},
	})
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}

	requestBody, err := json.Marshal(map[string]string{"model": "gpt-4o"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodPost, runtimeconfig.ChatCompletionsPath, http.NoBody)
	firstReq.Body = ioNopCloserBytes(requestBody)
	firstReq.Header.Set("Content-Type", "application/json")
	firstResp := httptest.NewRecorder()
	rt.Handler.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first response code = %d, want %d", firstResp.Code, http.StatusOK)
	}

	mu.Lock()
	settings.UpstreamBase = server2.URL
	settings.RequiredHeaders = map[string]string{"X-Test-Header": "two"}
	authConfig.Default = "user-b"
	mu.Unlock()

	secondReq := httptest.NewRequest(http.MethodPost, runtimeconfig.ChatCompletionsPath, http.NoBody)
	secondReq.Body = ioNopCloserBytes(requestBody)
	secondReq.Header.Set("Content-Type", "application/json")
	secondResp := httptest.NewRecorder()
	rt.Handler.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second response code = %d, want %d", secondResp.Code, http.StatusOK)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(server1Hits) != 1 {
		t.Fatalf("expected exactly 1 request to server1, got %d", len(server1Hits))
	}
	if len(server2Hits) != 1 {
		t.Fatalf("expected exactly 1 request to server2 after state update, got %d", len(server2Hits))
	}
	if got := server1Hits[0].authHeader; got != "Bearer token-a" {
		t.Fatalf("first auth header = %q, want %q", got, "Bearer token-a")
	}
	if got := server1Hits[0].testHeader; got != "one" {
		t.Fatalf("first test header = %q, want %q", got, "one")
	}
	if got := server2Hits[0].authHeader; got != "Bearer token-b" {
		t.Fatalf("second auth header = %q, want %q", got, "Bearer token-b")
	}
	if got := server2Hits[0].testHeader; got != "two" {
		t.Fatalf("second test header = %q, want %q", got, "two")
	}
	if got := server2Hits[0].path; got != runtimeconfig.UpstreamChatCompletionsPath {
		t.Fatalf("second upstream path = %q, want %q", got, runtimeconfig.UpstreamChatCompletionsPath)
	}
}

func TestRuntimeObservabilityStartRecordPreservesModelAndAgent(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstreamServer.Close()

	sink := &capturingObservabilitySink{}
	rt, err := NewRuntimeWithContext(context.Background(), RuntimeDeps{
		SettingsFunc: func() (runtimeconfig.RuntimeSettings, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			settings.UpstreamBase = upstreamServer.URL
			return settings, nil
		},
		AuthFunc: func() (runtimeconfig.AuthConfig, error) {
			return runtimeconfig.AuthConfig{
				Default: "user",
				Accounts: []runtimeconfig.Account{
					{User: "user", GhToken: "token"},
				},
			}, nil
		},
		Observability: sink,
		ModelCatalog:  &runtimeTestCatalog{},
		ModelLoader:   testLoader{},
	})
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"assistant","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, runtimeconfig.ChatCompletionsPath, http.NoBody)
	req.Body = ioNopCloserBytes(body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Copilot-Account", "user")
	resp := httptest.NewRecorder()
	rt.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("response code = %d, want %d", resp.Code, http.StatusOK)
	}

	started := sink.startedRecords()
	if len(started) != 1 {
		t.Fatalf("expected 1 started record, got %d", len(started))
	}
	if got := started[0].Model; got != "gpt-4o" {
		t.Fatalf("start record model = %q, want %q", got, "gpt-4o")
	}
	if !started[0].IsAgent {
		t.Fatalf("expected IsAgent=true in start record")
	}
	if got := started[0].Path; got != runtimeconfig.ChatCompletionsPath {
		t.Fatalf("start record path = %q, want %q", got, runtimeconfig.ChatCompletionsPath)
	}
}

func TestRuntimeDoUpstreamPreservesRequestContextWhenExternalContextProvided(t *testing.T) {
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstreamServer.Close()

	sink := &capturingObservabilitySink{}
	handler, err := proxy.NewHandler(&proxy.HandlerConfig{
		UpstreamURL: upstreamServer.URL,
		UpstreamMiddlewares: []middleware.Middleware{
			upstream.NewObservabilityMiddleware(sink),
		},
	})
	if err != nil {
		t.Fatalf("build proxy handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, runtimeconfig.UpstreamChatCompletionsPath, http.NoBody)
	req.Body = ioNopCloserBytes([]byte(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(requestctx.WithRequestContext(req.Context(), &requestctx.RequestContext{
		Start:              time.Now(),
		LocalPath:          runtimeconfig.ChatCompletionsPath,
		SourceLocalPath:    runtimeconfig.ChatCompletionsPath,
		TargetUpstreamPath: runtimeconfig.UpstreamChatCompletionsPath,
		Info: requestctx.RequestInfo{
			Model:       "gpt-4o",
			MappedModel: "gpt-4o",
			IsAgent:     true,
		},
	}))

	resp, err := (&Runtime{}).doUpstream(handler)(context.Background(), req)
	if err != nil {
		t.Fatalf("do upstream: %v", err)
	}
	_ = resp.Body.Close()

	started := sink.startedRecords()
	if len(started) != 1 {
		t.Fatalf("expected 1 started record, got %d", len(started))
	}
	if got := started[0].Model; got != "gpt-4o" {
		t.Fatalf("start record model = %q, want %q", got, "gpt-4o")
	}
	if !started[0].IsAgent {
		t.Fatalf("expected IsAgent=true in start record")
	}
}

type bytesReadCloser struct {
	data []byte
	read bool
}

func ioNopCloserBytes(data []byte) *bytesReadCloser {
	cloned := make([]byte, len(data))
	copy(cloned, data)
	return &bytesReadCloser{data: cloned}
}

func (r *bytesReadCloser) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	n := copy(p, r.data)
	return n, nil
}

func (r *bytesReadCloser) Close() error { return nil }
