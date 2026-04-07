package flow

import (
	"copilot-proxy/internal/runtime/reasoning"
	"net/http"
	"strings"

	requestctx "copilot-proxy/internal/runtime/request"
)

var allowedClientXHeaders = map[string]struct{}{
	"x-github-api-version": {},
	"x-interaction-type":   {},
	"x-interaction-id":     {},
}

func StripClientXHeaders(headers http.Header) {
	for key := range headers {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-") && !isAllowedClientXHeader(lower) {
			headers.Del(key)
		}
	}
}

func isAllowedClientXHeader(header string) bool {
	_, ok := allowedClientXHeaders[strings.ToLower(header)]
	return ok
}

func ApplyStaticHeaders(headers http.Header, values map[string]string, overwrite bool) {
	for key, value := range values {
		if overwrite || headers.Get(key) == "" {
			headers.Set(key, value)
		}
	}
}

func ApplyDynamicHeaders(headers http.Header, info requestctx.RequestInfo) {
	if info.IsAgent {
		headers.Set("X-Initiator", "agent")
	} else {
		headers.Set("X-Initiator", "user")
	}
	if info.IsVision {
		headers.Set("Copilot-Vision-Request", "true")
	}
}

func CloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}

func CloneReasoningPolicies(items []reasoning.Policy) []reasoning.Policy {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]reasoning.Policy, len(items))
	copy(cloned, items)
	return cloned
}
