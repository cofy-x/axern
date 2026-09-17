import datetime

from axern.common.file.v1 import file_pb2 as _file_pb2
from axern.control.common.v1 import common_pb2 as _common_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class OutputStream(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    OUTPUT_STREAM_UNSPECIFIED: _ClassVar[OutputStream]
    OUTPUT_STREAM_STDOUT: _ClassVar[OutputStream]
    OUTPUT_STREAM_STDERR: _ClassVar[OutputStream]

class SealedOutputStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SEALED_OUTPUT_STATUS_UNSPECIFIED: _ClassVar[SealedOutputStatus]
    SEALED_OUTPUT_STATUS_AVAILABLE: _ClassVar[SealedOutputStatus]
    SEALED_OUTPUT_STATUS_MISSING: _ClassVar[SealedOutputStatus]
    SEALED_OUTPUT_STATUS_REJECTED: _ClassVar[SealedOutputStatus]
    SEALED_OUTPUT_STATUS_CAPTURE_FAILED: _ClassVar[SealedOutputStatus]
    SEALED_OUTPUT_STATUS_NODE_UNAVAILABLE: _ClassVar[SealedOutputStatus]
OUTPUT_STREAM_UNSPECIFIED: OutputStream
OUTPUT_STREAM_STDOUT: OutputStream
OUTPUT_STREAM_STDERR: OutputStream
SEALED_OUTPUT_STATUS_UNSPECIFIED: SealedOutputStatus
SEALED_OUTPUT_STATUS_AVAILABLE: SealedOutputStatus
SEALED_OUTPUT_STATUS_MISSING: SealedOutputStatus
SEALED_OUTPUT_STATUS_REJECTED: SealedOutputStatus
SEALED_OUTPUT_STATUS_CAPTURE_FAILED: SealedOutputStatus
SEALED_OUTPUT_STATUS_NODE_UNAVAILABLE: SealedOutputStatus

class ExecSpec(_message.Message):
    __slots__ = ("argv", "env", "cwd", "timeout_seconds", "tty", "user")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ARGV_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    CWD_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    TTY_FIELD_NUMBER: _ClassVar[int]
    USER_FIELD_NUMBER: _ClassVar[int]
    argv: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    cwd: str
    timeout_seconds: int
    tty: bool
    user: str
    def __init__(self, argv: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., cwd: _Optional[str] = ..., timeout_seconds: _Optional[int] = ..., tty: _Optional[bool] = ..., user: _Optional[str] = ...) -> None: ...

class ExecRequest(_message.Message):
    __slots__ = ("spec", "allocation_id")
    SPEC_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    spec: ExecSpec
    allocation_id: str
    def __init__(self, spec: _Optional[_Union[ExecSpec, _Mapping]] = ..., allocation_id: _Optional[str] = ...) -> None: ...

class ExecResponse(_message.Message):
    __slots__ = ("exit_code", "stdout", "stderr", "stdout_truncated", "stderr_truncated")
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    STDOUT_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    STDERR_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    exit_code: int
    stdout: bytes
    stderr: bytes
    stdout_truncated: bool
    stderr_truncated: bool
    def __init__(self, exit_code: _Optional[int] = ..., stdout: _Optional[bytes] = ..., stderr: _Optional[bytes] = ..., stdout_truncated: _Optional[bool] = ..., stderr_truncated: _Optional[bool] = ...) -> None: ...

class TerminalResize(_message.Message):
    __slots__ = ("cols", "rows")
    COLS_FIELD_NUMBER: _ClassVar[int]
    ROWS_FIELD_NUMBER: _ClassVar[int]
    cols: int
    rows: int
    def __init__(self, cols: _Optional[int] = ..., rows: _Optional[int] = ...) -> None: ...

class ExecExit(_message.Message):
    __slots__ = ("exit_code", "message")
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    exit_code: int
    message: str
    def __init__(self, exit_code: _Optional[int] = ..., message: _Optional[str] = ...) -> None: ...

class ProcessOpen(_message.Message):
    __slots__ = ("spec", "allocation_id", "initial_size")
    SPEC_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    INITIAL_SIZE_FIELD_NUMBER: _ClassVar[int]
    spec: ExecSpec
    allocation_id: str
    initial_size: TerminalResize
    def __init__(self, spec: _Optional[_Union[ExecSpec, _Mapping]] = ..., allocation_id: _Optional[str] = ..., initial_size: _Optional[_Union[TerminalResize, _Mapping]] = ...) -> None: ...

class ProcessSignal(_message.Message):
    __slots__ = ("signal",)
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    signal: str
    def __init__(self, signal: _Optional[str] = ...) -> None: ...

class ProcessReady(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ProcessRequest(_message.Message):
    __slots__ = ("open", "stdin", "resize", "close_stdin", "signal")
    OPEN_FIELD_NUMBER: _ClassVar[int]
    STDIN_FIELD_NUMBER: _ClassVar[int]
    RESIZE_FIELD_NUMBER: _ClassVar[int]
    CLOSE_STDIN_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    open: ProcessOpen
    stdin: bytes
    resize: TerminalResize
    close_stdin: bool
    signal: ProcessSignal
    def __init__(self, open: _Optional[_Union[ProcessOpen, _Mapping]] = ..., stdin: _Optional[bytes] = ..., resize: _Optional[_Union[TerminalResize, _Mapping]] = ..., close_stdin: _Optional[bool] = ..., signal: _Optional[_Union[ProcessSignal, _Mapping]] = ...) -> None: ...

class ProcessResponse(_message.Message):
    __slots__ = ("stdout", "stderr", "exit", "ready")
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    EXIT_FIELD_NUMBER: _ClassVar[int]
    READY_FIELD_NUMBER: _ClassVar[int]
    stdout: bytes
    stderr: bytes
    exit: ExecExit
    ready: ProcessReady
    def __init__(self, stdout: _Optional[bytes] = ..., stderr: _Optional[bytes] = ..., exit: _Optional[_Union[ExecExit, _Mapping]] = ..., ready: _Optional[_Union[ProcessReady, _Mapping]] = ...) -> None: ...

class ReadOutputRequest(_message.Message):
    __slots__ = ("allocation_id", "cursor", "follow")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    FOLLOW_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    cursor: str
    follow: bool
    def __init__(self, allocation_id: _Optional[str] = ..., cursor: _Optional[str] = ..., follow: _Optional[bool] = ...) -> None: ...

class ReadOutputResponse(_message.Message):
    __slots__ = ("stream", "data", "next_cursor", "terminal", "truncated", "observed_at_unix_milli")
    STREAM_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    TERMINAL_FIELD_NUMBER: _ClassVar[int]
    TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_AT_UNIX_MILLI_FIELD_NUMBER: _ClassVar[int]
    stream: OutputStream
    data: bytes
    next_cursor: str
    terminal: bool
    truncated: bool
    observed_at_unix_milli: int
    def __init__(self, stream: _Optional[_Union[OutputStream, str]] = ..., data: _Optional[bytes] = ..., next_cursor: _Optional[str] = ..., terminal: _Optional[bool] = ..., truncated: _Optional[bool] = ..., observed_at_unix_milli: _Optional[int] = ...) -> None: ...

class SealedOutput(_message.Message):
    __slots__ = ("output_id", "path", "size_bytes", "sha256", "media_type", "format", "status", "reason", "sealed_at", "expires_at")
    OUTPUT_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    SEALED_AT_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    output_id: str
    path: str
    size_bytes: int
    sha256: str
    media_type: str
    format: _common_pb2.DeclaredOutputFormat
    status: SealedOutputStatus
    reason: str
    sealed_at: _timestamp_pb2.Timestamp
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, output_id: _Optional[str] = ..., path: _Optional[str] = ..., size_bytes: _Optional[int] = ..., sha256: _Optional[str] = ..., media_type: _Optional[str] = ..., format: _Optional[_Union[_common_pb2.DeclaredOutputFormat, str]] = ..., status: _Optional[_Union[SealedOutputStatus, str]] = ..., reason: _Optional[str] = ..., sealed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class GetSealedOutputManifestRequest(_message.Message):
    __slots__ = ("allocation_id",)
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    def __init__(self, allocation_id: _Optional[str] = ...) -> None: ...

class GetSealedOutputManifestResponse(_message.Message):
    __slots__ = ("outputs", "expires_at")
    OUTPUTS_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    outputs: _containers.RepeatedCompositeFieldContainer[SealedOutput]
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, outputs: _Optional[_Iterable[_Union[SealedOutput, _Mapping]]] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class DownloadSealedOutputRequest(_message.Message):
    __slots__ = ("allocation_id", "output_id", "offset")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_ID_FIELD_NUMBER: _ClassVar[int]
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    output_id: str
    offset: int
    def __init__(self, allocation_id: _Optional[str] = ..., output_id: _Optional[str] = ..., offset: _Optional[int] = ...) -> None: ...

class DownloadSealedOutputResponse(_message.Message):
    __slots__ = ("data", "next_offset", "eof")
    DATA_FIELD_NUMBER: _ClassVar[int]
    NEXT_OFFSET_FIELD_NUMBER: _ClassVar[int]
    EOF_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    next_offset: int
    eof: bool
    def __init__(self, data: _Optional[bytes] = ..., next_offset: _Optional[int] = ..., eof: _Optional[bool] = ...) -> None: ...

class CapabilityStatusRequest(_message.Message):
    __slots__ = ("allocation_id",)
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    def __init__(self, allocation_id: _Optional[str] = ...) -> None: ...

class CapabilityProviderDependencyStatus(_message.Message):
    __slots__ = ("name", "available", "reason")
    NAME_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    name: str
    available: bool
    reason: str
    def __init__(self, name: _Optional[str] = ..., available: _Optional[bool] = ..., reason: _Optional[str] = ...) -> None: ...

class CapabilityProviderStatus(_message.Message):
    __slots__ = ("name", "state", "available", "capabilities", "backend", "reason", "dependencies")
    NAME_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    BACKEND_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    DEPENDENCIES_FIELD_NUMBER: _ClassVar[int]
    name: str
    state: str
    available: bool
    capabilities: _containers.RepeatedScalarFieldContainer[str]
    backend: str
    reason: str
    dependencies: _containers.RepeatedCompositeFieldContainer[CapabilityProviderDependencyStatus]
    def __init__(self, name: _Optional[str] = ..., state: _Optional[str] = ..., available: _Optional[bool] = ..., capabilities: _Optional[_Iterable[str]] = ..., backend: _Optional[str] = ..., reason: _Optional[str] = ..., dependencies: _Optional[_Iterable[_Union[CapabilityProviderDependencyStatus, _Mapping]]] = ...) -> None: ...

class CapabilityProviderSummary(_message.Message):
    __slots__ = ("total", "available", "degraded", "unavailable")
    TOTAL_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    DEGRADED_FIELD_NUMBER: _ClassVar[int]
    UNAVAILABLE_FIELD_NUMBER: _ClassVar[int]
    total: int
    available: int
    degraded: int
    unavailable: int
    def __init__(self, total: _Optional[int] = ..., available: _Optional[int] = ..., degraded: _Optional[int] = ..., unavailable: _Optional[int] = ...) -> None: ...

class CapabilityStatusResponse(_message.Message):
    __slots__ = ("ready", "capabilities", "providers", "provider_summary")
    READY_FIELD_NUMBER: _ClassVar[int]
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    PROVIDERS_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_SUMMARY_FIELD_NUMBER: _ClassVar[int]
    ready: bool
    capabilities: _containers.RepeatedScalarFieldContainer[str]
    providers: _containers.RepeatedCompositeFieldContainer[CapabilityProviderStatus]
    provider_summary: CapabilityProviderSummary
    def __init__(self, ready: _Optional[bool] = ..., capabilities: _Optional[_Iterable[str]] = ..., providers: _Optional[_Iterable[_Union[CapabilityProviderStatus, _Mapping]]] = ..., provider_summary: _Optional[_Union[CapabilityProviderSummary, _Mapping]] = ...) -> None: ...

class StatFileRequest(_message.Message):
    __slots__ = ("allocation_id", "path")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class StatFileResponse(_message.Message):
    __slots__ = ("info",)
    INFO_FIELD_NUMBER: _ClassVar[int]
    info: _file_pb2.SandboxFileInfo
    def __init__(self, info: _Optional[_Union[_file_pb2.SandboxFileInfo, _Mapping]] = ...) -> None: ...

class ListDirRequest(_message.Message):
    __slots__ = ("allocation_id", "path")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class ListDirResponse(_message.Message):
    __slots__ = ("entries",)
    ENTRIES_FIELD_NUMBER: _ClassVar[int]
    entries: _containers.RepeatedCompositeFieldContainer[_file_pb2.SandboxFileInfo]
    def __init__(self, entries: _Optional[_Iterable[_Union[_file_pb2.SandboxFileInfo, _Mapping]]] = ...) -> None: ...

class ReadFileRequest(_message.Message):
    __slots__ = ("allocation_id", "path")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class ReadFileResponse(_message.Message):
    __slots__ = ("data",)
    DATA_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    def __init__(self, data: _Optional[bytes] = ...) -> None: ...

class WriteFileRequest(_message.Message):
    __slots__ = ("allocation_id", "path", "data", "create_parents")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    CREATE_PARENTS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    data: bytes
    create_parents: bool
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ..., data: _Optional[bytes] = ..., create_parents: _Optional[bool] = ...) -> None: ...

class WriteFileResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class MkdirRequest(_message.Message):
    __slots__ = ("allocation_id", "path", "parents")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    PARENTS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    parents: bool
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ..., parents: _Optional[bool] = ...) -> None: ...

class MkdirResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class RemoveRequest(_message.Message):
    __slots__ = ("allocation_id", "path", "recursive", "force")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    FORCE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    recursive: bool
    force: bool
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ..., recursive: _Optional[bool] = ..., force: _Optional[bool] = ...) -> None: ...

class RemoveResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ExistsRequest(_message.Message):
    __slots__ = ("allocation_id", "path")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class ExistsResponse(_message.Message):
    __slots__ = ("exists",)
    EXISTS_FIELD_NUMBER: _ClassVar[int]
    exists: bool
    def __init__(self, exists: _Optional[bool] = ...) -> None: ...

class CopyRequest(_message.Message):
    __slots__ = ("allocation_id", "src_path", "dst_path", "recursive", "overwrite")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    SRC_PATH_FIELD_NUMBER: _ClassVar[int]
    DST_PATH_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    OVERWRITE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    src_path: str
    dst_path: str
    recursive: bool
    overwrite: bool
    def __init__(self, allocation_id: _Optional[str] = ..., src_path: _Optional[str] = ..., dst_path: _Optional[str] = ..., recursive: _Optional[bool] = ..., overwrite: _Optional[bool] = ...) -> None: ...

class CopyResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class MoveRequest(_message.Message):
    __slots__ = ("allocation_id", "src_path", "dst_path", "overwrite")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    SRC_PATH_FIELD_NUMBER: _ClassVar[int]
    DST_PATH_FIELD_NUMBER: _ClassVar[int]
    OVERWRITE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    src_path: str
    dst_path: str
    overwrite: bool
    def __init__(self, allocation_id: _Optional[str] = ..., src_path: _Optional[str] = ..., dst_path: _Optional[str] = ..., overwrite: _Optional[bool] = ...) -> None: ...

class MoveResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ChmodRequest(_message.Message):
    __slots__ = ("allocation_id", "path", "mode", "recursive")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    mode: int
    recursive: bool
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ..., mode: _Optional[int] = ..., recursive: _Optional[bool] = ...) -> None: ...

class ChmodResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class TouchRequest(_message.Message):
    __slots__ = ("allocation_id", "path", "create", "mtime_ns")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    CREATE_FIELD_NUMBER: _ClassVar[int]
    MTIME_NS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    create: bool
    mtime_ns: int
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ..., create: _Optional[bool] = ..., mtime_ns: _Optional[int] = ...) -> None: ...

class TouchResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class UploadArchiveOpen(_message.Message):
    __slots__ = ("allocation_id", "path", "format", "create_parents", "overwrite", "symlink_policy")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    CREATE_PARENTS_FIELD_NUMBER: _ClassVar[int]
    OVERWRITE_FIELD_NUMBER: _ClassVar[int]
    SYMLINK_POLICY_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    format: _file_pb2.SandboxArchiveFormat
    create_parents: bool
    overwrite: bool
    symlink_policy: _file_pb2.SandboxArchiveSymlinkPolicy
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ..., format: _Optional[_Union[_file_pb2.SandboxArchiveFormat, str]] = ..., create_parents: _Optional[bool] = ..., overwrite: _Optional[bool] = ..., symlink_policy: _Optional[_Union[_file_pb2.SandboxArchiveSymlinkPolicy, str]] = ...) -> None: ...

