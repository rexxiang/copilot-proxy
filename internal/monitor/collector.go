package monitor

import (
	"sync"
	"time"
)

// Collector defines the interface for request statistics collection.
type Collector interface {
	RecordLocal(r *RequestRecord)
	Snapshot() Snapshot
	Reset()
}

// StatsResetter can clear aggregate counters while retaining request history.
type StatsResetter interface {
	ResetStats()
}

// DebugCollector extends Collector with debug logging capabilities.
type DebugCollector interface {
	Collector
	DebugEnabled() bool
	SetDebugEnabled(enabled bool)
	SetMaxDebugBodySize(size int)
	RecordWithDebug(r *RequestRecord, debug *DebugInfo)
}

// DefaultMaxDebugBodySize is the default max size for request/response body capture.
const DefaultMaxDebugBodySize = 4096

const (
	statusErrorMin = 400
)

// ThreadSafeCollector provides thread-safe request statistics collection.
type ThreadSafeCollector struct {
	mu               sync.RWMutex
	records          []RequestRecord
	maxLen           int
	byModel          map[string]*ModelStats
	byStatus         map[int]int64
	total            int64
	debugEnabled     bool
	maxDebugBodySize int
	// Active requests tracking (StatusCode == 0)
	activeRequests map[string]*RequestRecord // key: requestID
}

// NewCollector creates a new ThreadSafeCollector with the specified max history length.
func NewCollector(maxHistory int) *ThreadSafeCollector {
	return &ThreadSafeCollector{
		maxLen:           maxHistory,
		records:          make([]RequestRecord, 0, maxHistory),
		byModel:          make(map[string]*ModelStats),
		byStatus:         make(map[int]int64),
		maxDebugBodySize: DefaultMaxDebugBodySize,
		activeRequests:   make(map[string]*RequestRecord),
	}
}

// RecordLocal adds a new request record to the collector (internal use).
func (c *ThreadSafeCollector) RecordLocal(r *RequestRecord) {
	if r == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recordInternal(r)
}

// RecordStart records the beginning of a request (StatusCode == 0).
func (c *ThreadSafeCollector) RecordStart(r *RequestRecord) {
	if r == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Store a copy to avoid external modifications
	record := *r
	c.activeRequests[r.RequestID] = &record
}

// RecordFirstResponse updates an active request when the first upstream response arrives.
func (c *ThreadSafeCollector) RecordFirstResponse(
	requestID string,
	statusCode int,
	duration time.Duration,
	upstreamPath string,
	isStream bool,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	record, ok := c.activeRequests[requestID]
	if !ok {
		return
	}
	record.StatusCode = statusCode
	record.Duration = duration
	record.FirstResponseDuration = duration
	record.IsStream = isStream
	record.Streaming = isStream
	if upstreamPath != "" {
		record.UpstreamPath = upstreamPath
	}
}

// RecordComplete records the completion of a request and moves it from active to completed.
func (c *ThreadSafeCollector) RecordComplete(requestID string, statusCode int, duration time.Duration, upstreamPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Find the active request
	if record, ok := c.activeRequests[requestID]; ok {
		// Update status, duration, and upstream path
		record.StatusCode = statusCode
		record.Duration = duration
		if upstreamPath != "" {
			record.UpstreamPath = upstreamPath
		}
		if record.IsStream {
			record.Streaming = false
			if record.FirstResponseDuration <= 0 {
				record.FirstResponseDuration = duration
			}
		}

		// Move to completed records
		c.recordInternal(record)

		// Remove from active requests
		delete(c.activeRequests, requestID)
	}
}

// Snapshot returns a point-in-time copy of all statistics.
func (c *ThreadSafeCollector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Deep copy model stats
	byModel := make(map[string]*ModelStats, len(c.byModel))
	for k, v := range c.byModel {
		stats := *v
		byModel[k] = &stats
	}

	// Copy status counts
	byStatus := make(map[int]int64, len(c.byStatus))
	for k, v := range c.byStatus {
		byStatus[k] = v
	}

	// Copy recent requests
	recentRequests := make([]RequestRecord, len(c.records))
	copy(recentRequests, c.records)

	// Copy active requests
	activeRequests := make([]RequestRecord, 0, len(c.activeRequests))
	for _, record := range c.activeRequests {
		activeRequests = append(activeRequests, *record)
	}

	return Snapshot{
		TotalRequests:  c.total,
		ByModel:        byModel,
		ByStatus:       byStatus,
		RecentRequests: recentRequests,
		ActiveRequests: activeRequests,
	}
}

