package model

import (
	"context"
	"net/http"

	"copilot-proxy/internal/runtime/config"
)

// Service provides cached model access.
type Service struct {
	catalog MutableCatalog
	loader  Loader
	client  *http.Client
	proxy   string
}

// NewService builds a model service.
func NewService(catalog MutableCatalog, loader Loader, client *http.Client, proxy string) *Service {
	if catalog == nil {
		catalog = NewManager()
	}
	if client == nil {
		client = http.DefaultClient
	}
	if proxy == "" {
		proxy = "http://" + config.Default().ListenAddr
	}
	return &Service{catalog: catalog, loader: loader, client: client, proxy: proxy}
}

// List returns cached models.
func (s *Service) List() []ModelInfo {
	return s.catalog.GetModels()
}

// Refresh populates the catalog from loader or proxy.
func (s *Service) Refresh(ctx context.Context) ([]ModelInfo, error) {
	if s.loader != nil {
		data, err := s.loader.Load(ctx)
		if err != nil {
			return nil, err
		}
		s.catalog.SetModels(data)
		return data, nil
	}
	data, err := FetchViaProxy(ctx, s.client, s.proxy)
	if err != nil {
		return nil, err
	}
	s.catalog.SetModels(data)
	return data, nil
}
