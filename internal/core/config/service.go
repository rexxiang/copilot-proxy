package coreconfig

import (
	"sync"

	"copilot-proxy/internal/config"
)

// Service wraps persistent settings operations.
type Service struct {
	mu       sync.Mutex
	settings config.Settings
}

// NewService creates a config service preloaded with settings.
func NewService(settings config.Settings) *Service {
	return &Service{settings: settings}
}

// Current returns the active settings snapshot.
func (s *Service) Current() config.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSettings(s.settings)
}

// Update validates and persists new settings.
func (s *Service) Update(next config.Settings) (config.Settings, error) {
	if err := config.SaveSettings(&next); err != nil {
		return config.Settings{}, err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	return next, nil
}

// GetModelMappings returns the reasoning policies and fallback models.
func (s *Service) GetModelMappings() (map[string]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneStringMap(s.settings.ReasoningPoliciesMap), cloneStringSlice(s.settings.ClaudeHaikuFallbackModels)
}

// UpdateModelMappings replaces reasoning policies and haiku fallbacks.
func (s *Service) UpdateModelMappings(policies map[string]string, fallbacks []string) error {
	s.mu.Lock()
	s.settings.ReasoningPoliciesMap = cloneStringMap(policies)
	s.settings.ClaudeHaikuFallbackModels = cloneStringSlice(fallbacks)
	next := s.settings
	s.mu.Unlock()
	_, err := s.Update(next)
	return err
}

func cloneSettings(settings config.Settings) config.Settings {
	settings.RequiredHeaders = cloneStringMap(settings.RequiredHeaders)
	settings.ReasoningPoliciesMap = cloneStringMap(settings.ReasoningPoliciesMap)
	settings.ReasoningPolicies = append([]config.ReasoningPolicy(nil), settings.ReasoningPolicies...)
	settings.ClaudeHaikuFallbackModels = cloneStringSlice(settings.ClaudeHaikuFallbackModels)
	settings.ClaudeHaikuFallbackModelsUI = cloneHaikuSlice(settings.ClaudeHaikuFallbackModelsUI)
	return settings
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, len(input))
	copy(out, input)
	return out
}

func cloneHaikuSlice(input []config.HaikuFallbackModel) []config.HaikuFallbackModel {
	if len(input) == 0 {
		return nil
	}
	out := make([]config.HaikuFallbackModel, len(input))
	copy(out, input)
	return out
}
