package transform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	protocolpaths "copilot-proxy/internal/runtime/protocol/paths"
	requestctx "copilot-proxy/internal/runtime/request"
)

// EndpointCodec provides message-protocol adapters implemented by the upstream package.
type EndpointCodec struct {
	MessagesToChatRequest       func([]byte) ([]byte, bool)
	ChatToMessagesResponse      func([]byte) ([]byte, bool)
	ChatSSEToMessages           func(io.ReadCloser) io.ReadCloser
	MessagesToResponsesRequest  func([]byte) ([]byte, bool)
	ResponsesToMessagesResponse func([]byte) ([]byte, bool)
	ResponsesSSEToMessages      func(io.ReadCloser) io.ReadCloser
}

const (
	normalizedEffortLow    = "low"
	normalizedEffortMedium = "medium"
	normalizedEffortHigh   = "high"
)

// ApplyEndpointTransform rewrites request/response payloads across endpoint protocols.
func ApplyEndpointTransform(
	req *http.Request,
	rc *requestctx.RequestContext,
	codec EndpointCodec,
	forward func(*http.Request) (*http.Response, error),
) (*http.Response, error) {
	if req == nil {
		return forward(req)
	}
	if req.Method != http.MethodPost {
		return forward(req)
	}
	if rc == nil {
		rc = new(requestctx.RequestContext)
	}

	sourceLocal := rc.SourceLocalPath
	if sourceLocal == "" {
		if rc.LocalPath != "" {
			sourceLocal = rc.LocalPath
		} else {
			sourceLocal = req.URL.Path
		}
		rc.SourceLocalPath = sourceLocal
	}

	sourceUpstream, ok := LocalToUpstream(sourceLocal)
	if !ok {
		return forward(req)
	}
	targetUpstream := NormalizeUpstreamPath(rc.TargetUpstreamPath)
	if targetUpstream == "" || targetUpstream == sourceUpstream {
		return forward(req)
	}

	originalBody, ok := readRequestBody(req)
	if !ok {
		return jsonErrorResponse(req, http.StatusBadGateway, "failed to convert request for target endpoint"), nil
	}
	convertedReqBody, ok := convertRequestAcrossEndpoints(sourceLocal, targetUpstream, originalBody, codec)
	if !ok {
		return jsonErrorResponse(req, http.StatusBadGateway, "failed to convert request for target endpoint"), nil
	}
	applyTransformedRequestBody(req, rc, convertedReqBody)

	resp, err := forward(req)
	if err != nil || resp == nil {
		return resp, err
	}

	convertedResp, ok := convertResponseAcrossEndpoints(sourceLocal, targetUpstream, resp, codec)
	if !ok {
		return jsonErrorResponse(req, http.StatusBadGateway, "failed to convert upstream response"), nil
	}
	return convertedResp, nil
}

func applyTransformedRequestBody(req *http.Request, rc *requestctx.RequestContext, body []byte) {
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	if rc != nil {
		rc.Body = body
	}
}

func readRequestBody(req *http.Request) ([]byte, bool) {
	if req == nil || req.Body == nil {
		return nil, false
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, false
	}
	return body, true
}

func convertRequestAcrossEndpoints(sourceLocal, targetUpstream string, body []byte, codec EndpointCodec) ([]byte, bool) {
	if len(body) == 0 {
		return body, true
	}

	if NormalizeLocalPath(sourceLocal) == protocolpaths.MessagesPath {
		switch targetUpstream {
		case protocolpaths.UpstreamChatCompletionsPath:
			return codec.messagesToChatRequest(body)
		case protocolpaths.UpstreamResponsesPath:
			return codec.messagesToResponsesRequest(body)
		case protocolpaths.UpstreamMessagesPath:
			return body, true
		}
	}

	return nil, false
}

func convertResponseAcrossEndpoints(sourceLocal, targetUpstream string, resp *http.Response, codec EndpointCodec) (*http.Response, bool) {
	if resp == nil {
		return nil, false
	}

	// Pass through non-2xx responses (error responses) unchanged —
	// upstream error bodies have their own format and should not be converted.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, true
	}

	if isEventStreamResponse(resp) {
		return convertStreamingResponse(sourceLocal, targetUpstream, resp, codec)
	}
	return convertJSONResponse(sourceLocal, targetUpstream, resp, codec)
}

