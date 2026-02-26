package monitor

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DebugLogger writes detailed request/response logs to files.
// It maintains two log files:
// - Error log: always enabled, records 4xx/5xx responses (daily rotation)
// - Debug log: manually enabled, records all requests (daily rotation).
type DebugLogger struct {
	mu            sync.Mutex
	errorFile     *os.File // Error log file (always open when initialized)
	debugFile     *os.File // Debug log file (open when debug enabled)
	debugEnabled  bool
	errorFilePath string
	debugFilePath string
	logDir        string
	errorDate     string // Current error log date for rotation
	debugDate     string // Current debug log date for rotation
}

const (
	debugLogDirMode     = 0o700
	debugLogFileMode    = 0o600
	debugLogDateFmt     = "20060102"
	logFileExt          = ".md"
	errorLogSubdir      = "error"
	debugLogSubdir      = "debug"
	debugStatusErrorMin = 400
)

var (
	errLogDirNotSet = errors.New("log directory not set")
	errNilLogEntry  = errors.New("nil log entry")
)

// DebugLogEntry represents a single debug log entry.
type DebugLogEntry struct {
	Timestamp       string            `json:"timestamp"`
	Path            string            `json:"path"` // compatibility field: local -> upstream -> "-"
	LocalPath       string            `json:"local_path,omitempty"`
	UpstreamPath    string            `json:"upstream_path,omitempty"`
	Model           string            `json:"model,omitempty"`
	Account         string            `json:"account,omitempty"`
	StatusCode      int               `json:"status_code"`
	Duration        string            `json:"duration"`
	IsVision        bool              `json:"is_vision,omitempty"`
	IsAgent         bool              `json:"is_agent,omitempty"`
	UpstreamURL     string            `json:"upstream_url,omitempty"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	RequestBody     string            `json:"request_body,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	ResponseBody    string            `json:"response_body,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// NewDebugLogger creates a new debug logger.
// The logger is disabled by default and creates no file until initialized.
func NewDebugLogger() *DebugLogger {
	return &DebugLogger{}
}

// Init initializes the error log file (always enabled).
// This should be called at startup.
func (l *DebugLogger) Init(logDir string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.setLogDirLocked(logDir); err != nil {
		return err
	}

	// Open error log file (daily rotation)
	return l.rotateErrorLogLocked()
}

// rotateErrorLogLocked rotates the error log file if needed.
// Must be called with mu held.
func (l *DebugLogger) rotateErrorLogLocked() error {
	today := time.Now().Format(debugLogDateFmt)
	if l.errorDate == today && l.errorFile != nil {
		return nil // No rotation needed
	}

	// Close old file if open
	if l.errorFile != nil {
		if err := l.errorFile.Close(); err != nil {
			return fmt.Errorf("close error log file: %w", err)
		}
		l.errorFile = nil
	}

	filePath := filepath.Join(l.logDir, errorLogSubdir, today+logFileExt)
	l.errorDate = today

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, debugLogFileMode)
	if err != nil {
		return fmt.Errorf("open error log file: %w", err)
	}

	l.errorFilePath = filePath
	l.errorFile = f
	return nil
}

// rotateDebugLogLocked rotates the debug log file if needed.
// Must be called with mu held.
func (l *DebugLogger) rotateDebugLogLocked() error {
	today := time.Now().Format(debugLogDateFmt)
	if l.debugDate == today && l.debugFile != nil {
		return nil
	}

	if l.debugFile != nil {
		if err := l.debugFile.Close(); err != nil {
			return fmt.Errorf("close debug log file: %w", err)
		}
		l.debugFile = nil
	}

	filePath := filepath.Join(l.logDir, debugLogSubdir, today+logFileExt)
	l.debugDate = today

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, debugLogFileMode)
	if err != nil {
		return fmt.Errorf("open debug log file: %w", err)
	}

	l.debugFilePath = filePath
	l.debugFile = f
	return nil
}

