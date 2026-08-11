package nodeinventory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

const SnapshotVersion = "v1alpha2"

const (
	StatusReady    = "ready"
	StatusWarming  = "warming"
	StatusDegraded = "degraded"
	StatusError    = "error"
	StatusDisabled = "disabled"
)

type SourceStatus struct {
	Status        string     `json:"status"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type NodeResourceQuantity struct {
	CpuMilli              int64 `json:"cpu_milli"`
	MemoryBytes           int64 `json:"memory_bytes"`
	EphemeralStorageBytes int64 `json:"ephemeral_storage_bytes"`
}

type NodeInfo struct {
	NodeID             string                           `json:"node_id"`
	Name               string                           `json:"name"`
	CollectedAt        time.Time                        `json:"collected_at"`
	State              string                           `json:"state"`
	Labels             map[string]string                `json:"labels,omitempty"`
	CapabilitySnapshot *capabilityv1.CapabilitySnapshot `json:"capability_snapshot,omitempty"`
	Capacity           NodeResourceQuantity             `json:"capacity"`
	Allocatable        NodeResourceQuantity             `json:"allocatable"`
	MemoryBudget       MemoryBudgetInventory            `json:"memory_budget"`
}

type MemoryBudgetInventory struct {
	PhysicalCapacityBytes     int64     `json:"physical_capacity_bytes"`
	SourceAllocatableBytes    int64     `json:"source_allocatable_bytes"`
	DelegatedRootLimitBytes   int64     `json:"delegated_root_limit_bytes"`
	DelegatedRootLimitFinite  bool      `json:"delegated_root_limit_finite"`
	SystemReserveBytes        int64     `json:"system_reserve_bytes"`
	EffectiveAllocatableBytes int64     `json:"effective_allocatable_bytes"`
	LocalCommitmentBytes      int64     `json:"local_commitment_bytes"`
	CleanupDebtBytes          int64     `json:"cleanup_debt_bytes"`
	InternalCurrentBytes      int64     `json:"internal_current_bytes"`
	CapacityIdentity          string    `json:"capacity_identity"`
	Mode                      string    `json:"mode"`
	SampledAt                 time.Time `json:"sampled_at"`
	RetiringCgroupCount       int       `json:"retiring_cgroup_count"`
	OldestRetiringAgeSeconds  int64     `json:"oldest_retiring_age_seconds"`
	SystemReserveExhausted    bool      `json:"system_reserve_exhausted"`
}

// MarshalJSON keeps the inventory endpoint on ordinary JSON while delegating
// protobuf oneof encoding to protojson. Standard encoding/json cannot round
// trip CapabilityKey's generated oneof implementation.
func (n NodeInfo) MarshalJSON() ([]byte, error) {
	type nodeInfoAlias NodeInfo
	var snapshot json.RawMessage
	if n.CapabilitySnapshot != nil {
		payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(n.CapabilitySnapshot)
		if err != nil {
			return nil, fmt.Errorf("marshal capability snapshot: %w", err)
		}
		snapshot = payload
	}
	return json.Marshal(struct {
		*nodeInfoAlias
		CapabilitySnapshot json.RawMessage `json:"capability_snapshot,omitempty"`
	}{nodeInfoAlias: (*nodeInfoAlias)(&n), CapabilitySnapshot: snapshot})
}

func (n *NodeInfo) UnmarshalJSON(data []byte) error {
	if n == nil {
		return fmt.Errorf("unmarshal node info: nil receiver")
	}
	type nodeInfoAlias NodeInfo
	wire := struct {
		*nodeInfoAlias
		CapabilitySnapshot json.RawMessage `json:"capability_snapshot"`
	}{nodeInfoAlias: (*nodeInfoAlias)(n)}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	n.CapabilitySnapshot = nil
	if len(wire.CapabilitySnapshot) == 0 || bytes.Equal(bytes.TrimSpace(wire.CapabilitySnapshot), []byte("null")) {
		return nil
	}
	snapshot := &capabilityv1.CapabilitySnapshot{}
	if err := protojson.Unmarshal(wire.CapabilitySnapshot, snapshot); err != nil {
		return fmt.Errorf("unmarshal capability snapshot: %w", err)
	}
	n.CapabilitySnapshot = snapshot
	return nil
}

type CPUInventory struct {
	AxnodedCommittedMilli int64 `json:"axnoded_committed_milli"`
	AxnodedUsedMilli      int64 `json:"axnoded_used_milli"`
	AxnodedUnboundedCount int64 `json:"axnoded_unbounded_count"`
}

type MemoryInventory struct {
	AxnodedCommittedBytes int64 `json:"axnoded_committed_bytes"`
	AxnodedUsedBytes      int64 `json:"axnoded_used_bytes"`
	AxnodedUnboundedCount int64 `json:"axnoded_unbounded_count"`
}

type ResourceInventory struct {
	CPU              CPUInventory              `json:"cpu"`
	Memory           MemoryInventory           `json:"memory"`
	EphemeralStorage EphemeralStorageInventory `json:"ephemeral_storage"`
}

type EphemeralStorageInventory struct {
	AxnodedCommittedBytes int64 `json:"axnoded_committed_bytes"`
	AxnodedUsedBytes      int64 `json:"axnoded_used_bytes"`
	AxnodedUnboundedCount int64 `json:"axnoded_unbounded_count"`
}

type PoolInventory struct {
	Using       int `json:"using"`
	Idle        int `json:"idle"`
	Capacity    int `json:"capacity"`
	Unavailable int `json:"unavailable"`
}

type PoolsInventory struct {
	Cgroup       PoolInventory `json:"cgroup"`
	Interface    PoolInventory `json:"interface"`
	RuntimeSlots PoolInventory `json:"runtime_slots"`
}

type StorageInventoryEntry struct {
	Target                      string `json:"target"`
	Path                        string `json:"path,omitempty"`
	CapacityBytes               int64  `json:"capacity_bytes"`
	UsedBytes                   int64  `json:"used_bytes"`
	AvailableBytes              int64  `json:"available_bytes"`
	InodesTotal                 int64  `json:"inodes_total"`
	InodesUsed                  int64  `json:"inodes_used"`
	InodesAvailable             int64  `json:"inodes_available"`
	Collected                   bool   `json:"collected"`
	Error                       string `json:"error,omitempty"`
	SystemReserveBytes          int64  `json:"system_reserve_bytes"`
	ReservedBytes               int64  `json:"reserved_bytes"`
	AllocatableBytes            int64  `json:"allocatable_bytes"`
	ActiveReservations          int64  `json:"active_reservations"`
	FilesystemType              string `json:"filesystem_type,omitempty"`
	MountIdentity               string `json:"mount_identity,omitempty"`
	AllocationUsedBytes         int64  `json:"allocation_used_bytes"`
	UnlinkedBackingUsageUnknown bool   `json:"unlinked_backing_usage_unknown"`
}

type AxnodedComponentInventory struct {
	Status               string   `json:"status"`
	Error                string   `json:"error,omitempty"`
	Ready                bool     `json:"ready"`
	RunningContainers    int      `json:"running_containers"`
	RunningAllocationIDs []string `json:"running_allocation_ids,omitempty"`
	ActiveAllocationIDs  []string `json:"active_allocation_ids,omitempty"`
	RegisteredRuntimes   int      `json:"registered_runtimes"`
}

type ImagemgrComponentInventory struct {
	Status             string `json:"status"`
	Error              string `json:"error,omitempty"`
	Reachable          bool   `json:"reachable"`
	DaemonCount        int    `json:"daemon_count"`
	MountedImageCount  int    `json:"mounted_image_count"`
	ImportedImageCount int    `json:"imported_image_count"`
}

type ImagefsdComponentInventory struct {
	Status              string  `json:"status"`
	Error               string  `json:"error,omitempty"`
	Reachable           bool    `json:"reachable"`
	ChunkDBPresent      bool    `json:"chunkdb_present"`
	ChunkCount          int64   `json:"chunk_count"`
	ChunkDBUsedBytes    int64   `json:"chunkdb_used_bytes"`
	ChunkDBUsagePercent float64 `json:"chunkdb_usage_percent"`
}

type BPFNetComponentInventory struct {
	Status                string `json:"status"`
	Error                 string `json:"error,omitempty"`
	Enabled               bool   `json:"enabled"`
	Ready                 bool   `json:"ready"`
	Mode                  string `json:"mode,omitempty"`
	NeedsSNATFallback     bool   `json:"needs_snat_fallback"`
	NeedsFullDNATFallback bool   `json:"needs_full_dnat_fallback"`
	NeedsLocalhostCompat  bool   `json:"needs_localhost_compat"`
}

type VolumedComponentInventory struct {
	Status                             string    `json:"status"`
	Error                              string    `json:"error,omitempty"`
	Reachable                          bool      `json:"reachable"`
	PublishedVolumeCount               int       `json:"published_volume_count"`
	LastReconcileAt                    time.Time `json:"last_reconcile_at,omitempty"`
	LastReconcileError                 string    `json:"last_reconcile_error,omitempty"`
	LastReconcileRetainedCount         int       `json:"last_reconcile_retained_count"`
	LastReconcileUnpublishedCount      int       `json:"last_reconcile_unpublished_count"`
	LastReconcileActiveAllocationCount int       `json:"last_reconcile_active_allocation_count"`
	LastReconcileStaleAllocationCount  int       `json:"last_reconcile_stale_allocation_count"`
	LastReconcileInvalidVolumeCount    int       `json:"last_reconcile_invalid_volume_count"`
}

type ComponentsInventory struct {
	Axnoded  AxnodedComponentInventory  `json:"axnoded"`
	Imagemgr ImagemgrComponentInventory `json:"imagemgr"`
	Imagefsd ImagefsdComponentInventory `json:"imagefsd"`
	BPFNet   BPFNetComponentInventory   `json:"bpfnet"`
	Volumed  VolumedComponentInventory  `json:"volumed"`
}

type ChunkDBHeat struct {
	TotalChunks  int64   `json:"total_chunks"`
	UsedBytes    int64   `json:"used_bytes"`
	FreeBytes    int64   `json:"free_bytes"`
	TotalBytes   int64   `json:"total_bytes"`
	UsagePercent float64 `json:"usage_percent"`
}

type LocalityHeatEntry struct {
	Key                        string `json:"key"`
	RootfsType                 string `json:"rootfs_type"`
	MountType                  string `json:"mount_type"`
	Mounted                    bool   `json:"mounted"`
	RetainedRuntimeCount       int    `json:"retained_runtime_count"`
	RetainedRootfsCount        int    `json:"retained_rootfs_count"`
	RunningContainerCount      int    `json:"running_container_count"`
	NydusDaemonAlive           bool   `json:"nydus_daemon_alive"`
	ChunkDBTotalChunks         int64  `json:"chunkdb_total_chunks"`
	ChunkDBUsedBytes           int64  `json:"chunkdb_used_bytes"`
	ChunkDBRecentAccessAgeSecs int64  `json:"chunkdb_recent_access_age_secs"`
	PeerHealthyCount           int64  `json:"peer_healthy_count"`
	PeerUnhealthyCount         int64  `json:"peer_unhealthy_count"`
	PeerHintedCount            int64  `json:"peer_hinted_count"`
}

type HeatInventory struct {
	MountedImageURLs     []string            `json:"mounted_image_urls"`
	MountedRootfsCount   int                 `json:"mounted_rootfs_count"`
	NydusDaemonCount     int                 `json:"nydus_daemon_count"`
	RetainedRuntimeCount int                 `json:"retained_runtime_count"`
	RetainedRootfsCount  int                 `json:"retained_rootfs_count"`
	Locality             []LocalityHeatEntry `json:"locality"`
	ChunkDB              ChunkDBHeat         `json:"chunkdb"`
}

type NodeInventorySnapshot struct {
	Version    string                  `json:"version"`
	Node       NodeInfo                `json:"node"`
	Resources  ResourceInventory       `json:"resources"`
	Pools      PoolsInventory          `json:"pools"`
	Storage    []StorageInventoryEntry `json:"storage,omitempty"`
	Components ComponentsInventory     `json:"components"`
	Heat       HeatInventory           `json:"heat"`
	Sources    map[string]SourceStatus `json:"sources"`
	// AllocationMemoryObservations are a bounded control-plane report payload,
	// not part of the operator-facing aggregate inventory JSON.
	AllocationMemoryObservations []*nodev1.AllocationMemoryObservation `json:"-"`
}

func NewSnapshot() NodeInventorySnapshot {
	return NodeInventorySnapshot{
		Version: SnapshotVersion,
		Node: NodeInfo{
			Labels: map[string]string{},
		},
		Heat: HeatInventory{
			MountedImageURLs: []string{},
			Locality:         []LocalityHeatEntry{},
		},
		Sources:                      make(map[string]SourceStatus),
		AllocationMemoryObservations: []*nodev1.AllocationMemoryObservation{},
	}
}
