import datetime

from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class TunnelSessionStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TUNNEL_SESSION_STATUS_UNSPECIFIED: _ClassVar[TunnelSessionStatus]
    TUNNEL_SESSION_STATUS_PENDING: _ClassVar[TunnelSessionStatus]
    TUNNEL_SESSION_STATUS_RUNNING: _ClassVar[TunnelSessionStatus]
    TUNNEL_SESSION_STATUS_DEGRADED: _ClassVar[TunnelSessionStatus]
    TUNNEL_SESSION_STATUS_REVOKED: _ClassVar[TunnelSessionStatus]
    TUNNEL_SESSION_STATUS_EXPIRED: _ClassVar[TunnelSessionStatus]
    TUNNEL_SESSION_STATUS_FAILED: _ClassVar[TunnelSessionStatus]

class TunnelPeerKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TUNNEL_PEER_KIND_UNSPECIFIED: _ClassVar[TunnelPeerKind]
    TUNNEL_PEER_KIND_CLIENT: _ClassVar[TunnelPeerKind]
    TUNNEL_PEER_KIND_NODE: _ClassVar[TunnelPeerKind]

class TunnelSessionEventType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TUNNEL_SESSION_EVENT_TYPE_UNSPECIFIED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_CREATED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_RENEWED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_NODE_STATUS: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_REVOKED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_EXPIRED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_CLIENT_DISCONNECTED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_NODE_CONNECTED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_NODE_DISCONNECTED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_PAIRED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_DRAIN_REJECTED: _ClassVar[TunnelSessionEventType]
    TUNNEL_SESSION_EVENT_TYPE_RESOURCE_LIMITED: _ClassVar[TunnelSessionEventType]

class TunnelSessionEventReasonCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TUNNEL_SESSION_EVENT_REASON_CODE_UNSPECIFIED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_CREATED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_RENEWED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_NODE_RUNNING: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_NODE_DEGRADED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_NODE_FAILED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_MANUAL_REVOKE: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_DELETED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_EXPIRED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_CLIENT_CONNECTED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_CLIENT_DISCONNECTED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_NODE_CONNECTED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_NODE_DISCONNECTED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_PAIRED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_DRAIN_REJECTED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_RESOURCE_LIMITED: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_PONG_TIMEOUT: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_QUEUE_FULL: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_FRAME_TOO_LARGE: _ClassVar[TunnelSessionEventReasonCode]
    TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_OPPOSITE_MISSING: _ClassVar[TunnelSessionEventReasonCode]
TUNNEL_SESSION_STATUS_UNSPECIFIED: TunnelSessionStatus
TUNNEL_SESSION_STATUS_PENDING: TunnelSessionStatus
TUNNEL_SESSION_STATUS_RUNNING: TunnelSessionStatus
TUNNEL_SESSION_STATUS_DEGRADED: TunnelSessionStatus
TUNNEL_SESSION_STATUS_REVOKED: TunnelSessionStatus
TUNNEL_SESSION_STATUS_EXPIRED: TunnelSessionStatus
TUNNEL_SESSION_STATUS_FAILED: TunnelSessionStatus
TUNNEL_PEER_KIND_UNSPECIFIED: TunnelPeerKind
TUNNEL_PEER_KIND_CLIENT: TunnelPeerKind
TUNNEL_PEER_KIND_NODE: TunnelPeerKind
TUNNEL_SESSION_EVENT_TYPE_UNSPECIFIED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_CREATED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_RENEWED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_NODE_STATUS: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_REVOKED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_EXPIRED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_CLIENT_DISCONNECTED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_NODE_CONNECTED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_NODE_DISCONNECTED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_PAIRED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_DRAIN_REJECTED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_TYPE_RESOURCE_LIMITED: TunnelSessionEventType
TUNNEL_SESSION_EVENT_REASON_CODE_UNSPECIFIED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_CREATED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_RENEWED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_NODE_RUNNING: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_NODE_DEGRADED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_NODE_FAILED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_MANUAL_REVOKE: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_DELETED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_EXPIRED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_CLIENT_CONNECTED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_CLIENT_DISCONNECTED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_NODE_CONNECTED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_NODE_DISCONNECTED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_PAIRED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_DRAIN_REJECTED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_RESOURCE_LIMITED: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_PONG_TIMEOUT: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_QUEUE_FULL: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_FRAME_TOO_LARGE: TunnelSessionEventReasonCode
TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_OPPOSITE_MISSING: TunnelSessionEventReasonCode

