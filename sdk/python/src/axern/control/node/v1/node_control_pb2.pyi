import datetime

from axern.control.common.v1 import common_pb2 as _common_pb2
from axern.control.tunnel.v1 import tunnel_pb2 as _tunnel_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RootfsType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ROOTFS_TYPE_UNSPECIFIED: _ClassVar[RootfsType]
    ROOTFS_TYPE_LOCAL: _ClassVar[RootfsType]
    ROOTFS_TYPE_IMAGE: _ClassVar[RootfsType]
    ROOTFS_TYPE_S3: _ClassVar[RootfsType]

class MountType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MOUNT_TYPE_UNSPECIFIED: _ClassVar[MountType]
    MOUNT_TYPE_LOCAL: _ClassVar[MountType]
    MOUNT_TYPE_OCI: _ClassVar[MountType]
    MOUNT_TYPE_NYDUS: _ClassVar[MountType]
    MOUNT_TYPE_OSS: _ClassVar[MountType]

class ComponentState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    COMPONENT_STATE_UNSPECIFIED: _ClassVar[ComponentState]
    COMPONENT_STATE_READY: _ClassVar[ComponentState]
    COMPONENT_STATE_WARMING: _ClassVar[ComponentState]
    COMPONENT_STATE_DEGRADED: _ClassVar[ComponentState]
    COMPONENT_STATE_ERROR: _ClassVar[ComponentState]
    COMPONENT_STATE_DISABLED: _ClassVar[ComponentState]

class NodeState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    NODE_STATE_UNSPECIFIED: _ClassVar[NodeState]
    NODE_STATE_READY: _ClassVar[NodeState]
    NODE_STATE_DRAINING: _ClassVar[NodeState]
    NODE_STATE_DISABLED: _ClassVar[NodeState]
    NODE_STATE_UNREACHABLE: _ClassVar[NodeState]

class PlacementCandidateState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PLACEMENT_CANDIDATE_STATE_UNSPECIFIED: _ClassVar[PlacementCandidateState]
    PLACEMENT_CANDIDATE_STATE_ELIGIBLE: _ClassVar[PlacementCandidateState]
    PLACEMENT_CANDIDATE_STATE_REJECTED: _ClassVar[PlacementCandidateState]

class PlacementRejectionReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PLACEMENT_REJECTION_REASON_UNSPECIFIED: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_STALE_HEARTBEAT: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_STALE_SUMMARY: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_RUNTIME_UNSUPPORTED: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_AXNODED_NOT_READY: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_IMAGEMGR_UNAVAILABLE: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_IMAGEFSD_UNAVAILABLE: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_NODE_DRAINING: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_NODE_DISABLED: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_NODE_SELECTOR_MISMATCH: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_INSUFFICIENT_CPU: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_PORTS_UNSUPPORTED: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_NETWORK_UNSUPPORTED: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_CAPABILITY_UNSUPPORTED: _ClassVar[PlacementRejectionReason]
    PLACEMENT_REJECTION_REASON_NODE_RETIRED: _ClassVar[PlacementRejectionReason]
