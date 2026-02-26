package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"copilot-proxy/internal/middleware"
)

var (
	errInProcessHandlerRequired = errors.New("in-process handler is required")
	errInProcessRequestRequired = errors.New("in-process request is required")
	errInProcessInvocationPanic = errors.New("in-process invocation panic")
)

// Invoker performs HTTP requests through a proxy handler in-process.
type Invoker interface {
	Do(req *http.Request) (*http.Response, error)
}

// InProcessInvoker routes requests directly through an http.Handler without
// exposing a local listening port.
type InProcessInvoker struct {
	handler http.Handler
}

// NewInProcessInvoker creates an in-process invoker for the provided handler.
func NewInProcessInvoker(handler http.Handler) *InProcessInvoker {
	return &InProcessInvoker{handler: handler}
}

// Do executes a request by invoking the underlying handler directly.
func (i *InProcessInvoker) Do(req *http.Request) (resp *http.Response, err error) {
	if i == nil || i.handler == nil {
		return nil, errInProcessHandlerRequired
	}
	if req == nil {
		return nil, errInProcessRequestRequired
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", errInProcessInvocationPanic, recovered)
			resp = nil
		}
	}()

	recorder := httptest.NewRecorder()
	internalReq := req.Clone(middleware.WithInternalCall(req.Context()))
	i.handler.ServeHTTP(recorder, internalReq)
	return recorder.Result(), nil
}
