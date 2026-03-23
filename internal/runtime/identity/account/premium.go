package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	core "copilot-proxy/internal/runtime/types"
)

var ErrUserAPIFailed = errors.New("user API returned non-200")

type userResponse struct {
	CopilotPlan  string `json:"copilot_plan"`
	Organization struct {
		Name string `json:"name"`
	} `json:"organization"`
	QuotaSnapshots struct {
		PremiumInteractions struct {
			Entitlement      int64   `json:"entitlement"`
			Remaining        int64   `json:"remaining"`
			PercentRemaining float64 `json:"percent_remaining"`
			Unlimited        bool    `json:"unlimited"`
			ResetsAt         string  `json:"resets_at"`
		} `json:"premium_interactions"`
	} `json:"quota_snapshots"`
}

// FetchUserInfo retrieves Copilot subscription and quota information from GitHub API.
func FetchUserInfo(ctx context.Context, client *http.Client, baseURL, githubToken string) (*core.UserInfo, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/copilot_internal/user", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "token "+githubToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUserAPIFailed, resp.StatusCode)
	}

	var result userResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var resetDate time.Time
	if result.QuotaSnapshots.PremiumInteractions.ResetsAt != "" {
		resetDate, _ = time.Parse(time.RFC3339, result.QuotaSnapshots.PremiumInteractions.ResetsAt)
	}

	return &core.UserInfo{
		Plan:         result.CopilotPlan,
		Organization: result.Organization.Name,
		Quota: core.QuotaSnapshot{
			Entitlement:      result.QuotaSnapshots.PremiumInteractions.Entitlement,
			Remaining:        result.QuotaSnapshots.PremiumInteractions.Remaining,
			PercentRemaining: result.QuotaSnapshots.PremiumInteractions.PercentRemaining,
			Unlimited:        result.QuotaSnapshots.PremiumInteractions.Unlimited,
		},
		ResetDate: resetDate,
	}, nil
}