ROOTFS_TYPE_UNSPECIFIED: RootfsType
ROOTFS_TYPE_LOCAL: RootfsType
ROOTFS_TYPE_IMAGE: RootfsType
ROOTFS_TYPE_S3: RootfsType
MOUNT_TYPE_UNSPECIFIED: MountType
MOUNT_TYPE_LOCAL: MountType
MOUNT_TYPE_OCI: MountType
MOUNT_TYPE_NYDUS: MountType
MOUNT_TYPE_OSS: MountType
COMPONENT_STATE_UNSPECIFIED: ComponentState
COMPONENT_STATE_READY: ComponentState
COMPONENT_STATE_WARMING: ComponentState
COMPONENT_STATE_DEGRADED: ComponentState
COMPONENT_STATE_ERROR: ComponentState
COMPONENT_STATE_DISABLED: ComponentState
NODE_STATE_UNSPECIFIED: NodeState
NODE_STATE_READY: NodeState
NODE_STATE_DRAINING: NodeState
NODE_STATE_DISABLED: NodeState
NODE_STATE_UNREACHABLE: NodeState
PLACEMENT_CANDIDATE_STATE_UNSPECIFIED: PlacementCandidateState
PLACEMENT_CANDIDATE_STATE_ELIGIBLE: PlacementCandidateState
PLACEMENT_CANDIDATE_STATE_REJECTED: PlacementCandidateState
PLACEMENT_REJECTION_REASON_UNSPECIFIED: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_STALE_HEARTBEAT: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_STALE_SUMMARY: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_RUNTIME_UNSUPPORTED: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_AXNODED_NOT_READY: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_IMAGEMGR_UNAVAILABLE: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_IMAGEFSD_UNAVAILABLE: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_NODE_DRAINING: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_NODE_DISABLED: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_NODE_SELECTOR_MISMATCH: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_INSUFFICIENT_CPU: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_INSUFFICIENT_MEMORY: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_PORTS_UNSUPPORTED: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_NETWORK_UNSUPPORTED: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_CAPABILITY_UNSUPPORTED: PlacementRejectionReason
PLACEMENT_REJECTION_REASON_NODE_RETIRED: PlacementRejectionReason

class PoolState(_message.Message):
    __slots__ = ("using", "idle", "capacity", "unavailable")
    USING_FIELD_NUMBER: _ClassVar[int]
    IDLE_FIELD_NUMBER: _ClassVar[int]
    CAPACITY_FIELD_NUMBER: _ClassVar[int]
    UNAVAILABLE_FIELD_NUMBER: _ClassVar[int]
    using: int
    idle: int
    capacity: int
    unavailable: int
    def __init__(self, using: _Optional[int] = ..., idle: _Optional[int] = ..., capacity: _Optional[int] = ..., unavailable: _Optional[int] = ...) -> None: ...

class ResourcesSummary(_message.Message):
    __slots__ = ("axnoded_committed_milli", "axnoded_used_milli", "axnoded_cpu_unbounded_count", "axnoded_committed_bytes", "axnoded_used_bytes", "axnoded_memory_unbounded_count")
    AXNODED_COMMITTED_MILLI_FIELD_NUMBER: _ClassVar[int]
    AXNODED_USED_MILLI_FIELD_NUMBER: _ClassVar[int]
    AXNODED_CPU_UNBOUNDED_COUNT_FIELD_NUMBER: _ClassVar[int]
    AXNODED_COMMITTED_BYTES_FIELD_NUMBER: _ClassVar[int]
    AXNODED_USED_BYTES_FIELD_NUMBER: _ClassVar[int]
    AXNODED_MEMORY_UNBOUNDED_COUNT_FIELD_NUMBER: _ClassVar[int]
    axnoded_committed_milli: int
    axnoded_used_milli: int
    axnoded_cpu_unbounded_count: int
    axnoded_committed_bytes: int
    axnoded_used_bytes: int
    axnoded_memory_unbounded_count: int
    def __init__(self, axnoded_committed_milli: _Optional[int] = ..., axnoded_used_milli: _Optional[int] = ..., axnoded_cpu_unbounded_count: _Optional[int] = ..., axnoded_committed_bytes: _Optional[int] = ..., axnoded_used_bytes: _Optional[int] = ..., axnoded_memory_unbounded_count: _Optional[int] = ...) -> None: ...

class PoolsSummary(_message.Message):
    __slots__ = ("cgroup", "interface", "runtime_slots")
    CGROUP_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_FIELD_NUMBER: _ClassVar[int]
    RUNTIME_SLOTS_FIELD_NUMBER: _ClassVar[int]
    cgroup: PoolState
    interface: PoolState
    runtime_slots: PoolState
    def __init__(self, cgroup: _Optional[_Union[PoolState, _Mapping]] = ..., interface: _Optional[_Union[PoolState, _Mapping]] = ..., runtime_slots: _Optional[_Union[PoolState, _Mapping]] = ...) -> None: ...

