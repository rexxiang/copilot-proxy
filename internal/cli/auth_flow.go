package cli

import (
	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/config"
)

func newDefaultDeviceFlow() auth.DeviceFlow {
	return auth.DeviceFlow{
		ClientID: config.OAuthClientID,
		Scope:    config.OAuthScope,
		BaseURL:  config.GitHubBaseURL,
	}
}
