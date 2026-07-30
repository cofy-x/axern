package app

import (
	"context"
	"errors"
	"strings"
	"sync"
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
	appfunction "github.com/cofy-x/axern/control/controld/internal/application/function"
	appnode "github.com/cofy-x/axern/control/controld/internal/application/node"
	apprun "github.com/cofy-x/axern/control/controld/internal/application/run"
	"github.com/cofy-x/axern/control/controld/internal/artifactstore"
	"github.com/cofy-x/axern/control/controld/internal/catalog"
	"github.com/cofy-x/axern/control/controld/internal/functiondispatch"
	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	"github.com/cofy-x/axern/control/controld/internal/nodebridge"
	"github.com/cofy-x/axern/control/controld/internal/ociimage"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgaccess "github.com/cofy-x/axern/control/controld/internal/postgres/access"
	pgadmin "github.com/cofy-x/axern/control/controld/internal/postgres/admin"
	pgagentprofile "github.com/cofy-x/axern/control/controld/internal/postgres/agentprofile"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	pgfunction "github.com/cofy-x/axern/control/controld/internal/postgres/function"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	pgnodes "github.com/cofy-x/axern/control/controld/internal/postgres/nodes"
	pgrollout "github.com/cofy-x/axern/control/controld/internal/postgres/rollout"
	pgrun "github.com/cofy-x/axern/control/controld/internal/postgres/run"
	pgsecret "github.com/cofy-x/axern/control/controld/internal/postgres/secret"
	pgservice "github.com/cofy-x/axern/control/controld/internal/postgres/service"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	"github.com/cofy-x/axern/control/controld/internal/storagecoord"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

const (
	defaultHeartbeatFreshnessWindow        = 15 * time.Second
	defaultSummaryFreshnessWindow          = 15 * time.Second
	defaultExecutionLeaseTTL               = 5 * time.Minute
	defaultSandboxRuntime                  = "runsc"
	defaultReconcileInterval               = 2 * time.Second
	defaultReconcileTimeout                = 30 * time.Second
	defaultServiceRecoveryInterval         = 30 * time.Second
	defaultServiceReconcileWorkers         = 32
	defaultPostgresMaxConnections          = 48
	defaultServiceAllocationGlobalWorkers  = 256
	defaultServiceAllocationWorkersPerNode = 12
	defaultVolumeReclaimWorkers            = 8
	defaultVolumeReclaimWorkersPerNode     = 2
)

type Config struct {
	LifecycleContext                context.Context
	HeartbeatFreshnessWindow        time.Duration
	SummaryFreshnessWindow          time.Duration
	RuntimeTemplates                []*catalogv1.RuntimeTemplate
	PostgresDSN                     string
	PostgresMaxConnections          int32
	SecretsMasterKey                string
	ReconcileInterval               time.Duration
	ReconcileTimeout                time.Duration
	ServiceReconcileWorkers         int
	ServiceAllocationGlobalWorkers  int
	ServiceAllocationWorkersPerNode int
	TunnelEdgeTarget                string
	TunnelNodeEdgeTarget            string
	TunnelRelays                    string
	StoragedTarget                  string
	FunctionGatewayURL              string
	FunctionGatewayToken            string
	FunctionGatewayTimeout          time.Duration
	FunctionInvocationWorkers       int
	VolumeReclaimWorkers            int
	VolumeReclaimWorkersPerNode     int
	FunctionBundleBaseURL           string
	FunctionBundleToken             string
	RolloutWorkerToken              string
	ArtifactS3Endpoint              string
	ArtifactS3Region                string
	ArtifactS3Bucket                string
	ArtifactS3AccessKey             string
	ArtifactS3SecretKey             string
	ArtifactS3UsePathStyle          bool
	ArtifactTicketSigningKey        string
	ResourcePolicy                  resourcekernel.AdmissionPolicy

	NodeLifecycle   nodebridge.LifecycleClient
	ImageResolver   environmentkernel.ImageResolver
	FunctionInvoker appfunction.FunctionInvoker
}

type App struct {
	registry                        *nodekernel.Registry
	placement                       *placement.Engine
	catalog                         *catalog.Store
	nodeStore                       nodekernel.Store
	nodeLifecycle                   nodebridge.LifecycleClient
	nodeBridge                      *nodebridge.Bridge
	heartbeatFreshnessWindow        time.Duration
	summaryFreshnessWindow          time.Duration
	reconcileInterval               time.Duration
	reconcileTimeout                time.Duration
	serviceRecoveryInterval         time.Duration
	serviceReconcileWorkers         int
	serviceAllocationGlobalWorkers  int
	serviceAllocationWorkersPerNode int
	now                             func() time.Time
	imageResolver                   environmentkernel.ImageResolver
	functionInvoker                 appfunction.FunctionInvoker
	functionBundleBaseURL           string
	functionBundleToken             string
	functionInvocationWorkers       int
	volumeReclaimWorkers            int
	volumeReclaimWorkersPerNode     int
	rolloutWorkerToken              string
	resourcePolicy                  resourcekernel.AdmissionPolicy

	db                      *postgres.DB
	adminPG                 *pgadmin.Store
	accessPG                *pgaccess.Store
	accessControl           *appaccess.Service
	agentProfilePG          *pgagentprofile.Store
	allocationOwners        *pgallocation.OwnerReader
	runStore                *pgrun.Store
	rolloutPG               *pgrollout.Store
	functionPG              *pgfunction.Store
	namespacePG             *pgnamespace.Store
	secretDB                *pgsecret.Store
	servicePG               *pgservice.PGStore
	tunnelPG                *pgtunnel.Store
	storage                 storagecoord.Coordinator
	storageAdmin            appadmin.StorageBindingStore
	storageHealth           appadmin.StorageHealthSource
	reconcileCtx            context.Context
	cancelReconcile         context.CancelFunc
	stopCh                  chan struct{}
	pendingServiceReconcile *serviceReconcileQueue
	allocationReconcileWake chan struct{}
	stopOnce                sync.Once
	wg                      sync.WaitGroup
	metrics                 []sdkobs.ObservableRegistration

	reconcileHealth      *reconcilekernel.HealthTracker
	runReconciler        apprun.Reconciler
	nodeReconciler       appnode.AvailabilityReconciler
	serviceReconciler    servicekernel.Reconciler
	allocationReconciler servicekernel.AllocationReconciler
	volumeReclaimWorker  servicekernel.VolumeReclaimDispatcher
	functionController   *appfunction.Controller

	adminAPI          *apiadminv1.Server
	identityAPI       *apiidentityv1.Server
	publicAPI         *publicv1.Server
	gatewayAPI        *apigatewayv1.Server
	nodeAPI           *apinodev1.Server
	relayAPI          *apirelayv1.Server
	rolloutWorkerAPI  *rolloutworkerv1.Server
	artifactAccessAPI *artifactaccessv1.Server
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
	if cfg.ServiceReconcileWorkers <= 0 {
		cfg.ServiceReconcileWorkers = defaultServiceReconcileWorkers
	}
	if cfg.PostgresMaxConnections <= 0 {
		cfg.PostgresMaxConnections = defaultPostgresMaxConnections
	}
	if cfg.FunctionInvocationWorkers <= 0 {
		cfg.FunctionInvocationWorkers = defaultFunctionInvocationWorkers
	}
	if cfg.VolumeReclaimWorkers <= 0 {
		cfg.VolumeReclaimWorkers = defaultVolumeReclaimWorkers
	}
	if cfg.VolumeReclaimWorkersPerNode <= 0 {
		cfg.VolumeReclaimWorkersPerNode = defaultVolumeReclaimWorkersPerNode
	}
	if cfg.VolumeReclaimWorkersPerNode > cfg.VolumeReclaimWorkers {
		cfg.VolumeReclaimWorkersPerNode = cfg.VolumeReclaimWorkers
	}
	if cfg.ServiceAllocationGlobalWorkers <= 0 {
		cfg.ServiceAllocationGlobalWorkers = defaultServiceAllocationGlobalWorkers
	}
	if cfg.ServiceAllocationWorkersPerNode <= 0 {
		cfg.ServiceAllocationWorkersPerNode = defaultServiceAllocationWorkersPerNode
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
		catalog:                         catalog.NewStore(cfg.RuntimeTemplates),
		heartbeatFreshnessWindow:        cfg.HeartbeatFreshnessWindow,
		summaryFreshnessWindow:          cfg.SummaryFreshnessWindow,
		reconcileInterval:               cfg.ReconcileInterval,
		reconcileTimeout:                cfg.ReconcileTimeout,
		serviceRecoveryInterval:         defaultServiceRecoveryInterval,
		serviceReconcileWorkers:         cfg.ServiceReconcileWorkers,
		serviceAllocationGlobalWorkers:  cfg.ServiceAllocationGlobalWorkers,
		serviceAllocationWorkersPerNode: cfg.ServiceAllocationWorkersPerNode,
		resourcePolicy:                  cfg.ResourcePolicy,
		functionBundleBaseURL:           strings.TrimSpace(cfg.FunctionBundleBaseURL),
		functionBundleToken:             strings.TrimSpace(cfg.FunctionBundleToken),
		functionInvocationWorkers:       cfg.FunctionInvocationWorkers,
		volumeReclaimWorkers:            cfg.VolumeReclaimWorkers,
		volumeReclaimWorkersPerNode:     cfg.VolumeReclaimWorkersPerNode,
		rolloutWorkerToken:              strings.TrimSpace(cfg.RolloutWorkerToken),
		now: func() time.Time {
			return time.Now().UTC()
		},
		reconcileCtx:            reconcileCtx,
		cancelReconcile:         cancelReconcile,
		stopCh:                  make(chan struct{}),
		pendingServiceReconcile: newServiceReconcileQueue(),
		allocationReconcileWake: make(chan struct{}, 1),
		reconcileHealth: reconcilekernel.NewHealthTracker(
			reconcilekernel.ComponentRun,
			reconcilekernel.ComponentNode,
			reconcilekernel.ComponentService,
			reconcilekernel.ComponentAllocation,
			reconcilekernel.ComponentTunnel,
			reconcilekernel.ComponentFunction,
			reconcilekernel.ComponentRollout,
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
		a.nodeLifecycle = nodebridge.NewGRPCClient()
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
	a.allocationOwners = pgallocation.NewOwnerReader(db.Pool())
	a.nodeStore = pgnodes.NewPGStore(db)
	a.namespacePG = pgnamespace.NewStore(db)
	a.runStore = pgrun.NewStore(db, pgrun.WithAdmissionPolicy(a.resourcePolicy))
	a.functionPG = pgfunction.NewStore(db, cfg.FunctionInvocationWorkers)
	masterKey, err := secretkernel.NormalizeMasterKey(cfg.SecretsMasterKey)
	if err != nil {
		return err
	}
	a.secretDB, err = pgsecret.NewStore(db, masterKey)
	if err != nil {
		return err
	}
	a.agentProfilePG = pgagentprofile.NewStore(db, a.secretDB)
	rolloutOptions := []pgrollout.Option{pgrollout.WithNow(a.now)}
	if strings.TrimSpace(cfg.ArtifactS3Bucket) != "" {
		store, err := artifactstore.NewS3(context.Background(), artifactstore.S3Config{Endpoint: cfg.ArtifactS3Endpoint, Region: cfg.ArtifactS3Region, Bucket: cfg.ArtifactS3Bucket, AccessKey: cfg.ArtifactS3AccessKey, SecretKey: cfg.ArtifactS3SecretKey, UsePathStyle: cfg.ArtifactS3UsePathStyle})
		if err != nil {
			return err
		}
		rolloutOptions = append(rolloutOptions, pgrollout.WithArtifactStore(store))
		ticketKey, err := pgrollout.NormalizeArtifactTicketKey(cfg.ArtifactTicketSigningKey)
		if err != nil {
			return err
		}
		rolloutOptions = append(rolloutOptions, pgrollout.WithArtifactTicketKey(ticketKey))
	}
	a.rolloutPG = pgrollout.NewStore(db, a.agentProfilePG, a.secretDB, rolloutOptions...)
	if strings.TrimSpace(cfg.ArtifactS3Bucket) != "" {
		a.artifactAccessAPI = artifactaccessv1.New(a.rolloutPG)
	}
	a.servicePG = pgservice.NewPGStore(db, pgservice.WithAdmissionPolicy(a.resourcePolicy))
	relays, err := pgtunnel.ParseRelays(cfg.TunnelRelays)
	if err != nil {
		return err
	}
	a.tunnelPG = pgtunnel.NewStore(db, cfg.TunnelEdgeTarget, cfg.TunnelNodeEdgeTarget, pgtunnel.WithRelays(relays), pgtunnel.WithMasterKey(masterKey))
	a.nodeBridge = nodebridge.New(a.nodeLifecycle, nodebridge.Config{
		DefaultRuntime:      defaultSandboxRuntime,
		SecretValues:        a.secretDB,
		RegistryCredentials: a.secretDB,
	})
	if cfg.StoragedTarget != "" {
		storageClient, err := storagecoord.NewClient(cfg.StoragedTarget)
		if err != nil {
			return err
		}
		a.storage = storageClient
		a.storageAdmin = storageClient
		a.storageHealth = storageClient
	}
	if cfg.FunctionInvoker != nil {
		a.functionInvoker = cfg.FunctionInvoker
	} else if strings.TrimSpace(cfg.FunctionGatewayURL) != "" {
		invoker, err := functiondispatch.NewGateway(functiondispatch.GatewayConfig{
			URL:     cfg.FunctionGatewayURL,
			Token:   cfg.FunctionGatewayToken,
			Timeout: cfg.FunctionGatewayTimeout,
		})
		if err != nil {
			return err
		}
		a.functionInvoker = invoker
	}
	a.runReconciler = apprun.NewReconciler(a.runStore, a.nodeBridge)
	a.rolloutPG.StartNotifications()
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
		if a.servicePG != nil {
			a.servicePG.Close()
		}
		if a.runStore != nil {
			a.runStore.Close()
		}
		if a.functionPG != nil {
			a.functionPG.Close()
		}
		if a.rolloutPG != nil {
			a.rolloutPG.Close()
		}
		if closer, ok := a.storage.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if a.db != nil {
			a.db.Close()
		}
	})
	return nil
}