// Reset clears all collected statistics.
func (c *ThreadSafeCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.records = make([]RequestRecord, 0, c.maxLen)
	c.byModel = make(map[string]*ModelStats)
	c.byStatus = make(map[int]int64)
	c.activeRequests = make(map[string]*RequestRecord)
	c.total = 0
}

// ResetStats clears aggregate counters while keeping recent request logs.
func (c *ThreadSafeCollector) ResetStats() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.byModel = make(map[string]*ModelStats)
	c.byStatus = make(map[int]int64)
	c.total = 0
}

// Record implements the MetricsRecorder interface.
func (c *ThreadSafeCollector) Record(r *RequestRecord) {
	c.RecordLocal(r)
}

// DebugEnabled returns whether debug logging is enabled.
func (c *ThreadSafeCollector) DebugEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.debugEnabled
}

// SetDebugEnabled enables or disables debug logging.
func (c *ThreadSafeCollector) SetDebugEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.debugEnabled = enabled
}

// SetMaxDebugBodySize sets the maximum size for captured request/response bodies.
func (c *ThreadSafeCollector) SetMaxDebugBodySize(size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxDebugBodySize = size
}

// RecordWithDebug adds a request record with optional debug information.
// Debug info is only stored when debug mode is enabled.
func (c *ThreadSafeCollector) RecordWithDebug(r *RequestRecord, debug *DebugInfo) {
	if r == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Only attach debug info if debug mode is enabled
	if c.debugEnabled && debug != nil {
		// Truncate bodies to max size
		truncatedDebug := &DebugInfo{
			RequestHeaders:  debug.RequestHeaders,
			RequestBody:     "",
			ResponseHeaders: debug.ResponseHeaders,
			ResponseBody:    "",
			UpstreamURL:     debug.UpstreamURL,
			Error:           debug.Error,
		}
		if len(debug.RequestBody) > c.maxDebugBodySize {
			truncatedDebug.RequestBody = debug.RequestBody[:c.maxDebugBodySize] + "..."
		} else {
			truncatedDebug.RequestBody = debug.RequestBody
		}
		if len(debug.ResponseBody) > c.maxDebugBodySize {
			truncatedDebug.ResponseBody = debug.ResponseBody[:c.maxDebugBodySize] + "..."
		} else {
			truncatedDebug.ResponseBody = debug.ResponseBody
		}
		r.Debug = truncatedDebug
	}

	c.recordInternal(r)
}

// recordInternal is the internal record method (must be called with lock held).
func (c *ThreadSafeCollector) recordInternal(r *RequestRecord) {
	if r == nil {
		return
	}
	// Keep top-level totals/status as user-only counters.
	if !r.IsAgent {
		c.total++
		c.byStatus[r.StatusCode]++
	}

	if r.Model != "" {
		stats, ok := c.byModel[r.Model]
		if !ok {
			stats = &ModelStats{}
			c.byModel[r.Model] = stats
		}
		if r.IsAgent {
			stats.AgentReqs++
			if r.StatusCode >= statusErrorMin && r.StatusCode != StatusClientCanceled {
				stats.AgentErrors++
			}
		} else {
			stats.Count++
			stats.TotalTime += r.Duration
			if r.IsVision {
				stats.VisionReqs++
			}
			if r.StatusCode >= statusErrorMin && r.StatusCode != StatusClientCanceled {
				stats.Errors++
			}
		}
	}

	// Add to recent records (ring buffer behavior)
	if len(c.records) >= c.maxLen {
		// Shift records left, dropping oldest
		copy(c.records, c.records[1:])
		c.records[len(c.records)-1] = *r
	} else {
		c.records = append(c.records, *r)
	}
}

// RecordRequestWithDebugInfo records a request with debug information.
// This method is used by proxy.Handler when debug mode is enabled.
func (c *ThreadSafeCollector) RecordRequestWithDebugInfo(
	path, model, account string,
	statusCode int,
	duration time.Duration,
	isVision, isAgent bool,
	requestHeaders map[string]string,
	requestBody string,
	responseHeaders map[string]string,
	responseBody string,
	upstreamURL string,
	errMsg string,
) {
	record := &RequestRecord{
		Timestamp:  time.Now(),
		Path:       path,
		Model:      model,
		Account:    account,
		StatusCode: statusCode,
		Duration:   duration,
		IsVision:   isVision,
		IsAgent:    isAgent,
		Debug:      &DebugInfo{},
	}

	debug := &DebugInfo{
		RequestHeaders:  requestHeaders,
		RequestBody:     requestBody,
		ResponseHeaders: responseHeaders,
		ResponseBody:    responseBody,
		UpstreamURL:     upstreamURL,
		Error:           errMsg,
	}

	c.RecordWithDebug(record, debug)
}
