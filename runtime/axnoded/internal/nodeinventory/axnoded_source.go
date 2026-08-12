package nodeinventory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	"google.golang.org/protobuf/proto"
)

type runtimeCountFunc func() int
type readyFunc func() bool
type volumeHealthFunc func(context.Context) (*runtimevolumev1.VolumeManagerHealth, error)
type statfsFunc func(string) (StorageInventoryEntry, error)
type capabilitySnapshotFunc func(context.Context, time.Time) (*capabilityv1.CapabilitySnapshot, error)
type memoryCommitmentFunc func() (resources.MemoryCommitment, error)
type memoryCapacityObserverFunc func(resources.MemoryCapacitySnapshot) error
type memoryObservationRevisionFunc func() (int64, error)
type memoryPIDRolesVerifierFunc func(allocationID, runtimeName, workloadPath string, runtimePID int) error
type retiringMemoryLeasesFunc func() []resources.RetiringMemoryLease
type unackedStatusIDsFunc func() []string

var ErrCapabilitySnapshotWarming = errors.New("capability manager is warming")

type containerManagerView interface {
	List(...container.ListOption) []*container.Container
	ResourcePoolStatus(resources.ResourceName) (resources.PoolStatus, error)
	RuntimeCgroupPath(string) (string, error)
}

type langRuntimeManagerView interface {
	GetLangRuntime(string) *langruntime.LanguageRuntime
	List() []*langruntime.LanguageRuntime
	RetentionStats() langruntime.RetentionStats
}

type AxnodedSourceOptions struct {
	NodeID                    string
	Ready                     readyFunc
	RuntimeCount              runtimeCountFunc
	Container                 containerManagerView
	LangRuntime               langRuntimeManagerView
	ImageManager              *ImageManagerClient
	NodeResources             NodeResourceProvider
	CgroupDriver              os2.CgroupDriver
	NatBackend                string
	BPFNetPinPath             string
	NodeState                 string
	NodeLabels                map[string]string
	CapabilitySnapshot        capabilitySnapshotFunc
	LoadBPFNet                func(string) (bpfnet.Status, error)
	VolumeHealth              volumeHealthFunc
	StorageTargets            []StorageTarget
	StatFS                    statfsFunc
	RuntimeSlotCapacity       int
	MemoryBudgetEnabled       bool
	MemoryCgroupEnforced      bool
	CgroupRootName            string
	MemorySystemReserveBytes  int64
	MemoryCommitment          memoryCommitmentFunc
	MemoryCapacityObserver    memoryCapacityObserverFunc
	MemoryObservationRevision memoryObservationRevisionFunc
	MemoryPIDRolesVerifier    memoryPIDRolesVerifierFunc
	RetiringMemoryLeases      retiringMemoryLeasesFunc
	// UnackedStatusIDs extends active allocation ownership
	// through the control-plane status-report acknowledgement boundary. This
	// prevents a short-lived allocation from disappearing from node inventory
	// before controld has committed its terminal evidence.
	UnackedStatusIDs unackedStatusIDsFunc
	// DisabledResourcePools records pools intentionally omitted by node
	// configuration. A disabled pool does not constrain aggregate capacity and
	// does not make the complete node inventory unavailable.
	DisabledResourcePools []resources.ResourceName
}

type AxnodedSource struct {
	nodeID                    string
	ready                     readyFunc
	runtimeCount              runtimeCountFunc
	container                 containerManagerView
	langRuntime               langRuntimeManagerView
	imageManager              *ImageManagerClient
	nodeResources             NodeResourceProvider
	cgroupDriver              os2.CgroupDriver
	natBackend                string
	bpfnetPin                 string
	nodeState                 string
	nodeLabels                map[string]string
	capabilitySnapshot        capabilitySnapshotFunc
	loadBPFNet                func(string) (bpfnet.Status, error)
	volumeHealth              volumeHealthFunc
	storageTargets            []StorageTarget
	statFS                    statfsFunc
	disabledPools             map[resources.ResourceName]struct{}
	runtimeSlotCapacity       int
	memoryBudgetEnabled       bool
	memoryCgroupEnforced      bool
	cgroupRootName            string
	memorySystemReserveBytes  int64
	memoryCommitment          memoryCommitmentFunc
	memoryCapacityObserver    memoryCapacityObserverFunc
	memoryObservationRevision memoryObservationRevisionFunc
	memoryPIDRolesVerifier    memoryPIDRolesVerifierFunc
	retiringMemoryLeases      retiringMemoryLeasesFunc
	unackedStatusIDs          unackedStatusIDsFunc

	sampleMu       sync.Mutex
	prevCPUSamples map[string]cpuUsageSample
	resourceMu     sync.Mutex
	lastResources  *NodeResources
	lastResourceAt time.Time
	memoryHealthMu sync.Mutex
	reserveBlocked bool
	healthySamples int
}

