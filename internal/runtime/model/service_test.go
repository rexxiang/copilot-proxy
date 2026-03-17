package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelServiceRefreshUsesLoader(t *testing.T) {
	catalog := &stubCatalog{}
	loader := stubLoader{models: []ModelInfo{{ID: "gpt-5"}}}
	svc := NewService(catalog, loader, nil, "")

	got, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(got) != 1 || got[0].ID != "gpt-5" {
		t.Fatalf("unexpected refresh output: %+v", got)
	}
	if !catalog.setCalled {
		t.Fatalf("expected catalog to be updated")
	}
}

func TestModelServiceRefreshLoaderError(t *testing.T) {
	catalog := &stubCatalog{}
	loader := stubLoader{err: context.Canceled}
	svc := NewService(catalog, loader, nil, "")

	if _, err := svc.Refresh(context.Background()); err == nil {
		t.Fatalf("expected loader error, got nil")
	}
}

func TestNewServiceWithoutCatalogUsesPrivateCatalog(t *testing.T) {
	first := NewService(nil, nil, nil, "")
	second := NewService(nil, nil, nil, "")

	firstCatalog, ok := first.catalog.(interface{ SetModels([]ModelInfo) })
	if !ok {
		t.Fatalf("expected first service catalog to support SetModels")
	}
	firstCatalog.SetModels([]ModelInfo{{ID: "gpt-private"}})

	if got := second.List(); len(got) != 0 {
		t.Fatalf("expected second service to keep an isolated catalog, got %+v", got)
	}
}

func TestModelServiceRefreshViaProxyUsesSingleModelsPath(t *testing.T) {
	catalog := &stubCatalog{}
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":                   "gpt-5",
					"name":                 "GPT-5",
					"vendor":               "OpenAI",
					"model_picker_enabled": true,
					"capabilities": map[string]any{
						"type":   "chat",
						"family": "gpt-5",
						"limits": map[string]any{
							"max_context_window_tokens": 200000,
							"max_prompt_tokens":         200000,
							"max_output_tokens":         8000,
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	svc := NewService(catalog, nil, server.Client(), server.URL)
	got, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh via proxy: %v", err)
	}
	if requestedPath != "/copilot/models" {
		t.Fatalf("expected request path /copilot/models, got %q", requestedPath)
	}
	if len(got) != 1 || got[0].ID != "gpt-5" {
		t.Fatalf("unexpected models: %+v", got)
	}
	if !catalog.setCalled {
		t.Fatalf("expected catalog set from proxy refresh")
	}
}

// stubCatalog tracks SetModels invocations.
type stubCatalog struct {
	models    []ModelInfo
	setCalled bool
}

func (s *stubCatalog) GetModels() []ModelInfo {
	copied := make([]ModelInfo, len(s.models))
	copy(copied, s.models)
	return copied
}

func (s *stubCatalog) SetModels(items []ModelInfo) {
	s.setCalled = true
	s.models = make([]ModelInfo, len(items))
	copy(s.models, items)
}

type stubLoader struct {
	models []ModelInfo
	err    error
}

func (l stubLoader) Load(ctx context.Context) ([]ModelInfo, error) {
	return l.models, l.err
}