class TunnelSession(_message.Message):
    __slots__ = ("session_id", "allocation_id", "node_id", "node_target", "attempt", "remote_port", "local_target", "edge_target", "status", "reason", "bound_addr", "revoked", "created_at", "updated_at", "expires_at", "node_edge_target", "relay_id", "client_edge_target", "ready_at", "last_peer_event_at", "bytes_in", "bytes_out", "namespace", "creator_principal_id")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    REMOTE_PORT_FIELD_NUMBER: _ClassVar[int]
    LOCAL_TARGET_FIELD_NUMBER: _ClassVar[int]
    EDGE_TARGET_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    BOUND_ADDR_FIELD_NUMBER: _ClassVar[int]
    REVOKED_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    NODE_EDGE_TARGET_FIELD_NUMBER: _ClassVar[int]
    RELAY_ID_FIELD_NUMBER: _ClassVar[int]
    CLIENT_EDGE_TARGET_FIELD_NUMBER: _ClassVar[int]
    READY_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_PEER_EVENT_AT_FIELD_NUMBER: _ClassVar[int]
    BYTES_IN_FIELD_NUMBER: _ClassVar[int]
    BYTES_OUT_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    CREATOR_PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    allocation_id: str
    node_id: str
    node_target: str
    attempt: int
    remote_port: int
    local_target: str
    edge_target: str
    status: TunnelSessionStatus
    reason: str
    bound_addr: str
    revoked: bool
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    expires_at: _timestamp_pb2.Timestamp
    node_edge_target: str
    relay_id: str
    client_edge_target: str
    ready_at: _timestamp_pb2.Timestamp
    last_peer_event_at: _timestamp_pb2.Timestamp
    bytes_in: int
    bytes_out: int
    namespace: str
    creator_principal_id: str
    def __init__(self, session_id: _Optional[str] = ..., allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., node_target: _Optional[str] = ..., attempt: _Optional[int] = ..., remote_port: _Optional[int] = ..., local_target: _Optional[str] = ..., edge_target: _Optional[str] = ..., status: _Optional[_Union[TunnelSessionStatus, str]] = ..., reason: _Optional[str] = ..., bound_addr: _Optional[str] = ..., revoked: _Optional[bool] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., node_edge_target: _Optional[str] = ..., relay_id: _Optional[str] = ..., client_edge_target: _Optional[str] = ..., ready_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_peer_event_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., bytes_in: _Optional[int] = ..., bytes_out: _Optional[int] = ..., namespace: _Optional[str] = ..., creator_principal_id: _Optional[str] = ...) -> None: ...

class TunnelSessionEvent(_message.Message):
    __slots__ = ("event_id", "session_id", "event_type", "status", "reason", "bound_addr", "created_at", "reason_code", "relay_id", "peer_kind", "bytes_in", "bytes_out")
    EVENT_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    BOUND_ADDR_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    RELAY_ID_FIELD_NUMBER: _ClassVar[int]
    PEER_KIND_FIELD_NUMBER: _ClassVar[int]
    BYTES_IN_FIELD_NUMBER: _ClassVar[int]
    BYTES_OUT_FIELD_NUMBER: _ClassVar[int]
    event_id: int
    session_id: str
    event_type: TunnelSessionEventType
    status: TunnelSessionStatus
    reason: str
    bound_addr: str
    created_at: _timestamp_pb2.Timestamp
    reason_code: TunnelSessionEventReasonCode
    relay_id: str
    peer_kind: TunnelPeerKind
    bytes_in: int
    bytes_out: int
    def __init__(self, event_id: _Optional[int] = ..., session_id: _Optional[str] = ..., event_type: _Optional[_Union[TunnelSessionEventType, str]] = ..., status: _Optional[_Union[TunnelSessionStatus, str]] = ..., reason: _Optional[str] = ..., bound_addr: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., reason_code: _Optional[_Union[TunnelSessionEventReasonCode, str]] = ..., relay_id: _Optional[str] = ..., peer_kind: _Optional[_Union[TunnelPeerKind, str]] = ..., bytes_in: _Optional[int] = ..., bytes_out: _Optional[int] = ...) -> None: ...

