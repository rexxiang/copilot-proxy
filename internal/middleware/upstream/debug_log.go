package upstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/monitor"
)

// DebugLogMiddleware logs request/response details.
type DebugLogMiddleware struct {
	logger     middleware.DebugLogger
	buildEntry func(resp *http.Response, ctx context.Context) monitor.DebugLogEntry
}

const (
	debugResponseBodyLimit = 4096
)

// NewDebugLog builds a debug logging middleware.
func NewDebugLog(logger middleware.DebugLogger) DebugLogMiddleware {
	return DebugLogMiddleware{
		logger:     logger,
		buildEntry: BuildLogEntryFromContext,
	}
}

func (m DebugLogMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	resp, err := next()
	if m.logger == nil || resp == nil || m.buildEntry == nil {
		return resp, err
	}

	entry := m.buildEntry(resp, ctx.Request.Context())
	if isEventStreamResponseContentType(resp.Header.Get("Content-Type")) && resp.Body != nil {
		resp.Body = newStreamLoggingReadCloser(resp.Body, m.logger, &entry)
		return resp, err
	}
	if logErr := m.logger.Log(&entry); logErr != nil {
		slog.Error("debug log error", "err", logErr)
	}
	return resp, err
}

// BuildLogEntryFromContext constructs a debug log entry from response and context.
func BuildLogEntryFromContext(resp *http.Response, ctx context.Context) monitor.DebugLogEntry {
	req := resp.Request
	upstreamURL := ""
	upstreamPath := ""
	if req != nil && req.URL != nil {
		upstreamURL = req.URL.String()
		upstreamPath = req.URL.Path
	}
	localPath := requestLocalPath(ctx)
	path := choosePathForLog(localPath, upstreamPath)

	reqHeaders := requestHeaders(req, ctx)
	reqBody := requestBody(req, ctx)
	respHeaders := responseHeaders(resp)
	respBody := responseBody(resp)
	model := requestModel(ctx)
	accountName := requestAccount(ctx)
	isVision, isAgent := requestFlags(ctx)
	addRequestID(ctx, reqHeaders)
	addRequestMethod(resp, reqHeaders)
	duration := requestDuration(ctx)

	return monitor.DebugLogEntry{
		Timestamp:       time.Now().Format(time.RFC3339Nano),
		Path:            path,
		LocalPath:       localPath,
		UpstreamPath:    upstreamPath,
		Model:           model,
		Account:         accountName,
		StatusCode:      resp.StatusCode,
		Duration:        duration.String(),
		IsVision:        isVision,
		IsAgent:         isAgent,
		UpstreamURL:     upstreamURL,
		RequestHeaders:  monitor.MaskHeaders(reqHeaders),
		RequestBody:     reqBody,
		ResponseHeaders: respHeaders,
		ResponseBody:    respBody,
		Error:           "",
	}
}

func requestLocalPath(ctx context.Context) string {
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil {
		if rc.SourceLocalPath != "" {
			return rc.SourceLocalPath
		}
		return rc.LocalPath
	}
	return ""
}

func choosePathForLog(localPath, upstreamPath string) string {
	if localPath != "" {
		return localPath
	}
	if upstreamPath != "" {
		return upstreamPath
	}
	return "-"
}

func requestHeaders(req *http.Request, ctx context.Context) map[string]string {
	if req != nil {
		headers := make(map[string]string, len(req.Header))
		for key, values := range req.Header {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}
		return headers
	}
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil {
		return rc.Headers
	}
	return nil
}

func requestBody(req *http.Request, ctx context.Context) string {
	if req != nil && req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			defer func() {
				_ = body.Close()
			}()
			if bodyBytes, readErr := io.ReadAll(body); readErr == nil {
				return string(bodyBytes)
			}
		}
	}
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil && len(rc.Body) > 0 {
		return string(rc.Body)
	}
	return ""
}

func responseHeaders(resp *http.Response) map[string]string {
	respHeaders := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}
	return respHeaders
}

