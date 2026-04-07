package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
	"copilot-proxy/internal/runtime/config"
	accountcore "copilot-proxy/internal/runtime/identity/account"
	models "copilot-proxy/internal/runtime/model"
)

type stubCatalog struct {
	models []models.ModelInfo
}

func (s *stubCatalog) GetModels() []models.ModelInfo {
	copied := make([]models.ModelInfo, len(s.models))
	copy(copied, s.models)
	return copied
}

func (s *stubCatalog) SetModels(items []models.ModelInfo) {
	s.models = make([]models.ModelInfo, len(items))
	copy(s.models, items)
}

type stubLoader struct {
	models []models.ModelInfo
	err    error
}

//goland:noinspection GoUnusedParameter
func (s stubLoader) Load(ctx context.Context) ([]models.ModelInfo, error) {
	return s.models, s.err
}

func TestBuildServerUsesDefaultSettings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	deps := DefaultServerDeps()
	deps.ModelCatalog = &stubCatalog{}
	deps.ModelLoader = stubLoader{models: []models.ModelInfo{{ID: "gpt-4o"}}}
	runtime, err := buildServerWithDeps(&deps)
	if err != nil {
		t.Fatalf("buildServer error: %v", err)
	}
	if runtime.runtime == nil || runtime.runtime.Handler == nil {
		t.Fatalf("runtime handler should be initialized")
	}
	if runtime.runtime.ListenAddr != appsettings.DefaultSettings().ListenAddr {
		t.Fatalf("unexpected addr: %s", runtime.runtime.ListenAddr)
	}
}

func TestBuildServerFailsWhenModelLoadFails(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	deps := DefaultServerDeps()
	deps.ModelCatalog = &stubCatalog{}
	deps.ModelLoader = stubLoader{err: context.Canceled}
	if _, err := buildServerWithDeps(&deps); err == nil {
		t.Fatalf("expected buildServerWithDeps to fail when model preload fails")
	}
}

func TestActivateDefaultAccount(t *testing.T) {
	auth := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1"},
			{User: "u2"},
		},
	}

	var saved config.AuthConfig
	err := accountcore.ActivateDefaultAccount(auth, "u2", func(next config.AuthConfig) error {
		saved = next
		return nil
	})
	if err != nil {
		t.Fatalf("activate default account: %v", err)
	}
	if auth.Default != "u2" {
		t.Fatalf("expected in-memory default to be u2, got %q", auth.Default)
	}
	if saved.Default != "u2" {
		t.Fatalf("expected persisted default to be u2, got %q", saved.Default)
	}
}

func TestActivateDefaultAccountMissingUser(t *testing.T) {
	auth := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1"},
		},
	}

	err := accountcore.ActivateDefaultAccount(auth, "missing", func(next config.AuthConfig) error {
		return nil
	})
	if !errors.Is(err, config.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
	if auth.Default != "u1" {
		t.Fatalf("expected default to remain u1 after failure, got %q", auth.Default)
	}
}

func TestActivateDefaultAccountSaveFailureRollsBack(t *testing.T) {
	auth := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1"},
			{User: "u2"},
		},
	}

	err := accountcore.ActivateDefaultAccount(auth, "u2", func(next config.AuthConfig) error {
		return errors.New("save failed")
	})
	if err == nil {
		t.Fatalf("expected error when save callback fails")
	}
	if auth.Default != "u1" {
		t.Fatalf("expected default rollback to u1, got %q", auth.Default)
	}
}

func TestUpsertAccountPreserveDefaultKeepsExistingDefault(t *testing.T) {
	auth := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1", GhToken: "old-u1"},
		},
	}

	var saved config.AuthConfig
	err := accountcore.UpsertAccountPreserveDefault(auth, config.Account{
		User:    "u2",
		GhToken: "new-u2",
		AppID:   "",
	}, func(next config.AuthConfig) error {
		saved = next
		return nil
	})
	if err != nil {
		t.Fatalf("upsert account preserve default: %v", err)
	}
	if auth.Default != "u1" {
		t.Fatalf("expected default to remain u1, got %q", auth.Default)
	}
	if saved.Default != "u1" {
		t.Fatalf("expected persisted default to remain u1, got %q", saved.Default)
	}
	if len(auth.Accounts) != 2 {
		t.Fatalf("expected two accounts after upsert, got %d", len(auth.Accounts))
	}
}

