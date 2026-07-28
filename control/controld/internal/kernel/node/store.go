package nodekernel

import (
	"context"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

type RegisterParams struct {
	NodeID        string
	NodeTarget    string
	Runtimes      []string
	NodeAuthToken string
	Now           time.Time
}

type ReportParams struct {
	NodeID        string
	NodeTarget    string
	Runtimes      []string
	Summary       *nodev1.NodeSummary
	NodeAuthToken string
	Now           time.Time
}

type Store interface {
	Register(ctx context.Context, params RegisterParams) (*Record, error)
	Report(ctx context.Context, params ReportParams) (*Record, error)
	Authenticate(ctx context.Context, nodeID, nodeAuthToken string) error
	Load(ctx context.Context) ([]*Record, error)
}
