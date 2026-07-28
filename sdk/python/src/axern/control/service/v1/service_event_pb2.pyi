import datetime

from axern.control.service.v1 import service_types_pb2 as _service_types_pb2
from axern.control.common.v1 import common_pb2 as _common_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ServiceEventType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SERVICE_EVENT_TYPE_UNSPECIFIED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_ROLLOUT_STARTED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_REPLACEMENT_ADMITTED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_REPLACEMENT_RUNNING: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_OUTDATED_DRAINED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_SERVICE_DEGRADED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_SERVICE_RECOVERED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_REPLACEMENT_READY: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_LIVENESS_FAILED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_AUTOSCALE_TARGET_CHANGED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_DELETION_REQUESTED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_VOLUME_RECLAIM_RETRY: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_VOLUME_RECLAIMED: _ClassVar[ServiceEventType]
    SERVICE_EVENT_TYPE_DELETION_COMPLETED: _ClassVar[ServiceEventType]
SERVICE_EVENT_TYPE_UNSPECIFIED: ServiceEventType
SERVICE_EVENT_TYPE_ROLLOUT_STARTED: ServiceEventType
SERVICE_EVENT_TYPE_REPLACEMENT_ADMITTED: ServiceEventType
SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED: ServiceEventType
SERVICE_EVENT_TYPE_REPLACEMENT_RUNNING: ServiceEventType
SERVICE_EVENT_TYPE_OUTDATED_DRAINED: ServiceEventType
SERVICE_EVENT_TYPE_SERVICE_DEGRADED: ServiceEventType
SERVICE_EVENT_TYPE_SERVICE_RECOVERED: ServiceEventType
SERVICE_EVENT_TYPE_REPLACEMENT_READY: ServiceEventType
SERVICE_EVENT_TYPE_LIVENESS_FAILED: ServiceEventType
SERVICE_EVENT_TYPE_AUTOSCALE_TARGET_CHANGED: ServiceEventType
SERVICE_EVENT_TYPE_DELETION_REQUESTED: ServiceEventType
SERVICE_EVENT_TYPE_VOLUME_RECLAIM_RETRY: ServiceEventType
SERVICE_EVENT_TYPE_VOLUME_RECLAIMED: ServiceEventType
SERVICE_EVENT_TYPE_DELETION_COMPLETED: ServiceEventType

class ServiceEvent(_message.Message):
    __slots__ = ("id", "service_id", "replica_id", "type", "phase", "diagnostic_code", "message", "created_at")
    ID_FIELD_NUMBER: _ClassVar[int]
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    REPLICA_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    service_id: str
    replica_id: str
    type: ServiceEventType
    phase: _service_types_pb2.ServiceRolloutPhase
    diagnostic_code: _common_pb2.WorkloadDiagnosticCode
    message: str
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., service_id: _Optional[str] = ..., replica_id: _Optional[str] = ..., type: _Optional[_Union[ServiceEventType, str]] = ..., phase: _Optional[_Union[_service_types_pb2.ServiceRolloutPhase, str]] = ..., diagnostic_code: _Optional[_Union[_common_pb2.WorkloadDiagnosticCode, str]] = ..., message: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ListServiceEventsRequest(_message.Message):
    __slots__ = ("service_id", "limit")
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    limit: int
    def __init__(self, service_id: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class ListServiceEventsResponse(_message.Message):
    __slots__ = ("events",)
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[ServiceEvent]
    def __init__(self, events: _Optional[_Iterable[_Union[ServiceEvent, _Mapping]]] = ...) -> None: ...
