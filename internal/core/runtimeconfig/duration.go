package runtimeconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Duration struct {
	value time.Duration
	set   bool
}

var (
	ErrDurationEmpty        = errors.New("duration cannot be empty")
	ErrDurationNegative     = errors.New("duration must be >= 0 seconds")
	ErrDurationWholeSeconds = errors.New("duration must be in whole seconds")
	ErrInvalidDuration      = errors.New("invalid duration")
)

func NewDuration(value time.Duration) Duration {
	return Duration{value: value, set: true}
}

func (d Duration) Duration() time.Duration {
	return d.value
}

func (d Duration) IsSet() bool {
	return d.set
}

func (d Duration) String() string {
	if !d.set {
		return ""
	}
	return formatCompactDuration(d.value)
}

func (d Duration) MarshalJSON() ([]byte, error) {
	if !d.set {
		return []byte("null"), nil
	}
	data, err := json.Marshal(d.String())
	if err != nil {
		return nil, fmt.Errorf("encode duration: %w", err)
	}
	return data, nil
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*d = Duration{value: 0, set: false}
		return nil
	}
	d.set = true
	if data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("decode duration: %w", err)
		}
		if raw == "" {
			return ErrDurationEmpty
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("parse duration: %w", err)
		}
		if parsed < 0 {
			return ErrDurationNegative
		}
		if parsed%time.Second != 0 {
			return fmt.Errorf("%w: %s", ErrDurationWholeSeconds, raw)
		}
		d.value = parsed
		return nil
	}

	var raw json.Number
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode duration: %w", err)
	}
	seconds, err := raw.Int64()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidDuration, err)
	}
	if seconds < 0 {
		return ErrDurationNegative
	}
	d.value = time.Duration(seconds) * time.Second
	return nil
}

func formatCompactDuration(value time.Duration) string {
	if value == 0 {
		return "0s"
	}
	if value < 0 || value%time.Second != 0 {
		return value.String()
	}

	totalSeconds := int64(value / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	var builder strings.Builder
	if hours > 0 {
		builder.WriteString(fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		builder.WriteString(fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 {
		builder.WriteString(fmt.Sprintf("%ds", seconds))
	}
	if builder.Len() == 0 {
		return "0s"
	}
	return builder.String()
}
