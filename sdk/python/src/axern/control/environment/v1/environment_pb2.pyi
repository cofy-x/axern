import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EnvironmentMount(_message.Message):
    __slots__ = ("type", "source", "target", "options")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    type: str
    source: str
    target: str
    options: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, type: _Optional[str] = ..., source: _Optional[str] = ..., target: _Optional[str] = ..., options: _Optional[_Iterable[str]] = ...) -> None: ...

class OciImageDescriptor(_message.Message):
    __slots__ = ("digest", "media_type", "size_bytes", "annotations")
    class AnnotationsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    ANNOTATIONS_FIELD_NUMBER: _ClassVar[int]
    digest: str
    media_type: str
    size_bytes: int
    annotations: _containers.ScalarMap[str, str]
    def __init__(self, digest: _Optional[str] = ..., media_type: _Optional[str] = ..., size_bytes: _Optional[int] = ..., annotations: _Optional[_Mapping[str, str]] = ...) -> None: ...

class OciBaselinePolicy(_message.Message):
    __slots__ = ("capabilities", "no_file_limit")
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    NO_FILE_LIMIT_FIELD_NUMBER: _ClassVar[int]
    capabilities: _containers.RepeatedScalarFieldContainer[str]
    no_file_limit: int
    def __init__(self, capabilities: _Optional[_Iterable[str]] = ..., no_file_limit: _Optional[int] = ...) -> None: ...

class OciExecutionProfile(_message.Message):
    __slots__ = ("baseline",)
    BASELINE_FIELD_NUMBER: _ClassVar[int]
    baseline: OciBaselinePolicy
    def __init__(self, baseline: _Optional[_Union[OciBaselinePolicy, _Mapping]] = ...) -> None: ...

class ResolvedEnvironmentSpec(_message.Message):
    __slots__ = ("rootfs_readonly", "image_default_argv", "default_cwd", "default_env", "mounts", "image_descriptor", "execution_profile")
    class DefaultEnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ROOTFS_READONLY_FIELD_NUMBER: _ClassVar[int]
    IMAGE_DEFAULT_ARGV_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_CWD_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_ENV_FIELD_NUMBER: _ClassVar[int]
    MOUNTS_FIELD_NUMBER: _ClassVar[int]
    IMAGE_DESCRIPTOR_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_PROFILE_FIELD_NUMBER: _ClassVar[int]
    rootfs_readonly: bool
    image_default_argv: _containers.RepeatedScalarFieldContainer[str]
    default_cwd: str
    default_env: _containers.ScalarMap[str, str]
    mounts: _containers.RepeatedCompositeFieldContainer[EnvironmentMount]
    image_descriptor: OciImageDescriptor
    execution_profile: OciExecutionProfile
    def __init__(self, rootfs_readonly: _Optional[bool] = ..., image_default_argv: _Optional[_Iterable[str]] = ..., default_cwd: _Optional[str] = ..., default_env: _Optional[_Mapping[str, str]] = ..., mounts: _Optional[_Iterable[_Union[EnvironmentMount, _Mapping]]] = ..., image_descriptor: _Optional[_Union[OciImageDescriptor, _Mapping]] = ..., execution_profile: _Optional[_Union[OciExecutionProfile, _Mapping]] = ...) -> None: ...

class EnvironmentImageSource(_message.Message):
    __slots__ = ("ref", "rootfs_readonly", "registry_credential_id")
    REF_FIELD_NUMBER: _ClassVar[int]
    ROOTFS_READONLY_FIELD_NUMBER: _ClassVar[int]
    REGISTRY_CREDENTIAL_ID_FIELD_NUMBER: _ClassVar[int]
    ref: str
    rootfs_readonly: bool
    registry_credential_id: str
    def __init__(self, ref: _Optional[str] = ..., rootfs_readonly: _Optional[bool] = ..., registry_credential_id: _Optional[str] = ...) -> None: ...

class EnvironmentSpec(_message.Message):
    __slots__ = ("namespace", "image")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    image: EnvironmentImageSource
    def __init__(self, namespace: _Optional[str] = ..., image: _Optional[_Union[EnvironmentImageSource, _Mapping]] = ...) -> None: ...

class Environment(_message.Message):
    __slots__ = ("id", "namespace", "spec", "resolved_spec", "labels", "created_at")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    RESOLVED_SPEC_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    spec: EnvironmentSpec
    resolved_spec: ResolvedEnvironmentSpec
    labels: _containers.ScalarMap[str, str]
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., spec: _Optional[_Union[EnvironmentSpec, _Mapping]] = ..., resolved_spec: _Optional[_Union[ResolvedEnvironmentSpec, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ListFilter(_message.Message):
    __slots__ = ("namespace", "labels", "cursor", "page_size")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class CreateEnvironmentRequest(_message.Message):
    __slots__ = ("spec", "labels")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SPEC_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    spec: EnvironmentSpec
    labels: _containers.ScalarMap[str, str]
    def __init__(self, spec: _Optional[_Union[EnvironmentSpec, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateEnvironmentResponse(_message.Message):
    __slots__ = ("environment",)
    ENVIRONMENT_FIELD_NUMBER: _ClassVar[int]
    environment: Environment
    def __init__(self, environment: _Optional[_Union[Environment, _Mapping]] = ...) -> None: ...

class GetEnvironmentRequest(_message.Message):
    __slots__ = ("environment_id",)
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    environment_id: str
    def __init__(self, environment_id: _Optional[str] = ...) -> None: ...

class GetEnvironmentResponse(_message.Message):
    __slots__ = ("environment",)
    ENVIRONMENT_FIELD_NUMBER: _ClassVar[int]
    environment: Environment
    def __init__(self, environment: _Optional[_Union[Environment, _Mapping]] = ...) -> None: ...

class ListEnvironmentsRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: ListFilter
    def __init__(self, filter: _Optional[_Union[ListFilter, _Mapping]] = ...) -> None: ...

class ListEnvironmentsResponse(_message.Message):
    __slots__ = ("environments", "next_cursor")
    ENVIRONMENTS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    environments: _containers.RepeatedCompositeFieldContainer[Environment]
    next_cursor: str
    def __init__(self, environments: _Optional[_Iterable[_Union[Environment, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class DeleteEnvironmentRequest(_message.Message):
    __slots__ = ("environment_id",)
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    environment_id: str
    def __init__(self, environment_id: _Optional[str] = ...) -> None: ...

class DeleteEnvironmentResponse(_message.Message):
    __slots__ = ("environment",)
    ENVIRONMENT_FIELD_NUMBER: _ClassVar[int]
    environment: Environment
    def __init__(self, environment: _Optional[_Union[Environment, _Mapping]] = ...) -> None: ...
