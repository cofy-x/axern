import datetime

from axern.control.common.v1 import common_pb2 as _common_pb2
from axern.control.environment.v1 import environment_pb2 as _environment_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class FunctionStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    FUNCTION_STATUS_UNSPECIFIED: _ClassVar[FunctionStatus]
    FUNCTION_STATUS_DEPLOYING: _ClassVar[FunctionStatus]
    FUNCTION_STATUS_READY: _ClassVar[FunctionStatus]
    FUNCTION_STATUS_DEGRADED: _ClassVar[FunctionStatus]
    FUNCTION_STATUS_FAILED: _ClassVar[FunctionStatus]
    FUNCTION_STATUS_DELETING: _ClassVar[FunctionStatus]
    FUNCTION_STATUS_DELETED: _ClassVar[FunctionStatus]

class FunctionDeploymentStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    FUNCTION_DEPLOYMENT_STATUS_UNSPECIFIED: _ClassVar[FunctionDeploymentStatus]
    FUNCTION_DEPLOYMENT_STATUS_PENDING: _ClassVar[FunctionDeploymentStatus]
    FUNCTION_DEPLOYMENT_STATUS_WARMING: _ClassVar[FunctionDeploymentStatus]
    FUNCTION_DEPLOYMENT_STATUS_READY: _ClassVar[FunctionDeploymentStatus]
    FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO: _ClassVar[FunctionDeploymentStatus]
    FUNCTION_DEPLOYMENT_STATUS_DEGRADED: _ClassVar[FunctionDeploymentStatus]
    FUNCTION_DEPLOYMENT_STATUS_FAILED: _ClassVar[FunctionDeploymentStatus]

class FunctionInvocationStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    FUNCTION_INVOCATION_STATUS_UNSPECIFIED: _ClassVar[FunctionInvocationStatus]
    FUNCTION_INVOCATION_STATUS_ACCEPTED: _ClassVar[FunctionInvocationStatus]
    FUNCTION_INVOCATION_STATUS_QUEUED: _ClassVar[FunctionInvocationStatus]
    FUNCTION_INVOCATION_STATUS_RUNNING: _ClassVar[FunctionInvocationStatus]
    FUNCTION_INVOCATION_STATUS_SUCCEEDED: _ClassVar[FunctionInvocationStatus]
    FUNCTION_INVOCATION_STATUS_FAILED: _ClassVar[FunctionInvocationStatus]
    FUNCTION_INVOCATION_STATUS_CANCELLED: _ClassVar[FunctionInvocationStatus]
    FUNCTION_INVOCATION_STATUS_TIMED_OUT: _ClassVar[FunctionInvocationStatus]

class FunctionInvocationMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    FUNCTION_INVOCATION_MODE_UNSPECIFIED: _ClassVar[FunctionInvocationMode]
    FUNCTION_INVOCATION_MODE_SYNC: _ClassVar[FunctionInvocationMode]
    FUNCTION_INVOCATION_MODE_ASYNC: _ClassVar[FunctionInvocationMode]

class FunctionEventType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    FUNCTION_EVENT_TYPE_UNSPECIFIED: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_DEPLOY_STARTED: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_REVISION_CREATED: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_REVISION_ACTIVATED: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_WARM_POOL_READY: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_INVOCATION_STARTED: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_INVOCATION_SUCCEEDED: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_INVOCATION_FAILED: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_SCALING_DECISION: _ClassVar[FunctionEventType]
    FUNCTION_EVENT_TYPE_CLEANUP: _ClassVar[FunctionEventType]
