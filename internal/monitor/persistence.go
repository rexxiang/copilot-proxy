package monitor

import "copilot-proxy/internal/core/observability"

// PersistentCollector wraps a collector with file persistence.
type PersistentCollector = observability.PersistentCollector

// NewPersistentCollector creates a collector that persists to file.
func NewPersistentCollector(maxHistory int, filePath string) *PersistentCollector {
	return observability.NewPersistentCollector(maxHistory, filePath)
}
