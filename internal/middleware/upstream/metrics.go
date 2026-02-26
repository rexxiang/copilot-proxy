package upstream

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/monitor"
)

func selectMappedModel(info middleware.RequestInfo) string {
	if info.MappedModel != "" {
		return info.MappedModel
	}
	return info.Model
}

// MetricsMiddleware records request completion.
type MetricsMiddleware struct {
	metrics middleware.MetricsRecorder
}

type firstResponseRecorder interface {
	RecordFirstResponse(requestID string, statusCode int, duration time.Duration, upstreamPath string, isStream bool)
}

// NewMetrics builds a metrics middleware.
func NewMetrics(metrics middleware.MetricsRecorder) MetricsMiddleware {
	return MetricsMiddleware{metrics: metrics}
}

func (m MetricsMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	reqCtx := ctx.Request.Context()
	if m.metrics != nil {
		if rc, ok := middleware.RequestContextFrom(reqCtx); ok && rc != nil {
			upstreamPath := fallbackUpstreamPath(ctx.Request, rc)
			localPath := fallbackLocalPath(rc)
			startRecord := &monitor.RequestRecord{
				Timestamp:    rc.Start,
				Method:       ctx.Request.Method,
				Path:         localPath,
				Model:        selectMappedModel(rc.Info),
				Account:      rc.Account.User,
				RequestID:    rc.ID,
				IsVision:     rc.Info.IsVision,
				IsAgent:      rc.Info.IsAgent,
				UpstreamPath: upstreamPath,
			}
			m.metrics.RecordStart(startRecord)
		}
	}
	resp, err := next()
	if m.metrics == nil {
		return resp, err
	}
	rc, ok := middleware.RequestContextFrom(reqCtx)
	if !ok || rc == nil || rc.ID == "" {
		return resp, err
	}
	if resp == nil {
		if err != nil {
			upstreamPath := fallbackUpstreamPath(ctx.Request, rc)
			statusCode := statusCodeFromRequestError(err)
			m.metrics.RecordComplete(rc.ID, statusCode, time.Since(rc.Start), upstreamPath)
		}
		return resp, err
	}
	upstreamPath := responseUpstreamPath(resp, rc)
	if isEventStreamResponse(resp) && resp.Body != nil {
		if recorder, ok := m.metrics.(firstResponseRecorder); ok {
			recorder.RecordFirstResponse(rc.ID, resp.StatusCode, time.Since(rc.Start), upstreamPath, true)
		}
		resp.Body = newStreamMetricsReadCloser(
			resp.Body,
			reqCtx,
			resp.StatusCode,
			func(statusCode int) {
				m.metrics.RecordComplete(rc.ID, statusCode, time.Since(rc.Start), upstreamPath)
			},
		)
		return resp, err
	}

	m.metrics.RecordComplete(rc.ID, resp.StatusCode, time.Since(rc.Start), upstreamPath)
	return resp, err
}

func isTimeoutRequestError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func statusCodeFromRequestError(err error) int {
	if errors.Is(err, context.Canceled) {
		return monitor.StatusClientCanceled
	}
	if isTimeoutRequestError(err) {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func responseUpstreamPath(resp *http.Response, rc *middleware.RequestContext) string {
	if resp == nil {
		return fallbackUpstreamPath(nil, rc)
	}
	return fallbackUpstreamPath(resp.Request, rc)
}

func fallbackUpstreamPath(req *http.Request, rc *middleware.RequestContext) string {
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

func isEventStreamResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return isEventStreamResponseContentType(resp.Header.Get("Content-Type"))
}

type streamMetricsReadCloser struct {
	body               io.ReadCloser
	requestCtx         context.Context
	upstreamStatusCode int
	recordComplete     func(statusCode int)

	once      sync.Once
	mu        sync.Mutex
	sawEOF    bool
	streamErr error
}

var errMetricsStreamClosedBeforeEOF = errors.New("stream closed before EOF")

func newStreamMetricsReadCloser(
	body io.ReadCloser,
	requestCtx context.Context,
	upstreamStatusCode int,
	recordComplete func(statusCode int),
) io.ReadCloser {
	return &streamMetricsReadCloser{
		body:               body,
		requestCtx:         requestCtx,
		upstreamStatusCode: upstreamStatusCode,
		recordComplete:     recordComplete,
	}
}

func (r *streamMetricsReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if err == nil {
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		r.mu.Lock()
		r.sawEOF = true
		r.mu.Unlock()
		r.completeOnce(r.upstreamStatusCode)
		return n, io.EOF
	}

	r.mu.Lock()
	if r.streamErr == nil {
		r.streamErr = err
	}
	r.mu.Unlock()

	r.completeOnce(statusCodeFromRequestError(err))
	return n, err
}

func (r *streamMetricsReadCloser) Close() error {
	err := r.body.Close()
	if statusCode, shouldComplete := r.statusOnClose(err); shouldComplete {
		r.completeOnce(statusCode)
	}
	return err
}

func (r *streamMetricsReadCloser) statusOnClose(closeErr error) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sawEOF {
		return 0, false
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

	r.streamErr = errMetricsStreamClosedBeforeEOF
	return monitor.StatusClientCanceled, true
}

func (r *streamMetricsReadCloser) completeOnce(statusCode int) {
	r.once.Do(func() {
		if r.recordComplete != nil {
			r.recordComplete(statusCode)
		}
	})
}