class AxnodedSummary(_message.Message):
    __slots__ = ("state", "ready", "running_containers", "running_allocation_ids", "active_allocation_ids")
    STATE_FIELD_NUMBER: _ClassVar[int]
    READY_FIELD_NUMBER: _ClassVar[int]
    RUNNING_CONTAINERS_FIELD_NUMBER: _ClassVar[int]
    RUNNING_ALLOCATION_IDS_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_ALLOCATION_IDS_FIELD_NUMBER: _ClassVar[int]
    state: ComponentState
    ready: bool
    running_containers: int
    running_allocation_ids: _containers.RepeatedScalarFieldContainer[str]
    active_allocation_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, state: _Optional[_Union[ComponentState, str]] = ..., ready: _Optional[bool] = ..., running_containers: _Optional[int] = ..., running_allocation_ids: _Optional[_Iterable[str]] = ..., active_allocation_ids: _Optional[_Iterable[str]] = ...) -> None: ...

class ImagemgrSummary(_message.Message):
    __slots__ = ("state", "reachable", "daemon_count", "mounted_image_count", "imported_image_count")
    STATE_FIELD_NUMBER: _ClassVar[int]
    REACHABLE_FIELD_NUMBER: _ClassVar[int]
    DAEMON_COUNT_FIELD_NUMBER: _ClassVar[int]
    MOUNTED_IMAGE_COUNT_FIELD_NUMBER: _ClassVar[int]
    IMPORTED_IMAGE_COUNT_FIELD_NUMBER: _ClassVar[int]
    state: ComponentState
    reachable: bool
    daemon_count: int
    mounted_image_count: int
    imported_image_count: int
    def __init__(self, state: _Optional[_Union[ComponentState, str]] = ..., reachable: _Optional[bool] = ..., daemon_count: _Optional[int] = ..., mounted_image_count: _Optional[int] = ..., imported_image_count: _Optional[int] = ...) -> None: ...

class ImagefsdSummary(_message.Message):
    __slots__ = ("state", "reachable", "chunkdb_present", "chunk_count", "chunkdb_used_bytes", "chunkdb_usage_percent")
    STATE_FIELD_NUMBER: _ClassVar[int]
    REACHABLE_FIELD_NUMBER: _ClassVar[int]
    CHUNKDB_PRESENT_FIELD_NUMBER: _ClassVar[int]
    CHUNK_COUNT_FIELD_NUMBER: _ClassVar[int]
    CHUNKDB_USED_BYTES_FIELD_NUMBER: _ClassVar[int]
    CHUNKDB_USAGE_PERCENT_FIELD_NUMBER: _ClassVar[int]
    state: ComponentState
    reachable: bool
    chunkdb_present: bool
    chunk_count: int
    chunkdb_used_bytes: int
    chunkdb_usage_percent: float
    def __init__(self, state: _Optional[_Union[ComponentState, str]] = ..., reachable: _Optional[bool] = ..., chunkdb_present: _Optional[bool] = ..., chunk_count: _Optional[int] = ..., chunkdb_used_bytes: _Optional[int] = ..., chunkdb_usage_percent: _Optional[float] = ...) -> None: ...

class BpfNetSummary(_message.Message):
    __slots__ = ("state", "enabled", "ready", "mode", "needs_snat_fallback", "needs_full_dnat_fallback", "needs_localhost_compat")
    STATE_FIELD_NUMBER: _ClassVar[int]
    ENABLED_FIELD_NUMBER: _ClassVar[int]
    READY_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    NEEDS_SNAT_FALLBACK_FIELD_NUMBER: _ClassVar[int]
    NEEDS_FULL_DNAT_FALLBACK_FIELD_NUMBER: _ClassVar[int]
    NEEDS_LOCALHOST_COMPAT_FIELD_NUMBER: _ClassVar[int]
    state: ComponentState
    enabled: bool
    ready: bool
    mode: str
    needs_snat_fallback: bool
    needs_full_dnat_fallback: bool
    needs_localhost_compat: bool
    def __init__(self, state: _Optional[_Union[ComponentState, str]] = ..., enabled: _Optional[bool] = ..., ready: _Optional[bool] = ..., mode: _Optional[str] = ..., needs_snat_fallback: _Optional[bool] = ..., needs_full_dnat_fallback: _Optional[bool] = ..., needs_localhost_compat: _Optional[bool] = ...) -> None: ...

