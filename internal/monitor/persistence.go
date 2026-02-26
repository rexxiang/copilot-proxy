package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// PersistentCollector wraps ThreadSafeCollector with file persistence.
type PersistentCollector struct {
	*ThreadSafeCollector
	filePath string
	mu       sync.Mutex
	dirty    bool
}

const (
	hourRetentionDays  = 7
	dayRetentionDays   = 365
	dayHours           = 24
	persistenceDirMode = 0o700
)

// persistedData represents the JSON structure for persistence.
// Only activity data is persisted; records, by_model, by_status are session-only.
type persistedData struct {
	ActivityHour map[string]int `json:"activity_hour"` // "YYYY-MM-DD HH" -> count
	ActivityDay  map[string]int `json:"activity_day"`  // "YYYY-MM-DD" -> count
	SavedAt      time.Time      `json:"saved_at"`
}

// NewPersistentCollector creates a collector that persists to file.
func NewPersistentCollector(maxHistory int, filePath string) *PersistentCollector {
	pc := &PersistentCollector{
		ThreadSafeCollector: NewCollector(maxHistory),
		filePath:            filePath,
	}
	pc.load()
	return pc
}

// RecordLocal adds a record and marks for persistence (internal use).
func (pc *PersistentCollector) RecordLocal(r *RequestRecord) {
	pc.ThreadSafeCollector.RecordLocal(r)
	pc.mu.Lock()
	pc.dirty = true
	pc.mu.Unlock()
}

// Record implements the MetricsRecorder interface.
func (pc *PersistentCollector) Record(r *RequestRecord) {
	pc.ThreadSafeCollector.Record(r)
	pc.mu.Lock()
	pc.dirty = true
	pc.mu.Unlock()
}

// Save persists current state to file.
func (pc *PersistentCollector) Save() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if !pc.dirty {
		return nil
	}

	snap := pc.Snapshot()
	now := time.Now()

	// Retention periods
	hourRetention, dayRetention := activityRetentionDurations()

	// Convert time.Time keys to simplified string format, filtering old data
	// Hour: "YYYY-MM-DD HH" (e.g., "2024-01-28 14")
	activityHour := make(map[string]int)
	hourCutoff := now.Add(-hourRetention)
	for k, v := range snap.ActivityHour {
		if k.After(hourCutoff) {
			activityHour[k.Format("2006-01-02 15")] = v
		}
	}

	// Day: "YYYY-MM-DD" (e.g., "2024-01-28")
	activityDay := make(map[string]int)
	dayCutoff := now.Add(-dayRetention)
	for k, v := range snap.ActivityDay {
		if k.After(dayCutoff) {
			activityDay[k.Format("2006-01-02")] = v
		}
	}

	data := persistedData{
		ActivityHour: activityHour,
		ActivityDay:  activityDay,
		SavedAt:      now,
	}

	// Ensure directory exists
	dir := filepath.Dir(pc.filePath)
	if err := os.MkdirAll(dir, persistenceDirMode); err != nil {
		return fmt.Errorf("create metrics dir: %w", err)
	}

	file, err := os.Create(pc.filePath)
	if err != nil {
		return fmt.Errorf("create metrics file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("encode metrics: %w", err)
	}

	pc.dirty = false
	return nil
}

// load restores state from file.
func (pc *PersistentCollector) load() {
	file, err := os.Open(pc.filePath)
	if err != nil {
		return // File doesn't exist, start fresh
	}
	defer func() {
		_ = file.Close()
	}()

	var data persistedData
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return // Corrupted file, start fresh
	}

	now := time.Now()

	// Retention periods
	hourRetention, dayRetention := activityRetentionDurations()

	// Restore activity maps only (other data is session-only)
	pc.ThreadSafeCollector.mu.Lock()
	defer pc.ThreadSafeCollector.mu.Unlock()

	// Restore activity hour map: "YYYY-MM-DD HH" -> time.Time
	pc.activityHour = make(map[time.Time]int)
	hourCutoff := now.Add(-hourRetention)
	for k, v := range data.ActivityHour {
		if t, err := time.ParseInLocation("2006-01-02 15", k, time.Local); err == nil {
			if t.After(hourCutoff) {
				pc.activityHour[t] = v
			}
		}
	}

	// Restore activity day map: "YYYY-MM-DD" -> time.Time
	pc.activityDay = make(map[time.Time]int)
	dayCutoff := now.Add(-dayRetention)
	for k, v := range data.ActivityDay {
		if t, err := time.ParseInLocation("2006-01-02", k, time.Local); err == nil {
			if t.After(dayCutoff) {
				pc.activityDay[t] = v
			}
		}
	}
}

func activityRetentionDurations() (hourRetention time.Duration, dayRetention time.Duration) {
	return hourRetentionDays * dayHours * time.Hour, dayRetentionDays * dayHours * time.Hour
}

// StartAutoSave starts a goroutine that saves periodically.
func (pc *PersistentCollector) StartAutoSave(interval time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = pc.Save()
			case <-stop:
				_ = pc.Save()
				return
			}
		}
	}()
}
