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
	Config                      config.Config
	Store                       stateStore
	ContainerManager            func() *container.Manager
	RuntimeHandler              func(string) (contract.RuntimeHandler, error)
	LangRuntime                 *langrtmanager.LangRTManager
	Volumes                     *servicevolumes.Coordinator
	Networking                  *servicenetworking.Coordinator
	Probes                      *probes.Coordinator
	StartMetricSink             StartMetricSink
	ReportStatus                func(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time)
	InventoryChanged            func()
	RootfsCapabilityGate        func(context.Context, *runtime.StartRequest, string) error
	PreActivationCapabilityGate func(context.Context, *runtime.StartRequest, contract.ManagedRuntimeHandler, string) error
}

type Controller struct {
	config config.Config
	store  stateStore

	containerManager            func() *container.Manager
	runtimeHandlerFn            func(string) (contract.RuntimeHandler, error)
	lrtManager                  *langrtmanager.LangRTManager
	volumes                     *servicevolumes.Coordinator
	networking                  *servicenetworking.Coordinator
	probes                      *probes.Coordinator
	startMetricSink             StartMetricSink
	reportStatus                func(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time)
	inventoryChanged            func()
	rootfsCapabilityGate        func(context.Context, *runtime.StartRequest, string) error
	preActivationCapabilityGate func(context.Context, *runtime.StartRequest, contract.ManagedRuntimeHandler, string) error

	stateMu          sync.RWMutex
	allocationStates map[string]*allocationState

	allocationLifecycleLocks allocationKeyedLocks
	recordMutationLocks      allocationKeyedLocks
}

type internalConformanceContextKey struct{}
type nodeLocalStartContextKey struct{}

// StartInternalConformance runs the node-owned runtime self-test through the
// normal allocation workflow while carrying an unforgeable in-process marker.
// The marker cannot cross the lifecycle RPC boundary and therefore cannot be
// used by a workload to bypass capability admission.
func (h *Controller) StartInternalConformance(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	return h.Start(context.WithValue(ctx, internalConformanceContextKey{}, true), request)
}

func IsInternalConformance(ctx context.Context) bool {
	value, _ := ctx.Value(internalConformanceContextKey{}).(bool)
	return value
}

// WithNodeLocalStart marks an in-process operator-owned sandbox start. The
// marker is never represented in the lifecycle protobuf and therefore cannot
// cross a gRPC boundary. Node-local starts still use the ordinary capability
// admission and enforcement gates; the marker only selects their separate
// durable reporting ownership.
func WithNodeLocalStart(ctx context.Context) context.Context {
	return context.WithValue(ctx, nodeLocalStartContextKey{}, true)
}

func IsNodeLocalStart(ctx context.Context) bool {
	value, _ := ctx.Value(nodeLocalStartContextKey{}).(bool)
	return value
}

func NewController(options Options) *Controller {
	c := &Controller{
		config:                      options.Config,
		store:                       options.Store,
		containerManager:            options.ContainerManager,
		runtimeHandlerFn:            options.RuntimeHandler,
		lrtManager:                  options.LangRuntime,
		volumes:                     options.Volumes,
		networking:                  options.Networking,
		probes:                      options.Probes,
		startMetricSink:             options.StartMetricSink,
		reportStatus:                options.ReportStatus,
		inventoryChanged:            options.InventoryChanged,
		rootfsCapabilityGate:        options.RootfsCapabilityGate,
		preActivationCapabilityGate: options.PreActivationCapabilityGate,
		allocationStates:            make(map[string]*allocationState),
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

// LockAllocationLifecycle serializes the complete lifecycle contract for one
// allocation. The service facade uses this lock to keep capability admission,
// runtime creation, post-create verification, replay, and Delete in one lock
// domain. Callers must release the returned function and must not recursively
// acquire the same allocation.
func (c *Controller) LockAllocationLifecycle(allocationID string) func() {
	return c.allocationLifecycleLocks.Lock(allocationID)
}

// StartWithLifecycleHeld enters the runtime start workflow while the caller
// owns LockAllocationLifecycle for this allocation. It exists so the service
// facade can include its capability gates in the same critical section as the
// runtime side effects.
func (c *Controller) StartWithLifecycleHeld(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	if err := startplan.ValidateStartRequest(request); err != nil {
		return startErrorResponse(err.Error()), err
	}
	if strings.TrimSpace(request.GetContainerID()) == "" {
		return startErrorResponse("allocation id is required"), errord.ErrInvalidArgument
	}
	return c.startManagedContainerWithLifecycleHeld(ctx, request, false)
}

// ExistingActiveStartResponseWithLifecycleHeld resolves an idempotent replay
// while the caller owns LockAllocationLifecycle. The durable capability launch
// proof is checked by the facade; this method proves the runtime inventory is
// still active before that proof is replayed.
func (c *Controller) ExistingActiveStartResponseWithLifecycleHeld(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, bool, error) {
	return c.existingActiveStartResponse(ctx, request)
}

func (c *Controller) Delete(ctx context.Context, request *runtime.DeleteRequest) (*runtime.DeleteResponse, error) {
	return c.deleteManagedContainer(ctx, request)
}

func (c *Controller) CleanupFailedStart(ctx context.Context, allocationID string) error {
	return c.cleanupFailedStart(ctx, allocationID)
}

func (c *Controller) RestoreAllocationState(runtimeInventory map[string]struct{}) error {
	if err := c.loadAllocationStates(runtimeInventory); err != nil {
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

func (c *Controller) startReadinessWorker(containerID string, attempt int64, extraConfig startplan.ExtraConfig) {
	if c == nil || c.probes == nil {
		return
	}
	c.probes.StartReadiness(containerID, attempt, extraConfig.ReadinessProbe)
}

func (c *Controller) startLivenessWorker(containerID string, attempt int64, extraConfig startplan.ExtraConfig) {
	if c == nil || c.probes == nil {
		return
	}
	c.probes.StartLiveness(containerID, attempt, extraConfig.LivenessProbe)
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

func (c *Controller) reportStartRunningStatus(containerID string, attempt int64, extraConfig startplan.ExtraConfig, observedAt time.Time) {
	if c == nil || c.reportStatus == nil || attempt <= 0 || strings.TrimSpace(containerID) == "" {
		return
	}
	if extraConfig.ReadinessProbe != nil {
		return
	}
	c.reportStatus(containerID, attempt, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, 0, false, true, "", "", observedAt)
}
