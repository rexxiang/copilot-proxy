package observability

import (
	"sync"
	"time"
)

const DefaultMaxDebugBodySize = 4096

// Collector collects request lifecycle data and exposes lifecycle sinks.
type Collector struct {
	mu  sync.RWMutex
	obs *Observability

	debugEnabled     bool
	maxDebugBodySize int
}

// NewCollector creates a collector with the requested history.
func NewCollector(maxHistory int) *Collector {
	return &Collector{
		obs:              New(maxHistory, 0),
		maxDebugBodySize: DefaultMaxDebugBodySize,
	}
}

// RecordLocal records a fully populated request record.
func (c *Collector) RecordLocal(r *RequestRecord) {
	if c == nil || r == nil {
		return
	}
	c.obs.Record(r)
}

// RecordStart tracks an active request prior to completion.
func (c *Collector) RecordStart(r *RequestRecord) {
	if c == nil || r == nil {
		return
	}
	c.obs.RecordStart(r)
}

// RecordFirstResponse updates partial data when the upstream responds.
func (c *Collector) RecordFirstResponse(requestID string, statusCode int, duration time.Duration, upstreamPath string, isStream bool) {
	if c == nil {
		return
	}
	c.obs.RecordFirstResponse(requestID, statusCode, duration, upstreamPath, isStream)
}

// RecordComplete marks an active request as finished.
func (c *Collector) RecordComplete(requestID string, statusCode int, duration time.Duration, upstreamPath string) {
	if c == nil {
		return
	}
	c.obs.RecordComplete(requestID, statusCode, duration, upstreamPath)
}

// Snapshot returns the current metrics snapshot.
func (c *Collector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	return c.obs.Snapshot()
}

// Reset clears every captured slice.
func (c *Collector) Reset() {
	if c == nil {
		return
	}
	c.obs.Reset()
}

// ResetStats clears counters but keeps history.
func (c *Collector) ResetStats() {
	if c == nil {
		return
	}
	c.obs.ResetStats()
}

// Record mirrors RecordLocal to satisfy legacy interfaces.
func (c *Collector) Record(r *RequestRecord) {
	c.RecordLocal(r)
}

// DebugEnabled reports whether debug payload capture is active.
func (c *Collector) DebugEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.debugEnabled
}

// SetDebugEnabled toggles debug capture.
func (c *Collector) SetDebugEnabled(enabled bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.debugEnabled = enabled
}

// SetMaxDebugBodySize caps captured debug bodies.
func (c *Collector) SetMaxDebugBodySize(size int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if size <= 0 {
		size = DefaultMaxDebugBodySize
	}
	c.maxDebugBodySize = size
}

// RecordWithDebug attaches debug info when capture is enabled.
func (c *Collector) RecordWithDebug(r *RequestRecord, debug *DebugInfo) {
	if c == nil || r == nil {
		return
	}

	c.mu.RLock()
	enabled := c.debugEnabled
	maxBody := c.maxDebugBodySize
	c.mu.RUnlock()

	if enabled && debug != nil {
		truncated := &DebugInfo{
			RequestHeaders:  debug.RequestHeaders,
			ResponseHeaders: debug.ResponseHeaders,
			UpstreamURL:     debug.UpstreamURL,
			Error:           debug.Error,
		}
		if len(debug.RequestBody) > maxBody {
			truncated.RequestBody = debug.RequestBody[:maxBody] + "..."
		} else {
			truncated.RequestBody = debug.RequestBody
		}
		if len(debug.ResponseBody) > maxBody {
			truncated.ResponseBody = debug.ResponseBody[:maxBody] + "..."
		} else {
			truncated.ResponseBody = debug.ResponseBody
		}
		r.Debug = truncated
	}

	c.RecordLocal(r)
}

// RecordRequestWithDebugInfo builds a RequestRecord from raw data and records it.
func (c *Collector) RecordRequestWithDebugInfo(
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
	if c == nil {
		return
	}

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

// AddEvent appends an observability event.
func (c *Collector) AddEvent(event Event) {
	if c == nil {
		return
	}
	c.obs.AddEvent(event)
}

// Observability exposes the embedded snapshot engine.
func (c *Collector) Observability() *Observability {
	if c == nil {
		return nil
	}
	return c.obs
}
