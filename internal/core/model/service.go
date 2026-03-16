package model

import (
	"context"
	"net/http"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/models"
)

// Service provides cached model access.
type Service struct {
	catalog models.Catalog
	loader  models.Loader
	client  *http.Client
	proxy   string
}

// NewService builds a model service.
func NewService(catalog models.Catalog, loader models.Loader, client *http.Client, proxy string) *Service {
	if catalog == nil {
		catalog = models.DefaultModelsManager()
	}
	if client == nil {
		client = http.DefaultClient
	}
	if proxy == "" {
		proxy = "http://" + config.DefaultSettings().ListenAddr
	}
	return &Service{catalog: catalog, loader: loader, client: client, proxy: proxy}
}

// List returns cached models.
func (s *Service) List() []models.ModelInfo {
	return s.catalog.GetModels()
}

// Refresh populates the catalog from loader or proxy.
func (s *Service) Refresh(ctx context.Context) ([]models.ModelInfo, error) {
	if s.loader != nil {
		data, err := s.loader.Load(ctx)
		if err != nil {
			return nil, err
		}
		if setter, ok := s.catalog.(interface{ SetModels([]models.ModelInfo) }); ok {
			setter.SetModels(data)
		}
		return data, nil
	}
	data, err := models.FetchViaProxy(ctx, s.client, s.proxy+config.ModelsPath)
	if err != nil {
		return nil, err
	}
	if setter, ok := s.catalog.(interface{ SetModels([]models.ModelInfo) }); ok {
		setter.SetModels(data)
	}
	return data, nil
}
