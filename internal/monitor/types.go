package monitor

import "copilot-proxy/internal/core"

// RequestRecord mirrors the observability definition to keep the monitor API stable.
type RequestRecord = core.RequestRecord

type DebugInfo = core.DebugInfo

type ModelStats = core.ModelStats

type Snapshot = core.Snapshot

const StatusClientCanceled = core.StatusClientCanceled

// UserInfo contains Copilot subscription information.
type UserInfo = core.UserInfo

// Deprecated for backwards compatibility.
type QuotaSnapshot = core.QuotaSnapshot

// ModelInfo mirrors the persisted model metadata returned to the UI adapters.
type ModelInfo = core.ModelInfo
