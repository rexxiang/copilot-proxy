package execute

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	StreamModeBuffered StreamMode = iota
	StreamModeCallback
)

type StreamMode int

type ExecuteRequest struct {
	Method     string
	Path       string
	Headers    map[string]string
	Body       []byte
	AccountRef string
	ModelID    string
}

type ModelCapabilities struct {
	ID        string
	Endpoints []string
}

type ExecuteDeps struct {
	DoUpstream   func(ctx context.Context, req *http.Request) (*http.Response, error)
	ResolveToken func(ctx context.Context, accountRef string) (string, error)
	ResolveModel func(ctx context.Context, modelID string) (ModelCapabilities, error)
}

type ExecuteOptions struct {
	Mode              StreamMode
	ResultCallback    func(result ExecuteResult)
	TelemetryCallback func(event TelemetryEvent)
}

type ExecuteResult struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
	Telemetry  TelemetrySummary
	Error      string
}

type TelemetrySummary struct {
	Start      time.Time
	FirstByte  time.Time
	End        time.Time
	Duration   time.Duration
	StatusCode int
	Path       string
	Model      string
	IsStream   bool
}

type TelemetryEvent struct {
	Type       string    `json:"type"`
	Timestamp  time.Time `json:"timestamp"`
	Path       string    `json:"path,omitempty"`
	StatusCode int       `json:"status_code,omitempty"`
	Model      string    `json:"model,omitempty"`
	Error      string    `json:"error,omitempty"`
}

var (
	ErrMissingDoUpstream     = errors.New("execute: DoUpstream is required")
	ErrMissingResultCallback = errors.New("execute: ResultCallback is required")
)

func Execute(ctx context.Context, request ExecuteRequest, deps ExecuteDeps, opts ExecuteOptions) error {
	if deps.DoUpstream == nil {
		return ErrMissingDoUpstream
	}
	if opts.ResultCallback == nil {
		return ErrMissingResultCallback
	}
	if opts.Mode != StreamModeCallback && opts.Mode != StreamModeBuffered {
		opts.Mode = StreamModeBuffered
	}

	modelCap := ModelCapabilities{}
	if request.ModelID != "" && deps.ResolveModel != nil {
		if cap, err := deps.ResolveModel(ctx, request.ModelID); err == nil {
			modelCap = cap
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, request.Method, request.Path, bytes.NewReader(request.Body))
	if err != nil {
		return err
	}

	for k, v := range request.Headers {
		httpReq.Header.Set(k, v)
	}

	if deps.ResolveToken != nil {
		if token, err := deps.ResolveToken(ctx, request.AccountRef); err == nil {
			if token != "" {
				httpReq.Header.Set("Authorization", "Bearer "+token)
			}
		} else {
			return fmt.Errorf("resolve token: %w", err)
		}
	}

	telemetry := TelemetrySummary{Start: time.Now(), Path: request.Path, Model: modelCap.ID}
	emitTelemetry(opts.TelemetryCallback, TelemetryEvent{Type: "start", Timestamp: telemetry.Start, Path: request.Path, Model: telemetry.Model})

	resp, err := deps.DoUpstream(ctx, httpReq)
	if err != nil {
		emitTelemetry(opts.TelemetryCallback, telemetryErrorEvent(telemetry, err))
		opts.ResultCallback(ExecuteResult{Telemetry: telemetry, Error: err.Error()})
		return err
	}
	defer resp.Body.Close()

	telemetry.StatusCode = resp.StatusCode
	telemetry.IsStream = isSSEContent(resp.Header.Get("Content-Type"))

	if opts.Mode == StreamModeCallback {
		return handleStreaming(resp, headersMap(resp.Header), telemetry, opts)
	}
	return handleBuffered(resp, headersMap(resp.Header), telemetry, opts)
}

func handleBuffered(resp *http.Response, headers map[string]string, telemetry TelemetrySummary, opts ExecuteOptions) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		emitTelemetry(opts.TelemetryCallback, telemetryErrorEvent(telemetry, err))
		opts.ResultCallback(ExecuteResult{Telemetry: telemetry, Error: err.Error()})
		return err
	}
	telemetry.FirstByte = time.Now()
	telemetry.Duration = telemetry.FirstByte.Sub(telemetry.Start)
	telemetry.End = time.Now()
	telemetry.Duration = telemetry.End.Sub(telemetry.Start)
	emitTelemetry(opts.TelemetryCallback, TelemetryEvent{Type: "first_byte", Timestamp: telemetry.FirstByte, Path: telemetry.Path, StatusCode: telemetry.StatusCode, Model: telemetry.Model})
	emitTelemetry(opts.TelemetryCallback, TelemetryEvent{Type: "end", Timestamp: telemetry.End, Path: telemetry.Path, StatusCode: telemetry.StatusCode, Model: telemetry.Model})
	opts.ResultCallback(ExecuteResult{StatusCode: telemetry.StatusCode, Headers: headers, Body: body, Telemetry: telemetry})
	return nil
}

