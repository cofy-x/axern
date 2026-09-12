from axern.control.common.v1 import common_pb2 as _common_pb2
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
    __slots__ = ("allocation_id", "owner_type", "owner_id", "node_id", "node_target", "attempt", "lease")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    OWNER_TYPE_FIELD_NUMBER: _ClassVar[int]
    OWNER_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    LEASE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    owner_type: str
    owner_id: str
    node_id: str
    node_target: str
    attempt: int
    lease: _common_pb2.ExecutionLease
    def __init__(self, allocation_id: _Optional[str] = ..., owner_type: _Optional[str] = ..., owner_id: _Optional[str] = ..., node_id: _Optional[str] = ..., node_target: _Optional[str] = ..., attempt: _Optional[int] = ..., lease: _Optional[_Union[_common_pb2.ExecutionLease, _Mapping]] = ...) -> None: ...

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
