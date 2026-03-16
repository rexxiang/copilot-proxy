package observability

import (
	"sync"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector(100)
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}

	snap := c.Snapshot()
	if snap.TotalRequests != 0 {
		t.Errorf("expected 0 total requests, got %d", snap.TotalRequests)
	}
	if len(snap.ByModel) != 0 {
		t.Errorf("expected empty ByModel, got %d entries", len(snap.ByModel))
	}
}

func TestCollector_Record(t *testing.T) {
	c := NewCollector(100)

	now := time.Now()
	record := RequestRecord{
		Timestamp:  now,
		Path:       "/v1/chat/completions",
		Model:      "gpt-4o",
		Account:    "user1",
		StatusCode: 200,
		Duration:   100 * time.Millisecond,
		IsVision:   false,
		IsAgent:    false,
	}

	c.RecordLocal(&record)

	snap := c.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("expected 1 total request, got %d", snap.TotalRequests)
	}

	stats, ok := snap.ByModel["gpt-4o"]
	if !ok {
		t.Fatal("expected gpt-4o in ByModel")
	}
	if stats.Count != 1 {
		t.Errorf("expected count 1, got %d", stats.Count)
	}
	if stats.TotalTime != 100*time.Millisecond {
		t.Errorf("expected total time 100ms, got %v", stats.TotalTime)
	}

	if snap.ByStatus[200] != 1 {
		t.Errorf("expected 1 request with status 200, got %d", snap.ByStatus[200])
	}

	if len(snap.RecentRequests) != 1 {
		t.Errorf("expected 1 recent request, got %d", len(snap.RecentRequests))
	}
}

func TestCollector_RecordVisionAndAgent(t *testing.T) {
	c := NewCollector(100)

	c.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
		IsVision:   true,
		IsAgent:    false,
	})

	c.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
		IsVision:   false,
		IsAgent:    true,
	})

	snap := c.Snapshot()
	stats := snap.ByModel["gpt-4o"]
	if stats.VisionReqs != 1 {
		t.Errorf("expected 1 vision request, got %d", stats.VisionReqs)
	}
	if stats.AgentReqs != 1 {
		t.Errorf("expected 1 agent request, got %d", stats.AgentReqs)
	}
}

func TestCollector_AgentRequestTrackedSeparatelyInStatsAndHistory(t *testing.T) {
	c := NewCollector(100)
	now := time.Now()

	c.RecordLocal(&RequestRecord{
		Timestamp:  now,
		Model:      "gpt-4o",
		StatusCode: 200,
		Duration:   100 * time.Millisecond,
		IsAgent:    false,
	})
	c.RecordLocal(&RequestRecord{
		Timestamp:  now,
		Model:      "gpt-4o",
		StatusCode: 200,
		Duration:   900 * time.Millisecond,
		IsAgent:    true,
	})

	snap := c.Snapshot()
	if snap.TotalRequests != 1 {
		t.Fatalf("expected total requests to include only user requests, got %d", snap.TotalRequests)
	}
	stats, ok := snap.ByModel["gpt-4o"]
	if !ok {
		t.Fatalf("expected model stats for gpt-4o")
	}
	if stats.Count != 1 {
		t.Fatalf("expected model count to include only user requests, got %d", stats.Count)
	}
	if stats.AgentReqs != 1 {
		t.Fatalf("expected agent request counter to include agent requests, got %d", stats.AgentReqs)
	}
	if stats.TotalTime != 100*time.Millisecond {
		t.Fatalf("expected total time to include only user requests, got %v", stats.TotalTime)
	}
	if len(snap.RecentRequests) != 2 {
		t.Fatalf("expected recent requests to retain both user and agent records, got %d", len(snap.RecentRequests))
	}
}

func TestCollector_AgentOnlyRequestIncludedInModelStats(t *testing.T) {
	c := NewCollector(100)

	c.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
		IsAgent:    true,
	})

	snap := c.Snapshot()
	if snap.TotalRequests != 0 {
		t.Fatalf("expected zero user requests, got %d", snap.TotalRequests)
	}
	if len(snap.ByModel) != 1 {
		t.Fatalf("expected model stats for agent-only traffic, got %d", len(snap.ByModel))
	}
	stats, ok := snap.ByModel["gpt-4o"]
	if !ok {
		t.Fatalf("expected model stats for gpt-4o")
	}
	if stats.Count != 0 {
		t.Fatalf("expected user request count to remain zero, got %d", stats.Count)
	}
	if stats.AgentReqs != 1 {
		t.Fatalf("expected agent request count to be one, got %d", stats.AgentReqs)
	}
	if len(snap.RecentRequests) != 1 {
		t.Fatalf("expected recent requests to keep agent record, got %d", len(snap.RecentRequests))
	}
}

func TestCollector_RecordErrors(t *testing.T) {
	c := NewCollector(100)

	c.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 500,
	})
	c.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 429,
	})
	c.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	snap := c.Snapshot()
	stats := snap.ByModel["gpt-4o"]
	if stats.Errors != 2 {
		t.Errorf("expected 2 errors, got %d", stats.Errors)
	}
}

