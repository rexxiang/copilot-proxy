package app

import "testing"

func TestRunUnknownCommand(t *testing.T) {
	if err := Run([]string{"unknown"}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunAuthMissingSubcommand(t *testing.T) {
	if err := Run([]string{"auth"}); err == nil {
		t.Fatalf("expected error")
	}
}