func convertJSONResponse(sourceLocal, targetUpstream string, resp *http.Response, codec EndpointCodec) (*http.Response, bool) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, false
	}
	if len(bodyBytes) == 0 {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		resp.ContentLength = 0
		resp.Header.Set("Content-Length", "0")
		return resp, true
	}

	var converted []byte
	var ok bool

	if NormalizeLocalPath(sourceLocal) == protocolpaths.MessagesPath {
		switch targetUpstream {
		case protocolpaths.UpstreamChatCompletionsPath:
			converted, ok = codec.chatToMessagesResponse(bodyBytes)
		case protocolpaths.UpstreamResponsesPath:
			converted, ok = codec.responsesToMessagesResponse(bodyBytes)
		case protocolpaths.UpstreamMessagesPath:
			converted, ok = bodyBytes, true
		}
	}

	if !ok {
		return nil, false
	}

	resp.Body = io.NopCloser(bytes.NewReader(converted))
	resp.ContentLength = int64(len(converted))
	resp.Header.Set("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	resp.Header.Set("Content-Type", "application/json")
	return resp, true
}

func convertStreamingResponse(sourceLocal, targetUpstream string, resp *http.Response, codec EndpointCodec) (*http.Response, bool) {
	var transformed io.ReadCloser
	if NormalizeLocalPath(sourceLocal) == protocolpaths.MessagesPath {
		switch targetUpstream {
		case protocolpaths.UpstreamChatCompletionsPath:
			transformed = codec.chatSSEToMessages(resp.Body)
		case protocolpaths.UpstreamResponsesPath:
			transformed = codec.responsesSSEToMessages(resp.Body)
		case protocolpaths.UpstreamMessagesPath:
			transformed = resp.Body
		}
	}

	if transformed == nil {
		_ = resp.Body.Close()
		return nil, false
	}
	resp.Body = transformed
	resp.Header.Set("Content-Type", "text/event-stream")
	resp.ContentLength = -1
	resp.Header.Del("Content-Length")
	return resp, true
}

// NormalizeEffort normalizes reasoning effort values across all endpoint formats.
func NormalizeEffort(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "minimal":
		return normalizedEffortLow, true
	case "low":
		return normalizedEffortLow, true
	case "medium":
		return normalizedEffortMedium, true
	case "high":
		return normalizedEffortHigh, true
	case "max":
		return normalizedEffortHigh, true
	default:
		return "", false
	}
}

// errWriter wraps an io.Writer and stops writing after the first error.
// This prevents SSE translation goroutines from continuing to read upstream
// data when the downstream consumer has disconnected (pipe closed).
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	n, err := ew.w.Write(p)
	if err != nil {
		err = fmt.Errorf("write transformed stream: %w", err)
	}
	ew.err = err
	return n, err
}

func (ew *errWriter) failed() bool {
	return ew.err != nil
}

func (c EndpointCodec) messagesToChatRequest(body []byte) ([]byte, bool) {
	if c.MessagesToChatRequest == nil {
		return nil, false
	}
	return c.MessagesToChatRequest(body)
}

func (c EndpointCodec) chatToMessagesResponse(body []byte) ([]byte, bool) {
	if c.ChatToMessagesResponse == nil {
		return nil, false
	}
	return c.ChatToMessagesResponse(body)
}

func (c EndpointCodec) chatSSEToMessages(body io.ReadCloser) io.ReadCloser {
	if c.ChatSSEToMessages == nil {
		return nil
	}
	return c.ChatSSEToMessages(body)
}

func (c EndpointCodec) messagesToResponsesRequest(body []byte) ([]byte, bool) {
	if c.MessagesToResponsesRequest == nil {
		return nil, false
	}
	return c.MessagesToResponsesRequest(body)
}

func (c EndpointCodec) responsesToMessagesResponse(body []byte) ([]byte, bool) {
	if c.ResponsesToMessagesResponse == nil {
		return nil, false
	}
	return c.ResponsesToMessagesResponse(body)
}

func (c EndpointCodec) responsesSSEToMessages(body io.ReadCloser) io.ReadCloser {
	if c.ResponsesSSEToMessages == nil {
		return nil
	}
	return c.ResponsesSSEToMessages(body)
}

func isEventStreamResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.HasPrefix(contentType, "text/event-stream")
}

func jsonErrorResponse(req *http.Request, status int, message string) *http.Response {
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
