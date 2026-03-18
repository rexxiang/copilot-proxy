package upstream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"copilot-proxy/internal/middleware"
	requestctx "copilot-proxy/internal/runtime/request"
	core "copilot-proxy/internal/runtime/types"
)

type eventSink struct {
	events []core.Event
}

func (s *eventSink) RecordStart(_ *core.RequestRecord)                            {}
func (s *eventSink) RecordFirstResponse(string, int, time.Duration, string, bool) {}
func (s *eventSink) RecordComplete(string, int, time.Duration, string)            {}
func (s *eventSink) AddEvent(event core.Event) {
	s.events = append(s.events, event)
}
func (s *eventSink) Snapshot() core.Snapshot {
	return core.Snapshot{}
}

func TestObservabilityMiddlewareEmitsEvents(t *testing.T) {
	sink := &eventSink{}
	mw := NewObservabilityMiddleware(sink)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	rc := &requestctx.RequestContext{
		ID:              "req-events",
		LocalPath:       "/v1/responses",
		SourceLocalPath: "/v1/responses",
		AccountRef:      "user1",
		Start:           time.Now().Add(-time.Millisecond),
	}
	req = withRequestContext(req, rc)
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: hi\n\ndata: bye\n\n")),
			Request:    req,
		}, nil
	})
	if err != nil {
		t.Fatalf("observability handle: %v", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	if len(body) == 0 {
		t.Fatalf("expected non-empty body")
	}

	startIdx := -1
	firstIdx := -1
	completeIdx := -1
	for i, ev := range sink.events {
		switch ev.Type {
		case "request.start":
			startIdx = i
		case "request.first_response":
			firstIdx = i
		case "request.complete":
			completeIdx = i
		}
	}

	if startIdx < 0 || firstIdx < 0 || completeIdx < 0 {
		t.Fatalf("missing expected events: %v", sink.events)
	}
	if !(startIdx < firstIdx && firstIdx < completeIdx) {
		t.Fatalf("event order invalid: %v", sink.events)
	}

	if payloadID, _ := sink.events[firstIdx].Payload["request_id"]; payloadID != rc.ID {
		t.Fatalf("first response payload missing request_id, got %v", sink.events[firstIdx].Payload)
	}
	if status, ok := sink.events[firstIdx].Payload["status_code"]; !ok || status != http.StatusOK {
		t.Fatalf("expected status_code 200 in first response payload, got %v", sink.events[firstIdx].Payload)
	}
}

func TestObservabilityMiddlewareCompletesOnCanceledStream(t *testing.T) {
	sink := &eventSink{}
	mw := NewObservabilityMiddleware(sink)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	rc := &requestctx.RequestContext{
		ID:              "req-complete",
		LocalPath:       "/v1/responses",
		SourceLocalPath: "/v1/responses",
		AccountRef:      "user1",
		Start:           time.Now().Add(-time.Millisecond),
	}
	req = withRequestContext(req, rc)
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &canceledAfterFirstRead{
				chunk: []byte("data: cancel\n\n"),
			},
			Request: req,
		}, nil
	})
	if err != nil {
		t.Fatalf("observability handle: %v", err)
	}

	buf := make([]byte, 32)
	if _, readErr := resp.Body.Read(buf); readErr != nil {
		t.Fatalf("expected first read to succeed, got %v", readErr)
	}
	if _, readErr := resp.Body.Read(buf); !errors.Is(readErr, context.Canceled) {
		t.Fatalf("expected context.Canceled on second read, got %v", readErr)
	}
	_ = resp.Body.Close()

	firstIdx := -1
	completeIdx := -1
	var completeStatus any
	for i, ev := range sink.events {
		switch ev.Type {
		case "request.first_response":
			firstIdx = i
		case "request.complete":
			completeIdx = i
			completeStatus = ev.Payload["status_code"]
		}
	}

	if firstIdx < 0 || completeIdx < 0 {
		t.Fatalf("missing expected events: %v", sink.events)
	}
	if firstIdx >= completeIdx {
		t.Fatalf("event order invalid: %v", sink.events)
	}
	if status, ok := completeStatus.(int); !ok || status != core.StatusClientCanceled {
		t.Fatalf("expected completion status %d, got %v", core.StatusClientCanceled, completeStatus)
	}
}

func TestObservabilityMiddlewareCompletesOnRequestError(t *testing.T) {
	sink := &eventSink{}
	mw := NewObservabilityMiddleware(sink)
	errBoom := errors.New("boom")
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/responses", bytes.NewBufferString(`{}`))
	rc := &requestctx.RequestContext{
		ID:              "req-error",
		LocalPath:       "/v1/responses",
		SourceLocalPath: "/v1/responses",
		AccountRef:      "user1",
		Start:           time.Now().Add(-10 * time.Millisecond),
		Info: requestctx.RequestInfo{
			Model: "gpt-4o",
		},
	}
	req = withRequestContext(req, rc)
	ctx := &middleware.Context{Request: req}

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		return nil, errBoom
	})
	if resp != nil {
		closeResponse(resp)
	}
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected error propagation, got %v", err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(sink.events))
	}
	if sink.events[0].Type != "request.start" {
		t.Fatalf("expected first event request.start, got %s", sink.events[0].Type)
	}
	if sink.events[1].Type != "request.complete" {
		t.Fatalf("expected second event request.complete, got %s", sink.events[1].Type)
	}
	completePayload := sink.events[1].Payload
	if status, ok := completePayload["status_code"].(int); !ok || status != http.StatusBadGateway {
		t.Fatalf("expected status_code %d, got %v", http.StatusBadGateway, completePayload["status_code"])
	}
	if upstream, ok := completePayload["upstream_path"].(string); !ok || upstream != "/v1/responses" {
		t.Fatalf("expected upstream_path /v1/responses, got %v", completePayload["upstream_path"])
	}
}
