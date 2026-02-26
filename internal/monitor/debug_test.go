package monitor

import (
	"testing"
	"time"
)

func TestDebugInfoTruncation(t *testing.T) {
	info := DebugInfo{
		RequestHeaders: map[string]string{
			"Authorization": "Bearer sk-secret-token-123",
			"Content-Type":  "application/json",
		},
		RequestBody:     `{"model":"gpt-4","messages":[{"role":"user","content":"Hello world"}]}`,
		ResponseHeaders: map[string]string{"X-Request-Id": "abc123"},
		ResponseBody:    `{"choices":[{"message":{"content":"Hi there!"}}]}`,
	}

	// Test truncation
	truncated := info.TruncatedRequestBody(20)
	if len(truncated) > 23 { // 20 + "..."
		t.Errorf("TruncatedRequestBody should be <= 23 chars, got %d", len(truncated))
	}

	// Test header masking
	masked := info.MaskedHeaders()
	if masked["Authorization"] != "Bearer sk-***" {
		t.Errorf("Authorization header not masked: %s", masked["Authorization"])
	}
	if masked["Content-Type"] != "application/json" {
		t.Errorf("Content-Type should not be masked: %s", masked["Content-Type"])
	}
}

func TestCollectorDebugMode(t *testing.T) {
	c := NewCollector(100)

	// Debug mode should be off by default
	if c.DebugEnabled() {
		t.Error("Debug mode should be off by default")
	}

	// Enable debug mode
	c.SetDebugEnabled(true)
	if !c.DebugEnabled() {
		t.Error("Debug mode should be on after enabling")
	}

	// Record with debug info
	c.RecordWithDebug(&RequestRecord{
		Timestamp:  time.Now(),
		Path:       "/v1/chat/completions",
		Model:      "gpt-4",
		StatusCode: 200,
	}, &DebugInfo{
		RequestBody:  `{"model":"gpt-4"}`,
		ResponseBody: `{"choices":[]}`,
	})

	snapshot := c.Snapshot()
	if len(snapshot.RecentRequests) != 1 {
		t.Fatalf("Expected 1 request, got %d", len(snapshot.RecentRequests))
	}

	// Debug info should be present when debug mode is enabled
	if snapshot.RecentRequests[0].Debug == nil {
		t.Error("Debug info should be present when debug mode is enabled")
	}

	// Disable debug mode and record again
	c.SetDebugEnabled(false)
	c.RecordWithDebug(&RequestRecord{
		Timestamp:  time.Now(),
		Path:       "/v1/chat/completions",
		Model:      "gpt-4",
		StatusCode: 200,
	}, &DebugInfo{
		RequestBody:  `{"model":"gpt-4"}`,
		ResponseBody: `{"choices":[]}`,
	})

	snapshot = c.Snapshot()
	if len(snapshot.RecentRequests) != 2 {
		t.Fatalf("Expected 2 requests, got %d", len(snapshot.RecentRequests))
	}

	// Debug info should NOT be present when debug mode is disabled
	if snapshot.RecentRequests[1].Debug != nil {
		t.Error("Debug info should be nil when debug mode is disabled")
	}
}

func TestCollectorMaxDebugBodySize(t *testing.T) {
	c := NewCollector(100)
	c.SetDebugEnabled(true)
	c.SetMaxDebugBodySize(50)

	largeBody := make([]byte, 100)
	for i := range largeBody {
		largeBody[i] = 'a'
	}

	c.RecordWithDebug(&RequestRecord{
		Timestamp:  time.Now(),
		Path:       "/test",
		StatusCode: 200,
	}, &DebugInfo{
		RequestBody:  string(largeBody),
		ResponseBody: string(largeBody),
	})

	snapshot := c.Snapshot()
	if snapshot.RecentRequests[0].Debug == nil {
		t.Fatal("Debug info should be present")
	}

	// Body should be truncated
	if len(snapshot.RecentRequests[0].Debug.RequestBody) > 53 { // 50 + "..."
		t.Errorf("Request body should be truncated, got %d chars", len(snapshot.RecentRequests[0].Debug.RequestBody))
	}
}

func TestDebugCollectorInterface(t *testing.T) {
	c := NewCollector(100)

	// Verify DebugCollector interface is implemented
	var dc DebugCollector = c
	if dc != c {
		t.Error("ThreadSafeCollector should implement DebugCollector")
	}
}
