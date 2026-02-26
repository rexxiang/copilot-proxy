package transform

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/models"
)

type stubCatalog struct {
	models []models.ModelInfo
}

func (s *stubCatalog) GetModels() []models.ModelInfo {
	copied := make([]models.ModelInfo, len(s.models))
	copy(copied, s.models)
	return copied
}

func TestModelRewriteExactMatchCaseInsensitive(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o"}}}
	req := httptestRequest([]byte(`{"model":"GPT-4O"}`))

	RewriteModel(req, nil, catalog, nil)

	body := readBody(t, req)
	if !bytes.Contains(body, []byte(`"model":"GPT-4O"`)) {
		t.Fatalf("expected body unchanged, got %s", string(body))
	}
}

func TestModelRewriteMappedFallback(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "claude-sonnet-4.5", Family: "claude-sonnet-4.5"}}}
	req := httptestRequest([]byte(`{"model":"claude-sonnet-4-20250514"}`))

	RewriteModel(req, nil, catalog, nil)

	body := readBody(t, req)
	if !bytes.Contains(body, []byte(`"model":"claude-sonnet-4.5"`)) {
		t.Fatalf("expected mapped model rewrite, got %s", string(body))
	}
}

func TestModelRewriteNoMatchNoChange(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o"}}}
	req := httptestRequest([]byte(`{"model":"unknown-model"}`))

	RewriteModel(req, nil, catalog, nil)

	body := readBody(t, req)
	if !bytes.Contains(body, []byte(`"model":"unknown-model"`)) {
		t.Fatalf("expected body unchanged, got %s", string(body))
	}
}

func TestModelRewriteMissingModelNoop(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o"}}}
	req := httptestRequest([]byte(`{"messages":[{"role":"user","content":"hi"}]}`))

	RewriteModel(req, nil, catalog, nil)

	body := readBody(t, req)
	if !bytes.Contains(body, []byte(`"messages"`)) {
		t.Fatalf("expected body unchanged, got %s", string(body))
	}
}

func TestModelRewriteEmptyModelNoop(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o"}}}
	req := httptestRequest([]byte(`{"model":"","messages":[{"role":"user","content":"hi"}]}`))

	RewriteModel(req, nil, catalog, nil)

	body := readBody(t, req)
	if !bytes.Contains(body, []byte(`"model":""`)) {
		t.Fatalf("expected body unchanged, got %s", string(body))
	}
}

func TestModelRewriteSetsMappedModel(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "claude-sonnet-4.5", Family: "claude-sonnet-4.5"}}}
	req := httptestRequest([]byte(`{"model":"claude-sonnet-4-20250514"}`))
	rc := &middleware.RequestContext{}

	RewriteModel(req, rc, catalog, nil)

	if rc.Info.MappedModel != "claude-sonnet-4.5" {
		t.Fatalf("expected MappedModel to be mapped, got %q", rc.Info.MappedModel)
	}
}

func TestModelRewriteSetsMappedModelOnExactMatch(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o"}}}
	req := httptestRequest([]byte(`{"model":"gpt-4o"}`))
	rc := &middleware.RequestContext{}

	RewriteModel(req, rc, catalog, nil)

	if rc.Info.MappedModel != "gpt-4o" {
		t.Fatalf("expected MappedModel to be exact model, got %q", rc.Info.MappedModel)
	}
}

func TestModelRewriteStoresSelectedModelEndpoints(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{
		ID:        "gpt-4o",
		Endpoints: []string{"/responses", "/chat/completions"},
	}}}
	req := httptestRequest([]byte(`{"model":"gpt-4o"}`))
	rc := &middleware.RequestContext{}

	RewriteModel(req, rc, catalog, nil)

	if len(rc.Info.SelectedModelEndpoints) != 2 {
		t.Fatalf("expected selected endpoints to be recorded, got %v", rc.Info.SelectedModelEndpoints)
	}
	if rc.Info.SelectedModelEndpoints[0] != "/responses" {
		t.Fatalf("expected first selected endpoint /responses, got %q", rc.Info.SelectedModelEndpoints[0])
	}
}

func httptestRequest(body []byte) *http.Request {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewReader(body))
	return req
}

func readBody(t *testing.T, req *http.Request) []byte {
	t.Helper()
	clone := req
	if clone.GetBody != nil {
		reader, err := clone.GetBody()
		if err != nil {
			t.Fatalf("GetBody: %v", err)
		}
		defer func() {
			_ = reader.Close()
		}()
		data, _ := io.ReadAll(reader)
		return data
	}
	if clone.Body == nil {
		return nil
	}
	data, _ := io.ReadAll(clone.Body)
	return data
}
