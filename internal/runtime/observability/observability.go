package observability

import (
	"sync"
	"time"

	core "copilot-proxy/internal/runtime/types"
)

const (
	statusErrorMin       = 400
	defaultHistoryLength = 100
	defaultEventCapacity = 200
)

// StatusClientCanceled matches the HTTP status when a client aborts the request.
const StatusClientCanceled = core.StatusClientCanceled

type DebugInfo = core.DebugInfo

type RequestRecord = core.RequestRecord

type ModelStats = core.ModelStats

type Snapshot = core.Snapshot

type Event = core.Event

// Observability collects request activity and events.
type Observability struct {
	mu         sync.RWMutex
	maxHistory int
	maxEvents  int

	records  []RequestRecord
	active   map[string]*RequestRecord
	byModel  map[string]*ModelStats
	byStatus map[int]int64
	total    int64
	events   []Event
}

// New returns an initialized Observability collector.
func New(maxHistory, maxEvents int) *Observability {
	if maxHistory <= 0 {
		maxHistory = defaultHistoryLength
	}
	if maxEvents <= 0 {
		maxEvents = defaultEventCapacity
	}
	return &Observability{
		maxHistory: maxHistory,
		maxEvents:  maxEvents,
		records:    make([]RequestRecord, 0, maxHistory),
		active:     make(map[string]*RequestRecord),
		byModel:    make(map[string]*ModelStats),
		byStatus:   make(map[int]int64),
		events:     make([]Event, 0, maxEvents),
	}
}

// RecordStart stores an active request record for later completion.
func (o *Observability) RecordStart(r *RequestRecord) {
	if r == nil || r.RequestID == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	copy := *r
	o.active[r.RequestID] = &copy
}

// RecordFirstResponse updates the request metadata when the first upstream payload arrives.
func (o *Observability) RecordFirstResponse(requestID string, statusCode int, duration time.Duration, upstreamPath string, isStream bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, ok := o.active[requestID]
	if !ok {
		return
	}
	rec.StatusCode = statusCode
	rec.FirstResponseDuration = duration
	rec.Duration = duration
	if upstreamPath != "" {
		rec.UpstreamPath = upstreamPath
	}
	rec.IsStream = isStream
	if isStream {
		rec.Streaming = true
	}
}

// RecordComplete finalizes a request and archives it for snapshots.
func (o *Observability) RecordComplete(requestID string, statusCode int, duration time.Duration, upstreamPath string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, ok := o.active[requestID]
	if !ok {
		return
	}
	rec.StatusCode = statusCode
	rec.Duration = duration
	if upstreamPath != "" {
		rec.UpstreamPath = upstreamPath
	}
	if rec.IsStream && rec.Streaming {
		rec.Streaming = false
		if rec.FirstResponseDuration == 0 {
			rec.FirstResponseDuration = duration
		}
	}
	o.recordInternal(rec)
	delete(o.active, requestID)
}

// Record adds a completed record directly into the history.
func (o *Observability) Record(r *RequestRecord) {
	if r == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	copy := *r
	o.recordInternal(&copy)
}

// Snapshot returns a copy of the current metrics.
func (o *Observability) Snapshot() Snapshot {
	o.mu.RLock()
	defer o.mu.RUnlock()
	byModel := make(map[string]*ModelStats, len(o.byModel))
	for k, v := range o.byModel {
		stats := *v
		byModel[k] = &stats
	}
	byStatus := make(map[int]int64, len(o.byStatus))
	for k, v := range o.byStatus {
		byStatus[k] = v
	}
	recent := make([]RequestRecord, len(o.records))
	copy(recent, o.records)
	active := make([]RequestRecord, 0, len(o.active))
	for _, r := range o.active {
		active = append(active, *r)
	}
	return Snapshot{
		TotalRequests:  o.total,
		ByModel:        byModel,
		ByStatus:       byStatus,
		RecentRequests: recent,
		ActiveRequests: active,
	}
}

// AddEvent appends an event to the ring buffer.
func (o *Observability) AddEvent(event Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.events) >= o.maxEvents {
		copy(o.events, o.events[1:])
		o.events[len(o.events)-1] = event
		return
	}
	o.events = append(o.events, event)
}

// Events returns a copy of the captured events.
func (o *Observability) Events() []Event {
	o.mu.RLock()
	defer o.mu.RUnlock()
	events := make([]Event, len(o.events))
	copy(events, o.events)
	return events
}

// Reset clears all records, stats, active requests, and events.
func (o *Observability) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = make([]RequestRecord, 0, o.maxHistory)
	o.active = make(map[string]*RequestRecord)
	o.byModel = make(map[string]*ModelStats)
	o.byStatus = make(map[int]int64)
	o.total = 0
	o.events = make([]Event, 0, o.maxEvents)
}

// ResetStats clears aggregate counters while retaining request history.
func (o *Observability) ResetStats() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.byModel = make(map[string]*ModelStats)
	o.byStatus = make(map[int]int64)
	o.total = 0
}

func (o *Observability) recordInternal(r *RequestRecord) {
	if r == nil {
		return
	}
	if !r.IsAgent {
		o.total++
		o.byStatus[r.StatusCode]++
	}
	if r.Model != "" {
		stats := o.byModel[r.Model]
		if stats == nil {
			stats = &ModelStats{}
			o.byModel[r.Model] = stats
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
	if len(o.records) >= o.maxHistory {
		copy(o.records, o.records[1:])
		o.records[len(o.records)-1] = *r
		return
	}
	o.records = append(o.records, *r)
}
