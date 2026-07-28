from axern.common.file.v1 import file_pb2 as _file_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SandboxProcessState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SANDBOX_PROCESS_STATE_UNSPECIFIED: _ClassVar[SandboxProcessState]
    SANDBOX_PROCESS_STATE_RUNNING: _ClassVar[SandboxProcessState]
    SANDBOX_PROCESS_STATE_EXITED: _ClassVar[SandboxProcessState]
    SANDBOX_PROCESS_STATE_UNKNOWN: _ClassVar[SandboxProcessState]

class TaskAssetKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TASK_ASSET_KIND_UNSPECIFIED: _ClassVar[TaskAssetKind]
    TASK_ASSET_KIND_VERIFIER: _ClassVar[TaskAssetKind]
    TASK_ASSET_KIND_ORACLE: _ClassVar[TaskAssetKind]
SANDBOX_PROCESS_STATE_UNSPECIFIED: SandboxProcessState
SANDBOX_PROCESS_STATE_RUNNING: SandboxProcessState
SANDBOX_PROCESS_STATE_EXITED: SandboxProcessState
SANDBOX_PROCESS_STATE_UNKNOWN: SandboxProcessState
TASK_ASSET_KIND_UNSPECIFIED: TaskAssetKind
TASK_ASSET_KIND_VERIFIER: TaskAssetKind
TASK_ASSET_KIND_ORACLE: TaskAssetKind

class ExecSpec(_message.Message):
    __slots__ = ("argv", "env", "cwd", "timeout_seconds", "tty", "user", "managed_proxy")
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
    MANAGED_PROXY_FIELD_NUMBER: _ClassVar[int]
    argv: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    cwd: str
    timeout_seconds: int
    tty: bool
    user: str
    managed_proxy: ManagedProxySpec
    def __init__(self, argv: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., cwd: _Optional[str] = ..., timeout_seconds: _Optional[int] = ..., tty: _Optional[bool] = ..., user: _Optional[str] = ..., managed_proxy: _Optional[_Union[ManagedProxySpec, _Mapping]] = ...) -> None: ...

class ManagedProxySpec(_message.Message):
    __slots__ = ("provider", "upstream_base_url", "upstream_bearer_token")
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    UPSTREAM_BASE_URL_FIELD_NUMBER: _ClassVar[int]
    UPSTREAM_BEARER_TOKEN_FIELD_NUMBER: _ClassVar[int]
    provider: str
    upstream_base_url: str
    upstream_bearer_token: str
    def __init__(self, provider: _Optional[str] = ..., upstream_base_url: _Optional[str] = ..., upstream_bearer_token: _Optional[str] = ...) -> None: ...

class ManagedProxyReport(_message.Message):
    __slots__ = ("provider", "request_count", "response_count", "error_count", "report_json")
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    REQUEST_COUNT_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_COUNT_FIELD_NUMBER: _ClassVar[int]
    ERROR_COUNT_FIELD_NUMBER: _ClassVar[int]
    REPORT_JSON_FIELD_NUMBER: _ClassVar[int]
    provider: str
    request_count: int
    response_count: int
    error_count: int
    report_json: bytes
    def __init__(self, provider: _Optional[str] = ..., request_count: _Optional[int] = ..., response_count: _Optional[int] = ..., error_count: _Optional[int] = ..., report_json: _Optional[bytes] = ...) -> None: ...

class ExecRequest(_message.Message):
    __slots__ = ("spec", "allocation_id", "attempt", "execution_lease_token")
    SPEC_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    spec: ExecSpec
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, spec: _Optional[_Union[ExecSpec, _Mapping]] = ..., allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

class ExecResponse(_message.Message):
    __slots__ = ("exit_code", "stdout", "stderr", "stdout_truncated", "stderr_truncated", "managed_proxy_report")
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    STDOUT_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    STDERR_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    MANAGED_PROXY_REPORT_FIELD_NUMBER: _ClassVar[int]
    exit_code: int
    stdout: bytes
    stderr: bytes
    stdout_truncated: bool
    stderr_truncated: bool
    managed_proxy_report: ManagedProxyReport
    def __init__(self, exit_code: _Optional[int] = ..., stdout: _Optional[bytes] = ..., stderr: _Optional[bytes] = ..., stdout_truncated: _Optional[bool] = ..., stderr_truncated: _Optional[bool] = ..., managed_proxy_report: _Optional[_Union[ManagedProxyReport, _Mapping]] = ...) -> None: ...

