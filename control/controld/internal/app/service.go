package app

import (
	"context"
	"errors"
	"sync"
	"time"

	apiadminv1 "github.com/cofy-x/axern/control/controld/internal/api/adminv1"
	apigatewayv1 "github.com/cofy-x/axern/control/controld/internal/api/gatewayv1"
	apiidentityv1 "github.com/cofy-x/axern/control/controld/internal/api/identityv1"
	apinodev1 "github.com/cofy-x/axern/control/controld/internal/api/nodev1"
	publicv1 "github.com/cofy-x/axern/control/controld/internal/api/publicv1"
	apirelayv1 "github.com/cofy-x/axern/control/controld/internal/api/relayv1"
	appaccess "github.com/cofy-x/axern/control/controld/internal/application/access"
	appnode "github.com/cofy-x/axern/control/controld/internal/application/node"
	apprun "github.com/cofy-x/axern/control/controld/internal/application/run"
	"github.com/cofy-x/axern/control/controld/internal/environmenttemplate"
	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	"github.com/cofy-x/axern/control/controld/internal/nodebridge"
	"github.com/cofy-x/axern/control/controld/internal/ociimage"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgaccess "github.com/cofy-x/axern/control/controld/internal/postgres/access"
	pgadmin "github.com/cofy-x/axern/control/controld/internal/postgres/admin"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	pgnodes "github.com/cofy-x/axern/control/controld/internal/postgres/nodes"
	pgrun "github.com/cofy-x/axern/control/controld/internal/postgres/run"
	pgsecret "github.com/cofy-x/axern/control/controld/internal/postgres/secret"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	privateenvironmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/environment/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/credentials"
)

const (
	defaultHeartbeatFreshnessWindow = 15 * time.Second
	defaultSummaryFreshnessWindow   = 15 * time.Second
	defaultAllocationAccessGrantTTL = 5 * time.Minute
	defaultSandboxRuntime           = "runsc"
	defaultReconcileInterval        = 2 * time.Second
	defaultReconcileTimeout         = 30 * time.Second
	defaultPostgresMaxConnections   = 48
)

type Config struct {
	LifecycleContext         context.Context
	HeartbeatFreshnessWindow time.Duration
	SummaryFreshnessWindow   time.Duration
	EnvironmentTemplates     []*privateenvironmentv1.EnvironmentTemplate
	PostgresDSN              string
	PostgresMaxConnections   int32
	SecretsMasterKey         string
	ReconcileInterval        time.Duration
	ReconcileTimeout         time.Duration
	TunnelRelays             string
	ResourcePolicy           resourcekernel.AdmissionPolicy

	NodeLifecycle            nodebridge.LifecycleClient
	NodeTransportCredentials credentials.TransportCredentials
	ImageResolver            environmentkernel.ImageResolver
}

type App struct {
	registry                 *nodekernel.Registry
	placement                *placement.Engine
	templates                *environmenttemplate.Store
	nodeStore                nodekernel.Store
	nodeLifecycle            nodebridge.LifecycleClient
	nodeBridge               *nodebridge.Bridge
	heartbeatFreshnessWindow time.Duration
	summaryFreshnessWindow   time.Duration
	reconcileInterval        time.Duration
	reconcileTimeout         time.Duration
	now                      func() time.Time
	imageResolver            environmentkernel.ImageResolver
	resourcePolicy           resourcekernel.AdmissionPolicy

	db              *postgres.DB
	adminPG         *pgadmin.Store
	accessPG        *pgaccess.Store
	accessControl   *appaccess.Service
	runStore        *pgrun.Store
	namespacePG     *pgnamespace.Store
	secretDB        *pgsecret.Store
	tunnelPG        *pgtunnel.Store
	reconcileCtx    context.Context
	cancelReconcile context.CancelFunc
	stopCh          chan struct{}
	stopOnce        sync.Once
	wg              sync.WaitGroup
	metrics         []sdkobs.ObservableRegistration

	reconcileHealth *reconcilekernel.HealthTracker
	runReconciler   apprun.Reconciler
	nodeReconciler  appnode.AvailabilityReconciler

	adminAPI    *apiadminv1.Server
	identityAPI *apiidentityv1.Server
	publicAPI   *publicv1.Server
	gatewayAPI  *apigatewayv1.Server
	nodeAPI     *apinodev1.Server
	relayAPI    *apirelayv1.Server
}

func New(cfg Config) (*App, error) {
	return newApp(cfg, true)
}

