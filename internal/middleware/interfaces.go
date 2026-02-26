package middleware

import (
	"context"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/monitor"
)

// TokenProvider retrieves Copilot API tokens for accounts.
type TokenProvider interface {
	GetToken(ctx context.Context, account config.Account) (string, error)
}

// MetricsRecorder records request statistics.
type MetricsRecorder interface {
	RecordStart(record *monitor.RequestRecord)
	RecordComplete(requestID string, statusCode int, duration time.Duration, upstreamPath string)
	Record(record *monitor.RequestRecord)
}

// DebugLogger logs detailed request/response information.
type DebugLogger interface {
	Log(entry *monitor.DebugLogEntry) error
}