type cpuUsageSample struct {
	UsageNs     uint64
	CollectedAt time.Time
}

func NewAxnodedSource(opts AxnodedSourceOptions) *AxnodedSource {
	loadBPFNet := opts.LoadBPFNet
	if loadBPFNet == nil {
		loadBPFNet = bpfnetstatus.Load
	}
	disabledPools := make(map[resources.ResourceName]struct{}, len(opts.DisabledResourcePools))
	for _, name := range opts.DisabledResourcePools {
		disabledPools[name] = struct{}{}
	}
	runtimeSlotCapacity := opts.RuntimeSlotCapacity
	if runtimeSlotCapacity <= 0 {
		runtimeSlotCapacity = container.MaxContainerNum
	}
	runtimeSlotCapacity = min(runtimeSlotCapacity, container.MaxContainerNum)
	return &AxnodedSource{
		nodeID:                    strings.TrimSpace(opts.NodeID),
		ready:                     opts.Ready,
		runtimeCount:              opts.RuntimeCount,
		container:                 opts.Container,
		langRuntime:               opts.LangRuntime,
		imageManager:              opts.ImageManager,
		nodeResources:             defaultNodeResourceProvider(opts.NodeResources),
		cgroupDriver:              opts.CgroupDriver,
		natBackend:                opts.NatBackend,
		bpfnetPin:                 opts.BPFNetPinPath,
		nodeState:                 opts.NodeState,
		nodeLabels:                cloneStringMap(opts.NodeLabels),
		capabilitySnapshot:        opts.CapabilitySnapshot,
		loadBPFNet:                loadBPFNet,
		volumeHealth:              opts.VolumeHealth,
		storageTargets:            normalizeStorageTargets(opts.StorageTargets),
		statFS:                    defaultStatFS(opts.StatFS),
		disabledPools:             disabledPools,
		runtimeSlotCapacity:       runtimeSlotCapacity,
		memoryBudgetEnabled:       opts.MemoryBudgetEnabled,
		memoryCgroupEnforced:      opts.MemoryCgroupEnforced,
		cgroupRootName:            opts.CgroupRootName,
		memorySystemReserveBytes:  opts.MemorySystemReserveBytes,
		memoryCommitment:          opts.MemoryCommitment,
		memoryCapacityObserver:    opts.MemoryCapacityObserver,
		memoryObservationRevision: opts.MemoryObservationRevision,
		memoryPIDRolesVerifier:    opts.MemoryPIDRolesVerifier,
		retiringMemoryLeases:      opts.RetiringMemoryLeases,
		unackedStatusIDs:          opts.UnackedStatusIDs,
		prevCPUSamples:            make(map[string]cpuUsageSample),
	}
}

func (s *AxnodedSource) resourcePoolDisabled(name resources.ResourceName) bool {
	_, disabled := s.disabledPools[name]
	return disabled
}