func newApp(cfg Config, startBackgroundReconciler bool) (*App, error) {
	if cfg.HeartbeatFreshnessWindow <= 0 {
		cfg.HeartbeatFreshnessWindow = defaultHeartbeatFreshnessWindow
	}
	if cfg.SummaryFreshnessWindow <= 0 {
		cfg.SummaryFreshnessWindow = defaultSummaryFreshnessWindow
	}
	if cfg.ReconcileInterval <= 0 {
		cfg.ReconcileInterval = defaultReconcileInterval
	}
	if cfg.ReconcileTimeout <= 0 {
		cfg.ReconcileTimeout = defaultReconcileTimeout
	}
	if cfg.PostgresMaxConnections <= 0 {
		cfg.PostgresMaxConnections = defaultPostgresMaxConnections
	}
	cfg.ResourcePolicy = resourcekernel.NormalizeAdmissionPolicy(cfg.ResourcePolicy)
	if err := resourcekernel.ValidateAdmissionPolicy(cfg.ResourcePolicy); err != nil {
		return nil, err
	}

	lifecycleCtx := cfg.LifecycleContext
	if lifecycleCtx == nil {
		lifecycleCtx = context.Background()
	}
	reconcileCtx, cancelReconcile := context.WithCancel(lifecycleCtx)
	app := &App{
		registry: nodekernel.NewRegistry(),
		placement: placement.NewEngine(placement.Config{
			HeartbeatFreshnessWindow: cfg.HeartbeatFreshnessWindow,
			SummaryFreshnessWindow:   cfg.SummaryFreshnessWindow,
			ResourcePolicy:           cfg.ResourcePolicy,
		}),
		templates:                environmenttemplate.NewStore(cfg.EnvironmentTemplates),
		heartbeatFreshnessWindow: cfg.HeartbeatFreshnessWindow,
		summaryFreshnessWindow:   cfg.SummaryFreshnessWindow,
		reconcileInterval:        cfg.ReconcileInterval,
		reconcileTimeout:         cfg.ReconcileTimeout,
		resourcePolicy:           cfg.ResourcePolicy,
		now: func() time.Time {
			return time.Now().UTC()
		},
		reconcileCtx:    reconcileCtx,
		cancelReconcile: cancelReconcile,
		stopCh:          make(chan struct{}),
		reconcileHealth: reconcilekernel.NewHealthTracker(
			reconcilekernel.ComponentRun,
			reconcilekernel.ComponentNode,
			reconcilekernel.ComponentCapability,
			reconcilekernel.ComponentTunnel,
		),
	}
	if cfg.ImageResolver != nil {
		app.imageResolver = cfg.ImageResolver
	} else {
		app.imageResolver = ociimage.NewResolver()
	}
	if err := app.configureDependencies(cfg); err != nil {
		app.Close()
		return nil, err
	}
	if err := app.hydrateNodes(); err != nil {
		app.Close()
		return nil, err
	}
	if err := app.registerClusterMetrics(); err != nil {
		app.Close()
		return nil, err
	}
	app.buildAPIs()
	if startBackgroundReconciler {
		app.startReconciler()
	}
	return app, nil
}

func (a *App) configureDependencies(cfg Config) error {
	if cfg.NodeLifecycle != nil {
		a.nodeLifecycle = cfg.NodeLifecycle
	} else {
		a.nodeLifecycle = nodebridge.NewGRPCClient(cfg.NodeTransportCredentials)
	}

	if cfg.PostgresDSN == "" {
		return errors.New("postgres dsn is required")
	}

	db, err := postgres.Open(context.Background(), cfg.PostgresDSN, postgres.WithMaxConnections(cfg.PostgresMaxConnections))
	if err != nil {
		return err
	}
	if err := db.CheckMigrations(context.Background()); err != nil {
		db.Close()
		return err
	}
	a.db = db
	a.adminPG = pgadmin.NewStore(db)
	a.accessPG = pgaccess.NewStore(db)
	a.accessControl = appaccess.New(a.accessPG, a.now)
	a.nodeStore = pgnodes.NewPGStore(db)
	a.namespacePG = pgnamespace.NewStore(db)
	a.runStore = pgrun.NewStore(db, pgrun.WithAdmissionPolicy(a.resourcePolicy), pgrun.WithPlacementEvaluator(a.placement))
	masterKey, err := secretkernel.NormalizeMasterKey(cfg.SecretsMasterKey)
	if err != nil {
		return err
	}
	a.secretDB, err = pgsecret.NewStore(db, masterKey)
	if err != nil {
		return err
	}
	relays, err := pgtunnel.ParseRelays(cfg.TunnelRelays)
	if err != nil {
		return err
	}
	a.tunnelPG = pgtunnel.NewStore(db, pgtunnel.WithRelays(relays), pgtunnel.WithMasterKey(masterKey))
	a.nodeBridge = nodebridge.New(a.nodeLifecycle, nodebridge.Config{
		SecretValues:        a.secretDB,
		RegistryCredentials: a.secretDB,
	})
	a.runReconciler = apprun.NewReconciler(a.runStore, a.nodeBridge, "controld-"+uuid.NewString(), func() time.Time { return a.now() })
	return nil
}

func (a *App) hydrateNodes() error {
	records, err := a.nodeStore.Load(context.Background())
	if err != nil {
		return err
	}
	a.registry.Replace(records)
	return nil
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	a.stopOnce.Do(func() {
		if a.cancelReconcile != nil {
			a.cancelReconcile()
		}
		close(a.stopCh)
		a.wg.Wait()
		for _, registration := range a.metrics {
			if registration != nil {
				_ = registration.Unregister()
			}
		}
		if a.nodeLifecycle != nil {
			_ = a.nodeLifecycle.Close()
		}
		if a.runStore != nil {
			a.runStore.Close()
		}
		if a.tunnelPG != nil {
			a.tunnelPG.Close()
		}
		if a.db != nil {
			a.db.Close()
		}
	})
	return nil
}