class VolumedSummary(_message.Message):
    __slots__ = ("state", "reachable", "published_volume_count", "last_reconcile_at", "last_reconcile_error", "last_reconcile_retained_count", "last_reconcile_unpublished_count", "last_reconcile_active_allocation_count", "last_reconcile_stale_allocation_count", "last_reconcile_invalid_volume_count")
    STATE_FIELD_NUMBER: _ClassVar[int]
    REACHABLE_FIELD_NUMBER: _ClassVar[int]
    PUBLISHED_VOLUME_COUNT_FIELD_NUMBER: _ClassVar[int]
    LAST_RECONCILE_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_RECONCILE_ERROR_FIELD_NUMBER: _ClassVar[int]
    LAST_RECONCILE_RETAINED_COUNT_FIELD_NUMBER: _ClassVar[int]
    LAST_RECONCILE_UNPUBLISHED_COUNT_FIELD_NUMBER: _ClassVar[int]
    LAST_RECONCILE_ACTIVE_ALLOCATION_COUNT_FIELD_NUMBER: _ClassVar[int]
    LAST_RECONCILE_STALE_ALLOCATION_COUNT_FIELD_NUMBER: _ClassVar[int]
    LAST_RECONCILE_INVALID_VOLUME_COUNT_FIELD_NUMBER: _ClassVar[int]
    state: ComponentState
    reachable: bool
    published_volume_count: int
    last_reconcile_at: _timestamp_pb2.Timestamp
    last_reconcile_error: str
    last_reconcile_retained_count: int
    last_reconcile_unpublished_count: int
    last_reconcile_active_allocation_count: int
    last_reconcile_stale_allocation_count: int
    last_reconcile_invalid_volume_count: int
    def __init__(self, state: _Optional[_Union[ComponentState, str]] = ..., reachable: _Optional[bool] = ..., published_volume_count: _Optional[int] = ..., last_reconcile_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_reconcile_error: _Optional[str] = ..., last_reconcile_retained_count: _Optional[int] = ..., last_reconcile_unpublished_count: _Optional[int] = ..., last_reconcile_active_allocation_count: _Optional[int] = ..., last_reconcile_stale_allocation_count: _Optional[int] = ..., last_reconcile_invalid_volume_count: _Optional[int] = ...) -> None: ...

class NodeStorageSummary(_message.Message):
    __slots__ = ("target", "capacity_bytes", "used_bytes", "available_bytes", "inodes_total", "inodes_used", "inodes_available", "collected", "error")
    TARGET_FIELD_NUMBER: _ClassVar[int]
    CAPACITY_BYTES_FIELD_NUMBER: _ClassVar[int]
    USED_BYTES_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_BYTES_FIELD_NUMBER: _ClassVar[int]
    INODES_TOTAL_FIELD_NUMBER: _ClassVar[int]
    INODES_USED_FIELD_NUMBER: _ClassVar[int]
    INODES_AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    COLLECTED_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    target: str
    capacity_bytes: int
    used_bytes: int
    available_bytes: int
    inodes_total: int
    inodes_used: int
    inodes_available: int
    collected: bool
    error: str
    def __init__(self, target: _Optional[str] = ..., capacity_bytes: _Optional[int] = ..., used_bytes: _Optional[int] = ..., available_bytes: _Optional[int] = ..., inodes_total: _Optional[int] = ..., inodes_used: _Optional[int] = ..., inodes_available: _Optional[int] = ..., collected: _Optional[bool] = ..., error: _Optional[str] = ...) -> None: ...

