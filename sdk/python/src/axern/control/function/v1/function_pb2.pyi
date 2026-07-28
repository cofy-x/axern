import datetime

from axern.control.function.v1 import function_types_pb2 as _function_types_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class UploadFunctionBundleOpen(_message.Message):
    __slots__ = ("namespace", "name", "digest", "media_type", "size_bytes")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    digest: str
    media_type: str
    size_bytes: int
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., digest: _Optional[str] = ..., media_type: _Optional[str] = ..., size_bytes: _Optional[int] = ...) -> None: ...

class UploadFunctionBundleRequest(_message.Message):
    __slots__ = ("open", "chunk")
    OPEN_FIELD_NUMBER: _ClassVar[int]
    CHUNK_FIELD_NUMBER: _ClassVar[int]
    open: UploadFunctionBundleOpen
    chunk: bytes
    def __init__(self, open: _Optional[_Union[UploadFunctionBundleOpen, _Mapping]] = ..., chunk: _Optional[bytes] = ...) -> None: ...

class UploadFunctionBundleResponse(_message.Message):
    __slots__ = ("bundle",)
    BUNDLE_FIELD_NUMBER: _ClassVar[int]
    bundle: _function_types_pb2.FunctionBundleSource
    def __init__(self, bundle: _Optional[_Union[_function_types_pb2.FunctionBundleSource, _Mapping]] = ...) -> None: ...

