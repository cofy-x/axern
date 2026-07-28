import datetime

from axern.control.common.v1 import common_pb2 as _common_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RunStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RUN_STATUS_UNSPECIFIED: _ClassVar[RunStatus]
    RUN_STATUS_QUEUED: _ClassVar[RunStatus]
    RUN_STATUS_PLACED: _ClassVar[RunStatus]
    RUN_STATUS_STARTING: _ClassVar[RunStatus]
    RUN_STATUS_RUNNING: _ClassVar[RunStatus]
    RUN_STATUS_SUCCEEDED: _ClassVar[RunStatus]
    RUN_STATUS_FAILED: _ClassVar[RunStatus]
    RUN_STATUS_CANCELLED: _ClassVar[RunStatus]
RUN_STATUS_UNSPECIFIED: RunStatus
RUN_STATUS_QUEUED: RunStatus
RUN_STATUS_PLACED: RunStatus
RUN_STATUS_STARTING: RunStatus
RUN_STATUS_RUNNING: RunStatus
RUN_STATUS_SUCCEEDED: RunStatus
RUN_STATUS_FAILED: RunStatus
RUN_STATUS_CANCELLED: RunStatus

class Run(_message.Message):
    __slots__ = ("id", "namespace", "environment_id", "allocation_id", "attempt", "status", "config", "labels", "version", "created_at", "updated_at", "exit_code", "exit_code_known", "message", "diagnostic_code")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_KNOWN_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_CODE_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    environment_id: str
    allocation_id: str
    attempt: int
    status: RunStatus
    config: _common_pb2.ExecutionConfig
    labels: _containers.ScalarMap[str, str]
    version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    exit_code: int
    exit_code_known: bool
    message: str
    diagnostic_code: _common_pb2.WorkloadDiagnosticCode
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., environment_id: _Optional[str] = ..., allocation_id: _Optional[str] = ..., attempt: _Optional[int] = ..., status: _Optional[_Union[RunStatus, str]] = ..., config: _Optional[_Union[_common_pb2.ExecutionConfig, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., exit_code: _Optional[int] = ..., exit_code_known: _Optional[bool] = ..., message: _Optional[str] = ..., diagnostic_code: _Optional[_Union[_common_pb2.WorkloadDiagnosticCode, str]] = ...) -> None: ...

class RunListFilter(_message.Message):
    __slots__ = ("namespace", "statuses", "labels", "cursor", "page_size")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    STATUSES_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    statuses: _containers.RepeatedScalarFieldContainer[RunStatus]
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., statuses: _Optional[_Iterable[_Union[RunStatus, str]]] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class CreateRunRequest(_message.Message):
    __slots__ = ("namespace", "environment_id", "config", "labels")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    environment_id: str
    config: _common_pb2.ExecutionConfig
    labels: _containers.ScalarMap[str, str]
    def __init__(self, namespace: _Optional[str] = ..., environment_id: _Optional[str] = ..., config: _Optional[_Union[_common_pb2.ExecutionConfig, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateRunResponse(_message.Message):
    __slots__ = ("run",)
    RUN_FIELD_NUMBER: _ClassVar[int]
    run: Run
    def __init__(self, run: _Optional[_Union[Run, _Mapping]] = ...) -> None: ...

class GetRunRequest(_message.Message):
    __slots__ = ("run_id",)
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    def __init__(self, run_id: _Optional[str] = ...) -> None: ...

class GetRunResponse(_message.Message):
    __slots__ = ("run",)
    RUN_FIELD_NUMBER: _ClassVar[int]
    run: Run
    def __init__(self, run: _Optional[_Union[Run, _Mapping]] = ...) -> None: ...

class ListRunsRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: RunListFilter
    def __init__(self, filter: _Optional[_Union[RunListFilter, _Mapping]] = ...) -> None: ...

class ListRunsResponse(_message.Message):
    __slots__ = ("runs", "next_cursor")
    RUNS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    runs: _containers.RepeatedCompositeFieldContainer[Run]
    next_cursor: str
    def __init__(self, runs: _Optional[_Iterable[_Union[Run, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class CancelRunRequest(_message.Message):
    __slots__ = ("run_id",)
    RUN_ID_FIELD_NUMBER: _ClassVar[int]
    run_id: str
    def __init__(self, run_id: _Optional[str] = ...) -> None: ...

class CancelRunResponse(_message.Message):
    __slots__ = ("run",)
    RUN_FIELD_NUMBER: _ClassVar[int]
    run: Run
    def __init__(self, run: _Optional[_Union[Run, _Mapping]] = ...) -> None: ...
