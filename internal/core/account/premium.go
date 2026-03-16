package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
)

const premiumTTL = 30 * time.Second

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

// PremiumInfo exposes cached user info + snapshot state.
type PremiumInfo struct {
	Info      core.UserInfo
	Retrieved time.Time
}

// PremiumService caches premium interactions per user.
type PremiumService struct {
	mu      sync.Mutex
	client  *http.Client
	cache   map[string]PremiumInfo
	baseURL string
}

// NewPremiumService initializes the service with an HTTP client.
func NewPremiumService(client *http.Client, baseURL string) *PremiumService {
	if client == nil {
		client = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = config.GitHubAPIURL
	}
	return &PremiumService{client: client, cache: make(map[string]PremiumInfo), baseURL: baseURL}
}

// Get returns cached user info or refreshes when the TTL has elapsed or force is set.
func (p *PremiumService) Get(ctx context.Context, account config.Account, force bool) (core.UserInfo, error) {
	if account.User == "" {
		return core.UserInfo{}, config.ErrAccountNotFound
	}
	key := account.User
	p.mu.Lock()
	entry, ok := p.cache[key]
	if ok && !force && time.Since(entry.Retrieved) < premiumTTL {
		info := entry.Info
		p.mu.Unlock()
		return info, nil
	}
	p.mu.Unlock()

	info, err := FetchUserInfo(ctx, p.client, p.baseURL, account.GhToken)
	if err != nil {
		return core.UserInfo{}, err
	}
	var userInfo core.UserInfo
	if info != nil {
		userInfo = *info
	}
	p.mu.Lock()
	p.cache[key] = PremiumInfo{Info: userInfo, Retrieved: time.Now()}
	p.mu.Unlock()
	return userInfo, nil
}

// Invalidate removes cached premium metadata for the given user.
func (p *PremiumService) Invalidate(user string) {
	if user == "" {
		return
	}
	p.mu.Lock()
	delete(p.cache, user)
	p.mu.Unlock()
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
