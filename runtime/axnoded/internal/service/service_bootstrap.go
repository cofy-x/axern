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
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	ebpfnetwork "github.com/cofy-x/axern/runtime/axnoded/internal/network/ebpf"
	nodecapabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodestate"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	servicecontrolplane "github.com/cofy-x/axern/runtime/axnoded/internal/service/controlplane"
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
	config       config.Config
	runscHandler contract.SandboxRuntime

	containerManager *container.Manager

	store nodeStateStore

	environmentCache  *environmentcache.EnvironmentCache
	egressClient      egress.Manager
	egressCloser      io.Closer
	sandboxAccess     *sandboxaccess.Accessor
	sandboxTargets    *sandboxtarget.Resolver
	networking        *servicenetworking.Coordinator
	processController *process.Controller
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
	executionLeaseCancel      context.CancelFunc
	executionLeaseWG          sync.WaitGroup
	nodeIdentityCancel        context.CancelFunc
	nodeBootstrapTokenFile    string
	nodeIdentityWG            sync.WaitGroup
	controlPlaneReports       *servicecontrolplane.Coordinator
	allocationLifecycleOutbox *nodecontrol.AllocationLifecycleOutbox

	outputRetentionCancel context.CancelFunc
	outputRetentionWG     sync.WaitGroup

	ready atomic.Bool

	shutdownOnce sync.Once
	shutdownErr  error
}

type nodeStateStore interface {
	SaveSnapshot(bucket string, value proto.Message) error
	LoadSnapshot(bucket string, value proto.Message) error
	PutRecord(bucket, key string, value proto.Message) error
	GetRecord(bucket, key string, value proto.Message) error
	DeleteRecord(bucket, key string) error
	ForEachRecord(bucket string, visit func(key string, value []byte) error) error
	Close() error
}

