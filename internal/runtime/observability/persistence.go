package observability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const persistenceDirMode = 0o700

// persistedData represents the JSON structure for persistence.
type persistedData struct {
	SavedAt time.Time `json:"saved_at"`
}

// PersistentCollector wraps a collector with file persistence.
type PersistentCollector struct {
	*Collector
	ThreadSafeCollector *Collector
	filePath            string
	mu                  sync.Mutex
	dirty               bool
}

// NewPersistentCollector creates a collector that persists to file.
func NewPersistentCollector(maxHistory int, filePath string) *PersistentCollector {
	collector := NewCollector(maxHistory)
	pc := &PersistentCollector{
		Collector:           collector,
		ThreadSafeCollector: collector,
		filePath:            filePath,
	}
	pc.load()
	return pc
}

// RecordLocal adds a record and marks for persistence.
func (pc *PersistentCollector) RecordLocal(r *RequestRecord) {
	if pc == nil || pc.Collector == nil {
		return
	}
	pc.Collector.RecordLocal(r)
	pc.mu.Lock()
	pc.dirty = true
	pc.mu.Unlock()
}

// Record records a request record and marks dirty.
func (pc *PersistentCollector) Record(r *RequestRecord) {
	if pc == nil || pc.Collector == nil {
		return
	}
	pc.Collector.Record(r)
	pc.mu.Lock()
	pc.dirty = true
	pc.mu.Unlock()
}

// Save persists current state to file.
func (pc *PersistentCollector) Save() error {
	if pc == nil {
		return nil
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if !pc.dirty {
		return nil
	}

	now := time.Now()
	data := persistedData{SavedAt: now}

	dir := filepath.Dir(pc.filePath)
	if err := os.MkdirAll(dir, persistenceDirMode); err != nil {
		return fmt.Errorf("create metrics dir: %w", err)
	}

	file, err := os.Create(pc.filePath)
	if err != nil {
		return fmt.Errorf("create metrics file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("encode metrics: %w", err)
	}

	pc.dirty = false
	return nil
}

// load restores persisted state, ignoring failures.
func (pc *PersistentCollector) load() {
	if pc == nil {
		return
	}
	file, err := os.Open(pc.filePath)
	if err != nil {
		return
	}
	defer func() {
		_ = file.Close()
	}()

	_ = json.NewDecoder(file).Decode(&persistedData{})
}

// StartAutoSave runs a goroutine that saves periodically.
func (pc *PersistentCollector) StartAutoSave(interval time.Duration, stop <-chan struct{}) {
	if pc == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = pc.Save()
			case <-stop:
				_ = pc.Save()
				return
			}
		}
	}()
}

// Observability exposes the underlying collector.
func (pc *PersistentCollector) Observability() *Observability {
	if pc == nil || pc.Collector == nil {
		return nil
	}
	return pc.Collector.Observability()
}