class ComponentsSummary(_message.Message):
    __slots__ = ("axnoded", "imagemgr", "imagefsd", "bpfnet", "volumed")
    AXNODED_FIELD_NUMBER: _ClassVar[int]
    IMAGEMGR_FIELD_NUMBER: _ClassVar[int]
    IMAGEFSD_FIELD_NUMBER: _ClassVar[int]
    BPFNET_FIELD_NUMBER: _ClassVar[int]
    VOLUMED_FIELD_NUMBER: _ClassVar[int]
    axnoded: AxnodedSummary
    imagemgr: ImagemgrSummary
    imagefsd: ImagefsdSummary
    bpfnet: BpfNetSummary
    volumed: VolumedSummary
    def __init__(self, axnoded: _Optional[_Union[AxnodedSummary, _Mapping]] = ..., imagemgr: _Optional[_Union[ImagemgrSummary, _Mapping]] = ..., imagefsd: _Optional[_Union[ImagefsdSummary, _Mapping]] = ..., bpfnet: _Optional[_Union[BpfNetSummary, _Mapping]] = ..., volumed: _Optional[_Union[VolumedSummary, _Mapping]] = ...) -> None: ...

class LocalitySummary(_message.Message):
    __slots__ = ("key", "rootfs_type", "mount_type", "mounted", "retained_runtime_count", "retained_rootfs_count", "running_container_count", "nydus_daemon_alive", "chunkdb_total_chunks", "chunkdb_used_bytes", "chunkdb_recent_access_age_secs", "peer_healthy_count", "peer_unhealthy_count", "peer_hinted_count", "environment_id")
    KEY_FIELD_NUMBER: _ClassVar[int]
    ROOTFS_TYPE_FIELD_NUMBER: _ClassVar[int]
    MOUNT_TYPE_FIELD_NUMBER: _ClassVar[int]
    MOUNTED_FIELD_NUMBER: _ClassVar[int]
    RETAINED_RUNTIME_COUNT_FIELD_NUMBER: _ClassVar[int]
    RETAINED_ROOTFS_COUNT_FIELD_NUMBER: _ClassVar[int]
    RUNNING_CONTAINER_COUNT_FIELD_NUMBER: _ClassVar[int]
    NYDUS_DAEMON_ALIVE_FIELD_NUMBER: _ClassVar[int]
    CHUNKDB_TOTAL_CHUNKS_FIELD_NUMBER: _ClassVar[int]
    CHUNKDB_USED_BYTES_FIELD_NUMBER: _ClassVar[int]
    CHUNKDB_RECENT_ACCESS_AGE_SECS_FIELD_NUMBER: _ClassVar[int]
    PEER_HEALTHY_COUNT_FIELD_NUMBER: _ClassVar[int]
    PEER_UNHEALTHY_COUNT_FIELD_NUMBER: _ClassVar[int]
    PEER_HINTED_COUNT_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    key: str
    rootfs_type: RootfsType
    mount_type: MountType
    mounted: bool
    retained_runtime_count: int
    retained_rootfs_count: int
    running_container_count: int
    nydus_daemon_alive: bool
    chunkdb_total_chunks: int
    chunkdb_used_bytes: int
    chunkdb_recent_access_age_secs: int
    peer_healthy_count: int
    peer_unhealthy_count: int
    peer_hinted_count: int
    environment_id: str
    def __init__(self, key: _Optional[str] = ..., rootfs_type: _Optional[_Union[RootfsType, str]] = ..., mount_type: _Optional[_Union[MountType, str]] = ..., mounted: _Optional[bool] = ..., retained_runtime_count: _Optional[int] = ..., retained_rootfs_count: _Optional[int] = ..., running_container_count: _Optional[int] = ..., nydus_daemon_alive: _Optional[bool] = ..., chunkdb_total_chunks: _Optional[int] = ..., chunkdb_used_bytes: _Optional[int] = ..., chunkdb_recent_access_age_secs: _Optional[int] = ..., peer_healthy_count: _Optional[int] = ..., peer_unhealthy_count: _Optional[int] = ..., peer_hinted_count: _Optional[int] = ..., environment_id: _Optional[str] = ...) -> None: ...

