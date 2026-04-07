package api

import (
	"context"

	runtimeconfig "copilot-proxy/internal/runtime/config"
	coreaccount "copilot-proxy/internal/runtime/identity/account"
	auth "copilot-proxy/internal/runtime/identity/oauth"
	core "copilot-proxy/internal/runtime/types"
)

func (r *Engine) RequestCode(ctx context.Context) (auth.DeviceCodeResponse, error) {
	client := r.httpClientFactory()
	flow := auth.DeviceFlow{
		ClientID:   runtimeconfig.OAuthClientID,
		Scope:      runtimeconfig.OAuthScope,
		BaseURL:    r.githubOAuthBaseURL,
		HTTPClient: client,
	}
	return flow.RequestCodeWithContext(ctx)
}

func (r *Engine) PollToken(ctx context.Context, device auth.DeviceCodeResponse) (string, error) {
	client := r.httpClientFactory()
	flow := auth.DeviceFlow{
		ClientID:   runtimeconfig.OAuthClientID,
		Scope:      runtimeconfig.OAuthScope,
		BaseURL:    r.githubOAuthBaseURL,
		HTTPClient: client,
	}
	return flow.PollAccessTokenWithContext(ctx, device)
}

func (r *Engine) FetchUserInfo(ctx context.Context, tokenValue string) (*core.UserInfo, error) {
	client := r.httpClientFactory()
	return coreaccount.FetchUserInfo(ctx, client, r.githubAPIBaseURL, tokenValue)
}

func (r *Engine) FetchLogin(ctx context.Context, tokenValue string) (string, error) {
	client := r.httpClientFactory()
	return auth.FetchUserWithContext(ctx, client, r.githubAPIBaseURL, tokenValue)
}
