package app

import (
	"context"
	"time"

	apiadminv1 "github.com/cofy-x/axern/control/controld/internal/api/adminv1"
	apigatewayv1 "github.com/cofy-x/axern/control/controld/internal/api/gatewayv1"
	apiidentityv1 "github.com/cofy-x/axern/control/controld/internal/api/identityv1"
	apinodev1 "github.com/cofy-x/axern/control/controld/internal/api/nodev1"
	publicv1 "github.com/cofy-x/axern/control/controld/internal/api/publicv1"
	apirelayv1 "github.com/cofy-x/axern/control/controld/internal/api/relayv1"
	appaccess "github.com/cofy-x/axern/control/controld/internal/application/access"
	appadmin "github.com/cofy-x/axern/control/controld/internal/application/admin"
	appenvironment "github.com/cofy-x/axern/control/controld/internal/application/environment"
	appgateway "github.com/cofy-x/axern/control/controld/internal/application/gateway"
	appnode "github.com/cofy-x/axern/control/controld/internal/application/node"
	apprun "github.com/cofy-x/axern/control/controld/internal/application/run"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	pggateway "github.com/cofy-x/axern/control/controld/internal/postgres/gateway"
)

type publicProfile struct {
	environments publicv1.Environments
	secrets      publicv1.Secrets
	runs         publicv1.Runs
}

type nodeProfile struct {
	allocations appnode.AllocationControl
}

type apiProfile struct {
	admin            appadmin.AllocationLifecycleControl
	adminAudit       appadmin.AuditControl
	adminReliability appadmin.ReliabilityControl
	adminNodes       appadmin.NodeControl
	public           publicProfile
	node             nodeProfile
}

func (a *App) buildAPIs() {
	selector := a.newPlacementSelector()
	profile := a.buildAPIProfile(selector)
	a.adminAPI = apiadminv1.New(apiadminv1.Dependencies{
		Now:                        func() time.Time { return a.now() },
		AllocationLifecycleRetries: profile.admin,
		AdminAuditEvents:           profile.adminAudit,
		Reliability:                profile.adminReliability,
		Nodes:                      profile.adminNodes,
		CapabilityDiagnostics:      a.adminPG,
		NodeHeartbeatWindow:        a.heartbeatFreshnessWindow,
		NodeSummaryWindow:          a.summaryFreshnessWindow,
		Access:                     a.accessControl,
	})
	a.identityAPI = apiidentityv1.New()
	a.publicAPI = publicv1.New(publicv1.Dependencies{
		Now:          func() time.Time { return a.now() },
		Catalog:      a.catalog,
		Environments: profile.public.environments,
		Secrets:      profile.public.secrets,
		Runs:         profile.public.runs,
		Tunnels:      a.tunnelPG,
		Namespaces:   a.namespacePG,
		Quotas:       a.namespacePG,
	})
	a.nodeAPI = apinodev1.New(apinodev1.Dependencies{
		Now:         func() time.Time { return a.now() },
		NodeStore:   a.nodeStore,
		Registry:    a.registry,
		Reporter:    appnode.NewReporter(a.nodeStore, a.registry, profile.node.allocations, func() time.Time { return a.now() }),
		Allocations: profile.node.allocations,
		Tunnels:     a.tunnelPG,
	})
	a.nodeReconciler = appnode.NewAvailabilityReconciler(appnode.AvailabilityReconcilerDeps{
		Nodes:           a.nodeStore,
		Lifecycle:       a.registry,
		Allocations:     profile.node.allocations,
		HeartbeatWindow: a.heartbeatFreshnessWindow,
	})
	a.relayAPI = apirelayv1.New(apirelayv1.Dependencies{
		Now:     func() time.Time { return a.now() },
		Tunnels: a.tunnelPG,
	})
	if a.db != nil && a.runStore != nil {
		a.gatewayAPI = apigatewayv1.New(apigatewayv1.Dependencies{
			Now:        func() time.Time { return a.now() },
			Resolver:   appgateway.NewResolver(pggateway.NewReader(a.db), a.runStore),
			DefaultTTL: defaultExecutionLeaseTTL,
			Access:     a.accessControl,
			Tunnels:    a.tunnelPG,
		})
	}
}

func (a *App) buildAPIProfile(selector *placement.Selector) apiProfile {
	return a.authoritativeProfile(selector)
}

func (a *App) newPlacementSelector() *placement.Selector {
	return placement.NewSelector(
		a.registry,
		a.placement,
		func() time.Time { return a.now() },
		defaultSandboxRuntime,
	).WithObserver(placementMetricsObserver{})
}

func (a *App) buildPublicProfile(environments publicv1.Environments, secrets publicv1.Secrets, runs publicv1.Runs) publicProfile {
	return publicProfile{
		environments: environments,
		secrets:      secrets,
		runs:         runs,
	}
}

func (a *App) newAuthoritativeNodeProfile() nodeProfile {
	return nodeProfile{
		allocations: appnode.NewAuthoritative(a.runStore),
	}
}

func (a *App) authoritativeProfile(selector *placement.Selector) apiProfile {
	runs := apprun.NewAuthoritative(a.runStore, selector, a.nodeBridge)
	environments := appenvironment.NewAuthoritative(a.catalog, a.imageResolver, a.secretDB, a.runStore)
	profile := apiProfile{
		admin:      appadmin.NewAllocationLifecycleControl(a.adminPG),
		adminAudit: appadmin.NewAuditControl(a.adminPG),
		adminReliability: appadmin.NewReliabilityControl(a.adminPG, func() reconcilekernel.HealthSnapshot {
			if a.reconcileHealth == nil {
				return reconcilekernel.EmptyHealthSnapshot()
			}
			return a.reconcileHealth.Snapshot()
		}, a.backgroundReconcileTimeout(), nodeHealthSource{store: a.adminPG, heartbeatWindow: a.heartbeatFreshnessWindow, summaryWindow: a.summaryFreshnessWindow}),
		public: a.buildPublicProfile(
			environments,
			a.secretDB,
			runs,
		),
	}
	profile.adminNodes = appadmin.NewNodeControl(a.adminPG, a.registry, a.heartbeatFreshnessWindow)
	profile.node = a.newAuthoritativeNodeProfile()
	return profile
}

func (a *App) AdminV1Handler() *apiadminv1.Server { return a.adminAPI }

func (a *App) IdentityV1Handler() *apiidentityv1.Server { return a.identityAPI }

func (a *App) AccessControl() *appaccess.Service { return a.accessControl }

func (a *App) HasActivePlatformAdmin(ctx context.Context) (bool, error) {
	return a.accessControl.HasActivePlatformAdmin(ctx)
}

func (a *App) PublicV1Handler() *publicv1.Server { return a.publicAPI }

func (a *App) GatewayV1Handler() *apigatewayv1.Server { return a.gatewayAPI }

func (a *App) NodeV1Handler() *apinodev1.Server { return a.nodeAPI }

func (a *App) RelayV1Handler() *apirelayv1.Server { return a.relayAPI }
