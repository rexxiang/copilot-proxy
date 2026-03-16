package monitor

import "copilot-proxy/internal/core/observability"

// Collector defines the interface for request statistics collection.
type Collector interface {
	RecordLocal(r *RequestRecord)
	Snapshot() Snapshot
	Reset()
}

// StatsResetter can clear aggregate counters while retaining request history.
type StatsResetter interface {
	ResetStats()
}

// DebugCollector extends Collector with debug logging capabilities.
type DebugCollector interface {
	Collector
	DebugEnabled() bool
	SetDebugEnabled(enabled bool)
	SetMaxDebugBodySize(size int)
	RecordWithDebug(r *RequestRecord, debug *DebugInfo)
}

// ThreadSafeCollector is the resurfacing alias for the core collector implementation.
type ThreadSafeCollector = observability.Collector

// DefaultMaxDebugBodySize is the default max size for request/response body capture.
const DefaultMaxDebugBodySize = observability.DefaultMaxDebugBodySize

// Ensure compatibility with the legacy interfaces.
var (
	_ Collector      = (*ThreadSafeCollector)(nil)
	_ StatsResetter  = (*ThreadSafeCollector)(nil)
	_ DebugCollector = (*ThreadSafeCollector)(nil)
)

// NewCollector creates a new ThreadSafeCollector with the specified max history length.
func NewCollector(maxHistory int) *ThreadSafeCollector {
	return observability.NewCollector(maxHistory)
}
