import datetime

from axern.control.common.v1 import common_pb2 as _common_pb2
from axern.control.capability.v1 import capability_pb2 as _capability_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ServiceReplicaView(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SERVICE_REPLICA_VIEW_UNSPECIFIED: _ClassVar[ServiceReplicaView]
    SERVICE_REPLICA_VIEW_ALL: _ClassVar[ServiceReplicaView]
    SERVICE_REPLICA_VIEW_CURRENT: _ClassVar[ServiceReplicaView]
    SERVICE_REPLICA_VIEW_ENDED: _ClassVar[ServiceReplicaView]
    SERVICE_REPLICA_VIEW_UNHEALTHY: _ClassVar[ServiceReplicaView]
    SERVICE_REPLICA_VIEW_OUTDATED: _ClassVar[ServiceReplicaView]
    SERVICE_REPLICA_VIEW_UPDATED: _ClassVar[ServiceReplicaView]

class ServiceReplicaLifecycleRetryReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_UNSPECIFIED: _ClassVar[ServiceReplicaLifecycleRetryReason]
    SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_CREATE: _ClassVar[ServiceReplicaLifecycleRetryReason]
    SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_DELETE: _ClassVar[ServiceReplicaLifecycleRetryReason]
SERVICE_REPLICA_VIEW_UNSPECIFIED: ServiceReplicaView
SERVICE_REPLICA_VIEW_ALL: ServiceReplicaView
SERVICE_REPLICA_VIEW_CURRENT: ServiceReplicaView
SERVICE_REPLICA_VIEW_ENDED: ServiceReplicaView
SERVICE_REPLICA_VIEW_UNHEALTHY: ServiceReplicaView
SERVICE_REPLICA_VIEW_OUTDATED: ServiceReplicaView
SERVICE_REPLICA_VIEW_UPDATED: ServiceReplicaView
SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_UNSPECIFIED: ServiceReplicaLifecycleRetryReason
SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_CREATE: ServiceReplicaLifecycleRetryReason
SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_DELETE: ServiceReplicaLifecycleRetryReason

class ServiceReplica(_message.Message):
    __slots__ = ("id", "service_id", "node_id", "attempt", "status", "message", "exit_code", "exit_code_known", "created_at", "updated_at", "ended", "outdated", "diagnostic_code", "ready", "readiness_message", "lifecycle_retry", "workspace_preparation", "capability_conditions")
    ID_FIELD_NUMBER: _ClassVar[int]
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_KNOWN_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    ENDED_FIELD_NUMBER: _ClassVar[int]
    OUTDATED_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_CODE_FIELD_NUMBER: _ClassVar[int]
    READY_FIELD_NUMBER: _ClassVar[int]
    READINESS_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    LIFECYCLE_RETRY_FIELD_NUMBER: _ClassVar[int]
    WORKSPACE_PREPARATION_FIELD_NUMBER: _ClassVar[int]
    CAPABILITY_CONDITIONS_FIELD_NUMBER: _ClassVar[int]
    id: str
    service_id: str
    node_id: str
    attempt: int
    status: _common_pb2.AllocationStatus
    message: str
    exit_code: int
    exit_code_known: bool
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    ended: bool
    outdated: bool
    diagnostic_code: _common_pb2.WorkloadDiagnosticCode
    ready: bool
    readiness_message: str
    lifecycle_retry: ServiceReplicaLifecycleRetry
    workspace_preparation: _common_pb2.WorkspacePreparationFacts
    capability_conditions: _containers.RepeatedCompositeFieldContainer[_capability_pb2.CapabilityCondition]
    def __init__(self, id: _Optional[str] = ..., service_id: _Optional[str] = ..., node_id: _Optional[str] = ..., attempt: _Optional[int] = ..., status: _Optional[_Union[_common_pb2.AllocationStatus, str]] = ..., message: _Optional[str] = ..., exit_code: _Optional[int] = ..., exit_code_known: _Optional[bool] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., ended: _Optional[bool] = ..., outdated: _Optional[bool] = ..., diagnostic_code: _Optional[_Union[_common_pb2.WorkloadDiagnosticCode, str]] = ..., ready: _Optional[bool] = ..., readiness_message: _Optional[str] = ..., lifecycle_retry: _Optional[_Union[ServiceReplicaLifecycleRetry, _Mapping]] = ..., workspace_preparation: _Optional[_Union[_common_pb2.WorkspacePreparationFacts, _Mapping]] = ..., capability_conditions: _Optional[_Iterable[_Union[_capability_pb2.CapabilityCondition, _Mapping]]] = ...) -> None: ...

class ServiceReplicaLifecycleRetry(_message.Message):
    __slots__ = ("reason", "attempts", "last_error", "next_run_at")
    REASON_FIELD_NUMBER: _ClassVar[int]
    ATTEMPTS_FIELD_NUMBER: _ClassVar[int]
    LAST_ERROR_FIELD_NUMBER: _ClassVar[int]
    NEXT_RUN_AT_FIELD_NUMBER: _ClassVar[int]
    reason: ServiceReplicaLifecycleRetryReason
    attempts: int
    last_error: str
    next_run_at: _timestamp_pb2.Timestamp
    def __init__(self, reason: _Optional[_Union[ServiceReplicaLifecycleRetryReason, str]] = ..., attempts: _Optional[int] = ..., last_error: _Optional[str] = ..., next_run_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ServiceReplicaListFilter(_message.Message):
    __slots__ = ("statuses", "view")
    STATUSES_FIELD_NUMBER: _ClassVar[int]
    VIEW_FIELD_NUMBER: _ClassVar[int]
    statuses: _containers.RepeatedScalarFieldContainer[_common_pb2.AllocationStatus]
    view: ServiceReplicaView
    def __init__(self, statuses: _Optional[_Iterable[_Union[_common_pb2.AllocationStatus, str]]] = ..., view: _Optional[_Union[ServiceReplicaView, str]] = ...) -> None: ...

class GetServiceReplicaRequest(_message.Message):
    __slots__ = ("service_id", "replica_id")
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    REPLICA_ID_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    replica_id: str
    def __init__(self, service_id: _Optional[str] = ..., replica_id: _Optional[str] = ...) -> None: ...

class GetServiceReplicaResponse(_message.Message):
    __slots__ = ("replica",)
    REPLICA_FIELD_NUMBER: _ClassVar[int]
    replica: ServiceReplica
    def __init__(self, replica: _Optional[_Union[ServiceReplica, _Mapping]] = ...) -> None: ...

class ListServiceReplicasRequest(_message.Message):
    __slots__ = ("service_id", "filter")
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    FILTER_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    filter: ServiceReplicaListFilter
    def __init__(self, service_id: _Optional[str] = ..., filter: _Optional[_Union[ServiceReplicaListFilter, _Mapping]] = ...) -> None: ...

class ListServiceReplicasResponse(_message.Message):
    __slots__ = ("replicas",)
    REPLICAS_FIELD_NUMBER: _ClassVar[int]
    replicas: _containers.RepeatedCompositeFieldContainer[ServiceReplica]
    def __init__(self, replicas: _Optional[_Iterable[_Union[ServiceReplica, _Mapping]]] = ...) -> None: ...
