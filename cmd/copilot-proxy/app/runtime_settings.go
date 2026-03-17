package app

import (
	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
	"copilot-proxy/internal/core/runtimeconfig"
)

func loadRuntimeConfigFromAppSettings() (runtimeconfig.Config, error) {
	current, err := appsettings.LoadSettings()
	if err != nil {
		return runtimeconfig.Config{}, err
	}
	return appsettings.ToRuntimeConfig(current), nil
}
