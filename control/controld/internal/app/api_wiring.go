package app

import (
	"context"
	"strings"
	"time"

	apiadminv1 "github.com/cofy-x/axern/control/controld/internal/api/adminv1"
	artifactaccessv1 "github.com/cofy-x/axern/control/controld/internal/api/artifactaccessv1"
	apigatewayv1 "github.com/cofy-x/axern/control/controld/internal/api/gatewayv1"
	apiidentityv1 "github.com/cofy-x/axern/control/controld/internal/api/identityv1"
	apinodev1 "github.com/cofy-x/axern/control/controld/internal/api/nodev1"
	publicv1 "github.com/cofy-x/axern/control/controld/internal/api/publicv1"
	apirelayv1 "github.com/cofy-x/axern/control/controld/internal/api/relayv1"
	rolloutworkerv1 "github.com/cofy-x/axern/control/controld/internal/api/rolloutworkerv1"
	appaccess "github.com/cofy-x/axern/control/controld/internal/application/access"
	appadmin "github.com/cofy-x/axern/control/controld/internal/application/admin"
	appenvironment "github.com/cofy-x/axern/control/controld/internal/application/environment"
	appfunction "github.com/cofy-x/axern/control/controld/internal/application/function"
	appgateway "github.com/cofy-x/axern/control/controld/internal/application/gateway"
	appnode "github.com/cofy-x/axern/control/controld/internal/application/node"
	apprun "github.com/cofy-x/axern/control/controld/internal/application/run"
	appservice "github.com/cofy-x/axern/control/controld/internal/application/service"
	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	pggateway "github.com/cofy-x/axern/control/controld/internal/postgres/gateway"
)

type publicProfile struct {
	environments publicv1.Environments
	secrets      publicv1.Secrets
	runs         publicv1.Runs
	services     publicv1.Services
	functions    functionkernel.Control
}

type nodeProfile struct {
	allocations appnode.AllocationControl
}

type apiProfile struct {
	admin                appadmin.AllocationLifecycleControl
	adminAudit           appadmin.AuditControl
	adminReliability     appadmin.ReliabilityControl
	adminStorage         apiadminv1.Storage
	adminServices        apiadminv1.Services
	adminNodes           appadmin.NodeControl
	public               publicProfile
	node                 nodeProfile
	serviceReconciler    servicekernel.Reconciler
	allocationReconciler servicekernel.AllocationReconciler
	volumeReclaimWorker  servicekernel.VolumeReclaimDispatcher
	functionController   *appfunction.Controller
}

