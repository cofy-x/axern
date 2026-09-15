package app

import (
	"context"
	apinodev1 "github.com/cofy-x/axern/control/controld/internal/api/nodev1"
	appnode "github.com/cofy-x/axern/control/controld/internal/application/node"
	pgnodes "github.com/cofy-x/axern/control/controld/internal/postgres/nodes"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
)

func (a *App) RequireActiveNode(ctx context.Context, nodeID string) error {
	return a.nodeStore.RequireActive(ctx, nodeID)
}

func (a *App) NodeEnrollmentHandler(issuer workloadtls.FileIssuer) (*apinodev1.EnrollmentServer, error) {
	if _, err := issuer.Load(); err != nil {
		return nil, err
	}
	return &apinodev1.EnrollmentServer{
		Control: &appnode.Enrollment{Store: pgnodes.NewPGStore(a.db), Issuer: issuer},
		Cluster: issuer.Cluster,
		Now:     a.now,
	}, nil
}
