package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/proxy"
	runtimeconfig "copilot-proxy/internal/runtime/config"
	execute "copilot-proxy/internal/runtime/execute"
	requestctx "copilot-proxy/internal/runtime/request"
	core "copilot-proxy/internal/runtime/types"
)

func (rt *Runtime) buildExecuteHandler(proxyHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		if err := r.Body.Close(); err != nil {
			http.Error(w, fmt.Sprintf("close request body: %v", err), http.StatusInternalServerError)
			return
		}

		req := execute.ExecuteRequest{
			Method:     r.Method,
			Path:       r.URL.RequestURI(),
			Headers:    headersFrom(r.Header),
			Body:       body,
			AccountRef: r.Header.Get("X-Copilot-Account"),
			ModelID:    requestctx.InferModelID(body),
		}

		var wroteResponse bool
		opts := execute.ExecuteOptions{
			Mode:              execute.StreamModeCallback,
			ResultCallback:    rt.buildResultCallback(w, &wroteResponse),
			TelemetryCallback: rt.buildTelemetryCallback(req),
		}

		if rt.executor == nil {
			http.Error(w, "executor is not configured", http.StatusBadGateway)
			return
		}
		if err := rt.executor.Execute(r.Context(), core.RequestInvocation{
			Method: req.Method,
			Path:   req.Path,
			Body:   req.Body,
			Header: req.Headers,
		}, opts); err != nil && !errors.Is(err, context.Canceled) && !wroteResponse {
			if errors.Is(err, runtimeconfig.ErrNoAccountsConfigured) ||
				errors.Is(err, runtimeconfig.ErrDefaultAccountNotFound) ||
				errors.Is(err, runtimeconfig.ErrAccountNotFound) {
				middleware.WriteError(w, http.StatusUnauthorized, "no account available")
				return
			}
			http.Error(w, fmt.Sprintf("execute: %v", err), http.StatusBadGateway)
		}
	})
}

func (rt *Runtime) doUpstream(proxyHandler http.Handler) func(context.Context, *http.Request) (*http.Response, error) {
	invoker := proxy.NewInProcessInvoker(proxyHandler)
	return func(ctx context.Context, upstreamReq *http.Request) (*http.Response, error) {
		if upstreamReq == nil {
			return nil, errors.New("upstream request is required")
		}
		if ctx != nil {
			upstreamReq = upstreamReq.Clone(ctx)
		}
		return invoker.Do(upstreamReq)
	}
}

func (rt *Runtime) buildResultCallback(w http.ResponseWriter, wroteResponse *bool) func(execute.ExecuteResult) {
	flusher, _ := w.(http.Flusher)
	state := struct {
		headerWritten bool
	}{}
	return func(result execute.ExecuteResult) {
		if wroteResponse != nil {
			*wroteResponse = true
		}
		if result.Error != "" {
			if !state.headerWritten {
				w.WriteHeader(http.StatusBadGateway)
				state.headerWritten = true
			}
			if _, err := w.Write([]byte(result.Error)); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		if !state.headerWritten {
			status := result.StatusCode
			if status == 0 {
				status = http.StatusOK
			}
			writeHeaders(w.Header(), result.Headers)
			w.WriteHeader(status)
			state.headerWritten = true
		}

		if len(result.Body) > 0 {
			if _, err := w.Write(result.Body); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func (rt *Runtime) buildTelemetryCallback(req execute.ExecuteRequest) func(execute.TelemetryEvent) {
	return func(event execute.TelemetryEvent) {
		if rt.Observability == nil {
			return
		}
		path := event.Path
		if path == "" {
			path = req.Path
		}
		modelID := event.Model
		if modelID == "" {
			modelID = req.ModelID
		}
		payload := map[string]any{
			"path":        path,
			"status_code": event.StatusCode,
			"model":       modelID,
		}
		if event.Error != "" {
			payload["error"] = event.Error
		}
		rt.Observability.AddEvent(core.Event{
			Timestamp: event.Timestamp,
			Type:      "execute." + event.Type,
			Message:   "execute lifecycle event",
			Payload:   payload,
		})
	}
}

func headersFrom(src http.Header) map[string]string {
	headers := make(map[string]string, len(src))
	for k, values := range src {
		if len(values) > 0 {
			headers[k] = values[0]
		}
	}
	return headers
}

func writeHeaders(dst http.Header, values map[string]string) {
	for k, v := range values {
		dst.Set(k, v)
	}
}