FUNCTION_STATUS_UNSPECIFIED: FunctionStatus
FUNCTION_STATUS_DEPLOYING: FunctionStatus
FUNCTION_STATUS_READY: FunctionStatus
FUNCTION_STATUS_DEGRADED: FunctionStatus
FUNCTION_STATUS_FAILED: FunctionStatus
FUNCTION_STATUS_DELETING: FunctionStatus
FUNCTION_STATUS_DELETED: FunctionStatus
FUNCTION_DEPLOYMENT_STATUS_UNSPECIFIED: FunctionDeploymentStatus
FUNCTION_DEPLOYMENT_STATUS_PENDING: FunctionDeploymentStatus
FUNCTION_DEPLOYMENT_STATUS_WARMING: FunctionDeploymentStatus
FUNCTION_DEPLOYMENT_STATUS_READY: FunctionDeploymentStatus
FUNCTION_DEPLOYMENT_STATUS_SCALED_TO_ZERO: FunctionDeploymentStatus
FUNCTION_DEPLOYMENT_STATUS_DEGRADED: FunctionDeploymentStatus
FUNCTION_DEPLOYMENT_STATUS_FAILED: FunctionDeploymentStatus
FUNCTION_INVOCATION_STATUS_UNSPECIFIED: FunctionInvocationStatus
FUNCTION_INVOCATION_STATUS_ACCEPTED: FunctionInvocationStatus
FUNCTION_INVOCATION_STATUS_QUEUED: FunctionInvocationStatus
FUNCTION_INVOCATION_STATUS_RUNNING: FunctionInvocationStatus
FUNCTION_INVOCATION_STATUS_SUCCEEDED: FunctionInvocationStatus
FUNCTION_INVOCATION_STATUS_FAILED: FunctionInvocationStatus
FUNCTION_INVOCATION_STATUS_CANCELLED: FunctionInvocationStatus
FUNCTION_INVOCATION_STATUS_TIMED_OUT: FunctionInvocationStatus
FUNCTION_INVOCATION_MODE_UNSPECIFIED: FunctionInvocationMode
FUNCTION_INVOCATION_MODE_SYNC: FunctionInvocationMode
FUNCTION_INVOCATION_MODE_ASYNC: FunctionInvocationMode
FUNCTION_EVENT_TYPE_UNSPECIFIED: FunctionEventType
FUNCTION_EVENT_TYPE_DEPLOY_STARTED: FunctionEventType
FUNCTION_EVENT_TYPE_REVISION_CREATED: FunctionEventType
FUNCTION_EVENT_TYPE_REVISION_ACTIVATED: FunctionEventType
FUNCTION_EVENT_TYPE_WARM_POOL_READY: FunctionEventType
FUNCTION_EVENT_TYPE_INVOCATION_STARTED: FunctionEventType
FUNCTION_EVENT_TYPE_INVOCATION_SUCCEEDED: FunctionEventType
FUNCTION_EVENT_TYPE_INVOCATION_FAILED: FunctionEventType
FUNCTION_EVENT_TYPE_SCALING_DECISION: FunctionEventType
FUNCTION_EVENT_TYPE_CLEANUP: FunctionEventType

class Function(_message.Message):
    __slots__ = ("id", "namespace", "name", "active_revision_id", "spec", "status", "deployment_status", "labels", "version", "created_at", "updated_at", "message", "diagnostic_code")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_REVISION_ID_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    DEPLOYMENT_STATUS_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_CODE_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    name: str
    active_revision_id: str
    spec: FunctionSpec
    status: FunctionStatus
    deployment_status: FunctionDeploymentStatus
    labels: _containers.ScalarMap[str, str]
    version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    message: str
    diagnostic_code: _common_pb2.WorkloadDiagnosticCode
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ..., active_revision_id: _Optional[str] = ..., spec: _Optional[_Union[FunctionSpec, _Mapping]] = ..., status: _Optional[_Union[FunctionStatus, str]] = ..., deployment_status: _Optional[_Union[FunctionDeploymentStatus, str]] = ..., labels: _Optional[_Mapping[str, str]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., message: _Optional[str] = ..., diagnostic_code: _Optional[_Union[_common_pb2.WorkloadDiagnosticCode, str]] = ...) -> None: ...

