package publicv1

import (
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

type Server struct {
	agentprofilev1.UnimplementedAgentProfileControlServer
	catalogv1.UnimplementedRuntimeCatalogServer
	environmentv1.UnimplementedEnvironmentControlServer
	runv1.UnimplementedRunControlServer
	secretv1.UnimplementedSecretControlServer
	servicev1.UnimplementedServiceControlServer
	functionv1.UnimplementedFunctionControlServer
	tunnelv1.UnimplementedTunnelControlServer
	namespacev1.UnimplementedNamespaceControlServer
	quotav1.UnimplementedQuotaControlServer
	rolloutv1.UnimplementedRolloutControlServer

	deps Dependencies
}

func New(deps Dependencies) *Server {
	return &Server{deps: deps}
}
