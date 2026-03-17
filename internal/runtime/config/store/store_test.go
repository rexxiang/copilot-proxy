package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLoadJSONMissingReturnsDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	type payload struct {
		Value string `json:"value"`
	}
	defaultValue := payload{Value: "default"}

	path, err := Path("missing.json")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	got, err := LoadJSON(path, defaultValue)
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if got != defaultValue {
		t.Fatalf("got %+v, want %+v", got, defaultValue)
	}
}

func TestSaveAndLoadJSONRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	type payload struct {
		Value string `json:"value"`
	}
	input := payload{Value: "ok"}

	path, err := Path("roundtrip.json")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := SaveJSON(path, input); err != nil {
		t.Fatalf("save: %v", err)
	}
	output, err := LoadJSON(path, payload{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if output != input {
		t.Fatalf("got %+v, want %+v", output, input)
	}
}

func TestLoadJSONRejectsPathOutsideConfigDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path := filepath.Join(t.TempDir(), "outside.json")
	_, err := LoadJSON(path, map[string]string{})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("expected ErrInvalidPath, got %v", err)
	}
}
