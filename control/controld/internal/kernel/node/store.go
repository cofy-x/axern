package nodekernel

import (
	"context"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

type ReportParams struct {
	NodeID        string
	NodeTarget    string
	Summary       *nodev1.NodeSummary
	NodeAuthToken string
	Now           time.Time
}

type Store interface {
	Report(ctx context.Context, params ReportParams) (*Record, error)
	Authenticate(ctx context.Context, nodeID, nodeAuthToken string) error
	Load(ctx context.Context) ([]*Record, error)
}
