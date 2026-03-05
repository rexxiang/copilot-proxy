package cli

import (
	"fmt"

	"copilot-proxy/internal/monitor"
)

const statusHTTPErrorMin = 400

type agentPremiumRefreshDetector struct {
	prevEligible map[string]struct{}
}

func newAgentPremiumRefreshDetector() agentPremiumRefreshDetector {
	return agentPremiumRefreshDetector{
		prevEligible: make(map[string]struct{}),
	}
}

func (d *agentPremiumRefreshDetector) HasNewEligible(snapshot monitor.Snapshot, premiumSet map[string]struct{}) bool {
	if d == nil {
		return false
	}
	if d.prevEligible == nil {
		d.prevEligible = make(map[string]struct{})
	}

	current := make(map[string]struct{})
	hasNew := false
	for i := range snapshot.RecentRequests {
		record := snapshot.RecentRequests[i]
		signature, ok := eligibleAgentPremiumSignature(record, premiumSet)
		if !ok {
			continue
		}
		current[signature] = struct{}{}
		if _, seen := d.prevEligible[signature]; !seen {
			hasNew = true
		}
	}

	d.prevEligible = current
	return hasNew
}

func premiumModelSet(items []monitor.ModelInfo) map[string]struct{} {
	result := make(map[string]struct{})
	for i := range items {
		if !items[i].IsPremium || items[i].ID == "" {
			continue
		}
		result[items[i].ID] = struct{}{}
	}
	return result
}

func eligibleAgentPremiumSignature(record monitor.RequestRecord, premiumSet map[string]struct{}) (string, bool) {
	if !record.IsAgent {
		return "", false
	}
	if record.StatusCode <= 0 || record.StatusCode >= statusHTTPErrorMin {
		return "", false
	}
	if record.Model == "" {
		return "", false
	}
	if _, ok := premiumSet[record.Model]; !ok {
		return "", false
	}
	if record.RequestID != "" {
		return "request_id:" + record.RequestID, true
	}
	return fmt.Sprintf(
		"ts:%d|model:%s|status:%d|method:%s|path:%s|upstream:%s|agent:%t",
		record.Timestamp.UnixNano(),
		record.Model,
		record.StatusCode,
		record.Method,
		record.Path,
		record.UpstreamPath,
		record.IsAgent,
	), true
}
