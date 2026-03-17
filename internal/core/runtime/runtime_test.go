package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/core/runtimeconfig"
	"copilot-proxy/internal/models"
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
		SettingsFunc: func() (runtimeconfig.Config, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			return settings, nil
		},
		AuthFunc: func() (config.AuthConfig, error) {
			return config.AuthConfig{
				Default:  "user",
				Accounts: []config.Account{{User: "user", GhToken: "token"}},
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
		SettingsFunc: func() (runtimeconfig.Config, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			return settings, nil
		},
		AuthFunc: func() (config.AuthConfig, error) {
			return config.AuthConfig{}, nil
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
		SettingsFunc: func() (runtimeconfig.Config, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			return settings, nil
		},
		AuthFunc: func() (config.AuthConfig, error) {
			return config.AuthConfig{}, nil
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
		authConfig  = config.AuthConfig{}
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
	authConfig = config.AuthConfig{
		Default: "user-a",
		Accounts: []config.Account{
			{User: "user-a", GhToken: "token-a"},
			{User: "user-b", GhToken: "token-b"},
		},
	}

	rt, err := NewRuntimeWithContext(context.Background(), RuntimeDeps{
		SettingsFunc: func() (runtimeconfig.Config, error) {
			mu.Lock()
			defer mu.Unlock()
			return settings, nil
		},
		AuthFunc: func() (config.AuthConfig, error) {
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

	firstReq := httptest.NewRequest(http.MethodPost, config.ChatCompletionsPath, http.NoBody)
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

	secondReq := httptest.NewRequest(http.MethodPost, config.ChatCompletionsPath, http.NoBody)
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
	if got := server2Hits[0].path; got != config.UpstreamChatCompletionsPath {
		t.Fatalf("second upstream path = %q, want %q", got, config.UpstreamChatCompletionsPath)
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
