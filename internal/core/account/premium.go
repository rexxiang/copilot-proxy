package account

import (
	"context"
	"net/http"
	"sync"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/monitor"
)

const premiumTTL = 30 * time.Second

// PremiumInfo exposes cached user info + snapshot state.
type PremiumInfo struct {
	Info      monitor.UserInfo
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
func (p *PremiumService) Get(ctx context.Context, account config.Account, force bool) (monitor.UserInfo, error) {
	if account.User == "" {
		return monitor.UserInfo{}, config.ErrAccountNotFound
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

	info, err := monitor.FetchUserInfo(ctx, p.client, p.baseURL, account.GhToken)
	if err != nil {
		return monitor.UserInfo{}, err
	}
	var userInfo monitor.UserInfo
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
