package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"copilot-proxy/internal/config"
)

func TestPremiumServiceCaching(t *testing.T) {
	account := config.Account{User: "alice", GhToken: "token"}
	called := 0
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
		called++
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	svc := NewPremiumService(nil, server.URL)
	ctx := context.Background()

	info, err := svc.Get(ctx, account, false)
	if err != nil {
		t.Fatalf("prem service get: %v", err)
	}
	if info.Plan != "business" {
		t.Fatalf("unexpected plan: %s", info.Plan)
	}
	if called != 1 {
		t.Fatalf("expected 1 fetch, got %d", called)
	}

	if _, err := svc.Get(ctx, account, false); err != nil {
		t.Fatalf("prem service cached read: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected cache hit, got %d calls", called)
	}

	if _, err := svc.Get(ctx, account, true); err != nil {
		t.Fatalf("prem service force refresh: %v", err)
	}
	if called != 2 {
		t.Fatalf("expected forced refresh, got %d calls", called)
	}

	svc.Invalidate(account.User)
	if _, err := svc.Get(ctx, account, false); err != nil {
		t.Fatalf("prem service after invalidation: %v", err)
	}
	if called != 3 {
		t.Fatalf("expected new fetch after invalidation, got %d", called)
	}
}
