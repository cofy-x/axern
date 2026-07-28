import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AllocationLifecycleRetryOwnerType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_UNSPECIFIED: _ClassVar[AllocationLifecycleRetryOwnerType]
    ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_RUN: _ClassVar[AllocationLifecycleRetryOwnerType]
    ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_SERVICE: _ClassVar[AllocationLifecycleRetryOwnerType]

class AllocationLifecycleRetryReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ALLOCATION_LIFECYCLE_RETRY_REASON_UNSPECIFIED: _ClassVar[AllocationLifecycleRetryReason]
    ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE: _ClassVar[AllocationLifecycleRetryReason]
    ALLOCATION_LIFECYCLE_RETRY_REASON_DELETE: _ClassVar[AllocationLifecycleRetryReason]
ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_UNSPECIFIED: AllocationLifecycleRetryOwnerType
ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_RUN: AllocationLifecycleRetryOwnerType
ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_SERVICE: AllocationLifecycleRetryOwnerType
ALLOCATION_LIFECYCLE_RETRY_REASON_UNSPECIFIED: AllocationLifecycleRetryReason
ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE: AllocationLifecycleRetryReason
ALLOCATION_LIFECYCLE_RETRY_REASON_DELETE: AllocationLifecycleRetryReason

class AllocationLifecycleRetry(_message.Message):
    __slots__ = ("allocation_id", "owner_id", "owner_type", "environment_id", "reason", "node_id", "node_target", "attempt", "reconcile_attempts", "last_error", "next_run_at", "created_at", "updated_at", "age_seconds", "due", "clearable", "clear_blocked_reason")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    OWNER_ID_FIELD_NUMBER: _ClassVar[int]
    OWNER_TYPE_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    RECONCILE_ATTEMPTS_FIELD_NUMBER: _ClassVar[int]
    LAST_ERROR_FIELD_NUMBER: _ClassVar[int]
    NEXT_RUN_AT_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    AGE_SECONDS_FIELD_NUMBER: _ClassVar[int]
    DUE_FIELD_NUMBER: _ClassVar[int]
    CLEARABLE_FIELD_NUMBER: _ClassVar[int]
    CLEAR_BLOCKED_REASON_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    owner_id: str
    owner_type: AllocationLifecycleRetryOwnerType
    environment_id: str
    reason: AllocationLifecycleRetryReason
    node_id: str
    node_target: str
    attempt: int
    reconcile_attempts: int
    last_error: str
    next_run_at: _timestamp_pb2.Timestamp
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    age_seconds: int
    due: bool
    clearable: bool
    clear_blocked_reason: str
    def __init__(self, allocation_id: _Optional[str] = ..., owner_id: _Optional[str] = ..., owner_type: _Optional[_Union[AllocationLifecycleRetryOwnerType, str]] = ..., environment_id: _Optional[str] = ..., reason: _Optional[_Union[AllocationLifecycleRetryReason, str]] = ..., node_id: _Optional[str] = ..., node_target: _Optional[str] = ..., attempt: _Optional[int] = ..., reconcile_attempts: _Optional[int] = ..., last_error: _Optional[str] = ..., next_run_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., age_seconds: _Optional[int] = ..., due: _Optional[bool] = ..., clearable: _Optional[bool] = ..., clear_blocked_reason: _Optional[str] = ...) -> None: ...

class AllocationLifecycleRetryFilter(_message.Message):
    __slots__ = ("owner_type", "reason", "due_only")
    OWNER_TYPE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    DUE_ONLY_FIELD_NUMBER: _ClassVar[int]
    owner_type: AllocationLifecycleRetryOwnerType
    reason: AllocationLifecycleRetryReason
    due_only: bool
    def __init__(self, owner_type: _Optional[_Union[AllocationLifecycleRetryOwnerType, str]] = ..., reason: _Optional[_Union[AllocationLifecycleRetryReason, str]] = ..., due_only: _Optional[bool] = ...) -> None: ...

class ListAllocationLifecycleRetriesRequest(_message.Message):
    __slots__ = ("filter", "limit")
    FILTER_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    filter: AllocationLifecycleRetryFilter
    limit: int
    def __init__(self, filter: _Optional[_Union[AllocationLifecycleRetryFilter, _Mapping]] = ..., limit: _Optional[int] = ...) -> None: ...

class ListAllocationLifecycleRetriesResponse(_message.Message):
    __slots__ = ("retries",)
    RETRIES_FIELD_NUMBER: _ClassVar[int]
    retries: _containers.RepeatedCompositeFieldContainer[AllocationLifecycleRetry]
    def __init__(self, retries: _Optional[_Iterable[_Union[AllocationLifecycleRetry, _Mapping]]] = ...) -> None: ...

class ForceAllocationLifecycleRetryRequest(_message.Message):
    __slots__ = ("allocation_id", "reason", "operator_reason")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    reason: AllocationLifecycleRetryReason
    operator_reason: str
    def __init__(self, allocation_id: _Optional[str] = ..., reason: _Optional[_Union[AllocationLifecycleRetryReason, str]] = ..., operator_reason: _Optional[str] = ...) -> None: ...

class ForceAllocationLifecycleRetryResponse(_message.Message):
    __slots__ = ("retry",)
    RETRY_FIELD_NUMBER: _ClassVar[int]
    retry: AllocationLifecycleRetry
    def __init__(self, retry: _Optional[_Union[AllocationLifecycleRetry, _Mapping]] = ...) -> None: ...

class FailAllocationLifecycleRetryRequest(_message.Message):
    __slots__ = ("allocation_id", "reason", "operator_reason")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    reason: AllocationLifecycleRetryReason
    operator_reason: str
    def __init__(self, allocation_id: _Optional[str] = ..., reason: _Optional[_Union[AllocationLifecycleRetryReason, str]] = ..., operator_reason: _Optional[str] = ...) -> None: ...

class FailAllocationLifecycleRetryResponse(_message.Message):
    __slots__ = ("failed_retry",)
    FAILED_RETRY_FIELD_NUMBER: _ClassVar[int]
    failed_retry: AllocationLifecycleRetry
    def __init__(self, failed_retry: _Optional[_Union[AllocationLifecycleRetry, _Mapping]] = ...) -> None: ...

class ClearAllocationLifecycleRetryRequest(_message.Message):
    __slots__ = ("allocation_id", "reason", "operator_reason")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    reason: AllocationLifecycleRetryReason
    operator_reason: str
    def __init__(self, allocation_id: _Optional[str] = ..., reason: _Optional[_Union[AllocationLifecycleRetryReason, str]] = ..., operator_reason: _Optional[str] = ...) -> None: ...

class ClearAllocationLifecycleRetryResponse(_message.Message):
    __slots__ = ("cleared_retry",)
    CLEARED_RETRY_FIELD_NUMBER: _ClassVar[int]
    cleared_retry: AllocationLifecycleRetry
    def __init__(self, cleared_retry: _Optional[_Union[AllocationLifecycleRetry, _Mapping]] = ...) -> None: ...
