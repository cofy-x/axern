package adminv1

import adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"

type Server struct {
	adminv1.UnimplementedAllocationLifecycleAdminServer
	adminv1.UnimplementedAdminAuditServer
	adminv1.UnimplementedAdminReliabilityServer
	adminv1.UnimplementedNodeAdminServer
	adminv1.UnimplementedStorageAdminServer
	adminv1.UnimplementedServiceAdminServer
	adminv1.UnimplementedAccessAdminServer

	deps Dependencies
}

func New(deps Dependencies) *Server {
	return &Server{deps: deps}
}