func TestCollector_ClientCanceledCountedAsTotalButNotError(t *testing.T) {
	c := NewCollector(100)

	c.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: StatusClientCanceled,
		IsAgent:    false,
	})

	snap := c.Snapshot()
	if snap.TotalRequests != 1 {
		t.Fatalf("expected canceled request to be included in total, got %d", snap.TotalRequests)
	}
	if snap.ByStatus[StatusClientCanceled] != 1 {
		t.Fatalf("expected canceled request status count to be 1, got %d", snap.ByStatus[StatusClientCanceled])
	}
	stats, ok := snap.ByModel["gpt-4o"]
	if !ok {
		t.Fatalf("expected model stats for canceled request")
	}
	if stats.Errors != 0 {
		t.Fatalf("expected canceled request not to count as error, got %d", stats.Errors)
	}
}

func TestCollector_MaxHistory(t *testing.T) {
	maxLen := 5
	c := NewCollector(maxLen)

	for range 10 {
		c.RecordLocal(&RequestRecord{
			Timestamp:  time.Now(),
			Model:      "gpt-4o",
			StatusCode: 200,
		})
	}

	snap := c.Snapshot()
	if len(snap.RecentRequests) != maxLen {
		t.Errorf("expected %d recent requests, got %d", maxLen, len(snap.RecentRequests))
	}
	if snap.TotalRequests != 10 {
		t.Errorf("expected 10 total requests, got %d", snap.TotalRequests)
	}
}

func TestCollector_Reset(t *testing.T) {
	c := NewCollector(100)

	c.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	c.Reset()

	snap := c.Snapshot()
	if snap.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after reset, got %d", snap.TotalRequests)
	}
	if len(snap.RecentRequests) != 0 {
		t.Errorf("expected empty recent requests after reset, got %d", len(snap.RecentRequests))
	}
}

func TestCollector_ConcurrentAccess(t *testing.T) {
	c := NewCollector(100)
	var wg sync.WaitGroup

	// Concurrent writes
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.RecordLocal(&RequestRecord{
				Timestamp:  time.Now(),
				Model:      "gpt-4o",
				StatusCode: 200,
			})
		}()
	}

	// Concurrent reads
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Snapshot()
		}()
	}

	wg.Wait()

	snap := c.Snapshot()
	if snap.TotalRequests != 100 {
		t.Errorf("expected 100 total requests, got %d", snap.TotalRequests)
	}
}

func TestCollector_MultipleModels(t *testing.T) {
	c := NewCollector(100)

	models := []string{"gpt-4o", "gpt-3.5-turbo", "claude-3-opus"}
	for _, model := range models {
		for range 3 {
			c.RecordLocal(&RequestRecord{
				Timestamp:  time.Now(),
				Model:      model,
				StatusCode: 200,
			})
		}
	}

	snap := c.Snapshot()
	if len(snap.ByModel) != 3 {
		t.Errorf("expected 3 models, got %d", len(snap.ByModel))
	}
	for _, model := range models {
		if snap.ByModel[model].Count != 3 {
			t.Errorf("expected 3 requests for %s, got %d", model, snap.ByModel[model].Count)
		}
	}
}

func TestCollector_RecordFirstResponseAndCompleteForStream(t *testing.T) {
	c := NewCollector(100)
	start := time.Now().Add(-2 * time.Second)

	c.RecordStart(&RequestRecord{
		Timestamp:  start,
		RequestID:  "req-stream-1",
		Model:      "gpt-4o",
		StatusCode: 0,
		Path:       "/v1/responses",
	})

	c.RecordFirstResponse("req-stream-1", 200, 150*time.Millisecond, "/responses", true)

	activeSnap := c.Snapshot()
	if len(activeSnap.ActiveRequests) != 1 {
		t.Fatalf("expected one active stream request, got %d", len(activeSnap.ActiveRequests))
	}
	active := activeSnap.ActiveRequests[0]
	if active.StatusCode != 200 {
		t.Fatalf("expected status to update to first response code 200, got %d", active.StatusCode)
	}
	if active.FirstResponseDuration != 150*time.Millisecond {
		t.Fatalf("expected first response duration 150ms, got %v", active.FirstResponseDuration)
	}
	if active.Duration != 150*time.Millisecond {
		t.Fatalf("expected active duration to mirror first response duration, got %v", active.Duration)
	}
	if !active.IsStream {
		t.Fatalf("expected active request to be marked as stream")
	}
	if !active.Streaming {
		t.Fatalf("expected active request to be marked as streaming")
	}
	if active.UpstreamPath != "/responses" {
		t.Fatalf("expected active upstream path /responses, got %q", active.UpstreamPath)
	}

	c.RecordComplete("req-stream-1", 200, 1800*time.Millisecond, "/responses")

	doneSnap := c.Snapshot()
	if len(doneSnap.ActiveRequests) != 0 {
		t.Fatalf("expected no active requests after completion, got %d", len(doneSnap.ActiveRequests))
	}
	if len(doneSnap.RecentRequests) != 1 {
		t.Fatalf("expected one completed record, got %d", len(doneSnap.RecentRequests))
	}
	done := doneSnap.RecentRequests[0]
	if done.Duration != 1800*time.Millisecond {
		t.Fatalf("expected completed total duration 1800ms, got %v", done.Duration)
	}
	if done.FirstResponseDuration != 150*time.Millisecond {
		t.Fatalf("expected completed first response duration to be preserved, got %v", done.FirstResponseDuration)
	}
	if !done.IsStream {
		t.Fatalf("expected completed record to remain stream-marked")
	}
	if done.Streaming {
		t.Fatalf("expected completed record to clear streaming state")
	}
}
