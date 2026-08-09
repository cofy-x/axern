package allocation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	servicenetworking "github.com/cofy-x/axern/runtime/axnoded/internal/service/networking"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/probes"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	servicevolumes "github.com/cofy-x/axern/runtime/axnoded/internal/service/volumes"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type stateStore interface {
	PutRecord(bucket, key string, value proto.Message) error
	DeleteRecord(bucket, key string) error
	ForEachRecord(bucket string, visit func(key string, value []byte) error) error
}

type Options struct {
	Config           config.Config
	Store            stateStore
	ContainerManager func() *container.Manager
	RuntimeHandler   func(string) (contract.RuntimeHandler, error)
	LangRuntime      *langrtmanager.LangRTManager
	Volumes          *servicevolumes.Coordinator
	Networking       *servicenetworking.Coordinator
	Probes           *probes.Coordinator
	StartMetricSink  StartMetricSink
	ReportStatus     func(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time)
	InventoryChanged func()
}

type Controller struct {
	config config.Config
	store  stateStore

	containerManager func() *container.Manager
	runtimeHandlerFn func(string) (contract.RuntimeHandler, error)
	lrtManager       *langrtmanager.LangRTManager
	volumes          *servicevolumes.Coordinator
	networking       *servicenetworking.Coordinator
	probes           *probes.Coordinator
	startMetricSink  StartMetricSink
	reportStatus     func(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time)
	inventoryChanged func()

	stateMu          sync.RWMutex
	allocationStates map[string]*allocationState

	allocationLifecycleLocks allocationLifecycleLocks
}

func NewController(options Options) *Controller {
	c := &Controller{
		config:           options.Config,
		store:            options.Store,
		containerManager: options.ContainerManager,
		runtimeHandlerFn: options.RuntimeHandler,
		lrtManager:       options.LangRuntime,
		volumes:          options.Volumes,
		networking:       options.Networking,
		probes:           options.Probes,
		startMetricSink:  options.StartMetricSink,
		reportStatus:     options.ReportStatus,
		inventoryChanged: options.InventoryChanged,
		allocationStates: make(map[string]*allocationState),
	}
	if c.startMetricSink == nil {
		c.startMetricSink = DefaultStartMetricSink{}
	}
	return c
}

func (c *Controller) notifyInventoryChanged() {
	if c != nil && c.inventoryChanged != nil {
		c.inventoryChanged()
	}
}

func (c *Controller) Start(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	return c.startManagedContainer(ctx, request)
}

func (c *Controller) Delete(ctx context.Context, request *runtime.DeleteRequest) (*runtime.DeleteResponse, error) {
	return c.deleteManagedContainer(ctx, request)
}

func (c *Controller) CleanupFailedStart(ctx context.Context, allocationID string) error {
	return c.cleanupFailedStart(ctx, allocationID)
}

func (c *Controller) RestoreAllocationState() error {
	if err := c.loadAllocationStates(); err != nil {
		// Reconciliation is destructive: an incomplete recovery view must not
		// release leases that a still-running container may still be using.
		logrus.WithError(err).Warn("restore allocation state; skip mount lease reconciliation")
		return err
	}
	if err := c.lrtManager.ReconcileMountLeases(); err != nil {
		return fmt.Errorf("reconcile imagemgr mount leases: %w", err)
	}
	return nil
}

func (c *Controller) PrepareRuntimeTemplate(ctx context.Context, fr *runtime.RuntimeTemplate) (*langrtmanager.LanguageRuntime, error) {
	lrt, _, err := c.ensureLangRuntime(ctx, fr)
	return lrt, err
}

func (c *Controller) PrepareRuntimeTemplateWithSummary(ctx context.Context, fr *runtime.RuntimeTemplate) (*langrtmanager.LanguageRuntime, LangRuntimePrepareSummary, error) {
	return c.ensureLangRuntime(ctx, fr)
}

func (c *Controller) ScheduleExecutionEnvelopePrepare(lrt *langrtmanager.LanguageRuntime) {
	c.scheduleExecutionEnvelopePrepare(lrt)
}

func (c *Controller) CreateRuntimeContainer(ctx context.Context, lrt *langrtmanager.LanguageRuntime, templateRequest, createRequest *runtime.CreateContainerRequest, resourceSpec *commonv1.ResourceSpec, recorder contract.StartupPhaseRecorder) (*runtime.CreateContainerResponse, string, error) {
	return c.createContainer(ctx, lrt, templateRequest, createRequest, resourceSpec, recorder)
}