class UploadArchiveRequest(_message.Message):
    __slots__ = ("open", "chunk")
    OPEN_FIELD_NUMBER: _ClassVar[int]
    CHUNK_FIELD_NUMBER: _ClassVar[int]
    open: UploadArchiveOpen
    chunk: bytes
    def __init__(self, open: _Optional[_Union[UploadArchiveOpen, _Mapping]] = ..., chunk: _Optional[bytes] = ...) -> None: ...

class UploadArchiveResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class DownloadArchiveRequest(_message.Message):
    __slots__ = ("allocation_id", "path", "format", "symlink_policy")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    SYMLINK_POLICY_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    path: str
    format: _file_pb2.SandboxArchiveFormat
    symlink_policy: _file_pb2.SandboxArchiveSymlinkPolicy
    def __init__(self, allocation_id: _Optional[str] = ..., path: _Optional[str] = ..., format: _Optional[_Union[_file_pb2.SandboxArchiveFormat, str]] = ..., symlink_policy: _Optional[_Union[_file_pb2.SandboxArchiveSymlinkPolicy, str]] = ...) -> None: ...

class DownloadArchiveResponse(_message.Message):
    __slots__ = ("chunk",)
    CHUNK_FIELD_NUMBER: _ClassVar[int]
    chunk: bytes
    def __init__(self, chunk: _Optional[bytes] = ...) -> None: ...

