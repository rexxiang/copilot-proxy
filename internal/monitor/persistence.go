package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PersistentCollector wraps ThreadSafeCollector with file persistence.
type PersistentCollector struct {
	*ThreadSafeCollector
	filePath string
	mu       sync.Mutex
	dirty    bool
}

const (
	persistenceDirMode = 0o700
)

// persistedData represents the JSON structure for persistence.
// Request aggregates and records are session-only.
type persistedData struct {
	SavedAt time.Time `json:"saved_at"`
}

// NewPersistentCollector creates a collector that persists to file.
func NewPersistentCollector(maxHistory int, filePath string) *PersistentCollector {
	pc := &PersistentCollector{
		ThreadSafeCollector: NewCollector(maxHistory),
		filePath:            filePath,
	}
	pc.load()
	return pc
}

// RecordLocal adds a record and marks for persistence (internal use).
func (pc *PersistentCollector) RecordLocal(r *RequestRecord) {
	pc.ThreadSafeCollector.RecordLocal(r)
	pc.mu.Lock()
	pc.dirty = true
	pc.mu.Unlock()
}

// Record implements the MetricsRecorder interface.
func (pc *PersistentCollector) Record(r *RequestRecord) {
	pc.ThreadSafeCollector.Record(r)
	pc.mu.Lock()
	pc.dirty = true
	pc.mu.Unlock()
}

// Save persists current state to file.
func (pc *PersistentCollector) Save() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if !pc.dirty {
		return nil
	}

	now := time.Now()
	data := persistedData{SavedAt: now}

	// Ensure directory exists
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

// load restores state from file.
func (pc *PersistentCollector) load() {
	file, err := os.Open(pc.filePath)
	if err != nil {
		return // File doesn't exist, start fresh
	}
	defer func() {
		_ = file.Close()
	}()

	if err := json.NewDecoder(file).Decode(&persistedData{}); err != nil {
		return // Corrupted file, start fresh
	}
}

// StartAutoSave starts a goroutine that saves periodically.
func (pc *PersistentCollector) StartAutoSave(interval time.Duration, stop <-chan struct{}) {
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
