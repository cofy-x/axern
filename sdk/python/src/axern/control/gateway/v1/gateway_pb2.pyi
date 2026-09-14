import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AllocationAccessPurpose(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ALLOCATION_ACCESS_PURPOSE_UNSPECIFIED: _ClassVar[AllocationAccessPurpose]
    ALLOCATION_ACCESS_PURPOSE_INTERACTIVE: _ClassVar[AllocationAccessPurpose]
    ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT: _ClassVar[AllocationAccessPurpose]
ALLOCATION_ACCESS_PURPOSE_UNSPECIFIED: AllocationAccessPurpose
ALLOCATION_ACCESS_PURPOSE_INTERACTIVE: AllocationAccessPurpose
ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT: AllocationAccessPurpose

class ResolveAllocationTerminalRequest(_message.Message):
    __slots__ = ("allocation_id", "ttl_seconds", "client_certificate_fingerprint", "purpose")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    TTL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    CLIENT_CERTIFICATE_FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    PURPOSE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    ttl_seconds: int
    client_certificate_fingerprint: str
    purpose: AllocationAccessPurpose
    def __init__(self, allocation_id: _Optional[str] = ..., ttl_seconds: _Optional[int] = ..., client_certificate_fingerprint: _Optional[str] = ..., purpose: _Optional[_Union[AllocationAccessPurpose, str]] = ...) -> None: ...

class ResolveAllocationTerminalResponse(_message.Message):
    __slots__ = ("allocation_id", "run_id", "node_id", "node_target", "access_grant")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    ACCESS_GRANT_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    run_id: str
    node_id: str
    node_target: str
    access_grant: AllocationAccessGrant
    def __init__(self, allocation_id: _Optional[str] = ..., run_id: _Optional[str] = ..., node_id: _Optional[str] = ..., node_target: _Optional[str] = ..., access_grant: _Optional[_Union[AllocationAccessGrant, _Mapping]] = ...) -> None: ...

class AllocationAccessGrant(_message.Message):
    __slots__ = ("grant_id", "allocation_id", "node_id", "plaintext_token", "expires_at")
    GRANT_ID_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    PLAINTEXT_TOKEN_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    grant_id: str
    allocation_id: str
    node_id: str
    plaintext_token: str
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, grant_id: _Optional[str] = ..., allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., plaintext_token: _Optional[str] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ResolveTunnelRelayTargetRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class ResolveTunnelRelayTargetResponse(_message.Message):
    __slots__ = ("node_edge_target",)
    NODE_EDGE_TARGET_FIELD_NUMBER: _ClassVar[int]
    node_edge_target: str
    def __init__(self, node_edge_target: _Optional[str] = ...) -> None: ...
