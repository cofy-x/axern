package nodeinventory

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
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
	Ready               readyFunc
	RuntimeCount        runtimeCountFunc
	Container           containerManagerView
	LangRuntime         langRuntimeManagerView
	ImageManager        *ImageManagerClient
	NodeResources       NodeResourceProvider
	CgroupDriver        os2.CgroupDriver
	NatBackend          string
	BPFNetPinPath       string
	NodeState           string
	NodeLabels          map[string]string
	CapabilitySnapshot  capabilitySnapshotFunc
	LoadBPFNet          func(string) (bpfnet.Status, error)
	VolumeHealth        volumeHealthFunc
	StorageTargets      []StorageTarget
	StatFS              statfsFunc
	RuntimeSlotCapacity int
	// DisabledResourcePools records pools intentionally omitted by node
	// configuration. A disabled pool does not constrain aggregate capacity and
	// does not make the complete node inventory unavailable.
	DisabledResourcePools []resources.ResourceName
}

type AxnodedSource struct {
	ready               readyFunc
	runtimeCount        runtimeCountFunc
	container           containerManagerView
	langRuntime         langRuntimeManagerView
	imageManager        *ImageManagerClient
	nodeResources       NodeResourceProvider
	cgroupDriver        os2.CgroupDriver
	natBackend          string
	bpfnetPin           string
	nodeState           string
	nodeLabels          map[string]string
	capabilitySnapshot  capabilitySnapshotFunc
	loadBPFNet          func(string) (bpfnet.Status, error)
	volumeHealth        volumeHealthFunc
	storageTargets      []StorageTarget
	statFS              statfsFunc
	disabledPools       map[resources.ResourceName]struct{}
	runtimeSlotCapacity int

	sampleMu       sync.Mutex
	prevCPUSamples map[string]cpuUsageSample
	resourceMu     sync.Mutex
	lastResources  *NodeResources
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
		ready:               opts.Ready,
		runtimeCount:        opts.RuntimeCount,
		container:           opts.Container,
		langRuntime:         opts.LangRuntime,
		imageManager:        opts.ImageManager,
		nodeResources:       defaultNodeResourceProvider(opts.NodeResources),
		cgroupDriver:        opts.CgroupDriver,
		natBackend:          opts.NatBackend,
		bpfnetPin:           opts.BPFNetPinPath,
		nodeState:           opts.NodeState,
		nodeLabels:          cloneStringMap(opts.NodeLabels),
		capabilitySnapshot:  opts.CapabilitySnapshot,
		loadBPFNet:          loadBPFNet,
		volumeHealth:        opts.VolumeHealth,
		storageTargets:      normalizeStorageTargets(opts.StorageTargets),
		statFS:              defaultStatFS(opts.StatFS),
		disabledPools:       disabledPools,
		runtimeSlotCapacity: runtimeSlotCapacity,
		prevCPUSamples:      make(map[string]cpuUsageSample),
	}
}

func (s *AxnodedSource) resourcePoolDisabled(name resources.ResourceName) bool {
	_, disabled := s.disabledPools[name]
	return disabled
}

func (s *AxnodedSource) Collect(ctx context.Context) (NodeInventorySnapshot, bool) {
	now := time.Now().UTC()
	snapshot := NewSnapshot()
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
	s.collectStorageInventory(now, &snapshot)

	localReady := s.collectAxnodedInventory(now, &snapshot)
	s.collectImagemgrInventory(now, &snapshot)
	s.collectVolumedInventory(ctx, now, &snapshot)
	s.collectBPFNetInventory(now, &snapshot)
	sortLocalityEntries(snapshot.Heat.Locality)

	if snapshot.Node.Name == "" {
		snapshot.Node.Name = "unknown"
	}
	return snapshot, resourcesReady && localReady && capabilitiesReady
}

func (s *AxnodedSource) collectNodeResources(ctx context.Context, now time.Time, snapshot *NodeInventorySnapshot) bool {
	resources, err := s.nodeResources.Collect(ctx)
	if err == nil {
		s.resourceMu.Lock()
		s.lastResources = &resources
		s.resourceMu.Unlock()
		applyNodeResources(snapshot, resources)
		snapshot.Sources["node_resources"] = readySource(now)
		return true
	}

	s.resourceMu.Lock()
	cached := s.lastResources
	s.resourceMu.Unlock()
	if cached != nil {
		applyNodeResources(snapshot, *cached)
		snapshot.Sources["node_resources"] = degradedSource(err.Error(), now)
		return true
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