class FunctionRevision(_message.Message):
    __slots__ = ("id", "function_id", "namespace", "name", "revision_number", "spec", "source", "source_digest", "manifest_digest", "labels", "created_at", "created_by")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    REVISION_NUMBER_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    SOURCE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    MANIFEST_DIGEST_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_FIELD_NUMBER: _ClassVar[int]
    id: str
    function_id: str
    namespace: str
    name: str
    revision_number: int
    spec: FunctionSpec
    source: FunctionSource
    source_digest: str
    manifest_digest: str
    labels: _containers.ScalarMap[str, str]
    created_at: _timestamp_pb2.Timestamp
    created_by: str
    def __init__(self, id: _Optional[str] = ..., function_id: _Optional[str] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ..., revision_number: _Optional[int] = ..., spec: _Optional[_Union[FunctionSpec, _Mapping]] = ..., source: _Optional[_Union[FunctionSource, _Mapping]] = ..., source_digest: _Optional[str] = ..., manifest_digest: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., created_by: _Optional[str] = ...) -> None: ...

class FunctionDeployment(_message.Message):
    __slots__ = ("function_id", "active_revision_id", "status", "scaling", "desired_replicas", "ready_replicas", "active_invocations", "updated_at", "message", "diagnostic_code", "worker_service_id")
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_REVISION_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    SCALING_FIELD_NUMBER: _ClassVar[int]
    DESIRED_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    READY_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_INVOCATIONS_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_CODE_FIELD_NUMBER: _ClassVar[int]
    WORKER_SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    function_id: str
    active_revision_id: str
    status: FunctionDeploymentStatus
    scaling: FunctionScalingSpec
    desired_replicas: int
    ready_replicas: int
    active_invocations: int
    updated_at: _timestamp_pb2.Timestamp
    message: str
    diagnostic_code: _common_pb2.WorkloadDiagnosticCode
    worker_service_id: str
    def __init__(self, function_id: _Optional[str] = ..., active_revision_id: _Optional[str] = ..., status: _Optional[_Union[FunctionDeploymentStatus, str]] = ..., scaling: _Optional[_Union[FunctionScalingSpec, _Mapping]] = ..., desired_replicas: _Optional[int] = ..., ready_replicas: _Optional[int] = ..., active_invocations: _Optional[int] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., message: _Optional[str] = ..., diagnostic_code: _Optional[_Union[_common_pb2.WorkloadDiagnosticCode, str]] = ..., worker_service_id: _Optional[str] = ...) -> None: ...

class FunctionInvocation(_message.Message):
    __slots__ = ("id", "function_id", "function_name", "namespace", "revision_id", "status", "mode", "payload", "result", "error", "timeout", "duration", "request_id", "labels", "created_at", "started_at", "completed_at", "message", "diagnostic_code")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    FUNCTION_NAME_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    REVISION_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    RESULT_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    DURATION_FIELD_NUMBER: _ClassVar[int]
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_CODE_FIELD_NUMBER: _ClassVar[int]
    id: str
    function_id: str
    function_name: str
    namespace: str
    revision_id: str
    status: FunctionInvocationStatus
    mode: FunctionInvocationMode
    payload: FunctionPayload
    result: FunctionResult
    error: FunctionError
    timeout: _duration_pb2.Duration
    duration: _duration_pb2.Duration
    request_id: str
    labels: _containers.ScalarMap[str, str]
    created_at: _timestamp_pb2.Timestamp
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    message: str
    diagnostic_code: _common_pb2.WorkloadDiagnosticCode
    def __init__(self, id: _Optional[str] = ..., function_id: _Optional[str] = ..., function_name: _Optional[str] = ..., namespace: _Optional[str] = ..., revision_id: _Optional[str] = ..., status: _Optional[_Union[FunctionInvocationStatus, str]] = ..., mode: _Optional[_Union[FunctionInvocationMode, str]] = ..., payload: _Optional[_Union[FunctionPayload, _Mapping]] = ..., result: _Optional[_Union[FunctionResult, _Mapping]] = ..., error: _Optional[_Union[FunctionError, _Mapping]] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., duration: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., request_id: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., message: _Optional[str] = ..., diagnostic_code: _Optional[_Union[_common_pb2.WorkloadDiagnosticCode, str]] = ...) -> None: ...

