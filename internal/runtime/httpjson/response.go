package httpjson

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// ErrorResponse builds a JSON error response with stable payload shape.
func ErrorResponse(req *http.Request, status int, message string) *http.Response {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		payload = []byte(`{"error":"internal error"}`)
	}

	resp := new(http.Response)
	resp.StatusCode = status
	resp.Header = http.Header{"Content-Type": []string{"application/json"}}
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	resp.ContentLength = int64(len(payload))
	resp.Request = req
	return resp
}