func (s *AxnodedSource) Collect(ctx context.Context) (NodeInventorySnapshot, bool) {
	now := time.Now().UTC()
	snapshot := NewSnapshot()
	snapshot.Node.NodeID = s.nodeID
	snapshot.Node.CollectedAt = now
	snapshot.Node.Name, _ = os.Hostname()
	snapshot.Node.State = normalizeNodeState(s.nodeState)
	snapshot.Node.Labels = cloneStringMap(s.nodeLabels)
	capabilitiesReady := true
	if s.capabilitySnapshot != nil {
		capabilities, err := s.capabilitySnapshot(ctx, now)
		if capabilities != nil {
			snapshot.Node.CapabilitySnapshot = proto.Clone(capabilities).(*capabilityv1.CapabilitySnapshot)
		}
		if err != nil {
			capabilitiesReady = false
			if errors.Is(err, ErrCapabilitySnapshotWarming) {
				snapshot.Sources["node_capabilities"] = warmingSource(err.Error())
			} else {
				snapshot.Sources["node_capabilities"] = degradedSource(err.Error(), now)
			}
		} else {
			snapshot.Sources["node_capabilities"] = readySource(now)
		}
	}
	resourcesReady := s.collectNodeResources(ctx, now, &snapshot)
	memoryBudgetReady := s.collectMemoryBudget(now, resourcesReady, &snapshot)
	s.collectStorageInventory(now, &snapshot)

	localReady := s.collectAxnodedInventory(now, &snapshot)
	s.collectImagemgrInventory(now, &snapshot)
	s.collectVolumedInventory(ctx, now, &snapshot)
	s.collectBPFNetInventory(now, &snapshot)
	sortLocalityEntries(snapshot.Heat.Locality)
	// Node collected_at is the completion/publication boundary. Individual
	// observations retain their own sample times and must never appear newer
	// than the summary that carries them.
	snapshot.Node.CollectedAt = time.Now().UTC()

	if snapshot.Node.Name == "" {
		snapshot.Node.Name = "unknown"
	}
	return snapshot, resourcesReady && memoryBudgetReady && localReady && capabilitiesReady
}

func (s *AxnodedSource) collectMemoryBudget(now time.Time, nodeResourcesReady bool, snapshot *NodeInventorySnapshot) bool {
	if !s.memoryBudgetEnabled {
		return true
	}
	if !nodeResourcesReady {
		s.invalidateMemoryCapacity()
		snapshot.Sources["node_memory_budget"] = errorSource(fmt.Errorf("node resource source is unavailable"))
		return false
	}
	if s.memoryCommitment == nil {
		s.invalidateMemoryCapacity()
		snapshot.Sources["node_memory_budget"] = errorSource(fmt.Errorf("local memory commitment provider is unavailable"))
		return false
	}
	commitment, err := s.memoryCommitment()
	if err != nil {
		s.invalidateMemoryCapacity()
		snapshot.Sources["node_memory_budget"] = errorSource(err)
		return false
	}
	var sample hostlinux.NodeMemoryBudgetSample
	var mode string
	if s.memoryCgroupEnforced {
		sample, err = hostlinux.InspectEnforcedNodeMemoryBudget(
			snapshot.Node.Capacity.MemoryBytes,
			snapshot.Node.Allocatable.MemoryBytes,
			s.memorySystemReserveBytes,
			s.cgroupRootName,
		)
		mode = "cgroup_v2"
	} else {
		sample, err = hostlinux.InspectDevelopmentNodeMemoryBudget(
			snapshot.Node.Capacity.MemoryBytes,
			snapshot.Node.Allocatable.MemoryBytes,
		)
		mode = "disabled_dev"
	}
	if err != nil {
		s.invalidateMemoryCapacity()
		snapshot.Sources["node_memory_budget"] = errorSource(err)
		return false
	}
	sampledAt := time.Now().UTC()
	reserveExhausted := s.observeSystemReserveHealth(sample.SystemReserveExhausted)
	snapshot.Node.MemoryBudget = MemoryBudgetInventory{
		PhysicalCapacityBytes:     sample.PhysicalCapacityBytes,
		SourceAllocatableBytes:    sample.SourceAllocatableBytes,
		DelegatedRootLimitBytes:   sample.DelegatedRootLimitBytes,
		DelegatedRootLimitFinite:  sample.DelegatedRootLimitFinite,
		SystemReserveBytes:        sample.SystemReserveBytes,
		EffectiveAllocatableBytes: sample.EffectiveAllocatable,
		LocalCommitmentBytes:      commitment.CommittedBytes,
		CleanupDebtBytes:          commitment.CleanupDebtBytes,
		InternalCurrentBytes:      sample.InternalCurrentBytes,
		CapacityIdentity:          sample.CapacityIdentity,
		Mode:                      mode,
		SampledAt:                 sampledAt,
		RetiringCgroupCount:       commitment.RetiringCgroupCount,
		OldestRetiringAgeSeconds:  int64(commitment.OldestRetiringAge / time.Second),
		SystemReserveExhausted:    reserveExhausted,
	}
	for kind, value := range map[string]int64{
		"physical_capacity":           sample.PhysicalCapacityBytes,
		"source_allocatable":          sample.SourceAllocatableBytes,
		"delegated_root_limit":        sample.DelegatedRootLimitBytes,
		"system_reserve":              sample.SystemReserveBytes,
		"effective_allocatable":       sample.EffectiveAllocatable,
		"local_commitment":            commitment.CommittedBytes,
		"cleanup_debt":                commitment.CleanupDebtBytes,
		"internal_current":            sample.InternalCurrentBytes,
		"sandbox_current":             sample.SandboxCurrentBytes,
		"retiring_count":              int64(commitment.RetiringCgroupCount),
		"oldest_retiring_age_seconds": int64(commitment.OldestRetiringAge / time.Second),
		"system_reserve_exhausted":    boolInt64(reserveExhausted),
	} {
		metrics.RecordNodeMemoryBudget(kind, value)
	}
	if s.memoryCapacityObserver == nil {
		snapshot.Sources["node_memory_budget"] = errorSource(fmt.Errorf("local memory capacity observer is unavailable"))
		return false
	}
	if err := s.memoryCapacityObserver(resources.MemoryCapacitySnapshot{
		EffectiveAllocatableBytes: sample.EffectiveAllocatable,
		SandboxCurrentBytes:       sample.SandboxCurrentBytes,
		SystemReserveExhausted:    reserveExhausted,
		CapacityIdentity:          sample.CapacityIdentity,
		SampledAt:                 sampledAt,
	}); err != nil {
		snapshot.Sources["node_memory_budget"] = errorSource(fmt.Errorf("publish local memory capacity: %w", err))
		return false
	}
	snapshot.Node.Allocatable.MemoryBytes = sample.EffectiveAllocatable
	snapshot.Sources["node_memory_budget"] = readySource(sampledAt)
	return true
}

