package monitor

import (
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/models"
	"time"
)

// RequestRecord mirrors the observability definition to keep the monitor API stable.
type RequestRecord = core.RequestRecord

type DebugInfo = core.DebugInfo

type ModelStats = core.ModelStats

type Snapshot = core.Snapshot

const StatusClientCanceled = core.StatusClientCanceled

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