// setLogDirLocked sets log root and ensures required sub-directories exist.
// Must be called with mu held.
func (l *DebugLogger) setLogDirLocked(logDir string) error {
	if logDir == "" {
		return errLogDirNotSet
	}
	if err := os.MkdirAll(filepath.Join(logDir, errorLogSubdir), debugLogDirMode); err != nil {
		return fmt.Errorf("create error log dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(logDir, debugLogSubdir), debugLogDirMode); err != nil {
		return fmt.Errorf("create debug log dir: %w", err)
	}
	l.logDir = logDir
	return nil
}

// EnableDebug starts debug logging to a daily file.
func (l *DebugLogger) EnableDebug(logDir string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.debugEnabled {
		return nil // Already enabled
	}

	// Use existing logDir if not specified
	if logDir == "" {
		logDir = l.logDir
	}
	if err := l.setLogDirLocked(logDir); err != nil {
		return err
	}

	l.debugEnabled = true
	return l.rotateDebugLogLocked()
}

// DisableDebug stops debug logging and closes the debug file.
func (l *DebugLogger) DisableDebug() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.debugEnabled {
		return nil
	}

	l.debugEnabled = false
	if l.debugFile != nil {
		err := l.debugFile.Close()
		l.debugFile = nil
		l.debugDate = ""
		if err != nil {
			return fmt.Errorf("close debug log file: %w", err)
		}
		return nil
	}
	return nil
}

// DebugEnabled returns whether debug logging is enabled.
func (l *DebugLogger) DebugEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.debugEnabled
}

// Enabled returns whether the logger is initialized (error logging enabled).
// For backward compatibility.
func (l *DebugLogger) Enabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.errorFile != nil || l.debugEnabled
}

// DebugFilePath returns the current debug log file path (empty if not enabled).
func (l *DebugLogger) DebugFilePath() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.debugFilePath
}

// ErrorFilePath returns the current error log file path (empty if not initialized).
func (l *DebugLogger) ErrorFilePath() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.errorFilePath
}

// FilePath returns the current debug log file path.
// For backward compatibility with TUI display.
func (l *DebugLogger) FilePath() string {
	return l.DebugFilePath()
}

