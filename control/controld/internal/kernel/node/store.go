package nodekernel

import (
	"context"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

type ReportParams struct {
	NodeID         string
	NodeTarget     string
	Summary        *nodev1.NodeSummary
	NodeCredential string
	Now            time.Time
}

type Store interface {
	Report(ctx context.Context, params ReportParams) (*Record, error)
	Authenticate(ctx context.Context, nodeID, nodeCredential string) error
	Load(ctx context.Context) ([]*Record, error)
}