class ComputerUseStatusRequest(_message.Message):
    __slots__ = ("allocation_id",)
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    def __init__(self, allocation_id: _Optional[str] = ...) -> None: ...

class ComputerUseStatusResponse(_message.Message):
    __slots__ = ("available", "display", "backend", "reason", "dependencies")
    AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_FIELD_NUMBER: _ClassVar[int]
    BACKEND_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    DEPENDENCIES_FIELD_NUMBER: _ClassVar[int]
    available: bool
    display: str
    backend: str
    reason: str
    dependencies: _containers.RepeatedCompositeFieldContainer[ComputerUseDependencyStatus]
    def __init__(self, available: _Optional[bool] = ..., display: _Optional[str] = ..., backend: _Optional[str] = ..., reason: _Optional[str] = ..., dependencies: _Optional[_Iterable[_Union[ComputerUseDependencyStatus, _Mapping]]] = ...) -> None: ...

class ComputerUseDependencyStatus(_message.Message):
    __slots__ = ("name", "available", "reason")
    NAME_FIELD_NUMBER: _ClassVar[int]
    AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    name: str
    available: bool
    reason: str
    def __init__(self, name: _Optional[str] = ..., available: _Optional[bool] = ..., reason: _Optional[str] = ...) -> None: ...

class ComputerUseRegion(_message.Message):
    __slots__ = ("x", "y", "width", "height")
    X_FIELD_NUMBER: _ClassVar[int]
    Y_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    x: int
    y: int
    width: int
    height: int
    def __init__(self, x: _Optional[int] = ..., y: _Optional[int] = ..., width: _Optional[int] = ..., height: _Optional[int] = ...) -> None: ...