class ExecStreamOpen(_message.Message):
    __slots__ = ("spec", "allocation_id", "attempt", "execution_lease_token", "initial_size")
    SPEC_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    INITIAL_SIZE_FIELD_NUMBER: _ClassVar[int]
    spec: ExecSpec
    allocation_id: str
    attempt: int
    execution_lease_token: str
    initial_size: TerminalResize
    def __init__(self, spec: _Optional[_Union[ExecSpec, _Mapping]] = ..., allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., initial_size: _Optional[_Union[TerminalResize, _Mapping]] = ...) -> None: ...

class TerminalResize(_message.Message):
    __slots__ = ("cols", "rows")
    COLS_FIELD_NUMBER: _ClassVar[int]
    ROWS_FIELD_NUMBER: _ClassVar[int]
    cols: int
    rows: int
    def __init__(self, cols: _Optional[int] = ..., rows: _Optional[int] = ...) -> None: ...

class ExecStreamRequest(_message.Message):
    __slots__ = ("open", "stdin", "resize", "close_stdin")
    OPEN_FIELD_NUMBER: _ClassVar[int]
    STDIN_FIELD_NUMBER: _ClassVar[int]
    RESIZE_FIELD_NUMBER: _ClassVar[int]
    CLOSE_STDIN_FIELD_NUMBER: _ClassVar[int]
    open: ExecStreamOpen
    stdin: bytes
    resize: TerminalResize
    close_stdin: bool
    def __init__(self, open: _Optional[_Union[ExecStreamOpen, _Mapping]] = ..., stdin: _Optional[bytes] = ..., resize: _Optional[_Union[TerminalResize, _Mapping]] = ..., close_stdin: _Optional[bool] = ...) -> None: ...

class ExecExit(_message.Message):
    __slots__ = ("exit_code", "message", "managed_proxy_report")
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    MANAGED_PROXY_REPORT_FIELD_NUMBER: _ClassVar[int]
    exit_code: int
    message: str
    managed_proxy_report: ManagedProxyReport
    def __init__(self, exit_code: _Optional[int] = ..., message: _Optional[str] = ..., managed_proxy_report: _Optional[_Union[ManagedProxyReport, _Mapping]] = ...) -> None: ...

class ExecStreamResponse(_message.Message):
    __slots__ = ("stdout", "stderr", "exit")
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    EXIT_FIELD_NUMBER: _ClassVar[int]
    stdout: bytes
    stderr: bytes
    exit: ExecExit
    def __init__(self, stdout: _Optional[bytes] = ..., stderr: _Optional[bytes] = ..., exit: _Optional[_Union[ExecExit, _Mapping]] = ...) -> None: ...

class ProcessOpen(_message.Message):
    __slots__ = ("spec", "allocation_id", "attempt", "execution_lease_token")
    SPEC_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    spec: ExecSpec
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, spec: _Optional[_Union[ExecSpec, _Mapping]] = ..., allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

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

class ImageProcessMount(_message.Message):
    __slots__ = ("sandbox_path", "target_path", "readonly", "options")
    SANDBOX_PATH_FIELD_NUMBER: _ClassVar[int]
    TARGET_PATH_FIELD_NUMBER: _ClassVar[int]
    READONLY_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    sandbox_path: str
    target_path: str
    readonly: bool
    options: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, sandbox_path: _Optional[str] = ..., target_path: _Optional[str] = ..., readonly: _Optional[bool] = ..., options: _Optional[_Iterable[str]] = ...) -> None: ...

