package types

import (
	"errors"
	"strings"
	"time"
)

// Core defines the foundational contracts shared between the kernel,
// observability, account, config, and monitoring subsystems. These DTOs avoid
// importing anything outside the standard library so they can be referenced
// transitively without creating dependency cycles.
const (
	// StatusClientCanceled matches the HTTP status code sent when a client
	// aborts the connection mid-stream. Observability consumers rely on this
	// constant to prevent counting canceled requests as errors.
	StatusClientCanceled = 499
)

var (
	ErrNotStarted = errors.New("core: service not started")
)

// ServiceState represents the runtime state of a core service controller.
type ServiceState string

const (
	StateStopped ServiceState = "stopped"
	StateRunning ServiceState = "running"
)

// ServiceController orchestrates service lifecycle.
type ServiceController interface {
	Start() error
	Stop() error
	Status() ServiceState
}

// RequestInvocation describes an in-process HTTP invocation.
type RequestInvocation struct {
	Method string
	Path   string
	Body   []byte
	Header map[string]string
}

// ResponsePayload captures the payload returned from an in-process invocation.
type ResponsePayload struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// DebugInfo stores the granular request/response data that debug views display.
type DebugInfo struct {
	RequestHeaders  map[string]string
	RequestBody     string
	ResponseHeaders map[string]string
	ResponseBody    string
	UpstreamURL     string
	Error           string
}

// TruncatedRequestBody returns a version of the captured request body
// truncated to maxLen so UI renders remain bounded.
func (d *DebugInfo) TruncatedRequestBody(maxLen int) string {
	if d == nil {
		return ""
	}
	return truncateString(d.RequestBody, maxLen)
}

// TruncatedResponseBody returns a truncated copy of the response body.
func (d *DebugInfo) TruncatedResponseBody(maxLen int) string {
	if d == nil {
		return ""
	}
	return truncateString(d.ResponseBody, maxLen)
}

// MaskedHeaders redacts sensitive header values so debug views stay safe.
func (d *DebugInfo) MaskedHeaders() map[string]string {
	if d == nil {
		return nil
	}
	return MaskHeaders(d.RequestHeaders)
}

// RequestRecord represents a single request observed by the kernel or proxy.
type RequestRecord struct {
	Timestamp             time.Time
	Method                string
	Path                  string
	UpstreamPath          string
	Model                 string
	Account               string
	RequestID             string
	StatusCode            int
	Duration              time.Duration
	FirstResponseDuration time.Duration
	IsStream              bool
	Streaming             bool
	IsVision              bool
	IsAgent               bool
	Debug                 *DebugInfo
}

// ModelStats aggregates metrics for a single model.
type ModelStats struct {
	Count       int64
	Errors      int64
	AgentErrors int64
	TotalTime   time.Duration
	VisionReqs  int64
	AgentReqs   int64
}

// Snapshot is a point-in-time view of the metrics collector.
type Snapshot struct {
	TotalRequests  int64
	ByModel        map[string]*ModelStats
	ByStatus       map[int]int64
	RecentRequests []RequestRecord
	ActiveRequests []RequestRecord
}

// Event represents an observability diary entry.
type Event struct {
	Timestamp time.Time
	Type      string
	Message   string
	Payload   map[string]any
}

// QuotaSnapshot represents quota usage state for GitHub Copilot subscriptions.
type QuotaSnapshot struct {
	Entitlement      int64   // Total quota
	Remaining        int64   // Remaining quota
	PercentRemaining float64 // Remaining percentage
	Unlimited        bool    // Whether quota is unlimited
}

// UserInfo contains Copilot subscription information useful for the UI layers.
type UserInfo struct {
	Plan         string        // copilot_plan: "business", "individual"
	Organization string        // Organization name
	Quota        QuotaSnapshot // Premium interactions quota
	ResetDate    time.Time     // Quota reset date
}

// MaskHeaders redacts well-known sensitive headers.
func MaskHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for k, v := range headers {
		if sensitiveHeaders[strings.ToLower(k)] {
			result[k] = maskValue(v)
		} else {
			result[k] = v
		}
	}
	return result
}

const (
	maskPrefixLength    = 10
	truncateEllipsisLen = 3
)

var sensitiveHeaders = map[string]bool{
	"authorization": true,
	"x-api-key":     true,
	"api-key":       true,
	"cookie":        true,
	"set-cookie":    true,
}

func maskValue(v string) string {
	if len(v) <= maskPrefixLength {
		return "***"
	}
	return v[:maskPrefixLength] + "***"
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= truncateEllipsisLen {
		return s[:maxLen]
	}
	return s[:maxLen-truncateEllipsisLen] + "..."
}
