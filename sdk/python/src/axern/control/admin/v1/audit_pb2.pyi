import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AdminAuditOperation(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ADMIN_AUDIT_OPERATION_UNSPECIFIED: _ClassVar[AdminAuditOperation]
    ADMIN_AUDIT_OPERATION_FORCE_ALLOCATION_LIFECYCLE_RETRY: _ClassVar[AdminAuditOperation]
    ADMIN_AUDIT_OPERATION_FAIL_ALLOCATION_LIFECYCLE_RETRY: _ClassVar[AdminAuditOperation]
    ADMIN_AUDIT_OPERATION_CLEAR_ALLOCATION_LIFECYCLE_RETRY: _ClassVar[AdminAuditOperation]
    ADMIN_AUDIT_OPERATION_RETRY_STORAGE_BINDING: _ClassVar[AdminAuditOperation]
    ADMIN_AUDIT_OPERATION_PURGE_SERVICE: _ClassVar[AdminAuditOperation]
    ADMIN_AUDIT_OPERATION_RETIRE_NODE: _ClassVar[AdminAuditOperation]

class AdminAuditTargetType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ADMIN_AUDIT_TARGET_TYPE_UNSPECIFIED: _ClassVar[AdminAuditTargetType]
    ADMIN_AUDIT_TARGET_TYPE_ALLOCATION: _ClassVar[AdminAuditTargetType]
    ADMIN_AUDIT_TARGET_TYPE_STORAGE_BINDING: _ClassVar[AdminAuditTargetType]
    ADMIN_AUDIT_TARGET_TYPE_SERVICE: _ClassVar[AdminAuditTargetType]
    ADMIN_AUDIT_TARGET_TYPE_NODE: _ClassVar[AdminAuditTargetType]
ADMIN_AUDIT_OPERATION_UNSPECIFIED: AdminAuditOperation
ADMIN_AUDIT_OPERATION_FORCE_ALLOCATION_LIFECYCLE_RETRY: AdminAuditOperation
ADMIN_AUDIT_OPERATION_FAIL_ALLOCATION_LIFECYCLE_RETRY: AdminAuditOperation
ADMIN_AUDIT_OPERATION_CLEAR_ALLOCATION_LIFECYCLE_RETRY: AdminAuditOperation
ADMIN_AUDIT_OPERATION_RETRY_STORAGE_BINDING: AdminAuditOperation
ADMIN_AUDIT_OPERATION_PURGE_SERVICE: AdminAuditOperation
ADMIN_AUDIT_OPERATION_RETIRE_NODE: AdminAuditOperation
ADMIN_AUDIT_TARGET_TYPE_UNSPECIFIED: AdminAuditTargetType
ADMIN_AUDIT_TARGET_TYPE_ALLOCATION: AdminAuditTargetType
ADMIN_AUDIT_TARGET_TYPE_STORAGE_BINDING: AdminAuditTargetType
ADMIN_AUDIT_TARGET_TYPE_SERVICE: AdminAuditTargetType
ADMIN_AUDIT_TARGET_TYPE_NODE: AdminAuditTargetType

class AdminAuditEvent(_message.Message):
    __slots__ = ("event_id", "operation", "target_type", "target_id", "operator_reason", "created_at")
    EVENT_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    TARGET_TYPE_FIELD_NUMBER: _ClassVar[int]
    TARGET_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATOR_REASON_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    event_id: str
    operation: AdminAuditOperation
    target_type: AdminAuditTargetType
    target_id: str
    operator_reason: str
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, event_id: _Optional[str] = ..., operation: _Optional[_Union[AdminAuditOperation, str]] = ..., target_type: _Optional[_Union[AdminAuditTargetType, str]] = ..., target_id: _Optional[str] = ..., operator_reason: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class AdminAuditEventFilter(_message.Message):
    __slots__ = ("operation", "target_type", "target_id")
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    TARGET_TYPE_FIELD_NUMBER: _ClassVar[int]
    TARGET_ID_FIELD_NUMBER: _ClassVar[int]
    operation: AdminAuditOperation
    target_type: AdminAuditTargetType
    target_id: str
    def __init__(self, operation: _Optional[_Union[AdminAuditOperation, str]] = ..., target_type: _Optional[_Union[AdminAuditTargetType, str]] = ..., target_id: _Optional[str] = ...) -> None: ...

class ListAdminAuditEventsRequest(_message.Message):
    __slots__ = ("filter", "limit")
    FILTER_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    filter: AdminAuditEventFilter
    limit: int
    def __init__(self, filter: _Optional[_Union[AdminAuditEventFilter, _Mapping]] = ..., limit: _Optional[int] = ...) -> None: ...

class ListAdminAuditEventsResponse(_message.Message):
    __slots__ = ("events",)
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[AdminAuditEvent]
    def __init__(self, events: _Optional[_Iterable[_Union[AdminAuditEvent, _Mapping]]] = ...) -> None: ...
