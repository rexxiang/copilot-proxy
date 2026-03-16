package monitor

import (
	"context"
	"net/http"

	"copilot-proxy/internal/core/account"
)

var (
	// ErrUserAPIFailed reports that the GitHub user endpoint returned a non-200 status code.
	ErrUserAPIFailed = account.ErrUserAPIFailed
)

// FetchUserInfo retrieves Copilot subscription and quota information from GitHub API.
func FetchUserInfo(ctx context.Context, client *http.Client, baseURL, githubToken string) (*UserInfo, error) {
	return account.FetchUserInfo(ctx, client, baseURL, githubToken)
}
