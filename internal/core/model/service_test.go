package model

import (
	"context"
	"testing"

	"copilot-proxy/internal/models"
)

func TestModelServiceRefreshUsesLoader(t *testing.T) {
	catalog := &stubCatalog{}
	loader := stubLoader{models: []models.ModelInfo{{ID: "gpt-5"}}}
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

// stubCatalog tracks SetModels invocations.
type stubCatalog struct {
	models    []models.ModelInfo
	setCalled bool
}

func (s *stubCatalog) GetModels() []models.ModelInfo {
	copied := make([]models.ModelInfo, len(s.models))
	copy(copied, s.models)
	return copied
}

func (s *stubCatalog) SetModels(items []models.ModelInfo) {
	s.setCalled = true
	s.models = make([]models.ModelInfo, len(items))
	copy(s.models, items)
}

type stubLoader struct {
	models []models.ModelInfo
	err    error
}

func (l stubLoader) Load(ctx context.Context) ([]models.ModelInfo, error) {
	return l.models, l.err
}