func (c *Controller) DeleteRuntimeContainer(ctx context.Context, containerID string) error {
	_, err := c.deleteContainer(ctx, &runtime.DeleteContainerRequest{ID: containerID, Timeout: 0})
	return err
}

func (c *Controller) DeleteRuntimeContainerWithHandler(ctx context.Context, request *runtime.DeleteContainerRequest, target *container.Container, handler contract.RuntimeHandler, traceID, spanID string) (*runtime.DeleteContainerResponse, error) {
	return c.deleteContainerWithRuntime(ctx, request, target, handler, traceID, spanID)
}

func (c *Controller) ConfigureStartPorts(ctx context.Context, containerID, containerIP string, ports []string) error {
	return c.configureStartPorts(ctx, containerID, containerIP, ports)
}

func (c *Controller) runtimeMapping(containerID string) (*langrtmanager.LanguageRuntime, bool) {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	state := c.allocationStates[containerID]
	if state == nil || state.runtime == nil {
		return nil, false
	}
	return state.runtime, true
}

func (c *Controller) runtimeMappingCount() int {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	count := 0
	for _, state := range c.allocationStates {
		if state.runtime != nil {
			count++
		}
	}
	return count
}

func (c *Controller) SetStartMetricSink(sink StartMetricSink) {
	c.startMetricSink = sink
}

func (c *Controller) containers() *container.Manager {
	if c == nil || c.containerManager == nil {
		return nil
	}
	return c.containerManager()
}

func (c *Controller) runtimeHandler(runtimeName string) (contract.RuntimeHandler, error) {
	if c == nil || c.runtimeHandlerFn == nil {
		return nil, fmt.Errorf("runtime %s is not supported", runtimeName)
	}
	return c.runtimeHandlerFn(runtimeName)
}

func (c *Controller) checkRuntime(requestRuntime string) error {
	_, err := c.runtimeHandler(requestRuntime)
	return err
}

func (c *Controller) runtimeHandlerForContainer(id string) (*container.Container, contract.RuntimeHandler, error) {
	manager := c.containers()
	if manager == nil {
		return nil, nil, fmt.Errorf("container manager unavailable")
	}
	target, err := manager.Get(id)
	if err != nil {
		return nil, nil, err
	}
	if target.Metadata == nil {
		return nil, nil, errord.ErrInvalidContainer
	}
	handler, err := c.runtimeHandler(target.Metadata.RuntimeHandler)
	if err != nil {
		return nil, nil, err
	}
	return target, handler, nil
}

func (c *Controller) nodeVolumes() *servicevolumes.Coordinator {
	if c == nil {
		return nil
	}
	return c.volumes
}

func (c *Controller) sandboxNetworking() *servicenetworking.Coordinator {
	if c == nil {
		return nil
	}
	return c.networking
}

func (c *Controller) startReadinessWorker(containerID string, extraConfig startplan.ExtraConfig) {
	if c == nil || c.probes == nil {
		return
	}
	c.probes.StartReadiness(containerID, extraConfig.AllocationAttempt, extraConfig.ReadinessProbe)
}

func (c *Controller) startLivenessWorker(containerID string, extraConfig startplan.ExtraConfig) {
	if c == nil || c.probes == nil {
		return
	}
	c.probes.StartLiveness(containerID, extraConfig.AllocationAttempt, extraConfig.LivenessProbe)
}

func (c *Controller) stopReadinessWorker(containerID string) {
	if c == nil || c.probes == nil {
		return
	}
	c.probes.StopReadiness(containerID)
}

func (c *Controller) stopLivenessWorker(containerID string) {
	if c == nil || c.probes == nil {
		return
	}
	c.probes.StopLiveness(containerID)
}

func (c *Controller) reportStartRunningStatus(containerID string, extraConfig startplan.ExtraConfig, observedAt time.Time) {
	if c == nil || c.reportStatus == nil || extraConfig.AllocationAttempt <= 0 || strings.TrimSpace(containerID) == "" {
		return
	}
	if extraConfig.ReadinessProbe != nil {
		return
	}
	c.reportStatus(containerID, extraConfig.AllocationAttempt, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, 0, false, true, "", "", observedAt)
}
