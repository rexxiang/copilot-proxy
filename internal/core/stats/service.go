package stats

import (
	"copilot-proxy/internal/core/observability"
	"copilot-proxy/internal/monitor"
)

// Service exposes metrics and events derived from observability.
type Service struct {
	obs *observability.Observability
}

// NewService builds the stats service.
func NewService(obs *observability.Observability) *Service {
	return &Service{obs: obs}
}

// Snapshot returns the current metrics snapshot.
func (s *Service) Snapshot() observability.Snapshot {
	if s.obs == nil {
		return observability.Snapshot{}
	}
	return s.obs.Snapshot()
}

// MonitorSnapshot converts the observability snapshot into the monitor DTOs.
func (s *Service) MonitorSnapshot() monitor.Snapshot {
	if s.obs == nil {
		return monitor.Snapshot{}
	}
	return toMonitorSnapshot(s.obs.Snapshot())
}

// Events returns captured events.
func (s *Service) Events() []observability.Event {
	if s.obs == nil {
		return nil
	}
	return s.obs.Events()
}

// Reset clears snapshot counters.
func (s *Service) Reset() {
	if s.obs != nil {
		s.obs.ResetStats()
	}
}

func toMonitorSnapshot(obs observability.Snapshot) monitor.Snapshot {
	result := monitor.Snapshot{
		TotalRequests:  obs.TotalRequests,
		ByModel:        make(map[string]*monitor.ModelStats, len(obs.ByModel)),
		ByStatus:       make(map[int]int64, len(obs.ByStatus)),
		RecentRequests: make([]monitor.RequestRecord, len(obs.RecentRequests)),
		ActiveRequests: make([]monitor.RequestRecord, len(obs.ActiveRequests)),
	}
	for key, stats := range obs.ByModel {
		if stats == nil {
			continue
		}
		result.ByModel[key] = &monitor.ModelStats{
			Count:       stats.Count,
			Errors:      stats.Errors,
			AgentErrors: stats.AgentErrors,
			TotalTime:   stats.TotalTime,
			VisionReqs:  stats.VisionReqs,
			AgentReqs:   stats.AgentReqs,
		}
	}
	for key, value := range obs.ByStatus {
		result.ByStatus[key] = value
	}
	for i, record := range obs.RecentRequests {
		result.RecentRequests[i] = convertRecord(record)
	}
	for i, record := range obs.ActiveRequests {
		result.ActiveRequests[i] = convertRecord(record)
	}
	return result
}

func convertRecord(record observability.RequestRecord) monitor.RequestRecord {
	return monitor.RequestRecord{
		Timestamp:             record.Timestamp,
		Method:                record.Method,
		Path:                  record.Path,
		UpstreamPath:          record.UpstreamPath,
		Model:                 record.Model,
		Account:               record.Account,
		RequestID:             record.RequestID,
		StatusCode:            record.StatusCode,
		Duration:              record.Duration,
		FirstResponseDuration: record.FirstResponseDuration,
		IsStream:              record.IsStream,
		Streaming:             record.Streaming,
		IsVision:              record.IsVision,
		IsAgent:               record.IsAgent,
	}
}
