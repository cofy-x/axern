package nodekernel

import (
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

type DebugNode struct {
	NodeID           string              `json:"node_id"`
	NodeTarget       string              `json:"node_target"`
	Runtimes         []string            `json:"runtimes"`
	Fresh            bool                `json:"fresh"`
	HeartbeatFresh   bool                `json:"heartbeat_fresh"`
	SummaryFresh     bool                `json:"summary_fresh"`
	Lifecycle        LifecycleStatus     `json:"lifecycle"`
	FreshnessState   string              `json:"freshness_state"`
	HeartbeatAgeSecs int64               `json:"heartbeat_age_secs"`
	SummaryAgeSecs   int64               `json:"summary_age_secs"`
	RegisteredAt     time.Time           `json:"registered_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	CollectedAt      time.Time           `json:"collected_at"`
	RetiredAt        time.Time           `json:"retired_at,omitempty"`
	RetiredReason    string              `json:"retired_reason,omitempty"`
	Summary          *nodev1.NodeSummary `json:"summary,omitempty"`
}
