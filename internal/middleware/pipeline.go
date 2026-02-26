package middleware

import (
	"fmt"
	"net/http"
)

// Next represents the next middleware in the chain.
type Next func() (*http.Response, error)

// Middleware handles a request and can run logic before/after calling next.
type Middleware interface {
	Handle(ctx *Context, next Next) (*http.Response, error)
}

// MiddlewareFunc adapts a function to Middleware.
type MiddlewareFunc func(ctx *Context, next Next) (*http.Response, error)

func (f MiddlewareFunc) Handle(ctx *Context, next Next) (*http.Response, error) {
	return f(ctx, next)
}

// RoundTripperFunc adapts a function to http.RoundTripper.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Context holds request/response shared state.
type Context struct {
	Request *http.Request
}

// Pipeline executes middleware in an onion model around a base RoundTripper.
type Pipeline struct {
	base       http.RoundTripper
	middleware []Middleware
}

// NewPipeline constructs a pipeline with a base transport and middlewares.
func NewPipeline(base http.RoundTripper, middleware ...Middleware) *Pipeline {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Pipeline{base: base, middleware: middleware}
}

// Do executes the middleware chain and returns the response.
func (p *Pipeline) Do(ctx *Context) (*http.Response, error) {
	return p.call(0, ctx)
}

func (p *Pipeline) call(idx int, ctx *Context) (*http.Response, error) {
	if idx >= len(p.middleware) {
		resp, err := p.base.RoundTrip(ctx.Request)
		if err != nil {
			return nil, fmt.Errorf("round trip: %w", err)
		}
		return resp, nil
	}
	next := func() (*http.Response, error) {
		return p.call(idx+1, ctx)
	}
	resp, err := p.middleware[idx].Handle(ctx, next)
	if err != nil {
		return nil, fmt.Errorf("middleware: %w", err)
	}
	return resp, nil
}

// RoundTrip implements http.RoundTripper using the pipeline.
func (p *Pipeline) RoundTrip(req *http.Request) (*http.Response, error) {
	return p.Do(&Context{Request: req})
}
