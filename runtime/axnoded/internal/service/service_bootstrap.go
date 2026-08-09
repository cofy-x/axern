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
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/probes"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/process"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxaccess"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxcontrol"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxtarget"
	servicevolumes "github.com/cofy-x/axern/runtime/axnoded/internal/service/volumes"
	"github.com/cofy-x/axern/runtime/axnoded/internal/volume"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

var _ SandboxService = &sandboxService{}

// sandboxService is the NodeSandbox-facing facade assembled by NewSandboxService.
type sandboxService struct {
	config          config.Config
	runtimeHandlers *handlerregistry.Registry

	containerManager *container.Manager

	store nodeStateStore

	lrtManager        *langrtmanager.LangRTManager
	volumeClient      volume.Publisher
	volumeCloser      io.Closer
	volumes           *servicevolumes.Coordinator
	sandboxAccess     *sandboxaccess.Accessor
	sandboxTargets    *sandboxtarget.Resolver
	networking        *servicenetworking.Coordinator
	processController *process.Controller
	imageProcesses    *imageprocess.Controller
	sandboxController *sandboxcontrol.Controller
	allocations       *allocation.Controller

	probeCoordinator *probes.Coordinator
	probeAdapter     *probes.Adapter

	nodeInventorySource       *nodeinventory.AxnodedSource
	inventoryCollector        *nodeinventory.Collector
	capabilityManager         *nodecapabilitymanager.Manager
	capabilityRefreshCancel   context.CancelFunc
	capabilityRefreshWG       sync.WaitGroup
	capabilityReconcileMu     sync.Mutex
	capabilityReconciling     map[string]struct{}
	capabilityReconcileCtx    context.Context
	capabilityReconcileCancel context.CancelFunc
	controlPlaneReports       *servicecontrolplane.Coordinator

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
func NewSandboxService(cfg config.Config) (NodeOperatorService, error) {
	if err := configureNodeNetwork(cfg); err != nil {
		return nil, err
	}

	s, err := newSandboxServiceState(cfg)
	if err != nil {
		return nil, err
	}

	healthChan, err := s.initContainerRuntime()
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
	volumeDialCtx, cancelVolumeDial := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelVolumeDial()
	volumeClient, err := volume.Dial(volumeDialCtx, cfg.PluginConfig.RuntimeConfig.VolumeManagerSocketPath())
	if err != nil {
		return nil, err
	}
	stateDB, err := nodestate.Open(filepath.Join(cfg.StoreDir, "metadata.db"))
	if err != nil {
		_ = volumeClient.Close()
		return nil, err
	}

	s := &sandboxService{
		config:          cfg,
		store:           stateDB,
		runtimeHandlers: handlerregistry.New(cfg),
		lrtManager:      langrtmanager.NewLanguageRuntimeManager(langrtmanager.NewDefaultMounter(imageManagerEnabled, imageManagerSocket)),
		volumeClient:    volumeClient,
		volumeCloser:    volumeClient,
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
		h.containerManager.Stop()
	}
	if h.runtimeHandlers != nil {
		for item := range h.runtimeHandlers.Map().IterBuffered() {
			item.Val.ShutDown()
		}
	}
	if h.lrtManager != nil {
		h.lrtManager.Close()
	}
	h.closeVolume()
	h.closeNodeState()
}

func (h *sandboxService) configureServiceCollaborators() {
	h.configureProbeCoordinator()
	h.configureVolumeCoordinator()
	h.configureSandboxTargets()
	h.configureSandboxAccess()
	h.configureNetworking()
	h.configureProcessController()
	h.configureSandboxControl()
	h.configureControlPlaneReports()
	h.configureAllocationController()
	h.configureImageProcesses()
}

func (h *sandboxService) restorePersistentState() error {
	inventory, err := h.collectRuntimeInventory(context.Background())
	if err != nil {
		return err
	}
	retained := inventory.retained()
	if err := h.containerManager.ValidateRuntimeInventory(retained.allByRuntime()); err != nil {
		return fmt.Errorf("validate persisted container inventory: %w", err)
	}
	if err := h.cleanupTerminalRuntimeContainers(context.Background(), inventory); err != nil {
		return err
	}
	if err := h.allocationController().RestoreAllocationState(retained.allIDs()); err != nil {
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