func (s *AxnodedSource) invalidateMemoryCapacity() {
	if s.memoryCapacityObserver != nil {
		_ = s.memoryCapacityObserver(resources.MemoryCapacitySnapshot{Unavailable: true})
	}
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

// observeSystemReserveHealth blocks immediately on exhaustion and requires two
// independent healthy samples before reopening admission. With the fixed
// inventory cadence those samples are separated by one collection interval.
func (s *AxnodedSource) observeSystemReserveHealth(exhausted bool) bool {
	s.memoryHealthMu.Lock()
	defer s.memoryHealthMu.Unlock()
	if exhausted {
		s.reserveBlocked = true
		s.healthySamples = 0
		return true
	}
	if !s.reserveBlocked {
		return false
	}
	s.healthySamples++
	if s.healthySamples < 2 {
		return true
	}
	s.reserveBlocked = false
	s.healthySamples = 0
	return false
}

func (s *AxnodedSource) collectNodeResources(ctx context.Context, now time.Time, snapshot *NodeInventorySnapshot) bool {
	resources, err := s.nodeResources.Collect(ctx)
	if err == nil {
		cachedResources := resources
		cachedResources.Labels = cloneStringMap(resources.Labels)
		s.resourceMu.Lock()
		s.lastResources = &cachedResources
		s.lastResourceAt = now
		s.resourceMu.Unlock()
		applyNodeResources(snapshot, resources)
		snapshot.Sources["node_resources"] = readySource(now)
		return true
	}

	s.resourceMu.Lock()
	cached := s.lastResources
	lastSuccessAt := s.lastResourceAt
	s.resourceMu.Unlock()
	if cached != nil {
		applyNodeResources(snapshot, *cached)
		snapshot.Sources["node_resources"] = SourceStatus{
			Status:        StatusDegraded,
			LastSuccessAt: &lastSuccessAt,
			Error:         err.Error(),
		}
		return false
	}

	snapshot.Sources["node_resources"] = errorSource(err)
	return false
}

func applyNodeResources(snapshot *NodeInventorySnapshot, resources NodeResources) {
	snapshot.Node.Capacity = resources.Capacity
	snapshot.Node.Allocatable = resources.Allocatable
	labels := cloneStringMap(resources.Labels)
	for key, value := range snapshot.Node.Labels {
		labels[key] = value
	}
	snapshot.Node.Labels = labels
}