class DeployFunctionRequest(_message.Message):
    __slots__ = ("namespace", "name", "spec", "source", "labels", "wait_ready", "ready_timeout")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    WAIT_READY_FIELD_NUMBER: _ClassVar[int]
    READY_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    spec: _function_types_pb2.FunctionSpec
    source: _function_types_pb2.FunctionSource
    labels: _containers.ScalarMap[str, str]
    wait_ready: bool
    ready_timeout: _duration_pb2.Duration
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., spec: _Optional[_Union[_function_types_pb2.FunctionSpec, _Mapping]] = ..., source: _Optional[_Union[_function_types_pb2.FunctionSource, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., wait_ready: _Optional[bool] = ..., ready_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class DeployFunctionResponse(_message.Message):
    __slots__ = ("function", "revision", "deployment")
    FUNCTION_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    DEPLOYMENT_FIELD_NUMBER: _ClassVar[int]
    function: _function_types_pb2.Function
    revision: _function_types_pb2.FunctionRevision
    deployment: _function_types_pb2.FunctionDeployment
    def __init__(self, function: _Optional[_Union[_function_types_pb2.Function, _Mapping]] = ..., revision: _Optional[_Union[_function_types_pb2.FunctionRevision, _Mapping]] = ..., deployment: _Optional[_Union[_function_types_pb2.FunctionDeployment, _Mapping]] = ...) -> None: ...

class GetFunctionRequest(_message.Message):
    __slots__ = ("function_id", "namespace", "name")
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    function_id: str
    namespace: str
    name: str
    def __init__(self, function_id: _Optional[str] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class GetFunctionResponse(_message.Message):
    __slots__ = ("function", "active_revision", "deployment")
    FUNCTION_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_REVISION_FIELD_NUMBER: _ClassVar[int]
    DEPLOYMENT_FIELD_NUMBER: _ClassVar[int]
    function: _function_types_pb2.Function
    active_revision: _function_types_pb2.FunctionRevision
    deployment: _function_types_pb2.FunctionDeployment
    def __init__(self, function: _Optional[_Union[_function_types_pb2.Function, _Mapping]] = ..., active_revision: _Optional[_Union[_function_types_pb2.FunctionRevision, _Mapping]] = ..., deployment: _Optional[_Union[_function_types_pb2.FunctionDeployment, _Mapping]] = ...) -> None: ...

class ListFunctionsRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: _function_types_pb2.FunctionListFilter
    def __init__(self, filter: _Optional[_Union[_function_types_pb2.FunctionListFilter, _Mapping]] = ...) -> None: ...

class ListFunctionsResponse(_message.Message):
    __slots__ = ("functions", "next_cursor")
    FUNCTIONS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    functions: _containers.RepeatedCompositeFieldContainer[_function_types_pb2.Function]
    next_cursor: str
    def __init__(self, functions: _Optional[_Iterable[_Union[_function_types_pb2.Function, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class DeleteFunctionRequest(_message.Message):
    __slots__ = ("function_id", "namespace", "name")
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    function_id: str
    namespace: str
    name: str
    def __init__(self, function_id: _Optional[str] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class DeleteFunctionResponse(_message.Message):
    __slots__ = ("function",)
    FUNCTION_FIELD_NUMBER: _ClassVar[int]
    function: _function_types_pb2.Function
    def __init__(self, function: _Optional[_Union[_function_types_pb2.Function, _Mapping]] = ...) -> None: ...

class InvokeFunctionRequest(_message.Message):
    __slots__ = ("namespace", "name", "function_id", "revision_id", "mode", "payload", "timeout", "request_id", "labels")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    REVISION_ID_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    function_id: str
    revision_id: str
    mode: _function_types_pb2.FunctionInvocationMode
    payload: _function_types_pb2.FunctionPayload
    timeout: _duration_pb2.Duration
    request_id: str
    labels: _containers.ScalarMap[str, str]
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., function_id: _Optional[str] = ..., revision_id: _Optional[str] = ..., mode: _Optional[_Union[_function_types_pb2.FunctionInvocationMode, str]] = ..., payload: _Optional[_Union[_function_types_pb2.FunctionPayload, _Mapping]] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., request_id: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class InvokeFunctionResponse(_message.Message):
    __slots__ = ("invocation",)
    INVOCATION_FIELD_NUMBER: _ClassVar[int]
    invocation: _function_types_pb2.FunctionInvocation
    def __init__(self, invocation: _Optional[_Union[_function_types_pb2.FunctionInvocation, _Mapping]] = ...) -> None: ...

class GetFunctionInvocationRequest(_message.Message):
    __slots__ = ("invocation_id",)
    INVOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    invocation_id: str
    def __init__(self, invocation_id: _Optional[str] = ...) -> None: ...

class GetFunctionInvocationResponse(_message.Message):
    __slots__ = ("invocation",)
    INVOCATION_FIELD_NUMBER: _ClassVar[int]
    invocation: _function_types_pb2.FunctionInvocation
    def __init__(self, invocation: _Optional[_Union[_function_types_pb2.FunctionInvocation, _Mapping]] = ...) -> None: ...

class ListFunctionInvocationsRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: _function_types_pb2.FunctionInvocationListFilter
    def __init__(self, filter: _Optional[_Union[_function_types_pb2.FunctionInvocationListFilter, _Mapping]] = ...) -> None: ...

class ListFunctionInvocationsResponse(_message.Message):
    __slots__ = ("invocations", "next_cursor")
    INVOCATIONS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    invocations: _containers.RepeatedCompositeFieldContainer[_function_types_pb2.FunctionInvocation]
    next_cursor: str
    def __init__(self, invocations: _Optional[_Iterable[_Union[_function_types_pb2.FunctionInvocation, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class ListFunctionEventsRequest(_message.Message):
    __slots__ = ("function_id", "invocation_id", "revision_id", "limit")
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    REVISION_ID_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    function_id: str
    invocation_id: str
    revision_id: str
    limit: int
    def __init__(self, function_id: _Optional[str] = ..., invocation_id: _Optional[str] = ..., revision_id: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class ListFunctionEventsResponse(_message.Message):
    __slots__ = ("events",)
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[_function_types_pb2.FunctionEvent]
    def __init__(self, events: _Optional[_Iterable[_Union[_function_types_pb2.FunctionEvent, _Mapping]]] = ...) -> None: ...