func TestUpsertAccountPreserveDefaultSetsDefaultWhenNoAccounts(t *testing.T) {
	auth := &config.AuthConfig{}

	var saved config.AuthConfig
	err := accountcore.UpsertAccountPreserveDefault(auth, config.Account{
		User:    "u-new",
		GhToken: "token-new",
		AppID:   "",
	}, func(next config.AuthConfig) error {
		saved = next
		return nil
	})
	if err != nil {
		t.Fatalf("upsert account preserve default: %v", err)
	}
	if auth.Default != "u-new" {
		t.Fatalf("expected default to be new account, got %q", auth.Default)
	}
	if saved.Default != "u-new" {
		t.Fatalf("expected persisted default to be new account, got %q", saved.Default)
	}
}

func TestUpsertAccountPreserveDefaultSaveFailureRollsBack(t *testing.T) {
	auth := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1", GhToken: "old-u1"},
		},
	}

	err := accountcore.UpsertAccountPreserveDefault(auth, config.Account{
		User:    "u2",
		GhToken: "new-u2",
		AppID:   "",
	}, func(next config.AuthConfig) error {
		return errors.New("save failed")
	})
	if err == nil {
		t.Fatalf("expected save failure error")
	}
	if auth.Default != "u1" {
		t.Fatalf("expected default rollback to u1, got %q", auth.Default)
	}
	if len(auth.Accounts) != 1 || auth.Accounts[0].User != "u1" {
		t.Fatalf("expected accounts rollback to original state, got %+v", auth.Accounts)
	}
}

func TestIsExpectedShutdownError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "http err server closed",
			err:  http.ErrServerClosed,
			want: true,
		},
		{
			name: "wrapped context canceled",
			err:  fmt.Errorf("shutdown server: %w", context.Canceled),
			want: true,
		},
		{
			name: "regular error",
			err:  errors.New("boom"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExpectedShutdownError(tt.err)
			if got != tt.want {
				t.Fatalf("isExpectedShutdownError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type fakeTUIProgram struct {
	quitCalls    int
	releaseCalls int
	releaseErr   error
}

func (f *fakeTUIProgram) Quit() {
	f.quitCalls++
}

func (f *fakeTUIProgram) ReleaseTerminal() error {
	f.releaseCalls++
	return f.releaseErr
}

func TestQuitAndWaitForTUIWaitsForRunCompletion(t *testing.T) {
	program := &fakeTUIProgram{}
	tuiErr := make(chan error, 1)

	done := make(chan struct{})
	var gotErr error
	go func() {
		gotErr = quitAndWaitForTUI(program, tuiErr, 250*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("quitAndWaitForTUI returned before TUI run completed")
	case <-time.After(20 * time.Millisecond):
	}

	expected := errors.New("tui stopped")
	tuiErr <- expected

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("quitAndWaitForTUI did not finish after TUI run completion")
	}

	if !errors.Is(gotErr, expected) {
		t.Fatalf("expected run error %v, got %v", expected, gotErr)
	}
	if program.quitCalls != 1 {
		t.Fatalf("expected Quit called once, got %d", program.quitCalls)
	}
	if program.releaseCalls != 0 {
		t.Fatalf("expected ReleaseTerminal not called, got %d", program.releaseCalls)
	}
}

func TestQuitAndWaitForTUITimeoutReleasesTerminal(t *testing.T) {
	program := &fakeTUIProgram{}
	tuiErr := make(chan error)

	gotErr := quitAndWaitForTUI(program, tuiErr, 25*time.Millisecond)
	if gotErr != nil {
		t.Fatalf("expected nil error on timeout fallback, got %v", gotErr)
	}
	if program.quitCalls != 1 {
		t.Fatalf("expected Quit called once, got %d", program.quitCalls)
	}
	if program.releaseCalls != 1 {
		t.Fatalf("expected ReleaseTerminal called once, got %d", program.releaseCalls)
	}
}
