import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf import wrappers_pb2 as _wrappers_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class NamespaceQuotaEventType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    NAMESPACE_QUOTA_EVENT_TYPE_UNSPECIFIED: _ClassVar[NamespaceQuotaEventType]
    NAMESPACE_QUOTA_EVENT_TYPE_ADMISSION_REJECTED: _ClassVar[NamespaceQuotaEventType]

class NamespaceQuotaEventWorkloadType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_UNSPECIFIED: _ClassVar[NamespaceQuotaEventWorkloadType]
    NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_RUN: _ClassVar[NamespaceQuotaEventWorkloadType]
    NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_SERVICE: _ClassVar[NamespaceQuotaEventWorkloadType]

class NamespaceQuotaEventReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    NAMESPACE_QUOTA_EVENT_REASON_UNSPECIFIED: _ClassVar[NamespaceQuotaEventReason]
    NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU: _ClassVar[NamespaceQuotaEventReason]
    NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_MEMORY: _ClassVar[NamespaceQuotaEventReason]
    NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU_MEMORY: _ClassVar[NamespaceQuotaEventReason]
    NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_WRITABLE_LAYER: _ClassVar[NamespaceQuotaEventReason]
NAMESPACE_QUOTA_EVENT_TYPE_UNSPECIFIED: NamespaceQuotaEventType
NAMESPACE_QUOTA_EVENT_TYPE_ADMISSION_REJECTED: NamespaceQuotaEventType
NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_UNSPECIFIED: NamespaceQuotaEventWorkloadType
NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_RUN: NamespaceQuotaEventWorkloadType
NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_SERVICE: NamespaceQuotaEventWorkloadType
NAMESPACE_QUOTA_EVENT_REASON_UNSPECIFIED: NamespaceQuotaEventReason
NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU: NamespaceQuotaEventReason
NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_MEMORY: NamespaceQuotaEventReason
NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU_MEMORY: NamespaceQuotaEventReason
NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_WRITABLE_LAYER: NamespaceQuotaEventReason

class NamespaceQuota(_message.Message):
    __slots__ = ("namespace", "cpu_milli_limit", "memory_bytes_limit", "reserved_cpu_milli", "reserved_memory_bytes", "available_cpu_milli", "available_memory_bytes", "version", "created_at", "updated_at", "writable_layer_bytes_limit", "reserved_writable_layer_bytes", "available_writable_layer_bytes")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    CPU_MILLI_LIMIT_FIELD_NUMBER: _ClassVar[int]
    MEMORY_BYTES_LIMIT_FIELD_NUMBER: _ClassVar[int]
    RESERVED_CPU_MILLI_FIELD_NUMBER: _ClassVar[int]
    RESERVED_MEMORY_BYTES_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_CPU_MILLI_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_MEMORY_BYTES_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    WRITABLE_LAYER_BYTES_LIMIT_FIELD_NUMBER: _ClassVar[int]
    RESERVED_WRITABLE_LAYER_BYTES_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_WRITABLE_LAYER_BYTES_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    cpu_milli_limit: _wrappers_pb2.Int64Value
    memory_bytes_limit: _wrappers_pb2.Int64Value
    reserved_cpu_milli: int
    reserved_memory_bytes: int
    available_cpu_milli: _wrappers_pb2.Int64Value
    available_memory_bytes: _wrappers_pb2.Int64Value
    version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    writable_layer_bytes_limit: _wrappers_pb2.Int64Value
    reserved_writable_layer_bytes: int
    available_writable_layer_bytes: _wrappers_pb2.Int64Value
    def __init__(self, namespace: _Optional[str] = ..., cpu_milli_limit: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., memory_bytes_limit: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., reserved_cpu_milli: _Optional[int] = ..., reserved_memory_bytes: _Optional[int] = ..., available_cpu_milli: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., available_memory_bytes: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., writable_layer_bytes_limit: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., reserved_writable_layer_bytes: _Optional[int] = ..., available_writable_layer_bytes: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ...) -> None: ...

