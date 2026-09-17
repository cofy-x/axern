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
	"github.com/cofy-x/axern/runtime/axnoded/internal/egress"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	servicenetworking "github.com/cofy-x/axern/runtime/axnoded/internal/service/networking"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type stateStore interface {
	PutRecord(bucket, key string, value proto.Message) error
	GetRecord(bucket, key string, value proto.Message) error
	DeleteRecord(bucket, key string) error
	ForEachRecord(bucket string, visit func(key string, value []byte) error) error
}

type Options struct {
	Config                      config.Config
	Store                       stateStore
	ContainerManager            func() *container.Manager
	RunscHandler                contract.SandboxRuntime
	EnvironmentCache            *environmentcache.EnvironmentCache
	Networking                  *servicenetworking.Coordinator
	StartMetricSink             StartMetricSink
	ReportStatus                func(allocationID string, status commonv1.AllocationLifecycleState, exitCode *int32, ready bool, readinessMessage string, message string, observedAt time.Time)
	InventoryChanged            func()
	RootfsCapabilityGate        func(context.Context, *runtime.StartRequest, *environmentcache.RootFS) error
	PreActivationCapabilityGate func(context.Context, *runtime.StartRequest, contract.AllocationRuntime, string) error
	Egress                      egress.Manager
}

type Controller struct {
	config          config.Config
	outputRetention *allocationoutput.Retention
	store           stateStore

	containerManager            func() *container.Manager
	runscHandler                contract.SandboxRuntime
	environmentCache            *environmentcache.EnvironmentCache
	networking                  *servicenetworking.Coordinator
	startMetricSink             StartMetricSink
	reportStatus                func(allocationID string, status commonv1.AllocationLifecycleState, exitCode *int32, ready bool, readinessMessage string, message string, observedAt time.Time)
	inventoryChanged            func()
	rootfsCapabilityGate        func(context.Context, *runtime.StartRequest, *environmentcache.RootFS) error
	preActivationCapabilityGate func(context.Context, *runtime.StartRequest, contract.AllocationRuntime, string) error
	egress                      egress.Manager

	stateMu          sync.RWMutex
	allocationStates map[string]*allocationState

	allocationLifecycleLocks allocationKeyedLocks
	recordMutationLocks      allocationKeyedLocks
}

type internalConformanceContextKey struct{}

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

func NewController(options Options) *Controller {
	c := &Controller{
		config:                      options.Config,
		outputRetention:             allocationoutput.NewRetention(options.Config.RootDir),
		store:                       options.Store,
		containerManager:            options.ContainerManager,
		runscHandler:                options.RunscHandler,
		environmentCache:            options.EnvironmentCache,
		networking:                  options.Networking,
		startMetricSink:             options.StartMetricSink,
		reportStatus:                options.ReportStatus,
		inventoryChanged:            options.InventoryChanged,
		rootfsCapabilityGate:        options.RootfsCapabilityGate,
		preActivationCapabilityGate: options.PreActivationCapabilityGate,
		egress:                      options.Egress,
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
	return c.startAllocation(ctx, request)
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
	if strings.TrimSpace(request.GetAllocationID()) == "" {
		return startErrorResponse("allocation id is required"), errord.ErrInvalidArgument
	}
	return c.startAllocationWithLifecycleHeld(ctx, request)
}

// ExistingActiveStartResponseWithLifecycleHeld resolves an idempotent replay
// while the caller owns LockAllocationLifecycle. The durable capability launch
// verification is checked by the facade; this method confirms the runtime
// inventory is still active before that verification is replayed.
func (c *Controller) ExistingActiveStartResponseWithLifecycleHeld(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, bool, error) {
	return c.existingActiveStartResponse(ctx, request)
}

func (c *Controller) Delete(ctx context.Context, request *runtime.DeleteRequest) (*runtime.DeleteResponse, error) {
	return c.deleteAllocation(ctx, request)
}

func (c *Controller) DeleteControlPlane(ctx context.Context, request *runtime.DeleteRequest, nodeID string) (*runtime.DeleteResponse, error) {
	unlockLifecycle := c.allocationLifecycleLocks.Lock(request.GetID())
	defer unlockLifecycle()
	if c.HasAllocation(request.GetID()) && !c.HasAdmittedAllocation(request.GetID()) {
		return nil, fmt.Errorf("allocation %q has no admitted allocation record", request.GetID())
	}
	if c.HasAdmittedAllocation(request.GetID()) && !c.AdmittedAllocationMatches(request.GetID(), nodeID) {
		return nil, fmt.Errorf("allocation %q is not bound to node %q", request.GetID(), strings.TrimSpace(nodeID))
	}
	response, err := c.deleteAllocationWithLifecycleHeld(ctx, request)
	if err != nil {
		return response, err
	}
	return response, nil
}

// FailStopWorkload stops only the supervised Allocation workload. It retains
// sandboxd, runtime metadata, and AllocationState so the control-plane-owned
// Delete path can cross the output-sealing barrier before deleting the OCI
// sandbox. It shares the Allocation lifecycle lock with Start and Delete.
func (c *Controller) FailStopWorkload(ctx context.Context, allocationID string) error {
	if strings.TrimSpace(allocationID) == "" {
		return errord.ErrInvalidArgument
	}
	unlockLifecycle := c.allocationLifecycleLocks.Lock(allocationID)
	defer unlockLifecycle()
	target, handler, err := c.runtimeHandlerForContainer(allocationID)
	if err != nil {
		return err
	}
	if target.Status != nil && target.Status.Get().State() == runtime.ContainerState_CONTAINER_EXITED {
		return nil
	}
	_, err = handler.StopWorkload(ctx, contract.HandlerOptions{ContainerID: allocationID})
	return err
}

func (c *Controller) CleanupFailedStart(ctx context.Context, allocationID string) error {
	return c.cleanupFailedStart(ctx, allocationID)
}

func (c *Controller) CleanupPersistedFailedStart(ctx context.Context, allocationID string) error {
	return c.cleanupPersistedFailedStart(ctx, allocationID)
}

func (c *Controller) RestoreAllocationState(runtimeInventory map[string]struct{}) error {
	if err := c.loadAllocationStates(runtimeInventory); err != nil {
		// Reconciliation is destructive: an incomplete recovery view must not
		// release leases that a still-running container may still be using.
		logrus.WithError(err).Warn("restore allocation state; skip mount lease reconciliation")
		return err
	}
	if err := c.environmentCache.ReconcileMountLeases(); err != nil {
		return fmt.Errorf("reconcile imagemgr mount leases: %w", err)
	}
	return nil
}

func (c *Controller) ContainerIP(containerID string) string {
	if c == nil || c.containers() == nil {
		return ""
	}
	resource, err := c.containers().CollectResourceByID(containerID)
	if err != nil {
		return ""
	}
	return containerIPFromResource(resource)
}

func (c *Controller) runtimeMapping(containerID string) (*environmentcache.PreparedEnvironment, bool) {
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

func (c *Controller) runtimeHandlerForContainer(id string) (*container.Container, contract.SandboxRuntime, error) {
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
	if c.runscHandler == nil {
		return nil, nil, fmt.Errorf("runsc handler unavailable")
	}
	return target, c.runscHandler, nil
}

func (c *Controller) sandboxNetworking() *servicenetworking.Coordinator {
	if c == nil {
		return nil
	}
	return c.networking
}

func (c *Controller) reportStartRunningStatus(containerID string, observedAt time.Time) {
	if c == nil || c.reportStatus == nil || strings.TrimSpace(containerID) == "" {
		return
	}
	c.reportStatus(containerID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, nil, true, "", "", observedAt)
}
