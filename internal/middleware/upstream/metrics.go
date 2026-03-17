package upstream

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/runtime/config"
	core "copilot-proxy/internal/runtime/types"
)

const (
	localResponsesPath = config.ResponsesPath
)

// ObservabilityMiddleware reports lifecycle events to an observability sink.
type ObservabilityMiddleware struct {
	sink middleware.ObservabilitySink
}

// NewObservabilityMiddleware builds the middleware.
func NewObservabilityMiddleware(sink middleware.ObservabilitySink) ObservabilityMiddleware {
	return ObservabilityMiddleware{sink: sink}
}

func (m ObservabilityMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	req := ctx.Request
	if req == nil {
		return next()
	}

	rc := ensureRequestContext(req)
	rc.Headers = captureHeaders(req)
	ctx.Request = withRequestContext(req, rc)

	if m.sink != nil && rc != nil {
		record := &core.RequestRecord{
			Timestamp:    rc.Start,
			Method:       req.Method,
			Path:         fallbackLocalPath(rc),
			Model:        selectMappedModel(rc.Info),
			Account:      rc.Account.User,
			RequestID:    rc.ID,
			IsVision:     rc.Info.IsVision,
			IsAgent:      rc.Info.IsAgent,
			UpstreamPath: fallbackUpstreamPath(req, rc),
		}
		m.sink.RecordStart(record)
		m.addEvent("request.start", "request started", map[string]any{
			"request_id": rc.ID,
			"path":       record.Path,
			"model":      record.Model,
			"account":    record.Account,
		})
	}

	resp, err := next()
	if m.sink == nil {
		return resp, err
	}

	if rc == nil || rc.ID == "" {
		if resp == nil && err != nil && rc != nil {
			m.recordCompletion(rc, statusCodeFromRequestError(err), time.Since(rc.Start), fallbackUpstreamPath(req, rc))
		}
		return resp, err
	}

	upstreamPath := responseUpstreamPath(resp, rc)
	if isEventStreamResponse(resp) && resp.Body != nil {
		m.recordFirstResponse(rc, resp.StatusCode, time.Since(rc.Start), upstreamPath, true)
		resp.Body = newStreamObservabilityReadCloser(resp.Body, req.Context(), resp.StatusCode, func(statusCode int) {
			m.recordCompletion(rc, statusCode, time.Since(rc.Start), upstreamPath)
		})
		return resp, err
	}

	if resp == nil {
		m.recordCompletion(rc, statusCodeFromRequestError(err), time.Since(rc.Start), upstreamPath)
		return resp, err
	}

	m.recordCompletion(rc, resp.StatusCode, time.Since(rc.Start), upstreamPath)
	return resp, err
}

func (m ObservabilityMiddleware) recordFirstResponse(rc *middleware.RequestContext, statusCode int, duration time.Duration, upstreamPath string, isStream bool) {
	if rc == nil || rc.ID == "" || m.sink == nil {
		return
	}
	m.sink.RecordFirstResponse(rc.ID, statusCode, duration, upstreamPath, isStream)
	m.addEvent("request.first_response", "first response received", map[string]any{
		"request_id":    rc.ID,
		"status_code":   statusCode,
		"upstream_path": upstreamPath,
		"is_stream":     isStream,
		"delay_ms":      duration.Milliseconds(),
	})
}

func (m ObservabilityMiddleware) recordCompletion(rc *middleware.RequestContext, statusCode int, duration time.Duration, upstreamPath string) {
	if rc == nil || rc.ID == "" || m.sink == nil {
		return
	}
	m.sink.RecordComplete(rc.ID, statusCode, duration, upstreamPath)
	m.addEvent("request.complete", "request finished", map[string]any{
		"request_id":    rc.ID,
		"status_code":   statusCode,
		"upstream_path": upstreamPath,
		"duration_ms":   duration.Milliseconds(),
	})
}

func (m ObservabilityMiddleware) addEvent(eventType, message string, payload map[string]any) {
	if m.sink == nil {
		return
	}
	m.sink.AddEvent(core.Event{
		Timestamp: time.Now(),
		Type:      eventType,
		Message:   message,
		Payload:   payload,
	})
}