func responseBody(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	contentType := resp.Header.Get("Content-Type")
	if isEventStreamResponseContentType(contentType) {
		return ""
	}
	respBodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, debugResponseBodyLimit))
	if readErr != nil {
		return ""
	}
	resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(respBodyBytes), resp.Body))
	return string(respBodyBytes)
}

func requestModel(ctx context.Context) string {
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil {
		return selectMappedModel(rc.Info)
	}
	return ""
}

func requestAccount(ctx context.Context) string {
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil {
		return rc.Account.User
	}
	return ""
}

func requestFlags(ctx context.Context) (isVision, isAgent bool) {
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil {
		return rc.Info.IsVision, rc.Info.IsAgent
	}
	return
}

func addRequestID(ctx context.Context, headers map[string]string) {
	if headers == nil {
		return
	}
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil && rc.ID != "" {
		headers["X-Request-Id"] = rc.ID
	}
}

func addRequestMethod(resp *http.Response, headers map[string]string) {
	if headers == nil {
		return
	}
	if req := resp.Request; req != nil {
		headers["X-Method"] = req.Method
	}
}

func requestDuration(ctx context.Context) time.Duration {
	if rc, ok := middleware.RequestContextFrom(ctx); ok && rc != nil {
		return time.Since(rc.Start)
	}
	return 0
}

func isEventStreamResponseContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")
}

type streamLoggingReadCloser struct {
	body      io.ReadCloser
	logger    middleware.DebugLogger
	entry     monitor.DebugLogEntry
	buf       bytes.Buffer
	done      sseDoneDetector
	logged    bool
	sawEOF    bool
	streamErr error
}

var errStreamClosedBeforeEOF = errors.New("stream closed before EOF")

func newStreamLoggingReadCloser(
	body io.ReadCloser,
	logger middleware.DebugLogger,
	entry *monitor.DebugLogEntry,
) io.ReadCloser {
	var copied monitor.DebugLogEntry
	if entry != nil {
		copied = *entry
	}
	return &streamLoggingReadCloser{
		body:   body,
		logger: logger,
		entry:  copied,
	}
}

func (r *streamLoggingReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.capture(p[:n])
	}
	if err != nil {
		r.recordStreamEnd(err)
		r.logOnce()
	}
	if err == nil {
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	return n, fmt.Errorf("read stream body: %w", err)
}

func (r *streamLoggingReadCloser) Close() error {
	err := r.body.Close()
	r.done.Finalize()
	if !r.sawEOF && !r.done.Seen() && r.streamErr == nil {
		if err != nil {
			r.streamErr = err
		} else {
			r.streamErr = errStreamClosedBeforeEOF
		}
	}
	r.logOnce()
	if err != nil {
		return fmt.Errorf("close stream body: %w", err)
	}
	return nil
}

func (r *streamLoggingReadCloser) recordStreamEnd(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, io.EOF) {
		r.sawEOF = true
		r.done.Finalize()
		return
	}
	r.done.Finalize()
	if r.done.Seen() {
		return
	}
	if r.streamErr == nil {
		r.streamErr = err
	}
}

func (r *streamLoggingReadCloser) capture(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	r.done.Observe(chunk)
	_, _ = r.buf.Write(chunk)
}

func (r *streamLoggingReadCloser) logOnce() {
	if r.logged || r.logger == nil {
		return
	}
	r.logged = true
	r.entry.ResponseBody = r.buf.String()
	if reason := classifyStreamEndReason(r.streamErr); reason != "" {
		r.entry.Error = reason
	}
	if logErr := r.logger.Log(&r.entry); logErr != nil {
		slog.Error("debug log error", "err", logErr)
	}
}

func classifyStreamEndReason(err error) string {
	if err == nil || errors.Is(err, io.EOF) {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "stream canceled: context canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "stream canceled: context deadline exceeded"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "stream interrupted: unexpected EOF"
	case errors.Is(err, errStreamClosedBeforeEOF):
		return "stream canceled: downstream closed before EOF"
	default:
		return fmt.Sprintf("stream read error: %v", err)
	}
}