class ImageProcessSpec(_message.Message):
    __slots__ = ("image", "argv", "env", "cwd", "timeout_seconds", "tty", "user", "mounts", "managed_proxy")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    ARGV_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    CWD_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECONDS_FIELD_NUMBER: _ClassVar[int]
    TTY_FIELD_NUMBER: _ClassVar[int]
    USER_FIELD_NUMBER: _ClassVar[int]
    MOUNTS_FIELD_NUMBER: _ClassVar[int]
    MANAGED_PROXY_FIELD_NUMBER: _ClassVar[int]
    image: str
    argv: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    cwd: str
    timeout_seconds: int
    tty: bool
    user: str
    mounts: _containers.RepeatedCompositeFieldContainer[ImageProcessMount]
    managed_proxy: ManagedProxySpec
    def __init__(self, image: _Optional[str] = ..., argv: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., cwd: _Optional[str] = ..., timeout_seconds: _Optional[int] = ..., tty: _Optional[bool] = ..., user: _Optional[str] = ..., mounts: _Optional[_Iterable[_Union[ImageProcessMount, _Mapping]]] = ..., managed_proxy: _Optional[_Union[ManagedProxySpec, _Mapping]] = ...) -> None: ...

class ExecImageRequest(_message.Message):
    __slots__ = ("spec", "allocation_id", "attempt", "execution_lease_token")
    SPEC_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    spec: ImageProcessSpec
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, spec: _Optional[_Union[ImageProcessSpec, _Mapping]] = ..., allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

class ExecImageResponse(_message.Message):
    __slots__ = ("exit_code", "stdout", "stderr", "stdout_truncated", "stderr_truncated", "managed_proxy_report")
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    STDOUT_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    STDERR_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    MANAGED_PROXY_REPORT_FIELD_NUMBER: _ClassVar[int]
    exit_code: int
    stdout: bytes
    stderr: bytes
    stdout_truncated: bool
    stderr_truncated: bool
    managed_proxy_report: ManagedProxyReport
    def __init__(self, exit_code: _Optional[int] = ..., stdout: _Optional[bytes] = ..., stderr: _Optional[bytes] = ..., stdout_truncated: _Optional[bool] = ..., stderr_truncated: _Optional[bool] = ..., managed_proxy_report: _Optional[_Union[ManagedProxyReport, _Mapping]] = ...) -> None: ...

class ProcessImageOpen(_message.Message):
    __slots__ = ("spec", "allocation_id", "attempt", "execution_lease_token")
    SPEC_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    spec: ImageProcessSpec
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, spec: _Optional[_Union[ImageProcessSpec, _Mapping]] = ..., allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

class ProcessImageRequest(_message.Message):
    __slots__ = ("open", "stdin", "resize", "close_stdin", "signal")
    OPEN_FIELD_NUMBER: _ClassVar[int]
    STDIN_FIELD_NUMBER: _ClassVar[int]
    RESIZE_FIELD_NUMBER: _ClassVar[int]
    CLOSE_STDIN_FIELD_NUMBER: _ClassVar[int]
    SIGNAL_FIELD_NUMBER: _ClassVar[int]
    open: ProcessImageOpen
    stdin: bytes
    resize: TerminalResize
    close_stdin: bool
    signal: ProcessSignal
    def __init__(self, open: _Optional[_Union[ProcessImageOpen, _Mapping]] = ..., stdin: _Optional[bytes] = ..., resize: _Optional[_Union[TerminalResize, _Mapping]] = ..., close_stdin: _Optional[bool] = ..., signal: _Optional[_Union[ProcessSignal, _Mapping]] = ...) -> None: ...

class ProcessImageResponse(_message.Message):
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

class WaitSandboxRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

class WaitSandboxResponse(_message.Message):
    __slots__ = ("state", "exit_code", "exit_code_known", "message")
    STATE_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_KNOWN_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    state: SandboxProcessState
    exit_code: int
    exit_code_known: bool
    message: str
    def __init__(self, state: _Optional[_Union[SandboxProcessState, str]] = ..., exit_code: _Optional[int] = ..., exit_code_known: _Optional[bool] = ..., message: _Optional[str] = ...) -> None: ...

class CapabilityStatusRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

class CapabilityDependencyStatus(_message.Message):
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
    dependencies: _containers.RepeatedCompositeFieldContainer[CapabilityDependencyStatus]
    def __init__(self, name: _Optional[str] = ..., state: _Optional[str] = ..., available: _Optional[bool] = ..., capabilities: _Optional[_Iterable[str]] = ..., backend: _Optional[str] = ..., reason: _Optional[str] = ..., dependencies: _Optional[_Iterable[_Union[CapabilityDependencyStatus, _Mapping]]] = ...) -> None: ...

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

