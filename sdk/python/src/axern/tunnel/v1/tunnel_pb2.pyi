from axern.control.tunnel.v1 import tunnel_pb2 as _tunnel_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PeerOpen(_message.Message):
    __slots__ = ("session_id", "peer_kind", "token")
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PEER_KIND_FIELD_NUMBER: _ClassVar[int]
    TOKEN_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    peer_kind: _tunnel_pb2.TunnelPeerKind
    token: str
    def __init__(self, session_id: _Optional[str] = ..., peer_kind: _Optional[_Union[_tunnel_pb2.TunnelPeerKind, str]] = ..., token: _Optional[str] = ...) -> None: ...

class StreamOpen(_message.Message):
    __slots__ = ("stream_id",)
    STREAM_ID_FIELD_NUMBER: _ClassVar[int]
    stream_id: int
    def __init__(self, stream_id: _Optional[int] = ...) -> None: ...

class StreamData(_message.Message):
    __slots__ = ("stream_id", "data")
    STREAM_ID_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    stream_id: int
    data: bytes
    def __init__(self, stream_id: _Optional[int] = ..., data: _Optional[bytes] = ...) -> None: ...

class StreamClose(_message.Message):
    __slots__ = ("stream_id", "error")
    STREAM_ID_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    stream_id: int
    error: str
    def __init__(self, stream_id: _Optional[int] = ..., error: _Optional[str] = ...) -> None: ...

class Ping(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class Pong(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class TunnelFrame(_message.Message):
    __slots__ = ("peer_open", "stream_open", "stream_data", "stream_close", "ping", "pong")
    PEER_OPEN_FIELD_NUMBER: _ClassVar[int]
    STREAM_OPEN_FIELD_NUMBER: _ClassVar[int]
    STREAM_DATA_FIELD_NUMBER: _ClassVar[int]
    STREAM_CLOSE_FIELD_NUMBER: _ClassVar[int]
    PING_FIELD_NUMBER: _ClassVar[int]
    PONG_FIELD_NUMBER: _ClassVar[int]
    peer_open: PeerOpen
    stream_open: StreamOpen
    stream_data: StreamData
    stream_close: StreamClose
    ping: Ping
    pong: Pong
    def __init__(self, peer_open: _Optional[_Union[PeerOpen, _Mapping]] = ..., stream_open: _Optional[_Union[StreamOpen, _Mapping]] = ..., stream_data: _Optional[_Union[StreamData, _Mapping]] = ..., stream_close: _Optional[_Union[StreamClose, _Mapping]] = ..., ping: _Optional[_Union[Ping, _Mapping]] = ..., pong: _Optional[_Union[Pong, _Mapping]] = ...) -> None: ...
