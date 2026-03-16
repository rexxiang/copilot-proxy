package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/config"
	"copilot-proxy/internal/monitor"
)

func TestAccountServiceLoginFlow(t *testing.T) {
	flow := &stubFlow{
		device: auth.DeviceCodeResponse{
			DeviceCode:      "device",
			UserCode:        "code",
			VerificationURI: "https://verify",
			Interval:        1,
			ExpiresIn:       60,
		},
		token: "tok-123",
	}
	svc := New(config.AuthConfig{},
		WithDeviceFlowFactory(func() deviceFlow { return flow }),
		WithFetchUserFunc(func(ctx context.Context, client *http.Client, apiBaseURL, ghToken string) (string, error) {
			if ghToken != "tok-123" {
				return "", errors.New("unexpected token")
			}
			return "alice", nil
		}),
	)

	challenge, err := svc.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	if challenge.Seq == 0 {
		t.Fatalf("expected seq > 0")
	}

	result, err := svc.PollLogin(context.Background(), challenge.Seq)
	if err != nil {
		t.Fatalf("poll login: %v", err)
	}
	if result.Token != "tok-123" || result.Login != "alice" {
		t.Fatalf("unexpected login result: %+v", result)
	}

	if _, err := svc.PollLogin(context.Background(), challenge.Seq); !errors.Is(err, ErrNoLoginSession) {
		t.Fatalf("expected no login session after success, got %v", err)
	}
}

func TestAccountServiceCancelLogin(t *testing.T) {
	block := make(chan struct{})
	ready := make(chan struct{})
	flow := &stubFlow{
		device: auth.DeviceCodeResponse{DeviceCode: "x", UserCode: "y", VerificationURI: "https://verify", Interval: 1, ExpiresIn: 60},
		pollFn: func(ctx context.Context, device auth.DeviceCodeResponse) (string, error) {
			close(ready)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-block:
				return "done", nil
			}
		},
	}

	svc := New(config.AuthConfig{},
		WithDeviceFlowFactory(func() deviceFlow { return flow }),
		WithFetchUserFunc(func(ctx context.Context, client *http.Client, apiBaseURL, ghToken string) (string, error) {
			return "bob", nil
		}),
	)

	challenge, err := svc.BeginLogin(context.Background())
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}

	resultCh := make(chan error, 1)
	go func() {
		_, err := svc.PollLogin(context.Background(), challenge.Seq)
		resultCh <- err
	}()

	<-ready
	svc.CancelLogin(challenge.Seq)

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("poll login did not return after cancel")
	}
}

func TestAccountServiceListSnapshot(t *testing.T) {
	cfg := config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1", GhToken: "t1"},
			{User: "u2", GhToken: "t2"},
		},
	}
	svc := New(cfg)
	entries := svc.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(entries))
	}
	if !entries[0].IsDefault || entries[0].User != "u1" {
		t.Fatalf("unexpected default entry: %+v", entries[0])
	}
}

func TestAccountServicePremiumTTLRefresh(t *testing.T) {
	server, called := newPremiumServer(t)
	account := config.Account{User: "alice", GhToken: "token"}
	cfg := config.AuthConfig{Default: account.User, Accounts: []config.Account{account}}
	svc := New(cfg,
		WithPremiumService(NewPremiumService(server.Client(), server.URL)),
	)
	ctx := context.Background()

	if _, err := svc.PremiumInfo(ctx, false); err != nil {
		t.Fatalf("prem info: %v", err)
	}
	if *called != 1 {
		t.Fatalf("expected fetch, got %d", *called)
	}

	svc.premium.mu.Lock()
	entry := svc.premium.cache[account.User]
	entry.Retrieved = time.Now().Add(-premiumTTL - time.Second)
	svc.premium.cache[account.User] = entry
	svc.premium.mu.Unlock()

	if _, err := svc.PremiumInfo(ctx, false); err != nil {
		t.Fatalf("prem info refresh: %v", err)
	}
	if *called != 2 {
		t.Fatalf("expected TTL refresh, got %d", *called)
	}
}

func TestAccountServicePremiumForceRefresh(t *testing.T) {
	server, called := newPremiumServer(t)
	account := config.Account{User: "bob", GhToken: "token"}
	cfg := config.AuthConfig{Default: account.User, Accounts: []config.Account{account}}
	svc := New(cfg,
		WithPremiumService(NewPremiumService(server.Client(), server.URL)),
	)
	ctx := context.Background()

	if _, err := svc.PremiumInfo(ctx, false); err != nil {
		t.Fatalf("prem info: %v", err)
	}
	if *called != 1 {
		t.Fatalf("expected fetch, got %d", *called)
	}

	if _, err := svc.PremiumInfo(ctx, true); err != nil {
		t.Fatalf("prem info force refresh: %v", err)
	}
	if *called != 2 {
		t.Fatalf("expected force refresh, got %d", *called)
	}
}

func TestAccountServiceSwitchDefaultInvalidatesPremium(t *testing.T) {
	account := config.Account{User: "alice", GhToken: "token-a"}
	other := config.Account{User: "bob", GhToken: "token-b"}
	cfg := config.AuthConfig{
		Default:  account.User,
		Accounts: []config.Account{account, other},
	}
	svc := New(cfg, WithSaveAuthFunc(func(config.AuthConfig) error { return nil }))
	svc.premium.mu.Lock()
	svc.premium.cache[account.User] = PremiumInfo{Info: monitor.UserInfo{Plan: "A"}, Retrieved: time.Now()}
	svc.premium.cache[other.User] = PremiumInfo{Info: monitor.UserInfo{Plan: "B"}, Retrieved: time.Now()}
	svc.premium.mu.Unlock()

	if err := svc.SwitchDefault(other.User); err != nil {
		t.Fatalf("switch default: %v", err)
	}

	svc.premium.mu.Lock()
	_, exists := svc.premium.cache[other.User]
	svc.premium.mu.Unlock()

	if exists {
		t.Fatalf("expected premium cache for %q invalidated", other.User)
	}
}

func newPremiumServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	called := new(int)
	payload := map[string]any{
		"copilot_plan": "business",
		"organization": map[string]any{"name": "org"},
		"quota_snapshots": map[string]any{
			"premium_interactions": map[string]any{
				"entitlement":       10,
				"remaining":         5,
				"percent_remaining": 50.0,
				"unlimited":         false,
				"resets_at":         "2026-03-13T00:00:00Z",
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/copilot_internal/user" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		(*called)++
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Fatalf("encode payload: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server, called
}

// stubFlow satisfies the deviceFlow interface for tests.
type stubFlow struct {
	device auth.DeviceCodeResponse
	token  string
	pollFn func(context.Context, auth.DeviceCodeResponse) (string, error)
}

func (s *stubFlow) RequestCodeWithContext(ctx context.Context) (auth.DeviceCodeResponse, error) {
	return s.device, nil
}

func (s *stubFlow) PollAccessTokenWithContext(ctx context.Context, device auth.DeviceCodeResponse) (string, error) {
	if s.pollFn != nil {
		return s.pollFn(ctx, device)
	}
	return s.token, nil
}
