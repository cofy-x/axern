package nodekernel

import (
	"context"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
)

type ReportParams struct {
	NodeID     string
	NodeTarget string
	Summary    *nodev1.NodeSummary
	Now        time.Time
}

type Store interface {
	Report(ctx context.Context, params ReportParams) (*Record, error)
	RequireActive(ctx context.Context, nodeID string) error
	Load(ctx context.Context) ([]*Record, error)
}
