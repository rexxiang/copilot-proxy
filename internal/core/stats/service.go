package stats

import (
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/core/observability"
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

// MonitorSnapshot returns the observability snapshot using the core DTOs.
func (s *Service) MonitorSnapshot() core.Snapshot {
	if s.obs == nil {
		return core.Snapshot{}
	}
	return s.obs.Snapshot()
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
