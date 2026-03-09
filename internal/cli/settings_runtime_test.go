package cli

import (
	stderrors "errors"
	"strings"
	"testing"

	"copilot-proxy/internal/config"
)

var (
	errValidateFailed = stderrors.New("validate failed")
	errPersistFailed  = stderrors.New("persist failed")
	errPublishFailed  = stderrors.New("publish failed")
)

func TestSettingsRuntimeCoordinator_ApplySuccess(t *testing.T) {
	initial := config.DefaultSettings()
	candidate := initial
	candidate.MaxRetries = initial.MaxRetries + 2

	calls := make([]string, 0, 3)
	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: initial,
		ValidateRuntime: func(next config.Settings) (RuntimeValidationResult, error) {
			calls = append(calls, "validate")
			if next.MaxRetries != candidate.MaxRetries {
				t.Fatalf("unexpected candidate in validate: got %d want %d", next.MaxRetries, candidate.MaxRetries)
			}
			return "snapshot-ok", nil
		},
		PersistSettings: func(settings config.Settings) error {
			calls = append(calls, "persist")
			if settings.MaxRetries != candidate.MaxRetries {
				t.Fatalf("unexpected candidate in persist: got %d want %d", settings.MaxRetries, candidate.MaxRetries)
			}
			return nil
		},
		PublishRuntime: func(next config.Settings, validated RuntimeValidationResult) error {
			calls = append(calls, "publish")
			if next.MaxRetries != candidate.MaxRetries {
				t.Fatalf("unexpected candidate in publish: got %d want %d", next.MaxRetries, candidate.MaxRetries)
			}
			token, ok := validated.(string)
			if !ok || token != "snapshot-ok" {
				t.Fatalf("unexpected validated payload: %#v", validated)
			}
			return nil
		},
	})

	applied, err := coordinator.Apply(&candidate)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if got, want := strings.Join(calls, ","), "validate,persist,publish"; got != want {
		t.Fatalf("unexpected call order: got %q want %q", got, want)
	}
	if applied.MaxRetries != candidate.MaxRetries {
		t.Fatalf("unexpected applied max retries: got %d want %d", applied.MaxRetries, candidate.MaxRetries)
	}
	if coordinator.Current().MaxRetries != candidate.MaxRetries {
		t.Fatalf("expected coordinator current settings to update")
	}
}

func TestSettingsRuntimeCoordinator_ApplyValidateFailure(t *testing.T) {
	initial := config.DefaultSettings()
	candidate := initial
	candidate.MaxRetries = initial.MaxRetries + 1

	persistCalled := false
	publishCalled := false
	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: initial,
		ValidateRuntime: func(next config.Settings) (RuntimeValidationResult, error) {
			return nil, errValidateFailed
		},
		PersistSettings: func(settings config.Settings) error {
			persistCalled = true
			return nil
		},
		PublishRuntime: func(next config.Settings, validated RuntimeValidationResult) error {
			publishCalled = true
			return nil
		},
	})

	_, err := coordinator.Apply(&candidate)
	if err == nil {
		t.Fatalf("expected validate failure")
	}
	if persistCalled {
		t.Fatalf("persist should not be called on validate failure")
	}
	if publishCalled {
		t.Fatalf("publish should not be called on validate failure")
	}
	if coordinator.Current().MaxRetries != initial.MaxRetries {
		t.Fatalf("coordinator current should remain unchanged")
	}
}

func TestSettingsRuntimeCoordinator_ApplyPersistFailure(t *testing.T) {
	initial := config.DefaultSettings()
	candidate := initial
	candidate.MaxRetries = initial.MaxRetries + 1

	publishCalled := false
	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: initial,
		ValidateRuntime: func(next config.Settings) (RuntimeValidationResult, error) {
			return "validated", nil
		},
		PersistSettings: func(settings config.Settings) error {
			return errPersistFailed
		},
		PublishRuntime: func(next config.Settings, validated RuntimeValidationResult) error {
			publishCalled = true
			return nil
		},
	})

	_, err := coordinator.Apply(&candidate)
	if err == nil {
		t.Fatalf("expected persist failure")
	}
	if publishCalled {
		t.Fatalf("publish should not be called when persist fails")
	}
	if coordinator.Current().MaxRetries != initial.MaxRetries {
		t.Fatalf("coordinator current should remain unchanged on persist failure")
	}
}

func TestSettingsRuntimeCoordinator_ApplyPublishFailure(t *testing.T) {
	initial := config.DefaultSettings()
	candidate := initial
	candidate.MaxRetries = initial.MaxRetries + 1

	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: initial,
		ValidateRuntime: func(next config.Settings) (RuntimeValidationResult, error) {
			return "validated", nil
		},
		PersistSettings: func(settings config.Settings) error {
			return nil
		},
		PublishRuntime: func(next config.Settings, validated RuntimeValidationResult) error {
			return errPublishFailed
		},
	})

	_, err := coordinator.Apply(&candidate)
	if err == nil {
		t.Fatalf("expected publish failure")
	}
	if !strings.Contains(err.Error(), "persisted settings may require manual rollback") {
		t.Fatalf("expected persisted-settings warning in error, got %v", err)
	}
	if coordinator.Current().MaxRetries != initial.MaxRetries {
		t.Fatalf("coordinator current should remain unchanged on publish failure")
	}
}