class CreateTunnelSessionRequest(_message.Message):
    __slots__ = ("allocation_id", "remote_port", "local_target", "ttl", "wait_ready", "ready_timeout")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    REMOTE_PORT_FIELD_NUMBER: _ClassVar[int]
    LOCAL_TARGET_FIELD_NUMBER: _ClassVar[int]
    TTL_FIELD_NUMBER: _ClassVar[int]
    WAIT_READY_FIELD_NUMBER: _ClassVar[int]
    READY_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    remote_port: int
    local_target: str
    ttl: _duration_pb2.Duration
    wait_ready: bool
    ready_timeout: _duration_pb2.Duration
    def __init__(self, allocation_id: _Optional[str] = ..., remote_port: _Optional[int] = ..., local_target: _Optional[str] = ..., ttl: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., wait_ready: _Optional[bool] = ..., ready_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class CreateTunnelSessionResponse(_message.Message):
    __slots__ = ("session", "client_token")
    SESSION_FIELD_NUMBER: _ClassVar[int]
    CLIENT_TOKEN_FIELD_NUMBER: _ClassVar[int]
    session: TunnelSession
    client_token: str
    def __init__(self, session: _Optional[_Union[TunnelSession, _Mapping]] = ..., client_token: _Optional[str] = ...) -> None: ...

class GetTunnelSessionRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class GetTunnelSessionResponse(_message.Message):
    __slots__ = ("session",)
    SESSION_FIELD_NUMBER: _ClassVar[int]
    session: TunnelSession
    def __init__(self, session: _Optional[_Union[TunnelSession, _Mapping]] = ...) -> None: ...

class ListTunnelSessionsRequest(_message.Message):
    __slots__ = ("allocation_id", "node_id", "include_terminal", "namespace")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    INCLUDE_TERMINAL_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    node_id: str
    include_terminal: bool
    namespace: str
    def __init__(self, allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., include_terminal: _Optional[bool] = ..., namespace: _Optional[str] = ...) -> None: ...

class ListTunnelSessionsResponse(_message.Message):
    __slots__ = ("sessions",)
    SESSIONS_FIELD_NUMBER: _ClassVar[int]
    sessions: _containers.RepeatedCompositeFieldContainer[TunnelSession]
    def __init__(self, sessions: _Optional[_Iterable[_Union[TunnelSession, _Mapping]]] = ...) -> None: ...

class ListTunnelSessionEventsRequest(_message.Message):
    __slots__ = ("session_id", "limit")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    limit: int
    def __init__(self, session_id: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class ListTunnelSessionEventsResponse(_message.Message):
    __slots__ = ("events",)
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[TunnelSessionEvent]
    def __init__(self, events: _Optional[_Iterable[_Union[TunnelSessionEvent, _Mapping]]] = ...) -> None: ...

class InspectTunnelSessionRequest(_message.Message):
    __slots__ = ("session_id", "event_limit")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_LIMIT_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    event_limit: int
    def __init__(self, session_id: _Optional[str] = ..., event_limit: _Optional[int] = ...) -> None: ...

class InspectTunnelSessionResponse(_message.Message):
    __slots__ = ("session", "events")
    SESSION_FIELD_NUMBER: _ClassVar[int]
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    session: TunnelSession
    events: _containers.RepeatedCompositeFieldContainer[TunnelSessionEvent]
    def __init__(self, session: _Optional[_Union[TunnelSession, _Mapping]] = ..., events: _Optional[_Iterable[_Union[TunnelSessionEvent, _Mapping]]] = ...) -> None: ...

class RevokeTunnelSessionRequest(_message.Message):
    __slots__ = ("session_id", "reason")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    reason: str
    def __init__(self, session_id: _Optional[str] = ..., reason: _Optional[str] = ...) -> None: ...

class RevokeTunnelSessionResponse(_message.Message):
    __slots__ = ("session",)
    SESSION_FIELD_NUMBER: _ClassVar[int]
    session: TunnelSession
    def __init__(self, session: _Optional[_Union[TunnelSession, _Mapping]] = ...) -> None: ...

class RenewTunnelSessionRequest(_message.Message):
    __slots__ = ("session_id", "ttl", "client_token")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    TTL_FIELD_NUMBER: _ClassVar[int]
    CLIENT_TOKEN_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    ttl: _duration_pb2.Duration
    client_token: str
    def __init__(self, session_id: _Optional[str] = ..., ttl: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., client_token: _Optional[str] = ...) -> None: ...

class RenewTunnelSessionResponse(_message.Message):
    __slots__ = ("session",)
    SESSION_FIELD_NUMBER: _ClassVar[int]
    session: TunnelSession
    def __init__(self, session: _Optional[_Union[TunnelSession, _Mapping]] = ...) -> None: ...
