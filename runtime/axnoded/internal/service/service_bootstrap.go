package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	ebpfnetwork "github.com/cofy-x/axern/runtime/axnoded/internal/network/ebpf"
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

	nodeInventorySource *nodeinventory.AxnodedSource
	inventoryCollector  *nodeinventory.Collector
	controlPlaneReports *servicecontrolplane.Coordinator

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
	if err := h.allocationController().RestoreAllocationState(); err != nil {
		return err
	}
	activeIDs := h.allocationController().ActiveAllocationIDs()
	for _, handler := range h.containerManager.Handlers() {
		reconciler, ok := handler.(contract.PersistentStorageReconciler)
		if !ok {
			continue
		}
		if err := reconciler.ReconcilePersistentStorage(context.Background(), activeIDs); err != nil {
			return fmt.Errorf("reconcile %s persistent runtime storage: %w", handler.Name(), err)
		}
	}
	h.sandboxNetworking().LoadDnatRules()
	return nil
}
