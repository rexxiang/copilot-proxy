package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDurationUnmarshalStringSeconds(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`"60s"`), &d); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if d.Duration() != 60*time.Second {
		t.Fatalf("unexpected duration: %s", d.Duration())
	}
}

func TestDurationUnmarshalNumberSeconds(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`60`), &d); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if d.Duration() != 60*time.Second {
		t.Fatalf("unexpected duration: %s", d.Duration())
	}
}

func TestDurationUnmarshalRejectsSubSecond(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`"500ms"`), &d); err == nil {
		t.Fatalf("expected error for sub-second duration")
	}
}

func TestDurationUnmarshalRejectsFractionalSeconds(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`1.5`), &d); err == nil {
		t.Fatalf("expected error for fractional seconds")
	}
}
