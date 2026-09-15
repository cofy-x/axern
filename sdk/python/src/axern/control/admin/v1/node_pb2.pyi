import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from axern.control.capability.v1 import capability_pb2 as _capability_pb2
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
    ADMIN_NODE_LIFECYCLE_STATUS_REVOKED: _ClassVar[AdminNodeLifecycleStatus]
    ADMIN_NODE_LIFECYCLE_STATUS_RETIRED: _ClassVar[AdminNodeLifecycleStatus]
ADMIN_NODE_LIFECYCLE_STATUS_UNSPECIFIED: AdminNodeLifecycleStatus
ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE: AdminNodeLifecycleStatus
ADMIN_NODE_LIFECYCLE_STATUS_REVOKED: AdminNodeLifecycleStatus
ADMIN_NODE_LIFECYCLE_STATUS_RETIRED: AdminNodeLifecycleStatus

class AdminNode(_message.Message):
    __slots__ = ("node_id", "lifecycle_status", "heartbeat_fresh", "summary_fresh", "axnoded_ready", "heartbeat_age_seconds", "summary_age_seconds", "admitted_at", "last_heartbeat_at", "retired_at", "retired_reason")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    LIFECYCLE_STATUS_FIELD_NUMBER: _ClassVar[int]
    HEARTBEAT_FRESH_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FRESH_FIELD_NUMBER: _ClassVar[int]
    AXNODED_READY_FIELD_NUMBER: _ClassVar[int]
    HEARTBEAT_AGE_SECONDS_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_AGE_SECONDS_FIELD_NUMBER: _ClassVar[int]
    ADMITTED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_HEARTBEAT_AT_FIELD_NUMBER: _ClassVar[int]
    RETIRED_AT_FIELD_NUMBER: _ClassVar[int]
    RETIRED_REASON_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    lifecycle_status: AdminNodeLifecycleStatus
    heartbeat_fresh: bool
    summary_fresh: bool
    axnoded_ready: bool
    heartbeat_age_seconds: int
    summary_age_seconds: int
    admitted_at: _timestamp_pb2.Timestamp
    last_heartbeat_at: _timestamp_pb2.Timestamp
    retired_at: _timestamp_pb2.Timestamp
    retired_reason: str
    def __init__(self, node_id: _Optional[str] = ..., lifecycle_status: _Optional[_Union[AdminNodeLifecycleStatus, str]] = ..., heartbeat_fresh: _Optional[bool] = ..., summary_fresh: _Optional[bool] = ..., axnoded_ready: _Optional[bool] = ..., heartbeat_age_seconds: _Optional[int] = ..., summary_age_seconds: _Optional[int] = ..., admitted_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_heartbeat_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., retired_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., retired_reason: _Optional[str] = ...) -> None: ...

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

class AdmitAdminNodeRequest(_message.Message):
    __slots__ = ("node_id", "enrollment_token", "operator_reason")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    ENROLLMENT_TOKEN_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    enrollment_token: str
    operator_reason: str
    def __init__(self, node_id: _Optional[str] = ..., enrollment_token: _Optional[str] = ..., operator_reason: _Optional[str] = ...) -> None: ...

class AdmitAdminNodeResponse(_message.Message):
    __slots__ = ("node",)
    NODE_FIELD_NUMBER: _ClassVar[int]
    node: AdminNode
    def __init__(self, node: _Optional[_Union[AdminNode, _Mapping]] = ...) -> None: ...

class RevokeAdminNodeRequest(_message.Message):
    __slots__ = ("node_id", "operator_reason")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    operator_reason: str
    def __init__(self, node_id: _Optional[str] = ..., operator_reason: _Optional[str] = ...) -> None: ...

class RevokeAdminNodeResponse(_message.Message):
    __slots__ = ("node",)
    NODE_FIELD_NUMBER: _ClassVar[int]
    node: AdminNode
    def __init__(self, node: _Optional[_Union[AdminNode, _Mapping]] = ...) -> None: ...

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

class GetAllocationCapabilityDiagnosticsRequest(_message.Message):
    __slots__ = ("allocation_id",)
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    def __init__(self, allocation_id: _Optional[str] = ...) -> None: ...

class GetAllocationCapabilityDiagnosticsResponse(_message.Message):
    __slots__ = ("allocation_id", "node_id", "requirements", "condition_set")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    REQUIREMENTS_FIELD_NUMBER: _ClassVar[int]
    CONDITION_SET_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    node_id: str
    requirements: _containers.RepeatedCompositeFieldContainer[_capability_pb2.CapabilityRequirement]
    condition_set: _capability_pb2.CapabilityConditionSet
    def __init__(self, allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., requirements: _Optional[_Iterable[_Union[_capability_pb2.CapabilityRequirement, _Mapping]]] = ..., condition_set: _Optional[_Union[_capability_pb2.CapabilityConditionSet, _Mapping]] = ...) -> None: ...
