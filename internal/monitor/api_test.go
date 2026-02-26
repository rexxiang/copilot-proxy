package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchUserInfo(t *testing.T) {
	userResponse := map[string]any{
		"copilot_plan": "business",
		"organization": map[string]any{
			"name": "TestOrg",
		},
		"quota_snapshots": map[string]any{
			"premium_interactions": map[string]any{
				"entitlement":       100,
				"remaining":         75,
				"percent_remaining": 75.0,
				"unlimited":         false,
				"resets_at":         "2024-02-01T00:00:00Z",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/copilot_internal/user" {
			t.Errorf("expected path /copilot_internal/user, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token github-token" {
			t.Errorf("expected Authorization header with github-token")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(userResponse); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := &http.Client{}
	info, err := FetchUserInfo(context.Background(), client, server.URL, "github-token")
	if err != nil {
		t.Fatalf("FetchUserInfo failed: %v", err)
	}

	if info.Plan != "business" {
		t.Errorf("expected plan business, got %s", info.Plan)
	}
	if info.Organization != "TestOrg" {
		t.Errorf("expected org TestOrg, got %s", info.Organization)
	}
	if info.Quota.Entitlement != 100 {
		t.Errorf("expected entitlement 100, got %d", info.Quota.Entitlement)
	}
	if info.Quota.Remaining != 75 {
		t.Errorf("expected remaining 75, got %d", info.Quota.Remaining)
	}
	if info.Quota.PercentRemaining != 75.0 {
		t.Errorf("expected percent_remaining 75.0, got %f", info.Quota.PercentRemaining)
	}
	if info.Quota.Unlimited {
		t.Error("expected unlimited to be false")
	}
}

func TestFetchUserInfoUnlimited(t *testing.T) {
	userResponse := map[string]any{
		"copilot_plan": "individual",
		"quota_snapshots": map[string]any{
			"premium_interactions": map[string]any{
				"unlimited": true,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(userResponse); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := &http.Client{}
	info, err := FetchUserInfo(context.Background(), client, server.URL, "github-token")
	if err != nil {
		t.Fatalf("FetchUserInfo failed: %v", err)
	}

	if !info.Quota.Unlimited {
		t.Error("expected unlimited to be true")
	}
}
