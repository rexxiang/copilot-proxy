//go:build cgo
// +build cgo

package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"copilot-proxy/internal/core"
)

type smokeCallbackResult struct {
	payload string
	err     error
	id      uint64
}

func TestCopilotProxyCoreSmoke(t *testing.T) {
	handle := CopilotProxyCore_Create()
	if handle == nil {
		t.Fatal("CopilotProxyCore_Create returned nil")
	}
	defer CopilotProxyCore_Destroy(handle)

	proxy := (*copilotProxyCore)(handle)
	callbackCh := make(chan smokeCallbackResult, 1)
	proxy.SetCallback(func(payload string, err error, id uint64) {
		callbackCh <- smokeCallbackResult{payload: payload, err: err, id: id}
	})

	if status := CopilotProxyCore_Start(handle); status != 0 {
		t.Fatalf("start failed: %d", status)
	}
	defer CopilotProxyCore_Stop(handle)

	request := core.RequestInvocation{
		Method: http.MethodGet,
		Path:   "/test",
		Header: map[string]string{"x-test": "smoke"},
		Body:   []byte("payload"),
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	if err := proxy.Invoke(string(raw)); err != nil {
		t.Fatalf("invoke failed: %v", err)
	}

	select {
	case result := <-callbackCh:
		if result.err != nil {
			t.Fatalf("callback reported error: %v", result.err)
		}
		if result.payload == "" {
			t.Fatal("callback payload empty")
		}
		var response core.ResponsePayload
		if err := json.Unmarshal([]byte(result.payload), &response); err != nil {
			t.Fatalf("malformed response payload: %v", err)
		}
		if response.StatusCode != http.StatusNotImplemented {
			t.Fatalf("unexpected status: %d", response.StatusCode)
		}
		if result.id == 0 {
			t.Fatal("invalid invocation id")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback")
	}
}

func TestCopilotProxyCoreStartStopIdempotent(t *testing.T) {
	handle := CopilotProxyCore_Create()
	if handle == nil {
		t.Fatal("CopilotProxyCore_Create returned nil")
	}
	defer CopilotProxyCore_Destroy(handle)

	if status := CopilotProxyCore_Start(handle); status != 0 {
		t.Fatalf("first start failed: %d", status)
	}
	if status := CopilotProxyCore_Start(handle); status != 0 {
		t.Fatalf("second start failed: %d", status)
	}

	if status := CopilotProxyCore_Stop(handle); status != 0 {
		t.Fatalf("first stop failed: %d", status)
	}
	if status := CopilotProxyCore_Stop(handle); status != 0 {
		t.Fatalf("second stop failed: %d", status)
	}
}