func captureHeaders(req *http.Request) map[string]string {
	if req == nil {
		return nil
	}
	headers := make(map[string]string)
	for k, v := range req.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return headers
}

func responseUpstreamPath(resp *http.Response, rc *middleware.RequestContext) string {
	if resp == nil {
		return fallbackUpstreamPath(nil, rc)
	}
	return fallbackUpstreamPath(resp.Request, rc)
}

func fallbackUpstreamPath(req *http.Request, rc *middleware.RequestContext) string {
	if rc != nil && rc.TargetUpstreamPath != "" {
		return rc.TargetUpstreamPath
	}
	if req != nil && req.URL != nil && req.URL.Path != "" {
		return req.URL.Path
	}
	if rc != nil {
		return rc.LocalPath
	}
	return ""
}

func fallbackLocalPath(rc *middleware.RequestContext) string {
	if rc == nil {
		return ""
	}
	if rc.SourceLocalPath != "" {
		return rc.SourceLocalPath
	}
	return rc.LocalPath
}

type streamObservabilityReadCloser struct {
	body               io.ReadCloser
	requestCtx         context.Context
	upstreamStatusCode int
	recordComplete     func(statusCode int)
	doneDetector       sseDoneDetector

	once      sync.Once
	mu        sync.Mutex
	sawEOF    bool
	streamErr error
}

var errStreamClosedBeforeEOF = errors.New("stream closed before EOF")

func newStreamObservabilityReadCloser(body io.ReadCloser, ctx context.Context, upstreamStatusCode int, recordComplete func(int)) io.ReadCloser {
	return &streamObservabilityReadCloser{
		body:               body,
		requestCtx:         ctx,
		upstreamStatusCode: upstreamStatusCode,
		recordComplete:     recordComplete,
	}
}

func (r *streamObservabilityReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.doneDetector.Observe(p[:n])
		r.mu.Unlock()
	}
	if err == nil {
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		r.mu.Lock()
		r.sawEOF = true
		r.doneDetector.Finalize()
		r.mu.Unlock()
		r.completeOnce(r.upstreamStatusCode)
		return n, io.EOF
	}

	r.mu.Lock()
	r.doneDetector.Finalize()
	if r.streamErr == nil {
		r.streamErr = err
	}
	doneSeen := r.doneDetector.Seen()
	r.mu.Unlock()

	statusCode := statusCodeFromRequestError(err)
	if doneSeen {
		statusCode = r.upstreamStatusCode
	}
	r.completeOnce(statusCode)
	return n, err
}

func (r *streamObservabilityReadCloser) Close() error {
	err := r.body.Close()
	if statusCode, shouldComplete := r.statusOnClose(err); shouldComplete {
		r.completeOnce(statusCode)
	}
	return err
}

func (r *streamObservabilityReadCloser) statusOnClose(closeErr error) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sawEOF {
		return 0, false
	}
	r.doneDetector.Finalize()
	if r.doneDetector.Seen() {
		return r.upstreamStatusCode, true
	}
	if r.streamErr != nil {
		return statusCodeFromRequestError(r.streamErr), true
	}
	if closeErr != nil {
		return statusCodeFromRequestError(closeErr), true
	}
	if r.requestCtx != nil {
		if ctxErr := r.requestCtx.Err(); ctxErr != nil {
			return statusCodeFromRequestError(ctxErr), true
		}
	}

	r.streamErr = errStreamClosedBeforeEOF
	return 499, true
}

func (r *streamObservabilityReadCloser) completeOnce(statusCode int) {
	r.once.Do(func() {
		if r.recordComplete != nil {
			r.recordComplete(statusCode)
		}
	})
}

func selectMappedModel(info middleware.RequestInfo) string {
	if info.MappedModel != "" {
		return info.MappedModel
	}
	return info.Model
}

func statusCodeFromRequestError(err error) int {
	if errors.Is(err, context.Canceled) {
		return 499
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return http.StatusGatewayTimeout
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func isEventStreamResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return isEventStreamResponseContentType(resp.Header.Get("Content-Type"))
}

func isEventStreamResponseContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")
}

func isTimeoutRequestError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
