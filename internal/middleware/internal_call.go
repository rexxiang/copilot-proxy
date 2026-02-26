package middleware

import "context"

type internalCallKey struct{}

// WithInternalCall marks request context as an internal in-process invocation.
func WithInternalCall(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, internalCallKey{}, true)
}

// IsInternalCall returns true when request originated from in-process invoker.
func IsInternalCall(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	flag, ok := ctx.Value(internalCallKey{}).(bool)
	return ok && flag
}
