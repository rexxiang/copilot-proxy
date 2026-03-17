package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	errAuthMissingSubcommand = errors.New("auth: missing subcommand")
	errUnknownCommand        = errors.New("unknown command")
)

func Run(args []string) error {
	const authArgsMin = 2
	// Parse global flags
	noTUI := false
	var filteredArgs []string
	for _, arg := range args {
		if arg == "--no-tui" {
			noTUI = true
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	if len(filteredArgs) == 0 {
		return runServerWithTUI(!noTUI)
	}

	switch filteredArgs[0] {
	case "auth":
		if len(filteredArgs) < authArgsMin {
			return errAuthMissingSubcommand
		}
		return runAuth(filteredArgs[1:])
	case "help", "--help", "-h":
		return printHelp()
	default:
		return fmt.Errorf("%w: %s", errUnknownCommand, filteredArgs[0])
	}
}

func printHelp() error {
	help := `
copilot-proxy - GitHub Copilot API proxy with OpenAI-compatible endpoints

Usage:
  copilot-proxy [flags]
  copilot-proxy <command> [arguments]

Commands:
  auth login     Authenticate via GitHub device flow
  auth ls        List and manage accounts
  auth rm <user> Remove an account

Flags:
  --no-tui       Disable TUI monitor when starting server

By default, the server starts with a TUI monitor panel (if TTY is available).
Use --no-tui to run in headless mode (suitable for background/daemon mode).
`
	_, err := fmt.Fprintln(os.Stdout, strings.TrimSpace(help))
	if err != nil {
		return fmt.Errorf("print help: %w", err)
	}
	return nil
}

// runServer is implemented in server.go
