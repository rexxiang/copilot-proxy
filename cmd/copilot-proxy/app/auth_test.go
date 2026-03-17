package app

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"copilot-proxy/internal/core/account"
)

func TestPrintAccountsIncludesDefaultLabel(t *testing.T) {
	accounts := []account.AccountDTO{
		{User: "u1"},
		{User: "u2", IsDefault: true},
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
		os.Stdout = oldStdout
	})

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()

	if err := printAccounts(accounts); err != nil {
		t.Fatalf("printAccounts: %v", err)
	}
	_ = writer.Close()
	output := <-done

	if !strings.Contains(output, "u1") {
		t.Fatalf("missing account line: %q", output)
	}
	if !strings.Contains(output, "u2 (default)") {
		t.Fatalf("missing default label: %q", output)
	}
}

func TestRunAuthListTUI_NoAccountsConfigured(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		_ = reader.Close()
		os.Stdout = oldStdout
	})

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		done <- buf.String()
	}()

	if err := runAuthListTUI(stubAccountManager{}); err != nil {
		t.Fatalf("runAuthListTUI: %v", err)
	}
	_ = writer.Close()
	output := <-done

	if !strings.Contains(output, "No accounts configured") {
		t.Fatalf("unexpected output: %q", output)
	}
}

type stubAccountManager struct {
	accounts []account.AccountDTO
}

func (s stubAccountManager) List() []account.AccountDTO {
	return s.accounts
}

func (stubAccountManager) SwitchDefault(string) error {
	return nil
}

func (stubAccountManager) Remove(string) error {
	return nil
}
