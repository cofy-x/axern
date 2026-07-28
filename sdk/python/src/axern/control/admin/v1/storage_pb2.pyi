import datetime

from axern.control.storage.v1 import storage_types_pb2 as _storage_types_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StorageBinding(_message.Message):
    __slots__ = ("binding_id", "claim_id", "namespace", "claim_name", "workload_id", "workload_type", "allocation_id", "node_id", "status", "message", "created_at", "updated_at", "published_at", "released_at")
    BINDING_ID_FIELD_NUMBER: _ClassVar[int]
    CLAIM_ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    CLAIM_NAME_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_ID_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_TYPE_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    PUBLISHED_AT_FIELD_NUMBER: _ClassVar[int]
    RELEASED_AT_FIELD_NUMBER: _ClassVar[int]
    binding_id: str
    claim_id: str
    namespace: str
    claim_name: str
    workload_id: str
    workload_type: str
    allocation_id: str
    node_id: str
    status: _storage_types_pb2.VolumeStatus
    message: str
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    published_at: _timestamp_pb2.Timestamp
    released_at: _timestamp_pb2.Timestamp
    def __init__(self, binding_id: _Optional[str] = ..., claim_id: _Optional[str] = ..., namespace: _Optional[str] = ..., claim_name: _Optional[str] = ..., workload_id: _Optional[str] = ..., workload_type: _Optional[str] = ..., allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., status: _Optional[_Union[_storage_types_pb2.VolumeStatus, str]] = ..., message: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., published_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., released_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class StorageBindingFilter(_message.Message):
    __slots__ = ("statuses", "namespace", "claim_name", "workload_id", "allocation_id", "node_id")
    STATUSES_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    CLAIM_NAME_FIELD_NUMBER: _ClassVar[int]
    WORKLOAD_ID_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    statuses: _containers.RepeatedScalarFieldContainer[_storage_types_pb2.VolumeStatus]
    namespace: str
    claim_name: str
    workload_id: str
    allocation_id: str
    node_id: str
    def __init__(self, statuses: _Optional[_Iterable[_Union[_storage_types_pb2.VolumeStatus, str]]] = ..., namespace: _Optional[str] = ..., claim_name: _Optional[str] = ..., workload_id: _Optional[str] = ..., allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ...) -> None: ...

class ListStorageBindingsRequest(_message.Message):
    __slots__ = ("filter", "limit")
    FILTER_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    filter: StorageBindingFilter
    limit: int
    def __init__(self, filter: _Optional[_Union[StorageBindingFilter, _Mapping]] = ..., limit: _Optional[int] = ...) -> None: ...

class ListStorageBindingsResponse(_message.Message):
    __slots__ = ("bindings",)
    BINDINGS_FIELD_NUMBER: _ClassVar[int]
    bindings: _containers.RepeatedCompositeFieldContainer[StorageBinding]
    def __init__(self, bindings: _Optional[_Iterable[_Union[StorageBinding, _Mapping]]] = ...) -> None: ...

class RetryStorageBindingRequest(_message.Message):
    __slots__ = ("binding_id", "operator_reason")
    BINDING_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    binding_id: str
    operator_reason: str
    def __init__(self, binding_id: _Optional[str] = ..., operator_reason: _Optional[str] = ...) -> None: ...

class RetryStorageBindingResponse(_message.Message):
    __slots__ = ("binding",)
    BINDING_FIELD_NUMBER: _ClassVar[int]
    binding: StorageBinding
    def __init__(self, binding: _Optional[_Union[StorageBinding, _Mapping]] = ...) -> None: ...

class StorageReclaim(_message.Message):
    __slots__ = ("claim_id", "namespace", "claim_name", "service_id", "node_id", "attempt", "next_retry_at", "last_error", "updated_at")
    CLAIM_ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    CLAIM_NAME_FIELD_NUMBER: _ClassVar[int]
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    NEXT_RETRY_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_ERROR_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    claim_id: str
    namespace: str
    claim_name: str
    service_id: str
    node_id: str
    attempt: int
    next_retry_at: _timestamp_pb2.Timestamp
    last_error: str
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, claim_id: _Optional[str] = ..., namespace: _Optional[str] = ..., claim_name: _Optional[str] = ..., service_id: _Optional[str] = ..., node_id: _Optional[str] = ..., attempt: _Optional[int] = ..., next_retry_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_error: _Optional[str] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ListStorageReclaimsRequest(_message.Message):
    __slots__ = ("namespace", "service_id", "node_id", "limit")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    service_id: str
    node_id: str
    limit: int
    def __init__(self, namespace: _Optional[str] = ..., service_id: _Optional[str] = ..., node_id: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class ListStorageReclaimsResponse(_message.Message):
    __slots__ = ("reclaims",)
    RECLAIMS_FIELD_NUMBER: _ClassVar[int]
    reclaims: _containers.RepeatedCompositeFieldContainer[StorageReclaim]
    def __init__(self, reclaims: _Optional[_Iterable[_Union[StorageReclaim, _Mapping]]] = ...) -> None: ...