class FunctionEvent(_message.Message):
    __slots__ = ("id", "function_id", "invocation_id", "revision_id", "type", "message", "details", "created_at")
    class DetailsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    INVOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    REVISION_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    DETAILS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    function_id: str
    invocation_id: str
    revision_id: str
    type: FunctionEventType
    message: str
    details: _containers.ScalarMap[str, str]
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., function_id: _Optional[str] = ..., invocation_id: _Optional[str] = ..., revision_id: _Optional[str] = ..., type: _Optional[_Union[FunctionEventType, str]] = ..., message: _Optional[str] = ..., details: _Optional[_Mapping[str, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class FunctionSpec(_message.Message):
    __slots__ = ("runtime", "handler", "initializer", "source", "timeout", "config", "scaling", "retention", "worker_source")
    RUNTIME_FIELD_NUMBER: _ClassVar[int]
    HANDLER_FIELD_NUMBER: _ClassVar[int]
    INITIALIZER_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    SCALING_FIELD_NUMBER: _ClassVar[int]
    RETENTION_FIELD_NUMBER: _ClassVar[int]
    WORKER_SOURCE_FIELD_NUMBER: _ClassVar[int]
    runtime: str
    handler: str
    initializer: str
    source: FunctionSourceSpec
    timeout: _duration_pb2.Duration
    config: _common_pb2.ExecutionConfig
    scaling: FunctionScalingSpec
    retention: FunctionRetentionPolicy
    worker_source: FunctionWorkerSource
    def __init__(self, runtime: _Optional[str] = ..., handler: _Optional[str] = ..., initializer: _Optional[str] = ..., source: _Optional[_Union[FunctionSourceSpec, _Mapping]] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., config: _Optional[_Union[_common_pb2.ExecutionConfig, _Mapping]] = ..., scaling: _Optional[_Union[FunctionScalingSpec, _Mapping]] = ..., retention: _Optional[_Union[FunctionRetentionPolicy, _Mapping]] = ..., worker_source: _Optional[_Union[FunctionWorkerSource, _Mapping]] = ...) -> None: ...

class FunctionWorkerSource(_message.Message):
    __slots__ = ("environment_id", "environment")
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_FIELD_NUMBER: _ClassVar[int]
    environment_id: str
    environment: _environment_pb2.EnvironmentSpec
    def __init__(self, environment_id: _Optional[str] = ..., environment: _Optional[_Union[_environment_pb2.EnvironmentSpec, _Mapping]] = ...) -> None: ...

class FunctionSourceSpec(_message.Message):
    __slots__ = ("bundle", "image")
    BUNDLE_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    bundle: FunctionBundleSource
    image: FunctionImageSource
    def __init__(self, bundle: _Optional[_Union[FunctionBundleSource, _Mapping]] = ..., image: _Optional[_Union[FunctionImageSource, _Mapping]] = ...) -> None: ...

class FunctionSource(_message.Message):
    __slots__ = ("bundle", "image")
    BUNDLE_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    bundle: FunctionBundleSource
    image: FunctionImageSource
    def __init__(self, bundle: _Optional[_Union[FunctionBundleSource, _Mapping]] = ..., image: _Optional[_Union[FunctionImageSource, _Mapping]] = ...) -> None: ...

class FunctionBundleSource(_message.Message):
    __slots__ = ("digest", "media_type", "size_bytes", "storage_uri")
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    STORAGE_URI_FIELD_NUMBER: _ClassVar[int]
    digest: str
    media_type: str
    size_bytes: int
    storage_uri: str
    def __init__(self, digest: _Optional[str] = ..., media_type: _Optional[str] = ..., size_bytes: _Optional[int] = ..., storage_uri: _Optional[str] = ...) -> None: ...

class FunctionImageSource(_message.Message):
    __slots__ = ("ref", "digest", "registry_credential_id")
    REF_FIELD_NUMBER: _ClassVar[int]
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    REGISTRY_CREDENTIAL_ID_FIELD_NUMBER: _ClassVar[int]
    ref: str
    digest: str
    registry_credential_id: str
    def __init__(self, ref: _Optional[str] = ..., digest: _Optional[str] = ..., registry_credential_id: _Optional[str] = ...) -> None: ...

class FunctionScalingSpec(_message.Message):
    __slots__ = ("min_replicas", "max_replicas", "concurrency", "idle_timeout")
    MIN_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    MAX_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    CONCURRENCY_FIELD_NUMBER: _ClassVar[int]
    IDLE_TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    min_replicas: int
    max_replicas: int
    concurrency: int
    idle_timeout: _duration_pb2.Duration
    def __init__(self, min_replicas: _Optional[int] = ..., max_replicas: _Optional[int] = ..., concurrency: _Optional[int] = ..., idle_timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ...) -> None: ...

class FunctionRetentionPolicy(_message.Message):
    __slots__ = ("invocation_retention", "max_invocations")
    INVOCATION_RETENTION_FIELD_NUMBER: _ClassVar[int]
    MAX_INVOCATIONS_FIELD_NUMBER: _ClassVar[int]
    invocation_retention: _duration_pb2.Duration
    max_invocations: int
    def __init__(self, invocation_retention: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., max_invocations: _Optional[int] = ...) -> None: ...

class FunctionPayload(_message.Message):
    __slots__ = ("content_type", "data")
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    content_type: str
    data: bytes
    def __init__(self, content_type: _Optional[str] = ..., data: _Optional[bytes] = ...) -> None: ...

class FunctionResult(_message.Message):
    __slots__ = ("content_type", "data")
    CONTENT_TYPE_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    content_type: str
    data: bytes
    def __init__(self, content_type: _Optional[str] = ..., data: _Optional[bytes] = ...) -> None: ...

class FunctionError(_message.Message):
    __slots__ = ("code", "message", "type", "stack_trace", "details")
    class DetailsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    STACK_TRACE_FIELD_NUMBER: _ClassVar[int]
    DETAILS_FIELD_NUMBER: _ClassVar[int]
    code: str
    message: str
    type: str
    stack_trace: str
    details: _containers.ScalarMap[str, str]
    def __init__(self, code: _Optional[str] = ..., message: _Optional[str] = ..., type: _Optional[str] = ..., stack_trace: _Optional[str] = ..., details: _Optional[_Mapping[str, str]] = ...) -> None: ...

class FunctionListFilter(_message.Message):
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
    statuses: _containers.RepeatedScalarFieldContainer[FunctionStatus]
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., statuses: _Optional[_Iterable[_Union[FunctionStatus, str]]] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class FunctionInvocationListFilter(_message.Message):
    __slots__ = ("namespace", "function_id", "function_name", "revision_id", "statuses", "labels", "cursor", "page_size")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    FUNCTION_ID_FIELD_NUMBER: _ClassVar[int]
    FUNCTION_NAME_FIELD_NUMBER: _ClassVar[int]
    REVISION_ID_FIELD_NUMBER: _ClassVar[int]
    STATUSES_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    function_id: str
    function_name: str
    revision_id: str
    statuses: _containers.RepeatedScalarFieldContainer[FunctionInvocationStatus]
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., function_id: _Optional[str] = ..., function_name: _Optional[str] = ..., revision_id: _Optional[str] = ..., statuses: _Optional[_Iterable[_Union[FunctionInvocationStatus, str]]] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...
