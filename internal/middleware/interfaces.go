package middleware

import (
	"time"

	core "copilot-proxy/internal/runtime/types"
)

// ObservabilitySink records request lifecycle events and exposes snapshots.
type ObservabilitySink interface {
	RecordStart(record *core.RequestRecord)
	RecordFirstResponse(requestID string, statusCode int, duration time.Duration, upstreamPath string, isStream bool)
	RecordComplete(requestID string, statusCode int, duration time.Duration, upstreamPath string)
	AddEvent(event core.Event)
	Snapshot() core.Snapshot
}
