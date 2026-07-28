package relayv1

import (
	"time"

	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
)

type TunnelRelayControl interface{ tunnelkernel.RelayControl }

type Dependencies struct {
	Now     func() time.Time
	Tunnels TunnelRelayControl
}