// NewSandboxService creates a new sandbox service from an already parsed config.
func NewSandboxService(ctx context.Context, cfg config.Config, bootstrapTokenFile string) (NodeService, error) {
	if ctx == nil {
		return nil, fmt.Errorf("sandbox service context is required")
	}
	if err := cfg.ValidateNodeIdentity(); err != nil {
		return nil, err
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

	s.nodeBootstrapTokenFile = bootstrapTokenFile
	healthChan, err := s.initContainerRuntime(ctx)
	if err != nil {
		s.closeAfterInitializationFailure()
		return nil, err
	}
	if err := s.configureServiceCollaborators(); err != nil {
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
	retentionTTL, err := cfg.PluginConfig.RuntimeConfig.IdleEnvironmentRetentionTTLDuration()
	if err != nil {
		return nil, err
	}
	retentionMax := cfg.PluginConfig.RuntimeConfig.IdleEnvironmentRetentionMaxValue()
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
		config:           cfg,
		store:            stateDB,
		environmentCache: environmentcache.NewEnvironmentCache(environmentcache.NewDefaultMounter(imageManagerEnabled, imageManagerSocket)),
		egressClient:     egressClient,
		egressCloser:     egressClient,
	}
	if cfg.PluginConfig.ControlPlaneTargetValue() != "" {
		s.allocationLifecycleOutbox = nodecontrol.NewAllocationLifecycleOutbox(stateDB)
	}
	s.capabilityReconcileCtx, s.capabilityReconcileCancel = context.WithCancel(context.Background())
	s.environmentCache.ConfigureRetention(retentionTTL, retentionMax)
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
	if h.runscHandler != nil {
		h.runscHandler.ShutDown()
	}
	if h.environmentCache != nil {
		h.environmentCache.Close()
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

func (h *sandboxService) configureServiceCollaborators() error {
	if h == nil {
		return fmt.Errorf("configure service collaborators: sandbox service is required")
	}
	if h.runscHandler == nil {
		return fmt.Errorf("configure service collaborators: runsc handler is required")
	}
	if h.containerManager == nil {
		return fmt.Errorf("configure service collaborators: container manager is required")
	}
	if h.store == nil {
		return fmt.Errorf("configure service collaborators: node state store is required")
	}
	if h.environmentCache == nil {
		return fmt.Errorf("configure service collaborators: environment cache is required")
	}
	if h.egressClient == nil {
		return fmt.Errorf("configure service collaborators: egress manager is required")
	}
	h.configureSandboxTargets()
	h.configureSandboxAccess()
	h.configureNetworking()
	h.configureProcessController()
	h.configureSandboxControl()
	h.configureAllocationController()
	h.configureControlPlaneReports()
	return nil
}

func (h *sandboxService) restorePersistentState() error {
	inventory, err := h.collectRuntimeInventory(context.Background())
	if err != nil {
		return err
	}
	if err := h.containerManager.ValidateRuntimeInventory(inventory.allIDs()); err != nil {
		return fmt.Errorf("validate persisted container inventory: %w", err)
	}
	recoveryRecords, err := h.allocationController().InspectRecoveryRecords()
	if err != nil {
		return fmt.Errorf("validate persisted allocation authority: %w", err)
	}
	if err := h.cleanupInterruptedAllocationStarts(context.Background(), inventory, recoveryRecords); err != nil {
		return err
	}
	persistedAllocations := recoveryRecords.Intents
	durableInventory, discardInventory, err := h.partitionRuntimeInventory(inventory, persistedAllocations)
	if err != nil {
		return err
	}
	if err := h.recoverTerminalRuntimeCheckpoints(context.Background(), durableInventory); err != nil {
		return err
	}
	if err := h.seedTerminalAllocationLifecycleOutbox(persistedAllocations); err != nil {
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
	if reconciler, ok := h.runscHandler.(contract.RuntimeArtifactReconciler); ok {
		if err := reconciler.ReconcileRuntimeArtifacts(context.Background(), retained.allIDs()); err != nil {
			return fmt.Errorf("reconcile runsc runtime artifacts: %w", err)
		}
	}
	if err := h.containerManager.ReconcileRuntimeInventory(retained.allIDs()); err != nil {
		return fmt.Errorf("reconcile persisted container inventory: %w", err)
	}
	for id, state := range retained {
		if state != nil && state.Status == contract.ContainerStatusRunning {
			if err := h.containerManager.SyncRuntimeIdentityFromState(id, state); err != nil {
				return fmt.Errorf("restore runtime identity for allocation %s: %w", id, err)
			}
		}
	}
	if err := h.containerManager.ReconcileResourceClaims(); err != nil {
		return fmt.Errorf("reconcile persisted resource claims: %w", err)
	}
	return nil
}

// cleanupInterruptedAllocationStarts closes both create crash windows:
//
//   - a durable intent with no runsc container never reached OCI create; and
//   - a runsc container in created state never crossed OCI start.
//
// An unverified running or unknown container violates the create-before-start
// ordering and is retained fail-closed for operator inspection. Terminal
// containers are retained only when verified enforcement makes their exit
// evidence reportable to controld.
func (h *sandboxService) cleanupInterruptedAllocationStarts(ctx context.Context, inventory runtimeInventory, records allocation.RecoveryRecords) error {
	ids := make([]string, 0, len(records.Intents))
	for id := range records.Intents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		state, live := inventory[id]
		status := contract.ContainerStatusUnknown
		if state != nil {
			status = state.Status
		}
		_, enforcementVerified := records.EnforcementVerified[id]
		cleanup, err := interruptedStartRecoveryAction(live, status, enforcementVerified)
		if err != nil {
			return fmt.Errorf("recover allocation %s: %w", id, err)
		}
		if !cleanup {
			continue
		}
		if err := h.allocationController().CleanupPersistedFailedStart(ctx, id); err != nil {
			return fmt.Errorf("cleanup interrupted allocation start %s: %w", id, err)
		}
		delete(inventory, id)
		delete(records.Intents, id)
		delete(records.EnforcementVerified, id)
	}
	return nil
}

func interruptedStartRecoveryAction(live bool, status contract.ContainerStatus, enforcementVerified bool) (bool, error) {
	if !live || status == contract.ContainerStatusCreated {
		return true, nil
	}
	if enforcementVerified {
		return false, nil
	}
	if status == contract.ContainerStatusExited {
		return true, nil
	}
	return false, fmt.Errorf("unverified runtime container has uncertain execution state %q", status)
}

// partitionRuntimeInventory derives recovery ownership from the admitted
// Allocation record. Runtime metadata is never an ownership authority.
func (h *sandboxService) partitionRuntimeInventory(inventory runtimeInventory, persistedAllocations map[string]struct{}) (runtimeInventory, runtimeInventory, error) {
	durable := make(runtimeInventory, len(inventory))
	discard := make(runtimeInventory, len(inventory))
	for id, state := range inventory {
		_, hasState := persistedAllocations[id]
		if hasState {
			durable[id] = state
		} else {
			discard[id] = state
		}
	}
	return durable, discard, nil
}

func (h *sandboxService) cleanupDiscardOnRestartContainers(ctx context.Context, inventory runtimeInventory) error {
	ids := make([]string, 0, len(inventory))
	for id := range inventory {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := h.runscHandler.DeleteContainer(ctx, &runtimeapi.DeleteContainerRequest{ID: id, Timeout: 0}, contract.HandlerOptions{ContainerID: id, ForceDelete: true}); err != nil && !allocation.IsDeleteNotFound(err) {
			return fmt.Errorf("delete discard-on-restart runsc container %s: %w", id, err)
		}
	}
	return nil
}

type runtimeInventory map[string]*contract.UnionContainerState

func (h *sandboxService) collectRuntimeInventory(ctx context.Context) (runtimeInventory, error) {
	states, err := h.runscHandler.ListContainers(ctx, contract.HandlerOptions{})
	if err != nil {
		return nil, fmt.Errorf("list runsc containers before persistent-state reconciliation: %w", err)
	}
	inventory := make(runtimeInventory, len(states))
	for _, state := range states {
		if state == nil || state.ID == "" {
			return nil, fmt.Errorf("runsc returned an invalid container inventory entry")
		}
		switch state.Status {
		case contract.ContainerStatusCreated, contract.ContainerStatusRunning, contract.ContainerStatusExited, contract.ContainerStatusUnknown:
		default:
			return nil, fmt.Errorf("runsc container %s returned invalid status %q", state.ID, state.Status)
		}
		if _, duplicate := inventory[state.ID]; duplicate {
			return nil, fmt.Errorf("runsc returned duplicate container %s", state.ID)
		}
		inventory[state.ID] = state
	}
	return inventory, nil
}

// recoverTerminalRuntimeCheckpoints closes the crash window where runsc has
// durably recorded an exit but axnoded stopped before writing its lifecycle
// checkpoint. Terminal runtime state must never be deleted until the wait result
// (including confirmed termination with unavailable exit status) is durable.
func (h *sandboxService) recoverTerminalRuntimeCheckpoints(ctx context.Context, inventory runtimeInventory) error {
	ids := make([]string, 0)
	for id, state := range inventory {
		if state != nil && state.Status == contract.ContainerStatusExited {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		item, err := h.containerManager.Get(id)
		if err != nil {
			return fmt.Errorf("load terminal runtime checkpoint for %s: %w", id, err)
		}
		if item == nil || item.Status == nil {
			return fmt.Errorf("load terminal runtime checkpoint for %s: checkpoint unavailable", id)
		}
		if item.Status.Get().State() == runtimeapi.ContainerState_CONTAINER_EXITED {
			continue
		}
		exit, err := h.runscHandler.Wait(ctx, contract.HandlerOptions{ContainerID: id})
		if err != nil && !contract.IsExitStatusUnavailable(err) {
			return fmt.Errorf("recover runtime exit for %s: %w", id, err)
		}
		event := container.Event{Type: container.EventTypeExit, ContainerID: id, ExitedAt: exit.Timestamp}
		if err != nil {
			// Like the live monitor, preserve confirmed termination without
			// fabricating an exit code after a host/runtime crash.
			event.Reason = err.Error()
		} else {
			exitCode := int32(exit.Status)
			event.ExitCode = &exitCode
		}
		if _, err := h.containerManager.CheckpointRuntimeExit(event); err != nil {
			return fmt.Errorf("checkpoint recovered runtime exit for %s: %w", id, err)
		}
	}
	return nil
}

func (h *sandboxService) cleanupTerminalRuntimeContainers(ctx context.Context, inventory runtimeInventory) error {
	ids := make([]string, 0)
	for id, state := range inventory {
		if state != nil && state.Status == contract.ContainerStatusExited {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := h.runscHandler.DeleteContainer(ctx, &runtimeapi.DeleteContainerRequest{Timeout: 0}, contract.HandlerOptions{
			ContainerID: id,
			ForceDelete: true,
		}); err != nil {
			return fmt.Errorf("delete terminal runsc container %s before persistent-state reconciliation: %w", id, err)
		}
	}
	return nil
}

func (i runtimeInventory) retained() runtimeInventory {
	result := make(runtimeInventory, len(i))
	for id, state := range i {
		if state != nil && state.Status != contract.ContainerStatusExited {
			result[id] = state
		}
	}
	return result
}

func (i runtimeInventory) allIDs() map[string]struct{} {
	result := make(map[string]struct{}, len(i))
	for id := range i {
		result[id] = struct{}{}
	}
	return result
}
