package app

import (
	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
	runtimeconfig "copilot-proxy/internal/runtime/config"
)

func loadRuntimeConfigFromAppSettings() (runtimeconfig.RuntimeSettings, error) {
	current, err := appsettings.LoadSettings()
	if err != nil {
		return runtimeconfig.RuntimeSettings{}, err
	}
	return appsettings.ToRuntimeConfig(current), nil
}
