package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	runtimeapi "copilot-proxy/internal/runtime/api"
	runtimeconfig "copilot-proxy/internal/runtime/config"
	identitytoken "copilot-proxy/internal/runtime/identity/token"
	models "copilot-proxy/internal/runtime/model"
)

func resolveRuntimeToken(
	ctx context.Context,
	loadAuth func() (runtimeconfig.AuthConfig, error),
	accountRef string,
) (string, error) {
	if loadAuth == nil {
		return "", errors.New("auth loader is required")
	}

	auth, err := loadAuth()
	if err != nil {
		return "", fmt.Errorf("load auth: %w", err)
	}
	account, err := resolveRuntimeAccount(auth, accountRef)
	if err != nil {
		return "", err
	}
	return identitytoken.Resolve(ctx, account)
}

func resolveRuntimeAccount(auth runtimeconfig.AuthConfig, accountRef string) (runtimeconfig.Account, error) {
	if trimmed := strings.TrimSpace(accountRef); trimmed != "" {
		for _, account := range auth.Accounts {
			if account.User == trimmed {
				return account, nil
			}
		}
		return runtimeconfig.Account{}, runtimeconfig.ErrAccountNotFound
	}
	account, _, err := auth.DefaultAccount()
	return account, err
}

func resolveRuntimeModel(
	loadSettings func() (runtimeconfig.RuntimeSettings, error),
	catalog models.MutableCatalog,
	modelID string,
) (runtimeapi.ModelInfo, error) {
	resolved := runtimeapi.ModelInfo{ID: strings.TrimSpace(modelID)}
	if strings.TrimSpace(modelID) == "" || catalog == nil {
		return resolved, nil
	}

	selector := models.NewSelector()
	if loadSettings != nil {
		if settings, err := loadSettings(); err == nil {
			selector = models.NewSelectorWithConfig(models.SelectorConfig{
				ClaudeHaikuFallbackModels: settings.ClaudeHaikuFallbackModels,
			})
		}
	}

	selected, _, found := selector.SelectModelInfo(catalog.GetModels(), modelID)
	if !found {
		return resolved, nil
	}
	return runtimeapi.ModelInfo{
		ID:                       selected.ID,
		Endpoints:                append([]string(nil), selected.Endpoints...),
		SupportedReasoningEffort: append([]string(nil), selected.SupportedReasoningEffort...),
	}, nil
}

func preloadModels(
	ctx context.Context,
	settings *runtimeconfig.RuntimeSettings,
	auth *runtimeconfig.AuthConfig,
	client *http.Client,
	baseHeaders map[string]string,
	catalog models.MutableCatalog,
	loader models.Loader,
) error {
	if settings == nil || auth == nil {
		return errors.New("model catalog is required")
	}
	if catalog == nil {
		return errors.New("model catalog is required")
	}
	target, ok := catalog.(interface{ SetModels([]models.ModelInfo) })
	if !ok {
		return errors.New("model catalog does not support SetModels")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultModelTimeout)
	defer cancel()

	if loader != nil {
		loaded, err := loader.Load(ctx)
		if err != nil {
			return fmt.Errorf("load models: %w", err)
		}
		target.SetModels(loaded)
		return nil
	}

	account, _, err := auth.DefaultAccount()
	if err != nil {
		if errors.Is(err, runtimeconfig.ErrNoAccountsConfigured) || errors.Is(err, runtimeconfig.ErrDefaultAccountNotFound) {
			target.SetModels(nil)
			return nil
		}
		return fmt.Errorf("resolve default account: %w", err)
	}
	tokenValue, err := identitytoken.Resolve(ctx, account)
	if err != nil {
		return fmt.Errorf("fetch token: %w", err)
	}
	modelClient := client
	if modelClient == nil {
		modelClient = &http.Client{Timeout: defaultModelTimeout}
	}
	loaded, err := models.FetchModels(ctx, modelClient, settings.UpstreamBase, tokenValue, baseHeaders)
	if err != nil {
		return fmt.Errorf("fetch models: %w", err)
	}
	target.SetModels(loaded)
	return nil
}

// RefreshModels reloads model catalog through runtime core capabilities.
func (rt *Runtime) RefreshModels(ctx context.Context, accountRef string) ([]models.ModelInfo, error) {
	if rt == nil || rt.executor == nil {
		return nil, errRuntimeExecutorNotConfigured
	}
	if rt.resolveToken == nil {
		return nil, errRuntimeResolveTokenNotConfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tokenValue, err := rt.resolveToken(ctx, accountRef)
	if err != nil {
		return nil, fmt.Errorf("resolve token: %w", err)
	}
	items, err := rt.executor.FetchModels(ctx, tokenValue)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	if rt.ModelCatalog != nil {
		rt.ModelCatalog.SetModels(items)
	}
	return items, nil
}