class NodeSummary(_message.Message):
    __slots__ = ("collected_at", "resources", "pools", "components", "locality", "node_state", "labels", "capabilities", "capacity", "allocatable", "storage")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    COLLECTED_AT_FIELD_NUMBER: _ClassVar[int]
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    POOLS_FIELD_NUMBER: _ClassVar[int]
    COMPONENTS_FIELD_NUMBER: _ClassVar[int]
    LOCALITY_FIELD_NUMBER: _ClassVar[int]
    NODE_STATE_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    CAPACITY_FIELD_NUMBER: _ClassVar[int]
    ALLOCATABLE_FIELD_NUMBER: _ClassVar[int]
    STORAGE_FIELD_NUMBER: _ClassVar[int]
    collected_at: _timestamp_pb2.Timestamp
    resources: ResourcesSummary
    pools: PoolsSummary
    components: ComponentsSummary
    locality: _containers.RepeatedCompositeFieldContainer[LocalitySummary]
    node_state: NodeState
    labels: _containers.ScalarMap[str, str]
    capabilities: _containers.RepeatedScalarFieldContainer[str]
    capacity: _common_pb2.ResourceQuantity
    allocatable: _common_pb2.ResourceQuantity
    storage: _containers.RepeatedCompositeFieldContainer[NodeStorageSummary]
    def __init__(self, collected_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., resources: _Optional[_Union[ResourcesSummary, _Mapping]] = ..., pools: _Optional[_Union[PoolsSummary, _Mapping]] = ..., components: _Optional[_Union[ComponentsSummary, _Mapping]] = ..., locality: _Optional[_Iterable[_Union[LocalitySummary, _Mapping]]] = ..., node_state: _Optional[_Union[NodeState, str]] = ..., labels: _Optional[_Mapping[str, str]] = ..., capabilities: _Optional[_Iterable[str]] = ..., capacity: _Optional[_Union[_common_pb2.ResourceQuantity, _Mapping]] = ..., allocatable: _Optional[_Union[_common_pb2.ResourceQuantity, _Mapping]] = ..., storage: _Optional[_Iterable[_Union[NodeStorageSummary, _Mapping]]] = ...) -> None: ...

class RegisterNodeRequest(_message.Message):
    __slots__ = ("node_id", "runtimes", "node_target", "node_auth_token")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    RUNTIMES_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    NODE_AUTH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    runtimes: _containers.RepeatedScalarFieldContainer[str]
    node_target: str
    node_auth_token: str
    def __init__(self, node_id: _Optional[str] = ..., runtimes: _Optional[_Iterable[str]] = ..., node_target: _Optional[str] = ..., node_auth_token: _Optional[str] = ...) -> None: ...

class RegisterNodeResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ReportNodeRequest(_message.Message):
    __slots__ = ("node_id", "runtimes", "summary", "node_target", "node_auth_token")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    RUNTIMES_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    NODE_AUTH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    runtimes: _containers.RepeatedScalarFieldContainer[str]
    summary: NodeSummary
    node_target: str
    node_auth_token: str
    def __init__(self, node_id: _Optional[str] = ..., runtimes: _Optional[_Iterable[str]] = ..., summary: _Optional[_Union[NodeSummary, _Mapping]] = ..., node_target: _Optional[str] = ..., node_auth_token: _Optional[str] = ...) -> None: ...

class ReportNodeResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class AllocationStatusObservation(_message.Message):
    __slots__ = ("allocation_id", "attempt", "status", "exit_code", "exit_code_known", "message", "observed_at", "ready", "readiness_message")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_KNOWN_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_AT_FIELD_NUMBER: _ClassVar[int]
    READY_FIELD_NUMBER: _ClassVar[int]
    READINESS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    status: _common_pb2.AllocationStatus
    exit_code: int
    exit_code_known: bool
    message: str
    observed_at: _timestamp_pb2.Timestamp
    ready: bool
    readiness_message: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., status: _Optional[_Union[_common_pb2.AllocationStatus, str]] = ..., exit_code: _Optional[int] = ..., exit_code_known: _Optional[bool] = ..., message: _Optional[str] = ..., observed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., ready: _Optional[bool] = ..., readiness_message: _Optional[str] = ...) -> None: ...