class ProxyHTTPHeader(_message.Message):
    __slots__ = ("key", "value")
    KEY_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    key: str
    value: str
    def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...

class ProxyHTTPOpen(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "port", "method", "path", "query", "headers", "has_body", "content_length")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    METHOD_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    QUERY_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    HAS_BODY_FIELD_NUMBER: _ClassVar[int]
    CONTENT_LENGTH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    port: int
    method: str
    path: str
    query: str
    headers: _containers.RepeatedCompositeFieldContainer[ProxyHTTPHeader]
    has_body: bool
    content_length: int
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., port: _Optional[int] = ..., method: _Optional[str] = ..., path: _Optional[str] = ..., query: _Optional[str] = ..., headers: _Optional[_Iterable[_Union[ProxyHTTPHeader, _Mapping]]] = ..., has_body: _Optional[bool] = ..., content_length: _Optional[int] = ...) -> None: ...

class ProxyHTTPResponseHead(_message.Message):
    __slots__ = ("status_code", "headers")
    STATUS_CODE_FIELD_NUMBER: _ClassVar[int]
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    status_code: int
    headers: _containers.RepeatedCompositeFieldContainer[ProxyHTTPHeader]
    def __init__(self, status_code: _Optional[int] = ..., headers: _Optional[_Iterable[_Union[ProxyHTTPHeader, _Mapping]]] = ...) -> None: ...

class ProxyHTTPTrailers(_message.Message):
    __slots__ = ("headers",)
    HEADERS_FIELD_NUMBER: _ClassVar[int]
    headers: _containers.RepeatedCompositeFieldContainer[ProxyHTTPHeader]
    def __init__(self, headers: _Optional[_Iterable[_Union[ProxyHTTPHeader, _Mapping]]] = ...) -> None: ...

class ProxyHTTPRequest(_message.Message):
    __slots__ = ("open", "body", "close_body")
    OPEN_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    CLOSE_BODY_FIELD_NUMBER: _ClassVar[int]
    open: ProxyHTTPOpen
    body: bytes
    close_body: bool
    def __init__(self, open: _Optional[_Union[ProxyHTTPOpen, _Mapping]] = ..., body: _Optional[bytes] = ..., close_body: _Optional[bool] = ...) -> None: ...

class ProxyHTTPResponse(_message.Message):
    __slots__ = ("head", "body", "trailers", "error")
    HEAD_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    TRAILERS_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    head: ProxyHTTPResponseHead
    body: bytes
    trailers: ProxyHTTPTrailers
    error: str
    def __init__(self, head: _Optional[_Union[ProxyHTTPResponseHead, _Mapping]] = ..., body: _Optional[bytes] = ..., trailers: _Optional[_Union[ProxyHTTPTrailers, _Mapping]] = ..., error: _Optional[str] = ...) -> None: ...

class StatFileRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class StatFileResponse(_message.Message):
    __slots__ = ("info",)
    INFO_FIELD_NUMBER: _ClassVar[int]
    info: _file_pb2.SandboxFileInfo
    def __init__(self, info: _Optional[_Union[_file_pb2.SandboxFileInfo, _Mapping]] = ...) -> None: ...

class ListDirRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class ListDirResponse(_message.Message):
    __slots__ = ("entries",)
    ENTRIES_FIELD_NUMBER: _ClassVar[int]
    entries: _containers.RepeatedCompositeFieldContainer[_file_pb2.SandboxFileInfo]
    def __init__(self, entries: _Optional[_Iterable[_Union[_file_pb2.SandboxFileInfo, _Mapping]]] = ...) -> None: ...

class ReadFileRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class ReadFileResponse(_message.Message):
    __slots__ = ("data",)
    DATA_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    def __init__(self, data: _Optional[bytes] = ...) -> None: ...

class WriteFileRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path", "data", "create_parents")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    CREATE_PARENTS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    data: bytes
    create_parents: bool
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ..., data: _Optional[bytes] = ..., create_parents: _Optional[bool] = ...) -> None: ...

class WriteFileResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class MaterializeTaskAssetsRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "source_path", "target", "kind")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    SOURCE_PATH_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    source_path: str
    target: str
    kind: TaskAssetKind
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., source_path: _Optional[str] = ..., target: _Optional[str] = ..., kind: _Optional[_Union[TaskAssetKind, str]] = ...) -> None: ...

