import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SecretType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SECRET_TYPE_UNSPECIFIED: _ClassVar[SecretType]
    SECRET_TYPE_OPAQUE: _ClassVar[SecretType]
    SECRET_TYPE_DOCKER_CONFIG_JSON: _ClassVar[SecretType]
SECRET_TYPE_UNSPECIFIED: SecretType
SECRET_TYPE_OPAQUE: SecretType
SECRET_TYPE_DOCKER_CONFIG_JSON: SecretType

class Secret(_message.Message):
    __slots__ = ("id", "namespace", "type", "data_keys", "labels", "version", "created_at", "updated_at")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    DATA_KEYS_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    type: SecretType
    data_keys: _containers.RepeatedScalarFieldContainer[str]
    labels: _containers.ScalarMap[str, str]
    version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., type: _Optional[_Union[SecretType, str]] = ..., data_keys: _Optional[_Iterable[str]] = ..., labels: _Optional[_Mapping[str, str]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class SecretListFilter(_message.Message):
    __slots__ = ("namespace", "type", "labels", "cursor", "page_size")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    type: SecretType
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., type: _Optional[_Union[SecretType, str]] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class CreateSecretRequest(_message.Message):
    __slots__ = ("namespace", "type", "string_data", "labels")
    class StringDataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    STRING_DATA_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    type: SecretType
    string_data: _containers.ScalarMap[str, str]
    labels: _containers.ScalarMap[str, str]
    def __init__(self, namespace: _Optional[str] = ..., type: _Optional[_Union[SecretType, str]] = ..., string_data: _Optional[_Mapping[str, str]] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateSecretResponse(_message.Message):
    __slots__ = ("secret",)
    SECRET_FIELD_NUMBER: _ClassVar[int]
    secret: Secret
    def __init__(self, secret: _Optional[_Union[Secret, _Mapping]] = ...) -> None: ...

class GetSecretRequest(_message.Message):
    __slots__ = ("secret_id",)
    SECRET_ID_FIELD_NUMBER: _ClassVar[int]
    secret_id: str
    def __init__(self, secret_id: _Optional[str] = ...) -> None: ...

class GetSecretResponse(_message.Message):
    __slots__ = ("secret",)
    SECRET_FIELD_NUMBER: _ClassVar[int]
    secret: Secret
    def __init__(self, secret: _Optional[_Union[Secret, _Mapping]] = ...) -> None: ...

class ListSecretsRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: SecretListFilter
    def __init__(self, filter: _Optional[_Union[SecretListFilter, _Mapping]] = ...) -> None: ...

class ListSecretsResponse(_message.Message):
    __slots__ = ("secrets", "next_cursor")
    SECRETS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    secrets: _containers.RepeatedCompositeFieldContainer[Secret]
    next_cursor: str
    def __init__(self, secrets: _Optional[_Iterable[_Union[Secret, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class DeleteSecretRequest(_message.Message):
    __slots__ = ("secret_id",)
    SECRET_ID_FIELD_NUMBER: _ClassVar[int]
    secret_id: str
    def __init__(self, secret_id: _Optional[str] = ...) -> None: ...

class DeleteSecretResponse(_message.Message):
    __slots__ = ("secret",)
    SECRET_FIELD_NUMBER: _ClassVar[int]
    secret: Secret
    def __init__(self, secret: _Optional[_Union[Secret, _Mapping]] = ...) -> None: ...
