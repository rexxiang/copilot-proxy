package monitor

import (
	"copilot-proxy/internal/models"
	"strings"
	"time"
)

// RequestRecord represents a single proxied request.
type RequestRecord struct {
	Timestamp             time.Time
	Method                string        // HTTP method: GET, POST, etc.
	Path                  string        // Local path: /v1/chat/completions, /v1/responses, etc.
	UpstreamPath          string        // Upstream path: /chat/completions, /responses, etc.
	Model                 string        // gpt-4o, claude-3-opus, etc.
	Account               string        // User identifier
	RequestID             string        // Correlation ID
	StatusCode            int           // HTTP status code (0 = in progress)
	Duration              time.Duration // Request duration
	IsStream              bool          // Whether request is an SSE stream
	FirstResponseDuration time.Duration // Time to first response for stream requests
	Streaming             bool          // Whether stream is still active after first response
	IsVision              bool          // Contains image content
	IsAgent               bool          // Initiated by agent (not user)
	Debug                 *DebugInfo    // Optional debug information (only when debug mode enabled)
}

// DebugInfo contains detailed request/response information for debugging.
type DebugInfo struct {
	RequestHeaders  map[string]string // Request headers (sensitive values masked)
	RequestBody     string            // Request body (truncated)
	ResponseHeaders map[string]string // Response headers
	ResponseBody    string            // Response body (truncated)
	UpstreamURL     string            // Actual upstream URL called
	Error           string            // Error message if any
}

// sensitiveHeaders lists headers that should be masked in debug output.
var sensitiveHeaders = map[string]bool{
	"authorization": true,
	"x-api-key":     true,
	"api-key":       true,
	"cookie":        true,
	"set-cookie":    true,
}

const (
	// StatusClientCanceled marks a request canceled by client disconnect/context cancel.
	StatusClientCanceled = 499
	maskPrefixLength     = 10
	truncateEllipsisLen  = 3
)

// TruncatedRequestBody returns request body truncated to maxLen.
func (d *DebugInfo) TruncatedRequestBody(maxLen int) string {
	return truncateString(d.RequestBody, maxLen)
}

// TruncatedResponseBody returns response body truncated to maxLen.
func (d *DebugInfo) TruncatedResponseBody(maxLen int) string {
	return truncateString(d.ResponseBody, maxLen)
}

// MaskedHeaders returns request headers with sensitive values masked.
func (d *DebugInfo) MaskedHeaders() map[string]string {
	return MaskHeaders(d.RequestHeaders)
}

// MaskHeaders masks sensitive header values in a header map.
func MaskHeaders(headers map[string]string) map[string]string {
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

// maskValue masks a sensitive value, keeping prefix visible.
func maskValue(v string) string {
	if len(v) <= maskPrefixLength {
		return "***"
	}
	// Keep first 10 chars for identification (e.g., "Bearer sk-")
	return v[:maskPrefixLength] + "***"
}

// truncateString truncates a string to maxLen, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= truncateEllipsisLen {
		return s[:maxLen]
	}
	return s[:maxLen-truncateEllipsisLen] + "..."
}

// ModelStats holds statistics for a single model.
type ModelStats struct {
	Count       int64
	Errors      int64
	AgentErrors int64
	TotalTime   time.Duration
	VisionReqs  int64
	AgentReqs   int64
}

// Snapshot represents a point-in-time view of collected statistics.
type Snapshot struct {
	TotalRequests  int64
	ByModel        map[string]*ModelStats
	ByStatus       map[int]int64
	RecentRequests []RequestRecord
	ActiveRequests []RequestRecord // Requests currently in progress (StatusCode == 0)
	ActivityMinute map[time.Time]int
	ActivityHour   map[time.Time]int
	ActivityDay    map[time.Time]int
}

// UserInfo contains Copilot subscription information.
type UserInfo struct {
	Plan         string        // copilot_plan: "business", "individual"
	Organization string        // Organization name
	Quota        QuotaSnapshot // Premium interactions quota
	ResetDate    time.Time     // Quota reset date
}

// QuotaSnapshot represents quota usage state.
type QuotaSnapshot struct {
	Entitlement      int64   // Total quota
	Remaining        int64   // Remaining quota
	PercentRemaining float64 // Remaining percentage
	Unlimited        bool    // Whether quota is unlimited
}

type ModelInfo = models.ModelInfo
