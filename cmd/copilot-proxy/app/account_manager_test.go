package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"copilot-proxy/internal/runtime/config"
	accountcore "copilot-proxy/internal/runtime/identity/account"
	auth "copilot-proxy/internal/runtime/identity/oauth"
	core "copilot-proxy/internal/runtime/types"
)

func TestAccountManagerLoginFlow(t *testing.T) {
	state := newTestAuthState(config.AuthConfig{})
	manager := NewAccountManager(AccountManagerDeps{
		LoadAuth: state.LoadAuth,
		SaveAuth: state.SaveAuth,
		RequestCode: func(ctx context.Context) (auth.DeviceCodeResponse, error) {
			return auth.DeviceCodeResponse{
				DeviceCode:      "device",
				UserCode:        "code",
				VerificationURI: "https://verify",
				Interval:        1,
				ExpiresIn:       60,
			}, nil
		},
		PollToken: func(ctx context.Context, device auth.DeviceCodeResponse) (string, error) {
			if device.DeviceCode != "device" {
				return "", errors.New("unexpected device code")
			}
			return "tok-123", nil
		},
		FetchLogin: func(ctx context.Context, token string) (string, error) {
			if token != "tok-123" {
				return "", errors.New("unexpected token")
			}
			return "alice", nil
		},
	})

	challenge, err := manager.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	if challenge.Seq == 0 {
		t.Fatalf("expected seq > 0")
	}

	result, err := manager.PollLogin(context.Background(), challenge.Seq)
	if err != nil {
		t.Fatalf("poll login: %v", err)
	}
	if result.Token != "tok-123" || result.Login != "alice" {
		t.Fatalf("unexpected login result: %+v", result)
	}

	if _, err := manager.PollLogin(context.Background(), challenge.Seq); !errors.Is(err, ErrNoLoginSession) {
		t.Fatalf("expected no login session after success, got %v", err)
	}
}

func TestAccountManagerCancelLogin(t *testing.T) {
	state := newTestAuthState(config.AuthConfig{})
	block := make(chan struct{})
	ready := make(chan struct{})
	manager := NewAccountManager(AccountManagerDeps{
		LoadAuth: state.LoadAuth,
		SaveAuth: state.SaveAuth,
		RequestCode: func(ctx context.Context) (auth.DeviceCodeResponse, error) {
			return auth.DeviceCodeResponse{
				DeviceCode:      "device",
				UserCode:        "code",
				VerificationURI: "https://verify",
				Interval:        1,
				ExpiresIn:       60,
			}, nil
		},
		PollToken: func(ctx context.Context, device auth.DeviceCodeResponse) (string, error) {
			close(ready)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-block:
				return "tok-123", nil
			}
		},
		FetchLogin: func(ctx context.Context, token string) (string, error) {
			return "alice", nil
		},
	})

	challenge, err := manager.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}

	resultCh := make(chan error, 1)
	go func() {
		_, pollErr := manager.PollLogin(context.Background(), challenge.Seq)
		resultCh <- pollErr
	}()

	<-ready
	manager.CancelLogin(challenge.Seq)

	select {
	case pollErr := <-resultCh:
		if !errors.Is(pollErr, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", pollErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("poll login did not return after cancel")
	}
}

func TestAccountManagerPremiumTTLRefresh(t *testing.T) {
	account := config.Account{User: "alice", GhToken: "token"}
	state := newTestAuthState(config.AuthConfig{
		Default:  account.User,
		Accounts: []config.Account{account},
	})
	var fetches int
	manager := NewAccountManager(AccountManagerDeps{
		LoadAuth: state.LoadAuth,
		SaveAuth: state.SaveAuth,
		FetchUserInfo: func(ctx context.Context, token string) (*core.UserInfo, error) {
			fetches++
			return &core.UserInfo{Plan: "business"}, nil
		},
		Now: func() time.Time {
			return state.now()
		},
		PremiumTTL: time.Minute,
	})

	if _, err := manager.PremiumInfo(context.Background(), false); err != nil {
		t.Fatalf("premium info: %v", err)
	}
	if fetches != 1 {
		t.Fatalf("expected first fetch, got %d", fetches)
	}

	state.advance(2 * time.Minute)
	if _, err := manager.PremiumInfo(context.Background(), false); err != nil {
		t.Fatalf("premium info refresh: %v", err)
	}
	if fetches != 2 {
		t.Fatalf("expected ttl refresh, got %d", fetches)
	}
}

func TestAccountManagerSwitchDefaultInvalidatesPremium(t *testing.T) {
	alice := config.Account{User: "alice", GhToken: "token-a"}
	bob := config.Account{User: "bob", GhToken: "token-b"}
	state := newTestAuthState(config.AuthConfig{
		Default:  alice.User,
		Accounts: []config.Account{alice, bob},
	})
	manager := NewAccountManager(AccountManagerDeps{
		LoadAuth: state.LoadAuth,
		SaveAuth: state.SaveAuth,
		Now:      state.now,
	})
	manager.premiumCache[alice.User] = premiumCacheEntry{info: core.UserInfo{Plan: "A"}, retrieved: state.now()}
	manager.premiumCache[bob.User] = premiumCacheEntry{info: core.UserInfo{Plan: "B"}, retrieved: state.now()}

	if err := manager.SwitchDefault(bob.User); err != nil {
		t.Fatalf("switch default: %v", err)
	}
	if _, ok := manager.premiumCache[bob.User]; ok {
		t.Fatalf("expected premium cache for %q invalidated", bob.User)
	}

	current, _, err := manager.Current()
	if err != nil {
		t.Fatalf("current account: %v", err)
	}
	if current.User != bob.User {
		t.Fatalf("expected current account %q, got %q", bob.User, current.User)
	}
}

type testAuthState struct {
	mu   sync.Mutex
	auth config.AuthConfig
	t    time.Time
}

func newTestAuthState(auth config.AuthConfig) *testAuthState {
	return &testAuthState{
		auth: auth,
		t:    time.Date(2026, 3, 17, 10, 0, 0, 0, time.UTC),
	}
}

func (s *testAuthState) LoadAuth() (config.AuthConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneTestAuth(s.auth), nil
}

func (s *testAuthState) SaveAuth(next config.AuthConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth = cloneTestAuth(next)
	return nil
}

func (s *testAuthState) now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.t
}

func (s *testAuthState) advance(delta time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t = s.t.Add(delta)
}

func cloneTestAuth(auth config.AuthConfig) config.AuthConfig {
	cloned := auth
	cloned.Accounts = append([]config.Account(nil), auth.Accounts...)
	return cloned
}

var _ = accountcore.AccountDTO{}
