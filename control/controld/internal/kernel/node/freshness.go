package nodekernel

import (
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func HeartbeatAgeSecs(updatedAt, now time.Time) int64 {
	if updatedAt.IsZero() || now.Before(updatedAt) {
		return 0
	}
	return int64(now.Sub(updatedAt) / time.Second)
}

func SummaryCollectedAt(summary *nodev1.NodeSummary) time.Time {
	if summary == nil || summary.GetCollectedAt() == nil {
		return time.Time{}
	}
	return summary.GetCollectedAt().AsTime().UTC()
}

func SummaryAgeSecs(summary *nodev1.NodeSummary, now time.Time) int64 {
	collectedAt := SummaryCollectedAt(summary)
	if collectedAt.IsZero() || now.Before(collectedAt) {
		return 0
	}
	return int64(now.Sub(collectedAt) / time.Second)
}

func HeartbeatFresh(updatedAt, now time.Time, window time.Duration) bool {
	if updatedAt.IsZero() {
		return false
	}
	if now.Before(updatedAt) {
		return true
	}
	return now.Sub(updatedAt) <= window
}

func SummaryFresh(summary *nodev1.NodeSummary, now time.Time, window time.Duration) bool {
	collectedAt := SummaryCollectedAt(summary)
	if collectedAt.IsZero() {
		return false
	}
	if now.Before(collectedAt) {
		return true
	}
	return now.Sub(collectedAt) <= window
}

func ClassifyFreshnessState(heartbeatFresh, summaryFresh bool) string {
	switch {
	case heartbeatFresh && summaryFresh:
		return "fresh"
	case !heartbeatFresh && !summaryFresh:
		return "stale_both"
	case !heartbeatFresh:
		return "stale_heartbeat"
	default:
		return "stale_summary"
	}
}
