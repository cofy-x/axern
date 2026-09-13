package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	nodecontrol "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/egress"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	ebpfnetwork "github.com/cofy-x/axern/runtime/axnoded/internal/network/ebpf"
	nodecapabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodestate"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	servicecontrolplane "github.com/cofy-x/axern/runtime/axnoded/internal/service/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/imageprocess"
	servicenetworking "github.com/cofy-x/axern/runtime/axnoded/internal/service/networking"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/process"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxaccess"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxcontrol"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxtarget"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

var _ NodeService = &sandboxService{}

// sandboxService is the NodeSandbox-facing facade assembled by NewSandboxService.
type sandboxService struct {
	config          config.Config
	runtimeHandlers *handlerregistry.Registry

	containerManager *container.Manager

	store nodeStateStore

	lrtManager        *langrtmanager.LangRTManager
	egressClient      egress.Manager
	egressCloser      io.Closer
	sandboxAccess     *sandboxaccess.Accessor
	sandboxTargets    *sandboxtarget.Resolver
	networking        *servicenetworking.Coordinator
	processController *process.Controller
	imageProcesses    *imageprocess.Controller
	sandboxController *sandboxcontrol.Controller
	allocations       *allocation.Controller

	nodeInventorySource       *nodeinventory.AxnodedSource
	inventoryCollector        *nodeinventory.Collector
	capabilityManager         *nodecapabilitymanager.Manager
	capabilityRefreshCancel   context.CancelFunc
	capabilityRefreshWG       sync.WaitGroup
	capabilityReconcileMu     sync.Mutex
	capabilityReconciling     map[string]bool
	capabilityReconcileActive int
	capabilityReconcileCtx    context.Context
	capabilityReconcileCancel context.CancelFunc
	capabilityReconcileWG     sync.WaitGroup
	controlPlaneReports       *servicecontrolplane.Coordinator
	allocationLifecycleOutbox *nodecontrol.AllocationLifecycleOutbox
	memoryObservationMu       sync.Mutex
	memoryObservationNext     int64
	memoryObservationReserved int64

	ready atomic.Bool

	shutdownOnce sync.Once
	shutdownErr  error
}

type nodeStateStore interface {
	SaveSnapshot(bucket string, value proto.Message) error
	LoadSnapshot(bucket string, value proto.Message) error
	PutRecord(bucket, key string, value proto.Message) error
	DeleteRecord(bucket, key string) error
	ForEachRecord(bucket string, visit func(key string, value []byte) error) error
	Close() error
}

// NewSandboxService creates a new sandbox service from an already parsed config.
func NewSandboxService(ctx context.Context, cfg config.Config) (NodeService, error) {
	if ctx == nil {
		return nil, fmt.Errorf("sandbox service context is required")
	}
	if err := validateMemoryBoundaryConfiguration(cfg); err != nil {
		return nil, err
	}
	networkConfig, err := cfg.PluginConfig.NetworkConfig.Normalized()
	if err != nil {
		return nil, fmt.Errorf("normalize network config: %w", err)
	}
	cfg.PluginConfig.NetworkConfig = networkConfig
	if err := configureNodeNetwork(cfg); err != nil {
		return nil, err
	}

	s, err := newSandboxServiceState(cfg)
	if err != nil {
		return nil, err
	}

	healthChan, err := s.initContainerRuntime(ctx)
	if err != nil {
		s.closeAfterInitializationFailure()
		return nil, err
	}
	if err := s.restorePersistentState(); err != nil {
		s.closeAfterInitializationFailure()
		return nil, err
	}
	if err := s.initNodeInventory(); err != nil {
		s.closeAfterInitializationFailure()
		return nil, err
	}
	if err := s.initControlPlaneReporter(); err != nil {
		s.closeAfterInitializationFailure()
		return nil, err
	}
	s.watchContainerReadiness(healthChan)
	return s, nil
}