class NamespaceQuotaEvent(_message.Message):
    __slots__ = ("id", "namespace", "type", "workload_type", "workload_id", "environment_id", "reason", "requested_cpu_milli", "reserved_cpu_milli", "cpu_milli_limit", "available_cpu_milli", "requested_memory_bytes", "reserved_memory_bytes", "memory_bytes_limit", "available_memory_bytes", "message", "created_at", "requested_writable_layer_bytes", "reserved_writable_layer_bytes", "writable_layer_bytes_limit", "available_writable_layer_bytes")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_TYPE_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_ID_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_CPU_MILLI_FIELD_NUMBER: _ClassVar[int]
    RESERVED_CPU_MILLI_FIELD_NUMBER: _ClassVar[int]
    CPU_MILLI_LIMIT_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_CPU_MILLI_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_MEMORY_BYTES_FIELD_NUMBER: _ClassVar[int]
    RESERVED_MEMORY_BYTES_FIELD_NUMBER: _ClassVar[int]
    MEMORY_BYTES_LIMIT_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_MEMORY_BYTES_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_WRITABLE_LAYER_BYTES_FIELD_NUMBER: _ClassVar[int]
    RESERVED_WRITABLE_LAYER_BYTES_FIELD_NUMBER: _ClassVar[int]
    WRITABLE_LAYER_BYTES_LIMIT_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_WRITABLE_LAYER_BYTES_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    type: NamespaceQuotaEventType
    workload_type: NamespaceQuotaEventWorkloadType
    workload_id: str
    environment_id: str
    reason: NamespaceQuotaEventReason
    requested_cpu_milli: int
    reserved_cpu_milli: int
    cpu_milli_limit: _wrappers_pb2.Int64Value
    available_cpu_milli: _wrappers_pb2.Int64Value
    requested_memory_bytes: int
    reserved_memory_bytes: int
    memory_bytes_limit: _wrappers_pb2.Int64Value
    available_memory_bytes: _wrappers_pb2.Int64Value
    message: str
    created_at: _timestamp_pb2.Timestamp
    requested_writable_layer_bytes: int
    reserved_writable_layer_bytes: int
    writable_layer_bytes_limit: _wrappers_pb2.Int64Value
    available_writable_layer_bytes: _wrappers_pb2.Int64Value
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., type: _Optional[_Union[NamespaceQuotaEventType, str]] = ..., workload_type: _Optional[_Union[NamespaceQuotaEventWorkloadType, str]] = ..., workload_id: _Optional[str] = ..., environment_id: _Optional[str] = ..., reason: _Optional[_Union[NamespaceQuotaEventReason, str]] = ..., requested_cpu_milli: _Optional[int] = ..., reserved_cpu_milli: _Optional[int] = ..., cpu_milli_limit: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., available_cpu_milli: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., requested_memory_bytes: _Optional[int] = ..., reserved_memory_bytes: _Optional[int] = ..., memory_bytes_limit: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., available_memory_bytes: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., message: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., requested_writable_layer_bytes: _Optional[int] = ..., reserved_writable_layer_bytes: _Optional[int] = ..., writable_layer_bytes_limit: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., available_writable_layer_bytes: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ...) -> None: ...

class GetNamespaceQuotaRequest(_message.Message):
    __slots__ = ("namespace",)
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    def __init__(self, namespace: _Optional[str] = ...) -> None: ...

class GetNamespaceQuotaResponse(_message.Message):
    __slots__ = ("quota",)
    QUOTA_FIELD_NUMBER: _ClassVar[int]
    quota: NamespaceQuota
    def __init__(self, quota: _Optional[_Union[NamespaceQuota, _Mapping]] = ...) -> None: ...

class ListNamespaceQuotasRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListNamespaceQuotasResponse(_message.Message):
    __slots__ = ("quotas",)
    QUOTAS_FIELD_NUMBER: _ClassVar[int]
    quotas: _containers.RepeatedCompositeFieldContainer[NamespaceQuota]
    def __init__(self, quotas: _Optional[_Iterable[_Union[NamespaceQuota, _Mapping]]] = ...) -> None: ...

class NamespaceQuotaLimits(_message.Message):
    __slots__ = ("cpu_milli", "memory_bytes", "writable_layer_bytes")
    CPU_MILLI_FIELD_NUMBER: _ClassVar[int]
    MEMORY_BYTES_FIELD_NUMBER: _ClassVar[int]
    WRITABLE_LAYER_BYTES_FIELD_NUMBER: _ClassVar[int]
    cpu_milli: _wrappers_pb2.Int64Value
    memory_bytes: _wrappers_pb2.Int64Value
    writable_layer_bytes: _wrappers_pb2.Int64Value
    def __init__(self, cpu_milli: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., memory_bytes: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ..., writable_layer_bytes: _Optional[_Union[_wrappers_pb2.Int64Value, _Mapping]] = ...) -> None: ...

class SetNamespaceQuotaRequest(_message.Message):
    __slots__ = ("namespace", "limits")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    LIMITS_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    limits: NamespaceQuotaLimits
    def __init__(self, namespace: _Optional[str] = ..., limits: _Optional[_Union[NamespaceQuotaLimits, _Mapping]] = ...) -> None: ...

class SetNamespaceQuotaResponse(_message.Message):
    __slots__ = ("quota",)
    QUOTA_FIELD_NUMBER: _ClassVar[int]
    quota: NamespaceQuota
    def __init__(self, quota: _Optional[_Union[NamespaceQuota, _Mapping]] = ...) -> None: ...

class UnsetNamespaceQuotaRequest(_message.Message):
    __slots__ = ("namespace",)
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    def __init__(self, namespace: _Optional[str] = ...) -> None: ...

class UnsetNamespaceQuotaResponse(_message.Message):
    __slots__ = ("quota",)
    QUOTA_FIELD_NUMBER: _ClassVar[int]
    quota: NamespaceQuota
    def __init__(self, quota: _Optional[_Union[NamespaceQuota, _Mapping]] = ...) -> None: ...

class ListNamespaceQuotaEventsRequest(_message.Message):
    __slots__ = ("namespace", "limit")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    limit: int
    def __init__(self, namespace: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class ListNamespaceQuotaEventsResponse(_message.Message):
    __slots__ = ("events",)
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[NamespaceQuotaEvent]
    def __init__(self, events: _Optional[_Iterable[_Union[NamespaceQuotaEvent, _Mapping]]] = ...) -> None: ...
