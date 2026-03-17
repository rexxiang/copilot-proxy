package stats

import (
	"testing"
	"time"

	"copilot-proxy/internal/runtime/observability"
)

func TestStatsServiceMonitorSnapshot(t *testing.T) {
	obs := observability.New(10, 10)
	record := &observability.RequestRecord{
		Timestamp:    time.Now(),
		Method:       "POST",
		Path:         "/v1/chat/completions",
		Model:        "gpt-5-mini",
		Account:      "alice",
		RequestID:    "req-123",
		StatusCode:   200,
		Duration:     150 * time.Millisecond,
		UpstreamPath: "/chat/completions",
		IsVision:     true,
		IsAgent:      false,
	}
	obs.RecordStart(record)
	obs.RecordComplete(record.RequestID, record.StatusCode, record.Duration, record.UpstreamPath)

	svc := NewService(obs)
	snapshot := svc.MonitorSnapshot()
	if len(snapshot.RecentRequests) != 1 {
		t.Fatalf("expected 1 recent request, got %d", len(snapshot.RecentRequests))
	}
	got := snapshot.RecentRequests[0]
	if got.RequestID != "req-123" || got.Model != "gpt-5-mini" {
		t.Fatalf("unexpected record %+v", got)
	}
}