func handleStreaming(resp *http.Response, headers map[string]string, telemetry TelemetrySummary, opts ExecuteOptions) error {
	buf := make([]byte, 4*1024)
	firstChunk, err := readChunk(resp.Body, buf)
	if err != nil && err != io.EOF {
		emitTelemetry(opts.TelemetryCallback, telemetryErrorEvent(telemetry, err))
		opts.ResultCallback(ExecuteResult{Telemetry: telemetry, Error: err.Error()})
		return err
	}
	telemetry.FirstByte = time.Now()
	emitTelemetry(opts.TelemetryCallback, TelemetryEvent{Type: "first_byte", Timestamp: telemetry.FirstByte, Path: telemetry.Path, StatusCode: telemetry.StatusCode, Model: telemetry.Model})
	opts.ResultCallback(ExecuteResult{StatusCode: telemetry.StatusCode, Headers: headers, Body: firstChunk, Telemetry: telemetry})

	if err == io.EOF {
		telemetry.End = time.Now()
		telemetry.Duration = telemetry.End.Sub(telemetry.Start)
		emitTelemetry(opts.TelemetryCallback, TelemetryEvent{Type: "end", Timestamp: telemetry.End, Path: telemetry.Path, StatusCode: telemetry.StatusCode, Model: telemetry.Model})
		return nil
	}

	for {
		chunk, err := readChunk(resp.Body, buf)
		if len(chunk) > 0 {
			opts.ResultCallback(ExecuteResult{Body: chunk, Telemetry: telemetry})
		}
		if err == io.EOF {
			telemetry.End = time.Now()
			telemetry.Duration = telemetry.End.Sub(telemetry.Start)
			emitTelemetry(opts.TelemetryCallback, TelemetryEvent{Type: "end", Timestamp: telemetry.End, Path: telemetry.Path, StatusCode: telemetry.StatusCode, Model: telemetry.Model})
			return nil
		}
		if err != nil {
			emitTelemetry(opts.TelemetryCallback, telemetryErrorEvent(telemetry, err))
			opts.ResultCallback(ExecuteResult{Telemetry: telemetry, Error: err.Error()})
			return err
		}
	}
}

func readChunk(reader io.Reader, buf []byte) ([]byte, error) {
	n, err := reader.Read(buf)
	if n == 0 {
		return nil, err
	}
	chunk := make([]byte, n)
	copy(chunk, buf[:n])
	return chunk, err
}

func headersMap(h http.Header) map[string]string {
	if h == nil {
		return nil
	}
	result := make(map[string]string, len(h))
	for k := range h {
		result[k] = h.Get(k)
	}
	return result
}

func emitTelemetry(cb func(TelemetryEvent), event TelemetryEvent) {
	if cb == nil {
		return
	}
	cb(event)
}

func telemetryErrorEvent(telemetry TelemetrySummary, err error) TelemetryEvent {
	return TelemetryEvent{Type: "error", Timestamp: time.Now(), Path: telemetry.Path, StatusCode: telemetry.StatusCode, Model: telemetry.Model, Error: err.Error()}
}

func isSSEContent(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")
}
