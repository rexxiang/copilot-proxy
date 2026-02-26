package cli

import (
	stderrors "errors"
	"testing"

	"copilot-proxy/internal/config"
)

var (
	errSwitchFailed  = stderrors.New("switch failed")
	errPersistFailed = stderrors.New("persist failed")
)

func TestSettingsRuntimeCoordinator_ApplySuccess(t *testing.T) {
	initial := config.DefaultSettings()
	candidate := initial
	candidate.MaxRetries = initial.MaxRetries + 2

	switched := false
	persisted := false
	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: initial,
		SwitchRuntime: func(prev, next config.Settings) error {
			switched = true
			if next.MaxRetries != candidate.MaxRetries {
				t.Fatalf("unexpected candidate in switch: got %d want %d", next.MaxRetries, candidate.MaxRetries)
			}
			return nil
		},
		PersistSettings: func(settings config.Settings) error {
			persisted = true
			if settings.MaxRetries != candidate.MaxRetries {
				t.Fatalf("unexpected candidate in persist: got %d want %d", settings.MaxRetries, candidate.MaxRetries)
			}
			return nil
		},
		RollbackRuntime: func(settings config.Settings) error {
			t.Fatalf("rollback should not be called on success")
			return nil
		},
	})

	applied, err := coordinator.Apply(&candidate)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !switched {
		t.Fatalf("expected runtime switch to run")
	}
	if !persisted {
		t.Fatalf("expected persist to run")
	}
	if applied.MaxRetries != candidate.MaxRetries {
		t.Fatalf("unexpected applied max retries: got %d want %d", applied.MaxRetries, candidate.MaxRetries)
	}
	if coordinator.Current().MaxRetries != candidate.MaxRetries {
		t.Fatalf("expected coordinator current settings to update")
	}
}

func TestSettingsRuntimeCoordinator_ApplySwitchFailure(t *testing.T) {
	initial := config.DefaultSettings()
	candidate := initial
	candidate.MaxRetries = initial.MaxRetries + 1

	persistCalled := false
	rollbackCalled := false
	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: initial,
		SwitchRuntime: func(prev, next config.Settings) error {
			return errSwitchFailed
		},
		PersistSettings: func(settings config.Settings) error {
			persistCalled = true
			return nil
		},
		RollbackRuntime: func(settings config.Settings) error {
			rollbackCalled = true
			return nil
		},
	})

	_, err := coordinator.Apply(&candidate)
	if err == nil {
		t.Fatalf("expected switch failure")
	}
	if persistCalled {
		t.Fatalf("persist should not be called on switch failure")
	}
	if rollbackCalled {
		t.Fatalf("rollback should not be called when switch fails before apply")
	}
	if coordinator.Current().MaxRetries != initial.MaxRetries {
		t.Fatalf("coordinator current should remain unchanged")
	}
}

func TestSettingsRuntimeCoordinator_ApplyPersistFailureRollsBack(t *testing.T) {
	initial := config.DefaultSettings()
	candidate := initial
	candidate.MaxRetries = initial.MaxRetries + 1

	rollbackCalled := false
	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: initial,
		SwitchRuntime: func(prev, next config.Settings) error {
			return nil
		},
		PersistSettings: func(settings config.Settings) error {
			return errPersistFailed
		},
		RollbackRuntime: func(settings config.Settings) error {
			rollbackCalled = true
			if settings.MaxRetries != initial.MaxRetries {
				t.Fatalf("rollback should target initial settings")
			}
			return nil
		},
	})

	_, err := coordinator.Apply(&candidate)
	if err == nil {
		t.Fatalf("expected persist failure")
	}
	if !rollbackCalled {
		t.Fatalf("expected rollback on persist failure")
	}
	if coordinator.Current().MaxRetries != initial.MaxRetries {
		t.Fatalf("coordinator current should remain unchanged on persist failure")
	}
}
