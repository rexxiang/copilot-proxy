package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core/controller"
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

	ctrl, err := controller.NewServiceController(ctx, controller.ControllerDeps{})
	if err != nil {
		return fmt.Errorf("init controller: %w", err)
	}
	defer ctrl.Stop()

	svc := ctrl.AccountService()
	if svc == nil {
		return errors.New("account service unavailable")
	}

	challenge, err := svc.BeginLogin(ctx)
	if err != nil {
		return fmt.Errorf("request device code: %w", err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "Open %s and enter code %s\n", challenge.VerificationURI, challenge.UserCode); err != nil {
		return fmt.Errorf("write auth prompt: %w", err)
	}

	result, err := svc.PollLogin(ctx, challenge.Seq)
	if err != nil {
		return fmt.Errorf("poll login: %w", err)
	}
	if result.Login == "" || result.Token == "" {
		return fmt.Errorf("poll login: invalid credentials")
	}

	account := config.Account{
		User:    result.Login,
		GhToken: result.Token,
		AppID:   "",
	}
	if err := svc.Add(account); err != nil {
		return fmt.Errorf("save account: %w", err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "Account %s added\n", result.Login); err != nil {
		return fmt.Errorf("write auth success: %w", err)
	}
	return nil
}

func runAuthRemove(user string) error {
	ctrl, err := controller.NewServiceController(context.Background(), controller.ControllerDeps{})
	if err != nil {
		return fmt.Errorf("init controller: %w", err)
	}
	defer ctrl.Stop()

	svc := ctrl.AccountService()
	if svc == nil {
		return errors.New("account service unavailable")
	}
	if err := svc.Remove(user); err != nil {
		if errors.Is(err, config.ErrAccountNotFound) {
			return errAuthAccountNotFound
		}
		return fmt.Errorf("remove account: %w", err)
	}
	return nil
}

func runAuthList() error {
	ctrl, err := controller.NewServiceController(context.Background(), controller.ControllerDeps{})
	if err != nil {
		return fmt.Errorf("init controller: %w", err)
	}
	defer ctrl.Stop()

	svc := ctrl.AccountService()
	if svc == nil {
		return errors.New("account service unavailable")
	}
	return runAuthListTUI(svc)
}
