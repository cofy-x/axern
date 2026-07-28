package tunnelkernel

import (
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

type CreateParams struct {
	AllocationID string
	RemotePort   *int32
	LocalTarget  string
	TTL          time.Duration
	Now          time.Time
}

type CreateResult struct {
	Session     *tunnelv1.TunnelSession
	ClientToken string
	NodeToken   string
}
