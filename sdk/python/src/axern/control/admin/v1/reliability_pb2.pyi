import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AdminReliabilityStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ADMIN_RELIABILITY_STATUS_UNSPECIFIED: _ClassVar[AdminReliabilityStatus]
    ADMIN_RELIABILITY_STATUS_OK: _ClassVar[AdminReliabilityStatus]
    ADMIN_RELIABILITY_STATUS_DEGRADED: _ClassVar[AdminReliabilityStatus]

class ConsistencyStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONSISTENCY_STATUS_UNSPECIFIED: _ClassVar[ConsistencyStatus]
    CONSISTENCY_STATUS_OK: _ClassVar[ConsistencyStatus]
    CONSISTENCY_STATUS_INCONSISTENT: _ClassVar[ConsistencyStatus]

class ConsistencyIssueSeverity(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONSISTENCY_ISSUE_SEVERITY_UNSPECIFIED: _ClassVar[ConsistencyIssueSeverity]
    CONSISTENCY_ISSUE_SEVERITY_WARNING: _ClassVar[ConsistencyIssueSeverity]
    CONSISTENCY_ISSUE_SEVERITY_ERROR: _ClassVar[ConsistencyIssueSeverity]

class ConsistencyIssueCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONSISTENCY_ISSUE_CODE_UNSPECIFIED: _ClassVar[ConsistencyIssueCode]
    CONSISTENCY_ISSUE_CODE_ACTIVE_ACCESS_GRANT_ON_ENDED_ALLOCATION: _ClassVar[ConsistencyIssueCode]
    CONSISTENCY_ISSUE_CODE_ACTIVE_TUNNEL_ON_ENDED_ALLOCATION: _ClassVar[ConsistencyIssueCode]

class ConsistencyRepairOwner(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONSISTENCY_REPAIR_OWNER_UNSPECIFIED: _ClassVar[ConsistencyRepairOwner]
    CONSISTENCY_REPAIR_OWNER_RUN_CONTROLLER: _ClassVar[ConsistencyRepairOwner]
    CONSISTENCY_REPAIR_OWNER_NODE_LIFECYCLE: _ClassVar[ConsistencyRepairOwner]
    CONSISTENCY_REPAIR_OWNER_TUNNEL_CONTROLLER: _ClassVar[ConsistencyRepairOwner]

class ConsistencyRepairAction(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONSISTENCY_REPAIR_ACTION_UNSPECIFIED: _ClassVar[ConsistencyRepairAction]
    CONSISTENCY_REPAIR_ACTION_RUN_CLEANUP: _ClassVar[ConsistencyRepairAction]
    CONSISTENCY_REPAIR_ACTION_NODE_LIFECYCLE_RECONCILE: _ClassVar[ConsistencyRepairAction]
    CONSISTENCY_REPAIR_ACTION_TUNNEL_LIFECYCLE_RECONCILE: _ClassVar[ConsistencyRepairAction]

class ConsistencyRepairTargetType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONSISTENCY_REPAIR_TARGET_TYPE_UNSPECIFIED: _ClassVar[ConsistencyRepairTargetType]
    CONSISTENCY_REPAIR_TARGET_TYPE_ALLOCATION: _ClassVar[ConsistencyRepairTargetType]
    CONSISTENCY_REPAIR_TARGET_TYPE_RUN: _ClassVar[ConsistencyRepairTargetType]
    CONSISTENCY_REPAIR_TARGET_TYPE_TUNNEL_SESSION: _ClassVar[ConsistencyRepairTargetType]

class AdminReliabilitySignalCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ADMIN_RELIABILITY_SIGNAL_CODE_UNSPECIFIED: _ClassVar[AdminReliabilitySignalCode]
    ADMIN_RELIABILITY_SIGNAL_CODE_CONSISTENCY_ISSUES: _ClassVar[AdminReliabilitySignalCode]
    ADMIN_RELIABILITY_SIGNAL_CODE_ALLOCATION_LIFECYCLE_RETRIES: _ClassVar[AdminReliabilitySignalCode]
    ADMIN_RELIABILITY_SIGNAL_CODE_RECONCILE_FAILURES: _ClassVar[AdminReliabilitySignalCode]
    ADMIN_RELIABILITY_SIGNAL_CODE_NODE_FLEET: _ClassVar[AdminReliabilitySignalCode]
ADMIN_RELIABILITY_STATUS_UNSPECIFIED: AdminReliabilityStatus
ADMIN_RELIABILITY_STATUS_OK: AdminReliabilityStatus
ADMIN_RELIABILITY_STATUS_DEGRADED: AdminReliabilityStatus
CONSISTENCY_STATUS_UNSPECIFIED: ConsistencyStatus
CONSISTENCY_STATUS_OK: ConsistencyStatus
CONSISTENCY_STATUS_INCONSISTENT: ConsistencyStatus
CONSISTENCY_ISSUE_SEVERITY_UNSPECIFIED: ConsistencyIssueSeverity
CONSISTENCY_ISSUE_SEVERITY_WARNING: ConsistencyIssueSeverity
CONSISTENCY_ISSUE_SEVERITY_ERROR: ConsistencyIssueSeverity
CONSISTENCY_ISSUE_CODE_UNSPECIFIED: ConsistencyIssueCode
CONSISTENCY_ISSUE_CODE_ACTIVE_ACCESS_GRANT_ON_ENDED_ALLOCATION: ConsistencyIssueCode
CONSISTENCY_ISSUE_CODE_ACTIVE_TUNNEL_ON_ENDED_ALLOCATION: ConsistencyIssueCode
CONSISTENCY_REPAIR_OWNER_UNSPECIFIED: ConsistencyRepairOwner
CONSISTENCY_REPAIR_OWNER_RUN_CONTROLLER: ConsistencyRepairOwner
CONSISTENCY_REPAIR_OWNER_NODE_LIFECYCLE: ConsistencyRepairOwner
CONSISTENCY_REPAIR_OWNER_TUNNEL_CONTROLLER: ConsistencyRepairOwner
CONSISTENCY_REPAIR_ACTION_UNSPECIFIED: ConsistencyRepairAction
CONSISTENCY_REPAIR_ACTION_RUN_CLEANUP: ConsistencyRepairAction
CONSISTENCY_REPAIR_ACTION_NODE_LIFECYCLE_RECONCILE: ConsistencyRepairAction
CONSISTENCY_REPAIR_ACTION_TUNNEL_LIFECYCLE_RECONCILE: ConsistencyRepairAction
CONSISTENCY_REPAIR_TARGET_TYPE_UNSPECIFIED: ConsistencyRepairTargetType
CONSISTENCY_REPAIR_TARGET_TYPE_ALLOCATION: ConsistencyRepairTargetType
CONSISTENCY_REPAIR_TARGET_TYPE_RUN: ConsistencyRepairTargetType
CONSISTENCY_REPAIR_TARGET_TYPE_TUNNEL_SESSION: ConsistencyRepairTargetType
ADMIN_RELIABILITY_SIGNAL_CODE_UNSPECIFIED: AdminReliabilitySignalCode
ADMIN_RELIABILITY_SIGNAL_CODE_CONSISTENCY_ISSUES: AdminReliabilitySignalCode
ADMIN_RELIABILITY_SIGNAL_CODE_ALLOCATION_LIFECYCLE_RETRIES: AdminReliabilitySignalCode
ADMIN_RELIABILITY_SIGNAL_CODE_RECONCILE_FAILURES: AdminReliabilitySignalCode
ADMIN_RELIABILITY_SIGNAL_CODE_NODE_FLEET: AdminReliabilitySignalCode

class ConsistencyCounts(_message.Message):
    __slots__ = ("active_allocations", "active_access_grants", "active_tunnels", "allocation_lifecycle_retries", "issues")
    ACTIVE_ALLOCATIONS_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_ACCESS_GRANTS_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_TUNNELS_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_LIFECYCLE_RETRIES_FIELD_NUMBER: _ClassVar[int]
    ISSUES_FIELD_NUMBER: _ClassVar[int]
    active_allocations: int
    active_access_grants: int
    active_tunnels: int
    allocation_lifecycle_retries: int
    issues: int
    def __init__(self, active_allocations: _Optional[int] = ..., active_access_grants: _Optional[int] = ..., active_tunnels: _Optional[int] = ..., allocation_lifecycle_retries: _Optional[int] = ..., issues: _Optional[int] = ...) -> None: ...

class ConsistencyIssue(_message.Message):
    __slots__ = ("code", "severity", "allocation_id", "run_id", "node_id", "status", "detail", "repair_owner", "repair_action", "automatic_repair", "repair_target_type", "repair_target_id")
    CODE_FIELD_NUMBER: _ClassVar[int]
    SEVERITY_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    DETAIL_FIELD_NUMBER: _ClassVar[int]
    REPAIR_OWNER_FIELD_NUMBER: _ClassVar[int]
    REPAIR_ACTION_FIELD_NUMBER: _ClassVar[int]
    AUTOMATIC_REPAIR_FIELD_NUMBER: _ClassVar[int]
    REPAIR_TARGET_TYPE_FIELD_NUMBER: _ClassVar[int]
    REPAIR_TARGET_ID_FIELD_NUMBER: _ClassVar[int]
    code: ConsistencyIssueCode
    severity: ConsistencyIssueSeverity
    allocation_id: str
    run_id: str
    node_id: str
    status: str
    detail: str
    repair_owner: ConsistencyRepairOwner
    repair_action: ConsistencyRepairAction
    automatic_repair: bool
    repair_target_type: ConsistencyRepairTargetType
    repair_target_id: str
    def __init__(self, code: _Optional[_Union[ConsistencyIssueCode, str]] = ..., severity: _Optional[_Union[ConsistencyIssueSeverity, str]] = ..., allocation_id: _Optional[str] = ..., run_id: _Optional[str] = ..., node_id: _Optional[str] = ..., status: _Optional[str] = ..., detail: _Optional[str] = ..., repair_owner: _Optional[_Union[ConsistencyRepairOwner, str]] = ..., repair_action: _Optional[_Union[ConsistencyRepairAction, str]] = ..., automatic_repair: _Optional[bool] = ..., repair_target_type: _Optional[_Union[ConsistencyRepairTargetType, str]] = ..., repair_target_id: _Optional[str] = ...) -> None: ...

class ConsistencySnapshot(_message.Message):
    __slots__ = ("status", "counts", "issues", "truncated")
    STATUS_FIELD_NUMBER: _ClassVar[int]
    COUNTS_FIELD_NUMBER: _ClassVar[int]
    ISSUES_FIELD_NUMBER: _ClassVar[int]
    TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    status: ConsistencyStatus
    counts: ConsistencyCounts
    issues: _containers.RepeatedCompositeFieldContainer[ConsistencyIssue]
    truncated: bool
    def __init__(self, status: _Optional[_Union[ConsistencyStatus, str]] = ..., counts: _Optional[_Union[ConsistencyCounts, _Mapping]] = ..., issues: _Optional[_Iterable[_Union[ConsistencyIssue, _Mapping]]] = ..., truncated: _Optional[bool] = ...) -> None: ...

class AdminReliabilitySignal(_message.Message):
    __slots__ = ("code", "message")
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    code: AdminReliabilitySignalCode
    message: str
    def __init__(self, code: _Optional[_Union[AdminReliabilitySignalCode, str]] = ..., message: _Optional[str] = ...) -> None: ...

class AdminReliabilityHealth(_message.Message):
    __slots__ = ("status", "consistency", "allocation_lifecycle_retries", "due_allocation_lifecycle_retries", "reconcile_unhealthy_components", "signals", "reconcile_components", "node_fleet_health")
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CONSISTENCY_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_LIFECYCLE_RETRIES_FIELD_NUMBER: _ClassVar[int]
    DUE_ALLOCATION_LIFECYCLE_RETRIES_FIELD_NUMBER: _ClassVar[int]
    RECONCILE_UNHEALTHY_COMPONENTS_FIELD_NUMBER: _ClassVar[int]
    SIGNALS_FIELD_NUMBER: _ClassVar[int]
    RECONCILE_COMPONENTS_FIELD_NUMBER: _ClassVar[int]
    NODE_FLEET_HEALTH_FIELD_NUMBER: _ClassVar[int]
    status: AdminReliabilityStatus
    consistency: ConsistencySnapshot
    allocation_lifecycle_retries: int
    due_allocation_lifecycle_retries: int
    reconcile_unhealthy_components: int
    signals: _containers.RepeatedCompositeFieldContainer[AdminReliabilitySignal]
    reconcile_components: _containers.RepeatedCompositeFieldContainer[ReconcileComponentHealth]
    node_fleet_health: AdminNodeFleetHealth
    def __init__(self, status: _Optional[_Union[AdminReliabilityStatus, str]] = ..., consistency: _Optional[_Union[ConsistencySnapshot, _Mapping]] = ..., allocation_lifecycle_retries: _Optional[int] = ..., due_allocation_lifecycle_retries: _Optional[int] = ..., reconcile_unhealthy_components: _Optional[int] = ..., signals: _Optional[_Iterable[_Union[AdminReliabilitySignal, _Mapping]]] = ..., reconcile_components: _Optional[_Iterable[_Union[ReconcileComponentHealth, _Mapping]]] = ..., node_fleet_health: _Optional[_Union[AdminNodeFleetHealth, _Mapping]] = ...) -> None: ...

class ReconcileComponentHealth(_message.Message):
    __slots__ = ("component", "running", "last_started_at", "last_success_at", "last_error_at", "last_error", "consecutive_failures")
    COMPONENT_FIELD_NUMBER: _ClassVar[int]
    RUNNING_FIELD_NUMBER: _ClassVar[int]
    LAST_STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_SUCCESS_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_ERROR_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_ERROR_FIELD_NUMBER: _ClassVar[int]
    CONSECUTIVE_FAILURES_FIELD_NUMBER: _ClassVar[int]
    component: str
    running: bool
    last_started_at: _timestamp_pb2.Timestamp
    last_success_at: _timestamp_pb2.Timestamp
    last_error_at: _timestamp_pb2.Timestamp
    last_error: str
    consecutive_failures: int
    def __init__(self, component: _Optional[str] = ..., running: _Optional[bool] = ..., last_started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_success_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_error_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_error: _Optional[str] = ..., consecutive_failures: _Optional[int] = ...) -> None: ...

class CheckConsistencyRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class CheckConsistencyResponse(_message.Message):
    __slots__ = ("snapshot",)
    SNAPSHOT_FIELD_NUMBER: _ClassVar[int]
    snapshot: ConsistencySnapshot
    def __init__(self, snapshot: _Optional[_Union[ConsistencySnapshot, _Mapping]] = ...) -> None: ...

class GetAdminReliabilityHealthRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetAdminReliabilityHealthResponse(_message.Message):
    __slots__ = ("health",)
    HEALTH_FIELD_NUMBER: _ClassVar[int]
    health: AdminReliabilityHealth
    def __init__(self, health: _Optional[_Union[AdminReliabilityHealth, _Mapping]] = ...) -> None: ...

class AdminNodeFleetHealth(_message.Message):
    __slots__ = ("active_nodes", "ready_nodes", "stale_heartbeat_nodes", "stale_summary_nodes", "not_ready_nodes", "unavailable", "error")
    ACTIVE_NODES_FIELD_NUMBER: _ClassVar[int]
    READY_NODES_FIELD_NUMBER: _ClassVar[int]
    STALE_HEARTBEAT_NODES_FIELD_NUMBER: _ClassVar[int]
    STALE_SUMMARY_NODES_FIELD_NUMBER: _ClassVar[int]
    NOT_READY_NODES_FIELD_NUMBER: _ClassVar[int]
    UNAVAILABLE_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    active_nodes: int
    ready_nodes: int
    stale_heartbeat_nodes: int
    stale_summary_nodes: int
    not_ready_nodes: int
    unavailable: bool
    error: str
    def __init__(self, active_nodes: _Optional[int] = ..., ready_nodes: _Optional[int] = ..., stale_heartbeat_nodes: _Optional[int] = ..., stale_summary_nodes: _Optional[int] = ..., not_ready_nodes: _Optional[int] = ..., unavailable: _Optional[bool] = ..., error: _Optional[str] = ...) -> None: ...