class BatchReportAllocationStatusRequest(_message.Message):
    __slots__ = ("node_id", "node_auth_token", "observations")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_AUTH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    OBSERVATIONS_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    node_auth_token: str
    observations: _containers.RepeatedCompositeFieldContainer[AllocationStatusObservation]
    def __init__(self, node_id: _Optional[str] = ..., node_auth_token: _Optional[str] = ..., observations: _Optional[_Iterable[_Union[AllocationStatusObservation, _Mapping]]] = ...) -> None: ...

class BatchReportAllocationStatusResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class WatchExecutionLeasesRequest(_message.Message):
    __slots__ = ("node_id", "after_revision", "node_auth_token")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_REVISION_FIELD_NUMBER: _ClassVar[int]
    NODE_AUTH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    after_revision: int
    node_auth_token: str
    def __init__(self, node_id: _Optional[str] = ..., after_revision: _Optional[int] = ..., node_auth_token: _Optional[str] = ...) -> None: ...

class WatchExecutionLeasesResponse(_message.Message):
    __slots__ = ("leases", "current_revision")
    LEASES_FIELD_NUMBER: _ClassVar[int]
    CURRENT_REVISION_FIELD_NUMBER: _ClassVar[int]
    leases: _containers.RepeatedCompositeFieldContainer[_common_pb2.ExecutionLease]
    current_revision: int
    def __init__(self, leases: _Optional[_Iterable[_Union[_common_pb2.ExecutionLease, _Mapping]]] = ..., current_revision: _Optional[int] = ...) -> None: ...

class WatchTunnelSessionsRequest(_message.Message):
    __slots__ = ("node_id", "after_revision", "node_auth_token")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_REVISION_FIELD_NUMBER: _ClassVar[int]
    NODE_AUTH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    after_revision: int
    node_auth_token: str
    def __init__(self, node_id: _Optional[str] = ..., after_revision: _Optional[int] = ..., node_auth_token: _Optional[str] = ...) -> None: ...

class NodeTunnelSession(_message.Message):
    __slots__ = ("session", "node_token")
    SESSION_FIELD_NUMBER: _ClassVar[int]
    NODE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    session: _tunnel_pb2.TunnelSession
    node_token: str
    def __init__(self, session: _Optional[_Union[_tunnel_pb2.TunnelSession, _Mapping]] = ..., node_token: _Optional[str] = ...) -> None: ...

class WatchTunnelSessionsResponse(_message.Message):
    __slots__ = ("sessions", "current_revision")
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    CURRENT_REVISION_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[NodeTunnelSession]
    current_revision: int
    def __init__(self, sessions: _Optional[_Iterable[_Union[NodeTunnelSession, _Mapping]]] = ..., current_revision: _Optional[int] = ...) -> None: ...

class ReportTunnelSessionStatusRequest(_message.Message):
    __slots__ = ("session_id", "status", "reason", "bound_addr", "node_id", "node_auth_token")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    BOUND_ADDR_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_AUTH_TOKEN_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    status: _tunnel_pb2.TunnelSessionStatus
    reason: str
    bound_addr: str
    node_id: str
    node_auth_token: str
    def __init__(self, session_id: _Optional[str] = ..., status: _Optional[_Union[_tunnel_pb2.TunnelSessionStatus, str]] = ..., reason: _Optional[str] = ..., bound_addr: _Optional[str] = ..., node_id: _Optional[str] = ..., node_auth_token: _Optional[str] = ...) -> None: ...

class ReportTunnelSessionStatusResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class PlacementRank(_message.Message):
    __slots__ = ("mounted_match", "retained_rootfs_count", "retained_runtime_count", "nydus_daemon_alive", "chunkdb_recent_access_age_secs", "peer_healthy_count", "peer_hinted_count", "bpfnet_preferred", "idle_pool_ready", "axnoded_used_milli", "axnoded_used_bytes", "axnoded_active_instances")
    MOUNTED_MATCH_FIELD_NUMBER: _ClassVar[int]
    RETAINED_ROOTFS_COUNT_FIELD_NUMBER: _ClassVar[int]
    RETAINED_RUNTIME_COUNT_FIELD_NUMBER: _ClassVar[int]
    NYDUS_DAEMON_ALIVE_FIELD_NUMBER: _ClassVar[int]
    CHUNKDB_RECENT_ACCESS_AGE_SECS_FIELD_NUMBER: _ClassVar[int]
    PEER_HEALTHY_COUNT_FIELD_NUMBER: _ClassVar[int]
    PEER_HINTED_COUNT_FIELD_NUMBER: _ClassVar[int]
    BPFNET_PREFERRED_FIELD_NUMBER: _ClassVar[int]
    IDLE_POOL_READY_FIELD_NUMBER: _ClassVar[int]
    AXNODED_USED_MILLI_FIELD_NUMBER: _ClassVar[int]
    AXNODED_USED_BYTES_FIELD_NUMBER: _ClassVar[int]
    AXNODED_ACTIVE_INSTANCES_FIELD_NUMBER: _ClassVar[int]
    mounted_match: bool
    retained_rootfs_count: int
    retained_runtime_count: int
    nydus_daemon_alive: bool
    chunkdb_recent_access_age_secs: int
    peer_healthy_count: int
    peer_hinted_count: int
    bpfnet_preferred: bool
    idle_pool_ready: bool
    axnoded_used_milli: int
    axnoded_used_bytes: int
    axnoded_active_instances: int
    def __init__(self, mounted_match: _Optional[bool] = ..., retained_rootfs_count: _Optional[int] = ..., retained_runtime_count: _Optional[int] = ..., nydus_daemon_alive: _Optional[bool] = ..., chunkdb_recent_access_age_secs: _Optional[int] = ..., peer_healthy_count: _Optional[int] = ..., peer_hinted_count: _Optional[int] = ..., bpfnet_preferred: _Optional[bool] = ..., idle_pool_ready: _Optional[bool] = ..., axnoded_used_milli: _Optional[int] = ..., axnoded_used_bytes: _Optional[int] = ..., axnoded_active_instances: _Optional[int] = ...) -> None: ...

class PlacementCandidate(_message.Message):
    __slots__ = ("node_id", "state", "rejection_reasons", "heartbeat_age_secs", "summary_age_secs", "pools", "resources", "locality", "rank")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    REJECTION_REASONS_FIELD_NUMBER: _ClassVar[int]
    HEARTBEAT_AGE_SECS_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_AGE_SECS_FIELD_NUMBER: _ClassVar[int]
    POOLS_FIELD_NUMBER: _ClassVar[int]
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    LOCALITY_FIELD_NUMBER: _ClassVar[int]
    RANK_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    state: PlacementCandidateState
    rejection_reasons: _containers.RepeatedScalarFieldContainer[PlacementRejectionReason]
    heartbeat_age_secs: int
    summary_age_secs: int
    pools: PoolsSummary
    resources: ResourcesSummary
    locality: LocalitySummary
    rank: PlacementRank
    def __init__(self, node_id: _Optional[str] = ..., state: _Optional[_Union[PlacementCandidateState, str]] = ..., rejection_reasons: _Optional[_Iterable[_Union[PlacementRejectionReason, str]]] = ..., heartbeat_age_secs: _Optional[int] = ..., summary_age_secs: _Optional[int] = ..., pools: _Optional[_Union[PoolsSummary, _Mapping]] = ..., resources: _Optional[_Union[ResourcesSummary, _Mapping]] = ..., locality: _Optional[_Union[LocalitySummary, _Mapping]] = ..., rank: _Optional[_Union[PlacementRank, _Mapping]] = ...) -> None: ...
