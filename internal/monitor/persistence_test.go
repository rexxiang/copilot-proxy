package monitor

import (
	"os"
	"path/filepath"
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

	// Load into new collector - only activity data is restored
	pc2 := NewPersistentCollector(100, filePath)
	snap := pc2.Snapshot()

	// Records, by_model, by_status are NOT persisted (session-only)
	if snap.TotalRequests != 0 {
		t.Errorf("expected 0 total requests after load (not persisted), got %d", snap.TotalRequests)
	}
	if len(snap.ByModel) != 0 {
		t.Errorf("expected 0 models after load (not persisted), got %d", len(snap.ByModel))
	}

	// Activity data IS persisted
	// The 2 records should have created entries in activity_hour and activity_day
	if len(snap.ActivityHour) == 0 {
		t.Error("expected activity_hour data to be persisted")
	}
	if len(snap.ActivityDay) == 0 {
		t.Error("expected activity_day data to be persisted")
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

func TestPersistentCollector_SaveLoadPersistsAgentActivity(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "agent_metrics.json")

	pc := NewPersistentCollector(100, filePath)
	pc.RecordLocal(&RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4.1",
		StatusCode: 200,
		IsAgent:    true,
	})
	if err := pc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	pc2 := NewPersistentCollector(100, filePath)
	snap := pc2.Snapshot()

	if len(snap.ActivityHour) == 0 {
		t.Fatal("expected agent activity_hour data to be persisted")
	}
	if len(snap.ActivityDay) == 0 {
		t.Fatal("expected agent activity_day data to be persisted")
	}
}
