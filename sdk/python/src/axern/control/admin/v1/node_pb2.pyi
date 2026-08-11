import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from axern.control.capability.v1 import capability_pb2 as _capability_pb2
from axern.control.node.v1 import node_control_pb2 as _node_control_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AdminNodeLifecycleStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ADMIN_NODE_LIFECYCLE_STATUS_UNSPECIFIED: _ClassVar[AdminNodeLifecycleStatus]
    ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE: _ClassVar[AdminNodeLifecycleStatus]
    ADMIN_NODE_LIFECYCLE_STATUS_RETIRED: _ClassVar[AdminNodeLifecycleStatus]
ADMIN_NODE_LIFECYCLE_STATUS_UNSPECIFIED: AdminNodeLifecycleStatus
ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE: AdminNodeLifecycleStatus
ADMIN_NODE_LIFECYCLE_STATUS_RETIRED: AdminNodeLifecycleStatus

class AdminNode(_message.Message):
    __slots__ = ("node_id", "lifecycle_status", "heartbeat_fresh", "summary_fresh", "axnoded_ready", "heartbeat_age_seconds", "summary_age_seconds", "registered_at", "updated_at", "retired_at", "retired_reason")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    LIFECYCLE_STATUS_FIELD_NUMBER: _ClassVar[int]
    HEARTBEAT_FRESH_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FRESH_FIELD_NUMBER: _ClassVar[int]
    AXNODED_READY_FIELD_NUMBER: _ClassVar[int]
    HEARTBEAT_AGE_SECONDS_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_AGE_SECONDS_FIELD_NUMBER: _ClassVar[int]
    REGISTERED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    RETIRED_AT_FIELD_NUMBER: _ClassVar[int]
    RETIRED_REASON_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    lifecycle_status: AdminNodeLifecycleStatus
    heartbeat_fresh: bool
    summary_fresh: bool
    axnoded_ready: bool
    heartbeat_age_seconds: int
    summary_age_seconds: int
    registered_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    retired_at: _timestamp_pb2.Timestamp
    retired_reason: str
    def __init__(self, node_id: _Optional[str] = ..., lifecycle_status: _Optional[_Union[AdminNodeLifecycleStatus, str]] = ..., heartbeat_fresh: _Optional[bool] = ..., summary_fresh: _Optional[bool] = ..., axnoded_ready: _Optional[bool] = ..., heartbeat_age_seconds: _Optional[int] = ..., summary_age_seconds: _Optional[int] = ..., registered_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., retired_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., retired_reason: _Optional[str] = ...) -> None: ...

class ListAdminNodesRequest(_message.Message):
    __slots__ = ("lifecycle_status",)
    LIFECYCLE_STATUS_FIELD_NUMBER: _ClassVar[int]
    lifecycle_status: AdminNodeLifecycleStatus
    def __init__(self, lifecycle_status: _Optional[_Union[AdminNodeLifecycleStatus, str]] = ...) -> None: ...

class ListAdminNodesResponse(_message.Message):
    __slots__ = ("nodes",)
    NODES_FIELD_NUMBER: _ClassVar[int]
    nodes: _containers.RepeatedCompositeFieldContainer[AdminNode]
    def __init__(self, nodes: _Optional[_Iterable[_Union[AdminNode, _Mapping]]] = ...) -> None: ...

class RetireAdminNodeRequest(_message.Message):
    __slots__ = ("node_id", "operator_reason")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    operator_reason: str
    def __init__(self, node_id: _Optional[str] = ..., operator_reason: _Optional[str] = ...) -> None: ...

class RetireAdminNodeResponse(_message.Message):
    __slots__ = ("node",)
    NODE_FIELD_NUMBER: _ClassVar[int]
    node: AdminNode
    def __init__(self, node: _Optional[_Union[AdminNode, _Mapping]] = ...) -> None: ...

class GetNodeCapabilitySnapshotRequest(_message.Message):
    __slots__ = ("node_id",)
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    def __init__(self, node_id: _Optional[str] = ...) -> None: ...

class GetNodeCapabilitySnapshotResponse(_message.Message):
    __slots__ = ("snapshot",)
    SNAPSHOT_FIELD_NUMBER: _ClassVar[int]
    snapshot: _capability_pb2.CapabilitySnapshot
    def __init__(self, snapshot: _Optional[_Union[_capability_pb2.CapabilitySnapshot, _Mapping]] = ...) -> None: ...

