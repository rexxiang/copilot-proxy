package models

import (
	"sync"
	"time"
)

// Manager provides caller-owned caching for models data.
type Manager struct {
	mu       sync.RWMutex
	models   []ModelInfo
	loadedAt time.Time
}

// NewManager returns a fresh catalog instance owned by the caller.
func NewManager() *Manager {
	return &Manager{}
}

// GetModels returns a copy of the cached models.
func (m *Manager) GetModels() []ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ModelInfo, len(m.models))
	copy(result, m.models)
	return result
}

// SetModels updates the cached models.
func (m *Manager) SetModels(items []ModelInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.models = make([]ModelInfo, len(items))
	copy(m.models, items)
	m.loadedAt = time.Now()
}

// HasModels returns true if models are cached.
func (m *Manager) HasModels() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.models) > 0
}

// LoadedAt returns the time when models were last loaded.
func (m *Manager) LoadedAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadedAt
}
