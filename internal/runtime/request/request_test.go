package request

import (
	"context"
	"testing"
)

func TestParseRequestByPathWithOptions(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"ok"}]}`)
	info := ParseRequestByPathWithOptions("/v1/messages", body, ParseOptions{
		MessagesAgentDetectionRequestMode: true,
	})
	if info.Model != "claude-3-opus" {
		t.Fatalf("model = %q, want %q", info.Model, "claude-3-opus")
	}
	if !info.IsAgent {
		t.Fatalf("isAgent = %v, want true", info.IsAgent)
	}
}

func TestWithRequestContextRoundTrip(t *testing.T) {
	t.Parallel()

	in := &RequestContext{ID: "req-1"}
	ctx := WithRequestContext(context.Background(), in)
	got, ok := RequestContextFrom(ctx)
	if !ok {
		t.Fatal("expected request context in context")
	}
	if got == nil || got.ID != in.ID {
		t.Fatalf("request context mismatch: got %+v, want %+v", got, in)
	}
}
