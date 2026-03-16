package cli

import (
	"testing"
	"time"

	"copilot-proxy/internal/core"
)

func TestDetector_NewEligibleDetectedOnce(t *testing.T) {
	detector := newAgentPremiumRefreshDetector()
	premiumSet := map[string]struct{}{"gpt-4o": {}}
	now := time.Now()

	snapshot := core.Snapshot{
		RecentRequests: []core.RequestRecord{
			{
				RequestID:  "req-1",
				Timestamp:  now,
				Model:      "gpt-4o",
				StatusCode: 200,
				IsAgent:    true,
			},
		},
	}

	if !detector.HasNewEligible(snapshot, premiumSet) {
		t.Fatalf("expected first eligible record to be detected as new")
	}
	if detector.HasNewEligible(snapshot, premiumSet) {
		t.Fatalf("expected identical eligible record not to be detected as new twice")
	}
}

func TestDetector_RepeatedTickNoDuplicate(t *testing.T) {
	detector := newAgentPremiumRefreshDetector()
	premiumSet := map[string]struct{}{"gpt-4o": {}}
	now := time.Now()

	first := core.Snapshot{
		RecentRequests: []core.RequestRecord{
			{
				RequestID:  "req-1",
				Timestamp:  now,
				Model:      "gpt-4o",
				StatusCode: 200,
				IsAgent:    true,
			},
		},
	}
	second := core.Snapshot{
		RecentRequests: []core.RequestRecord{
			{
				RequestID:  "req-1",
				Timestamp:  now,
				Model:      "gpt-4o",
				StatusCode: 200,
				IsAgent:    true,
			},
			{
				RequestID:  "req-2",
				Timestamp:  now.Add(time.Second),
				Model:      "gpt-4o",
				StatusCode: 200,
				IsAgent:    true,
			},
		},
	}

	if !detector.HasNewEligible(first, premiumSet) {
		t.Fatalf("expected first snapshot to contain new eligible records")
	}
	if !detector.HasNewEligible(second, premiumSet) {
		t.Fatalf("expected second snapshot to detect newly added eligible record")
	}
	if detector.HasNewEligible(second, premiumSet) {
		t.Fatalf("expected repeated snapshot not to produce duplicate detection")
	}
}

func TestDetector_NonPremiumOrNonAgentOrErrorIgnored(t *testing.T) {
	detector := newAgentPremiumRefreshDetector()
	premiumSet := map[string]struct{}{"gpt-4o": {}}
	now := time.Now()

	snapshot := core.Snapshot{
		RecentRequests: []core.RequestRecord{
			{
				RequestID:  "user-premium",
				Timestamp:  now,
				Model:      "gpt-4o",
				StatusCode: 200,
				IsAgent:    false,
			},
			{
				RequestID:  "agent-non-premium",
				Timestamp:  now.Add(time.Second),
				Model:      "gpt-4o-mini",
				StatusCode: 200,
				IsAgent:    true,
			},
			{
				RequestID:  "agent-premium-error",
				Timestamp:  now.Add(2 * time.Second),
				Model:      "gpt-4o",
				StatusCode: 500,
				IsAgent:    true,
			},
		},
	}

	if detector.HasNewEligible(snapshot, premiumSet) {
		t.Fatalf("expected non-eligible records not to trigger detection")
	}
}