class MaterializeTaskAssetsResponse(_message.Message):
    __slots__ = ("duration_ms",)
    DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    duration_ms: int
    def __init__(self, duration_ms: _Optional[int] = ...) -> None: ...

class MkdirRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path", "parents")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    PARENTS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    parents: bool
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ..., parents: _Optional[bool] = ...) -> None: ...

class MkdirResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class RemoveRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path", "recursive", "force")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    FORCE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    recursive: bool
    force: bool
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ..., recursive: _Optional[bool] = ..., force: _Optional[bool] = ...) -> None: ...

class RemoveResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ExistsRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class ExistsResponse(_message.Message):
    __slots__ = ("exists",)
    EXISTS_FIELD_NUMBER: _ClassVar[int]
    exists: bool
    def __init__(self, exists: _Optional[bool] = ...) -> None: ...

class CopyRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "src_path", "dst_path", "recursive", "overwrite")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    SRC_PATH_FIELD_NUMBER: _ClassVar[int]
    DST_PATH_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    OVERWRITE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    src_path: str
    dst_path: str
    recursive: bool
    overwrite: bool
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., src_path: _Optional[str] = ..., dst_path: _Optional[str] = ..., recursive: _Optional[bool] = ..., overwrite: _Optional[bool] = ...) -> None: ...

class CopyResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class MoveRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "src_path", "dst_path", "overwrite")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    SRC_PATH_FIELD_NUMBER: _ClassVar[int]
    DST_PATH_FIELD_NUMBER: _ClassVar[int]
    OVERWRITE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    src_path: str
    dst_path: str
    overwrite: bool
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., src_path: _Optional[str] = ..., dst_path: _Optional[str] = ..., overwrite: _Optional[bool] = ...) -> None: ...

class MoveResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ChmodRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path", "mode", "recursive")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    RECURSIVE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    mode: int
    recursive: bool
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ..., mode: _Optional[int] = ..., recursive: _Optional[bool] = ...) -> None: ...

class ChmodResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class TouchRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path", "create", "mtime_ns")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    CREATE_FIELD_NUMBER: _ClassVar[int]
    MTIME_NS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    create: bool
    mtime_ns: int
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ..., create: _Optional[bool] = ..., mtime_ns: _Optional[int] = ...) -> None: ...

class TouchResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class UploadArchiveOpen(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path", "format", "create_parents", "overwrite", "symlink_policy")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    CREATE_PARENTS_FIELD_NUMBER: _ClassVar[int]
    OVERWRITE_FIELD_NUMBER: _ClassVar[int]
    SYMLINK_POLICY_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    format: _file_pb2.SandboxArchiveFormat
    create_parents: bool
    overwrite: bool
    symlink_policy: _file_pb2.SandboxArchiveSymlinkPolicy
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ..., format: _Optional[_Union[_file_pb2.SandboxArchiveFormat, str]] = ..., create_parents: _Optional[bool] = ..., overwrite: _Optional[bool] = ..., symlink_policy: _Optional[_Union[_file_pb2.SandboxArchiveSymlinkPolicy, str]] = ...) -> None: ...

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
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "path", "format", "symlink_policy")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    SYMLINK_POLICY_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    path: str
    format: _file_pb2.SandboxArchiveFormat
    symlink_policy: _file_pb2.SandboxArchiveSymlinkPolicy
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., path: _Optional[str] = ..., format: _Optional[_Union[_file_pb2.SandboxArchiveFormat, str]] = ..., symlink_policy: _Optional[_Union[_file_pb2.SandboxArchiveSymlinkPolicy, str]] = ...) -> None: ...

class DownloadArchiveResponse(_message.Message):
    __slots__ = ("chunk",)
    CHUNK_FIELD_NUMBER: _ClassVar[int]
    chunk: bytes
    def __init__(self, chunk: _Optional[bytes] = ...) -> None: ...

class ComputerUseStatusRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

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
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "show_cursor", "region", "format", "quality", "scale")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    SHOW_CURSOR_FIELD_NUMBER: _ClassVar[int]
    REGION_FIELD_NUMBER: _ClassVar[int]
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    QUALITY_FIELD_NUMBER: _ClassVar[int]
    SCALE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    show_cursor: bool
    region: ComputerUseRegion
    format: str
    quality: int
    scale: float
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., show_cursor: _Optional[bool] = ..., region: _Optional[_Union[ComputerUseRegion, _Mapping]] = ..., format: _Optional[str] = ..., quality: _Optional[int] = ..., scale: _Optional[float] = ...) -> None: ...

class ComputerUseScreenshotResponse(_message.Message):
    __slots__ = ("data", "content_type")
    DATA_FIELD_NUMBER: _ClassVar[int]
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    content_type: str
    def __init__(self, data: _Optional[bytes] = ..., content_type: _Optional[str] = ...) -> None: ...

class ComputerUseDisplayRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

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
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "action", "x", "y", "to_x", "to_y", "button", "direction", "amount")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    ACTION_FIELD_NUMBER: _ClassVar[int]
    X_FIELD_NUMBER: _ClassVar[int]
    Y_FIELD_NUMBER: _ClassVar[int]
    TO_X_FIELD_NUMBER: _ClassVar[int]
    TO_Y_FIELD_NUMBER: _ClassVar[int]
    BUTTON_FIELD_NUMBER: _ClassVar[int]
    DIRECTION_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    action: str
    x: int
    y: int
    to_x: int
    to_y: int
    button: str
    direction: str
    amount: int
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., action: _Optional[str] = ..., x: _Optional[int] = ..., y: _Optional[int] = ..., to_x: _Optional[int] = ..., to_y: _Optional[int] = ..., button: _Optional[str] = ..., direction: _Optional[str] = ..., amount: _Optional[int] = ...) -> None: ...

class ComputerUseMouseResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ComputerUseKeyboardRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "text", "key", "keys", "delay_ms")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    KEYS_FIELD_NUMBER: _ClassVar[int]
    DELAY_MS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    text: str
    key: str
    keys: _containers.RepeatedScalarFieldContainer[str]
    delay_ms: int
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., text: _Optional[str] = ..., key: _Optional[str] = ..., keys: _Optional[_Iterable[str]] = ..., delay_ms: _Optional[int] = ...) -> None: ...

class ComputerUseKeyboardResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class BrowserStatusRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

class BrowserStatusResponse(_message.Message):
    __slots__ = ("available", "command", "running", "pid", "url", "reason")
    AVAILABLE_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    RUNNING_FIELD_NUMBER: _ClassVar[int]
    PID_FIELD_NUMBER: _ClassVar[int]
    URL_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    available: bool
    command: str
    running: bool
    pid: int
    url: str
    reason: str
    def __init__(self, available: _Optional[bool] = ..., command: _Optional[str] = ..., running: _Optional[bool] = ..., pid: _Optional[int] = ..., url: _Optional[str] = ..., reason: _Optional[str] = ...) -> None: ...

class BrowserOpenRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "url")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    URL_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    url: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., url: _Optional[str] = ...) -> None: ...

class BrowserCloseRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ...) -> None: ...

class BrowserNavigateRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "url")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    URL_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    url: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., url: _Optional[str] = ...) -> None: ...

class BrowserResizeRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "width", "height")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    width: int
    height: int
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., width: _Optional[int] = ..., height: _Optional[int] = ...) -> None: ...

class BrowserClickRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "x", "y", "button")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    X_FIELD_NUMBER: _ClassVar[int]
    Y_FIELD_NUMBER: _ClassVar[int]
    BUTTON_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    x: int
    y: int
    button: str
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., x: _Optional[int] = ..., y: _Optional[int] = ..., button: _Optional[str] = ...) -> None: ...

class BrowserTypeRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "text", "delay_ms")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    DELAY_MS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    text: str
    delay_ms: int
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., text: _Optional[str] = ..., delay_ms: _Optional[int] = ...) -> None: ...

class BrowserWaitRequest(_message.Message):
    __slots__ = ("allocation_id", "attempt", "execution_lease_token", "timeout_ms")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_MS_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    attempt: int
    execution_lease_token: str
    timeout_ms: int
    def __init__(self, allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., execution_lease_token: _Optional[str] = ..., timeout_ms: _Optional[int] = ...) -> None: ...
