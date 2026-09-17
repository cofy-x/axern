package nodev1

import controlnodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"

type Server struct {
	controlnodev1.UnimplementedNodeControlServer

	deps Dependencies
}

func New(deps Dependencies) *Server {
	return &Server{deps: deps}
}