class ComputerUseScreenshotRequest(_message.Message):
    __slots__ = ("allocation_id", "show_cursor", "region", "format", "quality", "scale")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    SHOW_CURSOR_FIELD_NUMBER: _ClassVar[int]
    REGION_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    QUALITY_FIELD_NUMBER: _ClassVar[int]
    SCALE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    show_cursor: bool
    region: ComputerUseRegion
    format: str
    quality: int
    scale: float
    def __init__(self, allocation_id: _Optional[str] = ..., show_cursor: _Optional[bool] = ..., region: _Optional[_Union[ComputerUseRegion, _Mapping]] = ..., format: _Optional[str] = ..., quality: _Optional[int] = ..., scale: _Optional[float] = ...) -> None: ...

class ComputerUseScreenshotResponse(_message.Message):
    __slots__ = ("data", "content_type")
    DATA_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    content_type: str
    def __init__(self, data: _Optional[bytes] = ..., content_type: _Optional[str] = ...) -> None: ...

class ComputerUseDisplayRequest(_message.Message):
    __slots__ = ("allocation_id",)
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    def __init__(self, allocation_id: _Optional[str] = ...) -> None: ...

class ComputerUseDisplayResponse(_message.Message):
    __slots__ = ("display", "backend", "width", "height")
    DISPLAY_FIELD_NUMBER: _ClassVar[int]
    BACKEND_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    display: str
    backend: str
    width: int
    height: int
    def __init__(self, display: _Optional[str] = ..., backend: _Optional[str] = ..., width: _Optional[int] = ..., height: _Optional[int] = ...) -> None: ...

class ComputerUseMouseRequest(_message.Message):
    __slots__ = ("allocation_id", "action", "x", "y", "to_x", "to_y", "button", "direction", "amount")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    X_FIELD_NUMBER: _ClassVar[int]
    Y_FIELD_NUMBER: _ClassVar[int]
    TO_X_FIELD_NUMBER: _ClassVar[int]
    TO_Y_FIELD_NUMBER: _ClassVar[int]
    BUTTON_FIELD_NUMBER: _ClassVar[int]
    DIRECTION_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    action: str
    x: int
    y: int
    to_x: int
    to_y: int
    button: str
    direction: str
    amount: int
    def __init__(self, allocation_id: _Optional[str] = ..., action: _Optional[str] = ..., x: _Optional[int] = ..., y: _Optional[int] = ..., to_x: _Optional[int] = ..., to_y: _Optional[int] = ..., button: _Optional[str] = ..., direction: _Optional[str] = ..., amount: _Optional[int] = ...) -> None: ...

class ComputerUseMouseResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ComputerUseKeyboardRequest(_message.Message):
    __slots__ = ("allocation_id", "text", "key", "keys", "delay_ms")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    KEYS_FIELD_NUMBER: _ClassVar[int]
    DELAY_MS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    text: str
    key: str
    keys: _containers.RepeatedScalarFieldContainer[str]
    delay_ms: int
    def __init__(self, allocation_id: _Optional[str] = ..., text: _Optional[str] = ..., key: _Optional[str] = ..., keys: _Optional[_Iterable[str]] = ..., delay_ms: _Optional[int] = ...) -> None: ...

class ComputerUseKeyboardResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
