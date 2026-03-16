package middleware

import (
	"context"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
)

// TokenProvider retrieves Copilot API tokens for accounts.
type TokenProvider interface {
	GetToken(ctx context.Context, account config.Account) (string, error)
}

// ObservabilitySink records request lifecycle events and exposes snapshots.
type ObservabilitySink interface {
	RecordStart(record *core.RequestRecord)
	RecordFirstResponse(requestID string, statusCode int, duration time.Duration, upstreamPath string, isStream bool)
	RecordComplete(requestID string, statusCode int, duration time.Duration, upstreamPath string)
	AddEvent(event core.Event)
	Snapshot() core.Snapshot
}