// Log routes one entry to error/debug logs based on status code and debug mode.
func (l *DebugLogger) Log(entry *DebugLogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry == nil {
		return errNilLogEntry
	}

	shouldWriteError := entry.StatusCode >= debugStatusErrorMin
	shouldWriteDebug := l.debugEnabled
	if !shouldWriteError && !shouldWriteDebug {
		return nil
	}

	var firstErr error

	if shouldWriteError {
		if err := l.rotateErrorLogLocked(); err != nil {
			firstErr = fmt.Errorf("rotate error log: %w", err)
		} else if l.errorFile != nil {
			if err := writeLogEntry(l.errorFile, entry, "error"); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	if shouldWriteDebug {
		if err := l.rotateDebugLogLocked(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("rotate debug log: %w", err)
			}
		} else if l.debugFile != nil {
			if err := writeLogEntry(l.debugFile, entry, "debug"); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func writeLogEntry(file *os.File, entry *DebugLogEntry, logType string) error {
	data, err := formatHTTPLog(entry)
	if err != nil {
		return fmt.Errorf("format %s log: %w", logType, err)
	}

	if _, err := file.WriteString("###\n"); err != nil {
		return fmt.Errorf("write %s log header: %w", logType, err)
	}
	if _, err := file.WriteString(data); err != nil {
		return fmt.Errorf("write %s log: %w", logType, err)
	}
	return nil
}

func formatHTTPLog(logEntry *DebugLogEntry) (string, error) {
	if logEntry == nil {
		return "", errNilLogEntry
	}

	reqDate := logEntry.Timestamp
	respDate := logEntry.Timestamp
	if reqDate == "" {
		reqDate = time.Now().Format(time.RFC3339Nano)
		respDate = reqDate
	}
	if logEntry.Duration != "" {
		if d, err := time.ParseDuration(logEntry.Duration); err == nil {
			if t, err := time.Parse(time.RFC3339Nano, reqDate); err == nil {
				respDate = t.Add(d).Format(time.RFC3339Nano)
			}
		}
	}

	method := "UNKNOWN"
	if logEntry.RequestHeaders != nil {
		if m, ok := logEntry.RequestHeaders["X-Method"]; ok && m != "" {
			method = m
		}
	}
	localPath := chooseLocalPath(logEntry)
	upstreamPath := chooseUpstreamPath(logEntry)
	displayPath := chooseDisplayPath(logEntry, localPath, upstreamPath)
	url := logEntry.UpstreamURL
	if url == "" {
		url = displayPath
	}

	var sb strings.Builder
	sb.WriteString("# @date: ")
	sb.WriteString(reqDate)
	sb.WriteString("\n")
	if localPath != "" {
		sb.WriteString("# @local_path: ")
		sb.WriteString(localPath)
		sb.WriteString("\n")
	}
	if upstreamPath != "" {
		sb.WriteString("# @upstream_path: ")
		sb.WriteString(upstreamPath)
		sb.WriteString("\n")
	}
	sb.WriteString(method)
	sb.WriteString(" ")
	sb.WriteString(url)
	sb.WriteString("\n")
	sb.WriteString(formatHeaders(logEntry.RequestHeaders))
	sb.WriteString("\n")
	sb.WriteString(logEntry.RequestBody)
	sb.WriteString("\n\n---\n")
	sb.WriteString("# @date: ")
	sb.WriteString(respDate)
	sb.WriteString("\n")
	sb.WriteString("# @status: ")
	sb.WriteString(formatStatusLine(logEntry.StatusCode))
	sb.WriteString("\n")
	sb.WriteString(formatHeaders(logEntry.ResponseHeaders))
	sb.WriteString("\n")
	sb.WriteString(logEntry.ResponseBody)
	if logEntry.Error != "" {
		if logEntry.ResponseBody != "" {
			sb.WriteString("\n")
		}
		sb.WriteString(logEntry.Error)
	}
	sb.WriteString("\n")
	return sb.String(), nil
}

func chooseDisplayPath(logEntry *DebugLogEntry, localPath, upstreamPath string) string {
	if logEntry.Path != "" {
		return logEntry.Path
	}
	if localPath != "" {
		return localPath
	}
	if upstreamPath != "" {
		return upstreamPath
	}
	return "-"
}

func chooseLocalPath(logEntry *DebugLogEntry) string {
	return logEntry.LocalPath
}

func chooseUpstreamPath(logEntry *DebugLogEntry) string {
	if logEntry.UpstreamPath != "" {
		return logEntry.UpstreamPath
	}
	if logEntry.LocalPath == "" && logEntry.Path != "" {
		return logEntry.Path
	}
	return ""
}

func formatHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(headers[k])
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatStatusLine(statusCode int) string {
	if statusCode == 0 {
		return "0 Unknown"
	}
	text := http.StatusText(statusCode)
	if text == "" {
		text = "Unknown"
	}
	return fmt.Sprintf("%d %s", statusCode, text)
}

// Enable starts debug logging (backward compatibility).
//
// Deprecated: Use EnableDebug instead.
func (l *DebugLogger) Enable(logDir string) error {
	return l.EnableDebug(logDir)
}

// Disable stops debug logging (backward compatibility).
//
// Deprecated: Use DisableDebug instead.
func (l *DebugLogger) Disable() error {
	return l.DisableDebug()
}

// Close closes the logger, stopping all logging.
func (l *DebugLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var errs []error

	if l.debugFile != nil {
		if err := l.debugFile.Close(); err != nil {
			errs = append(errs, err)
		}
		l.debugFile = nil
	}
	l.debugEnabled = false
	l.debugDate = ""

	if l.errorFile != nil {
		if err := l.errorFile.Close(); err != nil {
			errs = append(errs, err)
		}
		l.errorFile = nil
	}
	l.errorDate = ""

	if len(errs) > 0 {
		return fmt.Errorf("close log files: %w", errs[0])
	}
	return nil
}

// CleanOldLogs removes log files older than maxAgeDays.
func CleanOldLogs(logDir string, maxAgeDays int) error {
	if err := cleanOldLogsInSubdir(filepath.Join(logDir, errorLogSubdir), maxAgeDays); err != nil {
		return err
	}
	if err := cleanOldLogsInSubdir(filepath.Join(logDir, debugLogSubdir), maxAgeDays); err != nil {
		return err
	}
	// Backward compatibility cleanup for legacy "*.log" files in root.
	return cleanLegacyRootLogs(logDir, maxAgeDays)
}

func cleanOldLogsInSubdir(dir string, maxAgeDays int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read log dir: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), logFileExt) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}

func cleanLegacyRootLogs(logDir string, maxAgeDays int) error {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read log dir: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "copilot-proxy-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(logDir, name))
		}
	}
	return nil
}
