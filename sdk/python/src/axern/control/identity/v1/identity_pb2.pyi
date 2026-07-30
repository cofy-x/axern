import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PrincipalIdentity(_message.Message):
    __slots__ = ("principal_id", "name", "display_name", "kind")
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    principal_id: str
    name: str
    display_name: str
    kind: str
    def __init__(self, principal_id: _Optional[str] = ..., name: _Optional[str] = ..., display_name: _Optional[str] = ..., kind: _Optional[str] = ...) -> None: ...

class CredentialIdentity(_message.Message):
    __slots__ = ("credential_id", "label", "fingerprint", "certificate_not_after")
    CREDENTIAL_ID_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    CERTIFICATE_NOT_AFTER_FIELD_NUMBER: _ClassVar[int]
    credential_id: str
    label: str
    fingerprint: str
    certificate_not_after: _timestamp_pb2.Timestamp
    def __init__(self, credential_id: _Optional[str] = ..., label: _Optional[str] = ..., fingerprint: _Optional[str] = ..., certificate_not_after: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class EffectiveRole(_message.Message):
    __slots__ = ("role", "scope_type", "namespace")
    ROLE_FIELD_NUMBER: _ClassVar[int]
    SCOPE_TYPE_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    role: str
    scope_type: str
    namespace: str
    def __init__(self, role: _Optional[str] = ..., scope_type: _Optional[str] = ..., namespace: _Optional[str] = ...) -> None: ...

class WhoAmIRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class WhoAmIResponse(_message.Message):
    __slots__ = ("principal", "credential", "roles")
    PRINCIPAL_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    ROLES_FIELD_NUMBER: _ClassVar[int]
    principal: PrincipalIdentity
    credential: CredentialIdentity
    roles: _containers.RepeatedCompositeFieldContainer[EffectiveRole]
    def __init__(self, principal: _Optional[_Union[PrincipalIdentity, _Mapping]] = ..., credential: _Optional[_Union[CredentialIdentity, _Mapping]] = ..., roles: _Optional[_Iterable[_Union[EffectiveRole, _Mapping]]] = ...) -> None: ...