func (a *App) buildAPIs() {
	selector := a.newPlacementSelector()
	profile := a.buildAPIProfile(selector)
	a.serviceReconciler = profile.serviceReconciler
	a.allocationReconciler = profile.allocationReconciler
	a.volumeReclaimWorker = profile.volumeReclaimWorker
	a.functionController = profile.functionController

	a.adminAPI = apiadminv1.New(apiadminv1.Dependencies{
		Now:                        func() time.Time { return a.now() },
		AllocationLifecycleRetries: profile.admin,
		AdminAuditEvents:           profile.adminAudit,
		Reliability:                profile.adminReliability,
		Storage:                    profile.adminStorage,
		Services:                   profile.adminServices,
		Nodes:                      profile.adminNodes,
		CapabilityDiagnostics:      a.adminPG,
		NodeHeartbeatWindow:        a.heartbeatFreshnessWindow,
		NodeSummaryWindow:          a.summaryFreshnessWindow,
		Access:                     a.accessControl,
	})
	a.identityAPI = apiidentityv1.New()
	a.publicAPI = publicv1.New(publicv1.Dependencies{
		Now:            func() time.Time { return a.now() },
		Catalog:        a.catalog,
		Environments:   profile.public.environments,
		Secrets:        profile.public.secrets,
		AgentProfiles:  a.agentProfilePG,
		Rollouts:       a.rolloutPG,
		Runs:           profile.public.runs,
		Services:       profile.public.services,
		ServiceWatcher: a.servicePG,
		Functions:      profile.public.functions,
		Tunnels:        a.tunnelPG,
		Namespaces:     a.namespacePG,
		Quotas:         a.namespacePG,
	})
	a.nodeAPI = apinodev1.New(apinodev1.Dependencies{
		Now:                    func() time.Time { return a.now() },
		NodeStore:              a.nodeStore,
		Registry:               a.registry,
		Reporter:               appnode.NewReporter(a.nodeStore, a.registry, profile.node.allocations, func() time.Time { return a.now() }),
		Allocations:            profile.node.allocations,
		Tunnels:                a.tunnelPG,
		NotifyServiceReconcile: a.notifyServiceReconcile,
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
	if a.rolloutPG != nil && strings.TrimSpace(a.rolloutWorkerToken) != "" {
		a.rolloutWorkerAPI = rolloutworkerv1.New(rolloutworkerv1.Dependencies{Now: func() time.Time { return a.now() }, Store: a.rolloutPG, BootstrapToken: a.rolloutWorkerToken})
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

func (a *App) buildPublicProfile(environments publicv1.Environments, secrets publicv1.Secrets, runs publicv1.Runs, services publicv1.Services, functions functionkernel.Control) publicProfile {
	return publicProfile{
		environments: environments,
		secrets:      secrets,
		runs:         runs,
		services:     services,
		functions:    functions,
	}
}

func (a *App) newAuthoritativeNodeProfile() nodeProfile {
	if a.servicePG != nil {
		return nodeProfile{
			allocations: appnode.NewAuthoritative(a.allocationOwners, a.runStore, a.servicePG),
		}
	}
	return nodeProfile{
		allocations: appnode.NewAuthoritative(a.allocationOwners, a.runStore, nil),
	}
}

func (a *App) newServiceController(selector *placement.Selector) servicekernel.Controller {
	return appservice.NewController(appservice.ControllerDeps{
		Store:           a.servicePG,
		Autoscaling:     a.servicePG,
		Allocations:     a.servicePG,
		Reconcile:       a.servicePG,
		Statuses:        a.servicePG,
		Events:          a.servicePG,
		Environments:    a.runStore,
		Selector:        selector,
		Lifecycle:       a.nodeBridge,
		Storage:         a.storage,
		NotifyReconcile: a.notifyServiceReconcile,
		NodeTarget: func(nodeID string) (string, bool) {
			record, ok := a.registry.Get(nodeID)
			if !ok || record == nil || !record.Active() || strings.TrimSpace(record.NodeTarget) == "" {
				return "", false
			}
			return record.NodeTarget, true
		},
		ReconcileConcurrency:               a.serviceReconcileWorkers,
		AllocationCreateGlobalConcurrency:  a.serviceAllocationGlobalWorkers,
		AllocationCreatePerNodeConcurrency: a.serviceAllocationWorkersPerNode,
	})
}

func (a *App) authoritativeProfile(selector *placement.Selector) apiProfile {
	runs := apprun.NewAuthoritative(a.runStore, selector, a.nodeBridge)
	environments := appenvironment.NewAuthoritative(a.catalog, a.imageResolver, a.secretDB, a.runStore)
	var services servicekernel.Controller
	if a.servicePG != nil {
		services = a.newServiceController(selector)
	}
	functions := appfunction.NewController(appfunction.ControllerDeps{
		Store:         a.functionPG,
		Environments:  environments,
		Services:      services,
		ServiceWatch:  a.servicePG,
		Invoker:       a.functionInvoker,
		BundleBaseURL: a.functionBundleBaseURL,
		BundleToken:   a.functionBundleToken,
	})
	profile := apiProfile{
		admin:      appadmin.NewAllocationLifecycleControl(a.adminPG),
		adminAudit: appadmin.NewAuditControl(a.adminPG),
		adminReliability: appadmin.NewReliabilityControl(a.adminPG, func() reconcilekernel.HealthSnapshot {
			if a.reconcileHealth == nil {
				return reconcilekernel.EmptyHealthSnapshot()
			}
			return a.reconcileHealth.Snapshot()
		}, a.backgroundReconcileTimeout(), a.storageHealth, nodeHealthSource{store: a.adminPG, heartbeatWindow: a.heartbeatFreshnessWindow, summaryWindow: a.summaryFreshnessWindow}),
		public: a.buildPublicProfile(
			environments,
			a.secretDB,
			runs,
			services,
			functions,
		),
	}
	if a.storageAdmin != nil {
		profile.adminStorage = appadmin.NewStorageControl(a.storageAdmin, a.adminPG, func() time.Time { return a.now() })
	}
	profile.serviceReconciler = services
	profile.adminServices = appadmin.NewServiceControl(services, a.adminPG)
	profile.adminNodes = appadmin.NewNodeControl(a.adminPG, a.registry, a.storageAdmin, a.heartbeatFreshnessWindow)
	profile.allocationReconciler = services
	profile.volumeReclaimWorker = services
	profile.functionController = functions
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

func (a *App) RolloutWorkerV1Handler() *rolloutworkerv1.Server   { return a.rolloutWorkerAPI }
func (a *App) ArtifactAccessV1Handler() *artifactaccessv1.Server { return a.artifactAccessAPI }
