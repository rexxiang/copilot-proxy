package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDebugLogger_EnableDisable(t *testing.T) {
	logger := NewDebugLogger()
	t.Cleanup(func() {
		_ = logger.Close()
	})
	tmpDir := t.TempDir()

	// Should be disabled by default
	if logger.DebugEnabled() {
		t.Error("debug should be disabled by default")
	}

	// Enable debug logging
	if err := logger.EnableDebug(tmpDir); err != nil {
		t.Fatalf("EnableDebug failed: %v", err)
	}

	if !logger.DebugEnabled() {
		t.Error("debug should be enabled after EnableDebug()")
	}

	// Check file path format
	fp := logger.DebugFilePath()
	if !strings.Contains(fp, filepath.Join("debug", "")) {
		t.Errorf("file path should be under debug directory, got %s", fp)
	}
	if !strings.HasSuffix(fp, ".md") {
		t.Errorf("file path should end with .md, got %s", fp)
	}

	// Disable logging
	if err := logger.DisableDebug(); err != nil {
		t.Fatalf("DisableDebug failed: %v", err)
	}

	if logger.DebugEnabled() {
		t.Error("debug should be disabled after DisableDebug()")
	}
}

type initializedDebugLogger struct {
	logger *DebugLogger
	dir    string
}

func newInitializedDebugLogger(t *testing.T) initializedDebugLogger {
	t.Helper()

	logger := NewDebugLogger()
	t.Cleanup(func() {
		_ = logger.Close()
	})
	tmpDir := t.TempDir()
	if err := logger.Init(tmpDir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return initializedDebugLogger{
		logger: logger,
		dir:    tmpDir,
	}
}

func TestDebugLogger_LogSkips2xxWhenDebugDisabled(t *testing.T) {
	ctx := newInitializedDebugLogger(t)
	logger := ctx.logger

	// Debug disabled + 2xx => no files written to debug.
	if err := logger.Log(&DebugLogEntry{
		Path:       "/test",
		StatusCode: 200,
		RequestHeaders: map[string]string{
			"X-Method": "POST",
		},
	}); err != nil {
		t.Fatalf("log 2xx with debug off: %v", err)
	}
	if logger.DebugFilePath() != "" {
		t.Fatalf("expected no debug file path when debug is disabled")
	}
}

func TestDebugLogger_LogWritesErrorWhenDebugDisabled(t *testing.T) {
	ctx := newInitializedDebugLogger(t)
	logger := ctx.logger

	// Debug disabled + 5xx => write to error file only.
	if err := logger.Log(&DebugLogEntry{
		Timestamp:  time.Now().Format(time.RFC3339),
		Path:       "/v1/chat/completions",
		StatusCode: 500,
		Error:      "internal server error",
		RequestHeaders: map[string]string{
			"X-Method": "POST",
		},
	}); err != nil {
		t.Fatalf("log 5xx with debug off: %v", err)
	}
	errorPath := logger.ErrorFilePath()
	if errorPath == "" {
		t.Fatal("error file path should not be empty")
	}
	if !strings.Contains(errorPath, filepath.Join("error", "")) {
		t.Fatalf("error file should be under error directory, got %s", errorPath)
	}
	if !strings.HasSuffix(errorPath, ".md") {
		t.Fatalf("error file should end with .md, got %s", errorPath)
	}
	errorData, err := os.ReadFile(errorPath)
	if err != nil {
		t.Fatalf("read error log file: %v", err)
	}
	if !strings.Contains(string(errorData), "500 Internal Server Error") {
		t.Fatalf("expected 500 status in error log, got: %s", string(errorData))
	}
}

func TestDebugLogger_LogWritesDebugWhenEnabled(t *testing.T) {
	ctx := newInitializedDebugLogger(t)
	logger := ctx.logger

	// Debug enabled + 2xx => write to debug file.
	if err := logger.EnableDebug(ctx.dir); err != nil {
		t.Fatalf("EnableDebug failed: %v", err)
	}
	if err := logger.Log(&DebugLogEntry{
		Timestamp:  "2024-01-15T10:30:00Z",
		Path:       "/v1/chat/completions",
		Model:      "gpt-4o",
		StatusCode: 200,
		Duration:   "1.5s",
		RequestHeaders: map[string]string{
			"X-Method":     "POST",
			"Content-Type": "application/json",
		},
		RequestBody:  `{"model":"gpt-4o"}`,
		ResponseBody: `{"choices":[]}`,
	}); err != nil {
		t.Fatalf("log 2xx with debug on: %v", err)
	}

	debugPath := logger.DebugFilePath()
	if debugPath == "" {
		t.Fatal("debug file path should not be empty")
	}
	data, err := os.ReadFile(debugPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "###") {
		t.Errorf("log should contain entry delimiter, got: %s", content)
	}
	if !strings.Contains(content, "# @date:") {
		t.Errorf("log should contain date header, got: %s", content)
	}
	if !strings.Contains(content, "# @status: 200 OK") {
		t.Errorf("log should contain status line, got: %s", content)
	}
	if !strings.Contains(content, "---") {
		t.Errorf("log should contain response separator, got: %s", content)
	}
	if !strings.Contains(content, "gpt-4o") {
		t.Errorf("log should contain model name, got: %s", content)
	}
	if !strings.Contains(content, "/v1/chat/completions") {
		t.Errorf("log should contain path, got: %s", content)
	}
}

func TestDebugLogger_LogWritesBothWhenErrorAndDebugEnabled(t *testing.T) {
	ctx := newInitializedDebugLogger(t)
	logger := ctx.logger

	if err := logger.EnableDebug(ctx.dir); err != nil {
		t.Fatalf("EnableDebug failed: %v", err)
	}
	// Seed debug and error files first so path fields are set.
	if err := logger.Log(&DebugLogEntry{
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		RequestHeaders: map[string]string{
			"X-Method": "POST",
		},
	}); err != nil {
		t.Fatalf("seed debug file: %v", err)
	}
	if err := logger.Log(&DebugLogEntry{
		Path:       "/v1/chat/completions",
		StatusCode: 500,
		RequestHeaders: map[string]string{
			"X-Method": "POST",
		},
	}); err != nil {
		t.Fatalf("seed error file: %v", err)
	}
	debugPath := logger.DebugFilePath()
	errorPath := logger.ErrorFilePath()

	// Debug enabled + 5xx => write to both debug and error.
	if err := logger.Log(&DebugLogEntry{
		Timestamp:  time.Now().Format(time.RFC3339),
		Path:       "/v1/chat/completions",
		StatusCode: 502,
		Error:      "upstream failed",
		RequestHeaders: map[string]string{
			"X-Method": "POST",
		},
	}); err != nil {
		t.Fatalf("log 5xx with debug on: %v", err)
	}
	debugData, err := os.ReadFile(debugPath)
	if err != nil {
		t.Fatalf("read debug log file after 5xx: %v", err)
	}
	if !strings.Contains(string(debugData), "502 Bad Gateway") {
		t.Fatalf("expected 502 status in debug log, got: %s", string(debugData))
	}
	errorData, err := os.ReadFile(errorPath)
	if err != nil {
		t.Fatalf("read error log file after 5xx: %v", err)
	}
	if !strings.Contains(string(errorData), "502 Bad Gateway") {
		t.Fatalf("expected 502 status in error log, got: %s", string(errorData))
	}
}

func TestDebugLogger_MultipleEnable(t *testing.T) {
	logger := NewDebugLogger()
	t.Cleanup(func() {
		_ = logger.Close()
	})
	tmpDir := t.TempDir()

	// Enable twice should not error
	if err := logger.EnableDebug(tmpDir); err != nil {
		t.Fatalf("first EnableDebug failed: %v", err)
	}

	fp1 := logger.DebugFilePath()

	if err := logger.EnableDebug(tmpDir); err != nil {
		t.Fatalf("second EnableDebug failed: %v", err)
	}

	// Should keep same file
	if logger.DebugFilePath() != fp1 {
		t.Error("second EnableDebug should not create new file")
	}

	_ = logger.Close()
}

func TestFormatHTTPLog_BadDuration(t *testing.T) {
	entry := DebugLogEntry{
		Timestamp:   "2024-01-15T10:30:00Z",
		Duration:    "not-a-duration",
		Path:        "/v1/chat/completions",
		UpstreamURL: "http://example.com/v1/chat/completions",
		StatusCode:  200,
		RequestHeaders: map[string]string{
			"X-Method": "POST",
		},
	}

	out, err := formatHTTPLog(&entry)
	if err != nil {
		t.Fatalf("formatHTTPLog failed: %v", err)
	}
	if !strings.Contains(out, "# @date: 2024-01-15T10:30:00Z") {
		t.Fatalf("expected request date, got %s", out)
	}
}

func TestFormatHTTPLog_PathFallbackAndMetadata(t *testing.T) {
	entry := DebugLogEntry{
		Timestamp:    "2024-01-15T10:30:00Z",
		LocalPath:    "",
		UpstreamPath: "/models",
		Path:         "",
		StatusCode:   200,
		RequestHeaders: map[string]string{
			"X-Method": "GET",
		},
	}

	out, err := formatHTTPLog(&entry)
	if err != nil {
		t.Fatalf("formatHTTPLog failed: %v", err)
	}
	if !strings.Contains(out, "# @upstream_path: /models") {
		t.Fatalf("expected upstream path metadata, got %s", out)
	}
	if !strings.Contains(out, "GET /models") {
		t.Fatalf("expected fallback request line path, got %s", out)
	}
}

func TestCleanOldLogs(t *testing.T) {
	tmpDir := t.TempDir()

	// Create old log file
	oldErrorDir := filepath.Join(tmpDir, "error")
	if err := os.MkdirAll(oldErrorDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(oldErrorDir, "20200101.md")
	if err := os.WriteFile(oldFile, []byte("old log"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Set mod time to 60 days ago
	oldTime := time.Now().AddDate(0, 0, -60)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Create new log file
	newDebugDir := filepath.Join(tmpDir, "debug")
	if err := os.MkdirAll(newDebugDir, 0o700); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(newDebugDir, "20250128.md")
	if err := os.WriteFile(newFile, []byte("new log"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create non-matching file (should not be deleted)
	otherFile := filepath.Join(tmpDir, "other-file.txt")
	if err := os.WriteFile(otherFile, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(otherFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Clean logs older than 30 days
	if err := CleanOldLogs(tmpDir, 30); err != nil {
		t.Fatalf("CleanOldLogs failed: %v", err)
	}

	// Old log should be deleted
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("old log file should be deleted")
	}

	// New log should still exist
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		t.Error("new log file should still exist")
	}

	// Other file should still exist (doesn't match pattern)
	if _, err := os.Stat(otherFile); os.IsNotExist(err) {
		t.Error("non-matching file should not be deleted")
	}
}

func TestCleanOldLogs_NonexistentDir(t *testing.T) {
	// Should not error on non-existent directory
	err := CleanOldLogs("/nonexistent/path/12345", 30)
	if err != nil {
		t.Errorf("CleanOldLogs should not error on non-existent dir: %v", err)
	}
}
