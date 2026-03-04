package models

import "strings"

type Selector struct{}

const (
	base10 = 10
)

func NewSelector() *Selector { return &Selector{} }

func (s *Selector) SelectModelInfo(models []ModelInfo, requested string) (ModelInfo, bool, bool) {
	if len(models) == 0 || strings.TrimSpace(requested) == "" {
		return zeroModelInfo(), false, false
	}

	if exactID, ok := s.SelectExactCaseInsensitive(models, requested); ok {
		if model, found := findModelByID(models, exactID); found {
			return model, false, true
		}
	}

	if mappedID, mapped := s.SelectMappedCaseInsensitive(models, requested); mapped {
		if model, ok := findModelByID(models, mappedID); ok {
			return model, true, true
		}
	}

	return zeroModelInfo(), false, false
}

func (s *Selector) Select(models []ModelInfo, requested string) (string, bool) {
	if len(models) == 0 || strings.TrimSpace(requested) == "" {
		return "", false
	}
	if exact, ok := s.SelectExactCaseInsensitive(models, requested); ok {
		return exact, false
	}
	if mapped, ok := s.SelectMappedCaseInsensitive(models, requested); ok {
		return mapped, true
	}
	return "", false
}

func (s *Selector) SelectExactCaseInsensitive(models []ModelInfo, requested string) (string, bool) {
	for i := range models {
		model := models[i]
		if strings.EqualFold(strings.TrimSpace(requested), strings.TrimSpace(model.ID)) {
			return model.ID, true
		}
		if strings.TrimSpace(model.Family) != "" && strings.EqualFold(strings.TrimSpace(requested), strings.TrimSpace(model.Family)) {
			return model.ID, true
		}
	}
	return "", false
}

func (s *Selector) SelectMappedCaseInsensitive(models []ModelInfo, requested string) (string, bool) {
	normalized := strings.TrimSpace(requested)
	if normalized == "" {
		return "", false
	}
	lowerRequested := strings.ToLower(normalized)
	switch {
	case strings.HasPrefix(lowerRequested, "claude-haiku-"):
		if selected, ok := findExactID(models, "gpt-5-mini"); ok {
			return selected, true
		}
		if selected, ok := findExactID(models, "grok-code-fast-1"); ok {
			return selected, true
		}
		if selected, ok := selectHighestVersion(models, "claude-haiku-"); ok {
			return selected, true
		}
	case strings.HasPrefix(lowerRequested, "claude-sonnet-"):
		if selected, ok := selectHighestVersion(models, "claude-sonnet-"); ok {
			return selected, true
		}
	case strings.HasPrefix(lowerRequested, "claude-opus-"):
		if selected, ok := selectHighestVersion(models, "claude-opus-"); ok {
			return selected, true
		}
	}

	return "", false
}

func findExactID(models []ModelInfo, id string) (string, bool) {
	for i := range models {
		model := models[i]
		if strings.EqualFold(model.ID, id) {
			return model.ID, true
		}
	}
	return "", false
}

func findModelByID(models []ModelInfo, id string) (ModelInfo, bool) {
	for i := range models {
		model := models[i]
		if strings.EqualFold(model.ID, id) {
			return model, true
		}
	}
	return zeroModelInfo(), false
}

func zeroModelInfo() ModelInfo {
	var empty ModelInfo
	return empty
}

func selectHighestVersion(models []ModelInfo, prefix string) (string, bool) {
	prefixLower := strings.ToLower(prefix)
	var best ModelInfo
	var bestSegs []int
	found := false
	for i := range models {
		model := models[i]
		idLower := strings.ToLower(model.ID)
		if !strings.HasPrefix(idLower, prefixLower) {
			continue
		}
		versionPart := strings.TrimPrefix(idLower, prefixLower)
		segments := parseVersionSegments(versionPart)
		if !found || compareSegments(segments, bestSegs) > 0 {
			best = model
			bestSegs = segments
			found = true
		}
	}
	if !found {
		return "", false
	}
	return best.ID, true
}

func parseVersionSegments(value string) []int {
	if value == "" {
		return []int{0}
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	})
	segments := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		segments = append(segments, parseLeadingInt(part))
	}
	if len(segments) == 0 {
		return []int{0}
	}
	return segments
}

func parseLeadingInt(value string) int {
	n := 0
	for i := range len(value) {
		ch := value[i]
		if ch < '0' || ch > '9' {
			break
		}
		n = n*base10 + int(ch-'0')
	}
	return n
}

func compareSegments(a, b []int) int {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := range maxLen {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}