// validateMemoryBoundaryConfiguration is deliberately called before network,
// state-store, runtime, or cgroup initialization. A production memory boundary
// with no qualified node reserve is a configuration error, not a late
// readiness condition that may leave host side effects behind.
func validateMemoryBoundaryConfiguration(cfg config.Config) error {
	if _, err := cfg.PluginConfig.ResourceConfig.CgroupRootNameValue(); err != nil {
		return err
	}
	mode, err := cfg.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
	if err != nil {
		return err
	}
	reserve := cfg.PluginConfig.ResourceConfig.MemorySystemReserveBytes
	switch mode {
	case config.CgroupEnforcementRequired:
		if reserve < config.RuntimeConformanceMemoryMaxBytes {
			return fmt.Errorf("memory_system_reserve_bytes must be at least %d bytes when cgroup_enforcement=required so runtime certification remains outside sandbox capacity", config.RuntimeConformanceMemoryMaxBytes)
		}
	case config.CgroupEnforcementDisabledDev:
		if reserve != 0 {
			return fmt.Errorf("memory_system_reserve_bytes must be zero when cgroup_enforcement=disabled_dev")
		}
	default:
		return fmt.Errorf("unsupported cgroup enforcement mode %q", mode)
	}
	return nil
}

func configureNodeNetwork(cfg config.Config) error {
	if cfg.PluginConfig.NetworkConfig.NatBackend != config.NatBackendEBPF {
		return nil
	}
	return ebpfnetwork.Configure(cfg.PluginConfig.NetworkConfig.BPFNet)
}

func newSandboxServiceState(cfg config.Config) (*sandboxService, error) {
	imageManagerEnabled := cfg.PluginConfig.RuntimeConfig.ImageManagerEnabledValue()
	imageManagerSocket := cfg.PluginConfig.RuntimeConfig.ImageManagerSocketPath()
	retentionTTL, err := cfg.PluginConfig.RuntimeConfig.IdleRuntimeRetentionTTLDuration()
	if err != nil {
		return nil, err
	}
	retentionMax := cfg.PluginConfig.RuntimeConfig.IdleRuntimeRetentionMaxValue()
	egressClient, err := egress.Dial(context.Background(), cfg.PluginConfig.RuntimeConfig.EgressManagerSocketPath())
	if err != nil {
		return nil, err
	}
	stateDB, err := nodestate.Open(filepath.Join(cfg.StoreDir, "metadata.db"))
	if err != nil {
		_ = egressClient.Close()
		return nil, err
	}

	s := &sandboxService{
		config:          cfg,
		store:           stateDB,
		runtimeHandlers: handlerregistry.New(cfg),
		lrtManager:      langrtmanager.NewLanguageRuntimeManager(langrtmanager.NewDefaultMounter(imageManagerEnabled, imageManagerSocket)),
		egressClient:    egressClient,
		egressCloser:    egressClient,
	}
	if cfg.PluginConfig.ControlPlaneTargetValue() != "" {
		s.allocationLifecycleOutbox = nodecontrol.NewAllocationLifecycleOutbox(stateDB)
	}
	s.capabilityReconcileCtx, s.capabilityReconcileCancel = context.WithCancel(context.Background())
	s.configureServiceCollaborators()
	s.lrtManager.ConfigureRetention(retentionTTL, retentionMax)
	return s, nil
}

func (h *sandboxService) closeNodeState() {
	if h == nil || h.store == nil {
		return
	}
	if err := h.store.Close(); err != nil {
		logrus.WithError(err).Warn("close node state database")
	}
}

func (h *sandboxService) closeAfterInitializationFailure() {
	if h == nil {
		return
	}
	if h.containerManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := h.containerManager.Stop(ctx); err != nil {
			logrus.WithError(err).Warn("stop container manager after initialization failure")
			cancel()
			return
		}
		cancel()
	}
	if h.runtimeHandlers != nil {
		for item := range h.runtimeHandlers.Map().IterBuffered() {
			item.Val.ShutDown()
		}
	}
	if h.lrtManager != nil {
		h.lrtManager.Close()
	}
	h.closeEgress()
	h.closeNodeState()
}

func (h *sandboxService) closeEgress() {
	if h == nil || h.egressCloser == nil {
		return
	}
	if err := h.egressCloser.Close(); err != nil {
		logrus.WithError(err).Warn("close egressd client")
	}
}

