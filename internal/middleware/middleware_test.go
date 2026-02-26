package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPipelineOnionOrder(t *testing.T) {
	order := make([]string, 0, 6)
	m1 := MiddlewareFunc(func(_ *Context, next Next) (*http.Response, error) {
		order = append(order, "m1:req")
		resp, err := next()
		order = append(order, "m1:resp")
		return resp, err
	})
	m2 := MiddlewareFunc(func(_ *Context, next Next) (*http.Response, error) {
		order = append(order, "m2:req")
		resp, err := next()
		order = append(order, "m2:resp")
		return resp, err
	})
	m3 := MiddlewareFunc(func(_ *Context, next Next) (*http.Response, error) {
		order = append(order, "m3:req")
		resp, err := next()
		order = append(order, "m3:resp")
		return resp, err
	})

	base := RoundTripperFunc(func(*http.Request) (*http.Response, error) {
		order = append(order, "upstream")
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})

	pipeline := NewPipeline(base, m1, m2, m3)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/test", http.NoBody)
	ctx := &Context{Request: req}
	resp, err := pipeline.Do(ctx)
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	expected := []string{"m1:req", "m2:req", "m3:req", "upstream", "m3:resp", "m2:resp", "m1:resp"}
	if len(order) != len(expected) {
		t.Fatalf("unexpected order length: %d", len(order))
	}
	for i, want := range expected {
		if order[i] != want {
			t.Fatalf("order[%d] = %s, want %s", i, order[i], want)
		}
	}
}