class AdminCapabilityTransition(_message.Message):
    __slots__ = ("transition_id", "node_id", "snapshot_id", "snapshot_sequence", "key", "old_state", "new_state", "old_evidence", "new_evidence", "old_reason_code", "new_reason_code", "reason", "observed_at", "reported_at")
    TRANSITION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    SNAPSHOT_ID_FIELD_NUMBER: _ClassVar[int]
    SNAPSHOT_SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    OLD_STATE_FIELD_NUMBER: _ClassVar[int]
    NEW_STATE_FIELD_NUMBER: _ClassVar[int]
    OLD_EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    NEW_EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    OLD_REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    NEW_REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_AT_FIELD_NUMBER: _ClassVar[int]
    REPORTED_AT_FIELD_NUMBER: _ClassVar[int]
    transition_id: str
    node_id: str
    snapshot_id: str
    snapshot_sequence: int
    key: _capability_pb2.CapabilityKey
    old_state: _capability_pb2.CapabilityState
    new_state: _capability_pb2.CapabilityState
    old_evidence: _capability_pb2.CapabilityEvidence
    new_evidence: _capability_pb2.CapabilityEvidence
    old_reason_code: _capability_pb2.CapabilityReasonCode
    new_reason_code: _capability_pb2.CapabilityReasonCode
    reason: str
    observed_at: _timestamp_pb2.Timestamp
    reported_at: _timestamp_pb2.Timestamp
    def __init__(self, transition_id: _Optional[str] = ..., node_id: _Optional[str] = ..., snapshot_id: _Optional[str] = ..., snapshot_sequence: _Optional[int] = ..., key: _Optional[_Union[_capability_pb2.CapabilityKey, _Mapping]] = ..., old_state: _Optional[_Union[_capability_pb2.CapabilityState, str]] = ..., new_state: _Optional[_Union[_capability_pb2.CapabilityState, str]] = ..., old_evidence: _Optional[_Union[_capability_pb2.CapabilityEvidence, _Mapping]] = ..., new_evidence: _Optional[_Union[_capability_pb2.CapabilityEvidence, _Mapping]] = ..., old_reason_code: _Optional[_Union[_capability_pb2.CapabilityReasonCode, str]] = ..., new_reason_code: _Optional[_Union[_capability_pb2.CapabilityReasonCode, str]] = ..., reason: _Optional[str] = ..., observed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., reported_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ListNodeCapabilityTransitionsRequest(_message.Message):
    __slots__ = ("node_id", "limit")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    limit: int
    def __init__(self, node_id: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class ListNodeCapabilityTransitionsResponse(_message.Message):
    __slots__ = ("transitions",)
    TRANSITIONS_FIELD_NUMBER: _ClassVar[int]
    transitions: _containers.RepeatedCompositeFieldContainer[AdminCapabilityTransition]
    def __init__(self, transitions: _Optional[_Iterable[_Union[AdminCapabilityTransition, _Mapping]]] = ...) -> None: ...

class AdminCapabilityReconcileItem(_message.Message):
    __slots__ = ("allocation_id", "node_id", "pending_dependencies", "attempts", "next_run_at", "lease_expires_at", "last_error", "updated_at")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    PENDING_DEPENDENCIES_FIELD_NUMBER: _ClassVar[int]
    ATTEMPTS_FIELD_NUMBER: _ClassVar[int]
    NEXT_RUN_AT_FIELD_NUMBER: _ClassVar[int]
    LEASE_EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_ERROR_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    node_id: str
    pending_dependencies: _containers.RepeatedCompositeFieldContainer[_capability_pb2.CapabilityDependency]
    attempts: int
    next_run_at: _timestamp_pb2.Timestamp
    lease_expires_at: _timestamp_pb2.Timestamp
    last_error: str
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., pending_dependencies: _Optional[_Iterable[_Union[_capability_pb2.CapabilityDependency, _Mapping]]] = ..., attempts: _Optional[int] = ..., next_run_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., lease_expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_error: _Optional[str] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ListCapabilityReconcileQueueRequest(_message.Message):
    __slots__ = ("node_id", "limit")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    limit: int
    def __init__(self, node_id: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class ListCapabilityReconcileQueueResponse(_message.Message):
    __slots__ = ("items",)
    ITEMS_FIELD_NUMBER: _ClassVar[int]
    items: _containers.RepeatedCompositeFieldContainer[AdminCapabilityReconcileItem]
    def __init__(self, items: _Optional[_Iterable[_Union[AdminCapabilityReconcileItem, _Mapping]]] = ...) -> None: ...

class GetAllocationCapabilityDiagnosticsRequest(_message.Message):
    __slots__ = ("allocation_id",)
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    def __init__(self, allocation_id: _Optional[str] = ...) -> None: ...

class GetAllocationCapabilityDiagnosticsResponse(_message.Message):
    __slots__ = ("allocation_id", "node_id", "required_dependencies", "admitted_dependencies", "condition_set", "reconcile", "allocation_attempt", "create_admission_recorded", "create_dependency_set_digest", "create_admitted_at", "memory_admission", "latest_memory_observation")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_DEPENDENCIES_FIELD_NUMBER: _ClassVar[int]
    ADMITTED_DEPENDENCIES_FIELD_NUMBER: _ClassVar[int]
    CONDITION_SET_FIELD_NUMBER: _ClassVar[int]
    RECONCILE_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    CREATE_ADMISSION_RECORDED_FIELD_NUMBER: _ClassVar[int]
    CREATE_DEPENDENCY_SET_DIGEST_FIELD_NUMBER: _ClassVar[int]
    CREATE_ADMITTED_AT_FIELD_NUMBER: _ClassVar[int]
    MEMORY_ADMISSION_FIELD_NUMBER: _ClassVar[int]
    LATEST_MEMORY_OBSERVATION_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    node_id: str
    required_dependencies: _containers.RepeatedCompositeFieldContainer[_capability_pb2.CapabilityDependency]
    admitted_dependencies: _containers.RepeatedCompositeFieldContainer[_capability_pb2.CapabilityDependency]
    condition_set: _capability_pb2.CapabilityConditionSet
    reconcile: AdminCapabilityReconcileItem
    allocation_attempt: int
    create_admission_recorded: bool
    create_dependency_set_digest: str
    create_admitted_at: _timestamp_pb2.Timestamp
    memory_admission: AllocationMemoryAdmissionEvidence
    latest_memory_observation: _node_control_pb2.AllocationMemoryObservation
    def __init__(self, allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., required_dependencies: _Optional[_Iterable[_Union[_capability_pb2.CapabilityDependency, _Mapping]]] = ..., admitted_dependencies: _Optional[_Iterable[_Union[_capability_pb2.CapabilityDependency, _Mapping]]] = ..., condition_set: _Optional[_Union[_capability_pb2.CapabilityConditionSet, _Mapping]] = ..., reconcile: _Optional[_Union[AdminCapabilityReconcileItem, _Mapping]] = ..., allocation_attempt: _Optional[int] = ..., create_admission_recorded: _Optional[bool] = ..., create_dependency_set_digest: _Optional[str] = ..., create_admitted_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., memory_admission: _Optional[_Union[AllocationMemoryAdmissionEvidence, _Mapping]] = ..., latest_memory_observation: _Optional[_Union[_node_control_pb2.AllocationMemoryObservation, _Mapping]] = ...) -> None: ...

class AllocationMemoryAdmissionEvidence(_message.Message):
    __slots__ = ("sandbox_memory_request_bytes", "sandbox_memory_limit_bytes", "node_memory_budget", "summary_collected_at", "node_local_commitment_bytes", "admitted_at")
    SANDBOX_MEMORY_REQUEST_BYTES_FIELD_NUMBER: _ClassVar[int]
    SANDBOX_MEMORY_LIMIT_BYTES_FIELD_NUMBER: _ClassVar[int]
    NODE_MEMORY_BUDGET_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_COLLECTED_AT_FIELD_NUMBER: _ClassVar[int]
    NODE_LOCAL_COMMITMENT_BYTES_FIELD_NUMBER: _ClassVar[int]
    ADMITTED_AT_FIELD_NUMBER: _ClassVar[int]
    sandbox_memory_request_bytes: int
    sandbox_memory_limit_bytes: int
    node_memory_budget: _node_control_pb2.NodeMemoryBudget
    summary_collected_at: _timestamp_pb2.Timestamp
    node_local_commitment_bytes: int
    admitted_at: _timestamp_pb2.Timestamp
    def __init__(self, sandbox_memory_request_bytes: _Optional[int] = ..., sandbox_memory_limit_bytes: _Optional[int] = ..., node_memory_budget: _Optional[_Union[_node_control_pb2.NodeMemoryBudget, _Mapping]] = ..., summary_collected_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., node_local_commitment_bytes: _Optional[int] = ..., admitted_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...
