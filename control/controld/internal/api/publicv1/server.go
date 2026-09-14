package publicv1

import (
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

type Server struct {
	environmentv1.UnimplementedEnvironmentControlServer
	runv1.UnimplementedRunControlServer
	secretv1.UnimplementedSecretControlServer
	tunnelv1.UnimplementedTunnelControlServer
	namespacev1.UnimplementedNamespaceControlServer
	quotav1.UnimplementedQuotaControlServer

	deps Dependencies
}

func New(deps Dependencies) *Server {
	return &Server{deps: deps}
}
