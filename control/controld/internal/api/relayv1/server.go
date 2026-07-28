package relayv1

import (
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
)

type Server struct {
	tunnelrelaycontrolv1.UnimplementedTunnelRelayControlServer

	deps Dependencies
}

func New(deps Dependencies) *Server {
	return &Server{deps: deps}
}
