package models

import "context"

// Catalog provides read-only access to cached models.
type Catalog interface {
	GetModels() []ModelInfo
}

// Loader loads models from upstream source.
type Loader interface {
	Load(ctx context.Context) ([]ModelInfo, error)
}
