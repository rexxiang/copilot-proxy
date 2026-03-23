package model

import "context"

// Catalog provides read-only access to cached models.
type Catalog interface {
	GetModels() []ModelInfo
}

// MutableCatalog provides caller-owned read/write access to cached models.
type MutableCatalog interface {
	Catalog
	SetModels(items []ModelInfo)
}

// Loader loads models from upstream source.
type Loader interface {
	Load(ctx context.Context) ([]ModelInfo, error)
}
