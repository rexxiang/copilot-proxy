package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/config"
)

var (
	errAuthRemoveMissingUser = errors.New("auth rm requires user-id")
	errAuthUnknownSubcommand = errors.New("unknown auth subcommand")
	errAuthAccountNotFound   = errors.New("account not found")
)

func runAuth(args []string) error {
	const authArgsMin = 2
	switch args[0] {
	case "login":
		return runAuthLogin()
	case "ls":
		return runAuthList()
	case "rm":
		if len(args) < authArgsMin {
			return errAuthRemoveMissingUser
		}
		return runAuthRemove(args[1])
	default:
		return fmt.Errorf("%w: %s", errAuthUnknownSubcommand, args[0])
	}
}

func runAuthLogin() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	flow := newDefaultDeviceFlow()
	device, err := flow.RequestCodeWithContext(ctx)
	if err != nil {
		return fmt.Errorf("request device code: %w", err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "Open %s and enter code %s\n", device.VerificationURI, device.UserCode); err != nil {
		return fmt.Errorf("write auth prompt: %w", err)
	}
	ghToken, err := flow.PollAccessTokenWithContext(ctx, device)
	if err != nil {
		return fmt.Errorf("poll access token: %w", err)
	}

	login, err := auth.FetchUserWithContext(ctx, nil, "", ghToken)
	if err != nil {
		return fmt.Errorf("fetch user: %w", err)
	}

	cfg, err := config.LoadAuth()
	if err != nil {
		return fmt.Errorf("load auth config: %w", err)
	}

	cfg.UpsertAccount(config.Account{User: login, GhToken: ghToken, AppID: ""})
	if err := config.SaveAuth(cfg); err != nil {
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

func runAuthRemove(user string) error {
	cfg, err := config.LoadAuth()
	if err != nil {
		return fmt.Errorf("load auth config: %w", err)
	}
	if removed := cfg.RemoveAccount(user); !removed {
		return errAuthAccountNotFound
	}
	if err := config.SaveAuth(cfg); err != nil {
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

func runAuthList() error {
	return runAuthListTUI()
}