func (h *sandboxService) configureServiceCollaborators() {
	h.configureSandboxTargets()
	h.configureSandboxAccess()
	h.configureNetworking()
	h.configureProcessController()
	h.configureSandboxControl()
	h.configureAllocationController()
	h.configureControlPlaneReports()
	h.configureImageProcesses()
}

func (h *sandboxService) restorePersistentState() error {
	inventory, err := h.collectRuntimeInventory(context.Background())
	if err != nil {
		return err
	}
	if err := h.containerManager.ValidateRuntimeInventory(inventory.allByRuntime()); err != nil {
		return fmt.Errorf("validate persisted container inventory: %w", err)
	}
	boundAllocations, err := h.allocationController().RestoreControlPlaneBindings()
	if err != nil {
		return fmt.Errorf("validate persisted control-plane allocation bindings: %w", err)
	}
	persistedAllocations, err := h.allocationController().PersistedAllocationIDs()
	if err != nil {
		return fmt.Errorf("validate persisted allocation authority: %w", err)
	}
	durableInventory, discardInventory, err := h.partitionRuntimeInventory(inventory, persistedAllocations, boundAllocations)
	if err != nil {
		return err
	}
	if err := h.seedTerminalAllocationLifecycleOutbox(boundAllocations); err != nil {
		return err
	}
	if err := h.cleanupTerminalRuntimeContainers(context.Background(), inventory); err != nil {
		return err
	}
	if err := h.cleanupDiscardOnRestartContainers(context.Background(), discardInventory.retained()); err != nil {
		return err
	}
	retained := durableInventory.retained()
	if err := h.allocationController().RestoreAllocationState(retained.allIDs()); err != nil {
		return err
	}
	if err := h.reconcileEgressPolicies(context.Background()); err != nil {
		return err
	}
	for _, handler := range h.containerManager.Handlers() {
		reconciler, ok := handler.(contract.PersistentStorageReconciler)
		if !ok {
			continue
		}
		if err := reconciler.ReconcilePersistentStorage(context.Background(), retained.forRuntime(handler.Name())); err != nil {
			return fmt.Errorf("reconcile %s persistent runtime storage: %w", handler.Name(), err)
		}
	}
	if err := h.containerManager.ReconcileRuntimeInventory(retained.allByRuntime()); err != nil {
		return fmt.Errorf("reconcile persisted container inventory: %w", err)
	}
	if err := h.containerManager.ReconcileResourceClaims(); err != nil {
		return fmt.Errorf("reconcile persisted resource claims: %w", err)
	}
	h.sandboxNetworking().LoadDnatRules()
	return nil
}

// partitionRuntimeInventory applies the explicit checkpoint recovery contract
// before any destructive action. Durable containers require both AllocationState
// and the independent controld admission binding. Session/self-test containers
// must be explicitly discardable and may never carry a control-plane binding.
func (h *sandboxService) partitionRuntimeInventory(inventory runtimeInventory, persistedAllocations, boundAllocations map[string]struct{}) (runtimeInventory, runtimeInventory, error) {
	durable := make(runtimeInventory, len(inventory))
	discard := make(runtimeInventory, len(inventory))
	for runtimeName, states := range inventory {
		durable[runtimeName] = make(map[string]contract.ContainerStatus)
		discard[runtimeName] = make(map[string]contract.ContainerStatus)
		for id, status := range states {
			item, err := h.containerManager.Get(id)
			if err != nil || item == nil || item.Metadata == nil {
				return nil, nil, fmt.Errorf("read recovery contract for runtime %s container %s", runtimeName, id)
			}
			_, hasState := persistedAllocations[id]
			_, bound := boundAllocations[id]
			switch item.Metadata.GetRecoveryMode() {
			case runtimeapi.ContainerRecoveryMode_CONTAINER_RECOVERY_MODE_DURABLE:
				if !hasState || !bound {
					return nil, nil, fmt.Errorf("durable runtime container %s is missing AllocationState or control-plane admission binding", id)
				}
				durable[runtimeName][id] = status
			case runtimeapi.ContainerRecoveryMode_CONTAINER_RECOVERY_MODE_DISCARD_ON_RESTART:
				if bound {
					return nil, nil, fmt.Errorf("discard-on-restart container %s has a control-plane admission binding", id)
				}
				discard[runtimeName][id] = status
			default:
				return nil, nil, fmt.Errorf("runtime container %s has no explicit recovery mode", id)
			}
		}
	}
	return durable, discard, nil
}

