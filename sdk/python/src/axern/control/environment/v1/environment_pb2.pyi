import datetime

from axern.control.catalog.v1 import catalog_pb2 as _catalog_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EnvironmentStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ENVIRONMENT_STATUS_UNSPECIFIED: _ClassVar[EnvironmentStatus]
    ENVIRONMENT_STATUS_PENDING: _ClassVar[EnvironmentStatus]
    ENVIRONMENT_STATUS_READY: _ClassVar[EnvironmentStatus]
    ENVIRONMENT_STATUS_FAILED: _ClassVar[EnvironmentStatus]
    ENVIRONMENT_STATUS_DELETING: _ClassVar[EnvironmentStatus]
    ENVIRONMENT_STATUS_DELETED: _ClassVar[EnvironmentStatus]
ENVIRONMENT_STATUS_UNSPECIFIED: EnvironmentStatus
ENVIRONMENT_STATUS_PENDING: EnvironmentStatus
ENVIRONMENT_STATUS_READY: EnvironmentStatus
ENVIRONMENT_STATUS_FAILED: EnvironmentStatus
ENVIRONMENT_STATUS_DELETING: EnvironmentStatus
ENVIRONMENT_STATUS_DELETED: EnvironmentStatus

class EnvironmentImageSource(_message.Message):
    __slots__ = ("ref", "digest", "rootfs_readonly", "registry_credential_id")
    REF_FIELD_NUMBER: _ClassVar[int]
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    ROOTFS_READONLY_FIELD_NUMBER: _ClassVar[int]
    REGISTRY_CREDENTIAL_ID_FIELD_NUMBER: _ClassVar[int]
    ref: str
    digest: str
    rootfs_readonly: bool
    registry_credential_id: str
    def __init__(self, ref: _Optional[str] = ..., digest: _Optional[str] = ..., rootfs_readonly: _Optional[bool] = ..., registry_credential_id: _Optional[str] = ...) -> None: ...

class EnvironmentSpec(_message.Message):
    __slots__ = ("namespace", "template_id", "template_version", "image")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    TEMPLATE_ID_FIELD_NUMBER: _ClassVar[int]
    TEMPLATE_VERSION_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    template_id: str
    template_version: str
    image: EnvironmentImageSource
    def __init__(self, namespace: _Optional[str] = ..., template_id: _Optional[str] = ..., template_version: _Optional[str] = ..., image: _Optional[_Union[EnvironmentImageSource, _Mapping]] = ...) -> None: ...

class Environment(_message.Message):
    __slots__ = ("id", "namespace", "status", "spec", "spec_hash", "resolved_template", "labels", "version", "created_at", "updated_at", "message")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    SPEC_HASH_FIELD_NUMBER: _ClassVar[int]
    RESOLVED_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    status: EnvironmentStatus
    spec: EnvironmentSpec
    spec_hash: str
    resolved_template: _catalog_pb2.RuntimeTemplate
    labels: _containers.ScalarMap[str, str]
    version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    message: str
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., status: _Optional[_Union[EnvironmentStatus, str]] = ..., spec: _Optional[_Union[EnvironmentSpec, _Mapping]] = ..., spec_hash: _Optional[str] = ..., resolved_template: _Optional[_Union[_catalog_pb2.RuntimeTemplate, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., message: _Optional[str] = ...) -> None: ...

class ListFilter(_message.Message):
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
    statuses: _containers.RepeatedScalarFieldContainer[EnvironmentStatus]
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., statuses: _Optional[_Iterable[_Union[EnvironmentStatus, str]]] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

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
