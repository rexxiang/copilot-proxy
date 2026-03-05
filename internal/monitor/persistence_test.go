package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistentCollector_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_metrics.json")

	// Create and populate collector
	pc := NewPersistentCollector(100, filePath)
	pc.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
		Duration:   100 * time.Millisecond,
	})
	pc.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "claude-3-opus",
		StatusCode: 500,
		Duration:   200 * time.Millisecond,
	})

	// Save
	if err := pc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("metrics file was not created")
	}

	contents, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	if !strings.Contains(string(contents), `"saved_at"`) {
		t.Fatalf("expected persisted file to include saved_at")
	}
	if strings.Contains(string(contents), `"activity_hour"`) || strings.Contains(string(contents), `"activity_day"`) {
		t.Fatalf("expected persisted file to omit legacy activity keys")
	}

	// Load into new collector - stats data remains session-only
	pc2 := NewPersistentCollector(100, filePath)
	snap := pc2.Snapshot()

	// Records, by_model, by_status are NOT persisted (session-only)
	if snap.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after load (not persisted), got %d", snap.TotalRequests)
	}
	if len(snap.ByModel) != 0 {
		t.Errorf("expected 0 models after load (not persisted), got %d", len(snap.ByModel))
	}
}

func TestPersistentCollector_LoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent.json")

	// Should not panic or error, just start fresh
	pc := NewPersistentCollector(100, filePath)
	snap := pc.Snapshot()

	if snap.TotalRequests != 0 {
		t.Errorf("expected 0 requests for new collector, got %d", snap.TotalRequests)
	}
}

func TestPersistentCollector_LoadCorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "corrupted.json")

	// Write corrupted data
	if err := os.WriteFile(filePath, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Should not panic, just start fresh
	pc := NewPersistentCollector(100, filePath)
	snap := pc.Snapshot()

	if snap.TotalRequests != 0 {
		t.Errorf("expected 0 requests for corrupted file, got %d", snap.TotalRequests)
	}
}

func TestPersistentCollector_DirtyFlag(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "metrics.json")

	pc := NewPersistentCollector(100, filePath)

	// First save should do nothing (not dirty)
	if err := pc.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("file should not exist when not dirty")
	}

	// Record makes it dirty
	pc.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	// Now save should create file
	if err := pc.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("file should exist after save when dirty")
	}
}

func TestPersistentCollector_RecordRequest(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "metrics.json")

	pc := NewPersistentCollector(100, filePath)

	// Use Record interface method
	pc.Record(&RequestRecord{
		Timestamp:    time.Now(),
		Method:       "POST",
		Path:         "/v1/chat/completions",
		UpstreamPath: "/chat/completions",
		Model:        "gpt-4o",
		Account:      "user1",
		StatusCode:   200,
		Duration:     100 * time.Millisecond,
		IsVision:     false,
		IsAgent:      false,
	})

	snap := pc.Snapshot()
	if snap.TotalRequests != 1 {
		t.Errorf("expected 1 request, got %d", snap.TotalRequests)
	}
}

func TestPersistentCollector_LoadIgnoresLegacyActivityData(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "legacy_metrics.json")

	legacy := `{
  "activity_hour": {"2026-03-05 09": 12},
  "activity_day": {"2026-03-05": 34},
  "saved_at": "2026-03-05T09:00:00Z"
}`
	if err := os.WriteFile(filePath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy metrics: %v", err)
	}

	pc := NewPersistentCollector(100, filePath)
	snap := pc.Snapshot()
	if snap.TotalRequests != 0 || len(snap.ByModel) != 0 || len(snap.RecentRequests) != 0 {
		t.Fatalf("expected clean session state after loading legacy metrics file")
	}

	pc.RecordLocal(&RequestRecord{Timestamp: time.Now(), Model: "gpt-4.1", StatusCode: 200})
	if err := pc.Save(); err != nil {
		t.Fatalf("save updated metrics: %v", err)
	}

	contents, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read updated metrics file: %v", err)
	}
	if strings.Contains(string(contents), `"activity_hour"`) || strings.Contains(string(contents), `"activity_day"`) {
		t.Fatalf("expected updated metrics file to drop legacy activity keys")
	}
}