func (h *sandboxService) cleanupDiscardOnRestartContainers(ctx context.Context, inventory runtimeInventory) error {
	handlers := h.containerManager.Handlers()
	sort.Slice(handlers, func(i, j int) bool { return handlers[i].Name() < handlers[j].Name() })
	for _, handler := range handlers {
		ids := make([]string, 0, len(inventory[handler.Name()]))
		for id := range inventory[handler.Name()] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if _, err := handler.DeleteContainer(ctx, &runtimeapi.DeleteContainerRequest{ID: id, Timeout: 0}, contract.HandlerOptions{ContainerID: id, ForceDelete: true}); err != nil && !allocation.IsDeleteNotFound(err) {
				return fmt.Errorf("delete discard-on-restart %s container %s: %w", handler.Name(), id, err)
			}
		}
	}
	return nil
}

type runtimeInventory map[string]map[string]contract.ContainerStatus

func (h *sandboxService) collectRuntimeInventory(ctx context.Context) (runtimeInventory, error) {
	inventory := make(runtimeInventory, len(h.containerManager.Handlers()))
	owners := make(map[string]string)
	for _, handler := range h.containerManager.Handlers() {
		runtimeName := handler.Name()
		states, err := handler.ListContainers(ctx, contract.HandlerOptions{})
		if err != nil {
			return nil, fmt.Errorf("list %s containers before persistent-state reconciliation: %w", runtimeName, err)
		}
		ids := make(map[string]contract.ContainerStatus, len(states))
		for _, state := range states {
			if state == nil || state.ID == "" {
				return nil, fmt.Errorf("runtime %s returned an invalid container inventory entry", runtimeName)
			}
			if owner, duplicate := owners[state.ID]; duplicate {
				return nil, fmt.Errorf("container %s is reported by both %s and %s", state.ID, owner, runtimeName)
			}
			switch state.Status {
			case contract.ContainerStatusCreated, contract.ContainerStatusRunning, contract.ContainerStatusExited, contract.ContainerStatusUnknown:
			default:
				return nil, fmt.Errorf("runtime %s container %s returned invalid status %q", runtimeName, state.ID, state.Status)
			}
			owners[state.ID] = runtimeName
			ids[state.ID] = state.Status
		}
		inventory[runtimeName] = ids
	}
	return inventory, nil
}

func (h *sandboxService) cleanupTerminalRuntimeContainers(ctx context.Context, inventory runtimeInventory) error {
	handlers := h.containerManager.Handlers()
	sort.Slice(handlers, func(i, j int) bool { return handlers[i].Name() < handlers[j].Name() })
	for _, handler := range handlers {
		ids := make([]string, 0)
		for id, status := range inventory[handler.Name()] {
			if status == contract.ContainerStatusExited {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			if _, err := handler.DeleteContainer(ctx, &runtimeapi.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{
				ContainerID: id,
				ForceDelete: true,
			}); err != nil {
				return fmt.Errorf("delete terminal %s container %s before persistent-state reconciliation: %w", handler.Name(), id, err)
			}
		}
	}
	return nil
}

func (i runtimeInventory) retained() runtimeInventory {
	result := make(runtimeInventory, len(i))
	for runtimeName, states := range i {
		result[runtimeName] = make(map[string]contract.ContainerStatus)
		for id, status := range states {
			if status != contract.ContainerStatusExited {
				result[runtimeName][id] = status
			}
		}
	}
	return result
}

func (i runtimeInventory) forRuntime(runtimeName string) map[string]struct{} {
	result := make(map[string]struct{}, len(i[runtimeName]))
	for id := range i[runtimeName] {
		result[id] = struct{}{}
	}
	return result
}

func (i runtimeInventory) allByRuntime() map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{}, len(i))
	for runtimeName := range i {
		result[runtimeName] = i.forRuntime(runtimeName)
	}
	return result
}

func (i runtimeInventory) allIDs() map[string]struct{} {
	result := make(map[string]struct{})
	for _, ids := range i {
		for id := range ids {
			result[id] = struct{}{}
		}
	}
	return result
}
