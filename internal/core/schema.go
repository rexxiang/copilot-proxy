package core

import (
	"net/http"
	"time"
)

// KernelSchema is the high-level contract that downstream tooling and CLIs
// agree on for observing and interacting with the kernel. The fields are
// intentionally light (mostly strings/maps) so they can evolve without
// creating dependency ripples until the implementation matures.
type KernelSchema struct {
	Name          string
	Version       string
	Lifecycle     KernelLifecycleState
	Services      []ServiceSchema
	Configuration map[string]any
	ErrorCodes    []ErrorCodeSchema
	Events        []EventSchema
}

// KernelLifecycleState tracks the broad lifecycle of the kernel.
type KernelLifecycleState string

const (
	KernelLifecycleUnknown KernelLifecycleState = "unknown"
	KernelLifecycleStopped KernelLifecycleState = "stopped"
	KernelLifecycleRunning KernelLifecycleState = "running"
	KernelLifecyclePaused  KernelLifecycleState = "paused"
)

// ServiceSchema describes a single runtime service (accounts, config, stats,
// etc.) so orchestrators know what endpoints to invoke.
type ServiceSchema struct {
	Name         string
	ID           string
	State        ServiceState
	Description  string
	Bindings     []DTOBinder
	Dependencies []string
}

// DTOBinder ties a canonical DTO definition to a service endpoint.
type DTOBinder struct {
	Method      string
	Path        string
	Description string
	Request     DTOModel
	Response    DTOModel
}

// DTOModel is a lightweight schema representation that downstream language
// bindings, documentation, and the C ABI can consume.
type DTOModel struct {
	Name           string
	Fields         []DTOField
	Representation string // e.g., "json", "jsonrpc", "sse"
}

// DTOField describes an individual JSON/property inside a DTOModel.
type DTOField struct {
	Name        string
	Type        string
	Required    bool
	Description string
	Example     any
}

// ErrorCodeSchema collects error code definitions emitted by the kernel.
type ErrorCodeSchema struct {
	Code        string
	Group       string
	Description string
	Severity    ErrorSeverity
}

// ErrorSeverity categorizes failure impact.
type ErrorSeverity string

const (
	ErrorSeverityInfo     ErrorSeverity = "info"
	ErrorSeverityWarning  ErrorSeverity = "warning"
	ErrorSeverityError    ErrorSeverity = "error"
	ErrorSeverityCritical ErrorSeverity = "critical"
)

// EventSchema describes telemetry or lifecycle events emitted by the kernel.
type EventSchema struct {
	Type        string
	Description string
	Timestamp   string // ISO-8601 hint
	Payload     []DTOField
}

// EventEnvelope is the concrete event payload that passes through debug/obs.
type EventEnvelope struct {
	Type    string
	Payload map[string]any
}

const (
	// KernelEventTypeStart marks the kernel lifecycle entering running state.
	KernelEventTypeStart = "kernel.start"
	// KernelEventTypeStop marks the kernel lifecycle transitioning to stopped.
	KernelEventTypeStop = "kernel.stop"
	// KernelEventTypeInvoke signals an in-process invocation.
	KernelEventTypeInvoke = "kernel.invoke"
)

const (
	// KernelErrorCodeNotRunning indicates the kernel was not running when
	// a request/operation required it.
	KernelErrorCodeNotRunning = "kernel.not_running"
	// KernelErrorCodeInvalidInvocation indicates the invocation payload was missing
	// required fields.
	KernelErrorCodeInvalidInvocation = "kernel.invalid_invocation"
)

// KernelErrorCodeContract lists the canonical kernel error codes.
var KernelErrorCodeContract = []ErrorCodeSchema{
	{
		Code:        KernelErrorCodeNotRunning,
		Group:       "kernel",
		Description: "kernel must be running before servicing this request",
		Severity:    ErrorSeverityError,
	},
	{
		Code:        KernelErrorCodeInvalidInvocation,
		Group:       "kernel",
		Description: "required invocation payload fields were missing or invalid",
		Severity:    ErrorSeverityError,
	},
}

// KernelEventContract enumerates the kernel lifecycle/observability events.
var KernelEventContract = []EventSchema{
	{
		Type:        KernelEventTypeStart,
		Description: "kernel entered the running state",
		Timestamp:   time.RFC3339,
		Payload: []DTOField{
			{Name: "lifecycle", Type: "string", Required: true, Description: "new lifecycle state", Example: KernelLifecycleRunning},
		},
	},
	{
		Type:        KernelEventTypeStop,
		Description: "kernel left the running state",
		Timestamp:   time.RFC3339,
		Payload: []DTOField{
			{Name: "lifecycle", Type: "string", Required: true, Description: "new lifecycle state", Example: KernelLifecycleStopped},
		},
	},
	{
		Type:        KernelEventTypeInvoke,
		Description: "an in-process request was routed through the kernel",
		Timestamp:   time.RFC3339,
		Payload: []DTOField{
			{Name: "method", Type: "string", Required: true, Description: "HTTP method dispatched", Example: http.MethodPost},
			{Name: "path", Type: "string", Required: true, Description: "path delivered to the kernel dispatcher", Example: "/v1/chat/completions"},
		},
	},
}
