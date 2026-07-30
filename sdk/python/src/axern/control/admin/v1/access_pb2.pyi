import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PrincipalKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PRINCIPAL_KIND_UNSPECIFIED: _ClassVar[PrincipalKind]
    PRINCIPAL_KIND_HUMAN: _ClassVar[PrincipalKind]
    PRINCIPAL_KIND_SERVICE: _ClassVar[PrincipalKind]

class PrincipalStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PRINCIPAL_STATUS_UNSPECIFIED: _ClassVar[PrincipalStatus]
    PRINCIPAL_STATUS_ACTIVE: _ClassVar[PrincipalStatus]
    PRINCIPAL_STATUS_DISABLED: _ClassVar[PrincipalStatus]

class AccessScopeType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ACCESS_SCOPE_TYPE_UNSPECIFIED: _ClassVar[AccessScopeType]
    ACCESS_SCOPE_TYPE_PLATFORM: _ClassVar[AccessScopeType]
    ACCESS_SCOPE_TYPE_NAMESPACE: _ClassVar[AccessScopeType]

class AccessRole(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ACCESS_ROLE_UNSPECIFIED: _ClassVar[AccessRole]
    ACCESS_ROLE_PLATFORM_ADMIN: _ClassVar[AccessRole]
    ACCESS_ROLE_NAMESPACE_ADMIN: _ClassVar[AccessRole]
    ACCESS_ROLE_NAMESPACE_EDITOR: _ClassVar[AccessRole]
    ACCESS_ROLE_NAMESPACE_VIEWER: _ClassVar[AccessRole]
    ACCESS_ROLE_ROLLOUT_EXECUTOR: _ClassVar[AccessRole]
PRINCIPAL_KIND_UNSPECIFIED: PrincipalKind
PRINCIPAL_KIND_HUMAN: PrincipalKind
PRINCIPAL_KIND_SERVICE: PrincipalKind
PRINCIPAL_STATUS_UNSPECIFIED: PrincipalStatus
PRINCIPAL_STATUS_ACTIVE: PrincipalStatus
PRINCIPAL_STATUS_DISABLED: PrincipalStatus
ACCESS_SCOPE_TYPE_UNSPECIFIED: AccessScopeType
ACCESS_SCOPE_TYPE_PLATFORM: AccessScopeType
ACCESS_SCOPE_TYPE_NAMESPACE: AccessScopeType
ACCESS_ROLE_UNSPECIFIED: AccessRole
ACCESS_ROLE_PLATFORM_ADMIN: AccessRole
ACCESS_ROLE_NAMESPACE_ADMIN: AccessRole
ACCESS_ROLE_NAMESPACE_EDITOR: AccessRole
ACCESS_ROLE_NAMESPACE_VIEWER: AccessRole
ACCESS_ROLE_ROLLOUT_EXECUTOR: AccessRole

class Principal(_message.Message):
    __slots__ = ("principal_id", "name", "display_name", "kind", "status", "version", "created_at", "updated_at")
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    principal_id: str
    name: str
    display_name: str
    kind: PrincipalKind
    status: PrincipalStatus
    version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, principal_id: _Optional[str] = ..., name: _Optional[str] = ..., display_name: _Optional[str] = ..., kind: _Optional[_Union[PrincipalKind, str]] = ..., status: _Optional[_Union[PrincipalStatus, str]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class PrincipalCredential(_message.Message):
    __slots__ = ("credential_id", "principal_id", "fingerprint", "certificate_not_after", "label", "created_at", "revoked_at")
    CREDENTIAL_ID_FIELD_NUMBER: _ClassVar[int]
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    CERTIFICATE_NOT_AFTER_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    REVOKED_AT_FIELD_NUMBER: _ClassVar[int]
    credential_id: str
    principal_id: str
    fingerprint: str
    certificate_not_after: _timestamp_pb2.Timestamp
    label: str
    created_at: _timestamp_pb2.Timestamp
    revoked_at: _timestamp_pb2.Timestamp
    def __init__(self, credential_id: _Optional[str] = ..., principal_id: _Optional[str] = ..., fingerprint: _Optional[str] = ..., certificate_not_after: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., label: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., revoked_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class RoleBinding(_message.Message):
    __slots__ = ("binding_id", "principal_id", "scope_type", "namespace", "role", "created_by_principal_id", "created_at", "revoked_by_principal_id", "revoked_at")
    BINDING_ID_FIELD_NUMBER: _ClassVar[int]
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    SCOPE_TYPE_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    CREATED_BY_PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    REVOKED_BY_PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    REVOKED_AT_FIELD_NUMBER: _ClassVar[int]
    binding_id: str
    principal_id: str
    scope_type: AccessScopeType
    namespace: str
    role: AccessRole
    created_by_principal_id: str
    created_at: _timestamp_pb2.Timestamp
    revoked_by_principal_id: str
    revoked_at: _timestamp_pb2.Timestamp
    def __init__(self, binding_id: _Optional[str] = ..., principal_id: _Optional[str] = ..., scope_type: _Optional[_Union[AccessScopeType, str]] = ..., namespace: _Optional[str] = ..., role: _Optional[_Union[AccessRole, str]] = ..., created_by_principal_id: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., revoked_by_principal_id: _Optional[str] = ..., revoked_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class CreatePrincipalRequest(_message.Message):
    __slots__ = ("name", "display_name", "kind")
    NAME_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    name: str
    display_name: str
    kind: PrincipalKind
    def __init__(self, name: _Optional[str] = ..., display_name: _Optional[str] = ..., kind: _Optional[_Union[PrincipalKind, str]] = ...) -> None: ...

class CreatePrincipalResponse(_message.Message):
    __slots__ = ("principal",)
    PRINCIPAL_FIELD_NUMBER: _ClassVar[int]
    principal: Principal
    def __init__(self, principal: _Optional[_Union[Principal, _Mapping]] = ...) -> None: ...

class ListPrincipalsRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListPrincipalsResponse(_message.Message):
    __slots__ = ("principals",)
    PRINCIPALS_FIELD_NUMBER: _ClassVar[int]
    principals: _containers.RepeatedCompositeFieldContainer[Principal]
    def __init__(self, principals: _Optional[_Iterable[_Union[Principal, _Mapping]]] = ...) -> None: ...

class DisablePrincipalRequest(_message.Message):
    __slots__ = ("principal_id",)
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    principal_id: str
    def __init__(self, principal_id: _Optional[str] = ...) -> None: ...

class DisablePrincipalResponse(_message.Message):
    __slots__ = ("principal",)
    PRINCIPAL_FIELD_NUMBER: _ClassVar[int]
    principal: Principal
    def __init__(self, principal: _Optional[_Union[Principal, _Mapping]] = ...) -> None: ...

class AddPrincipalCredentialRequest(_message.Message):
    __slots__ = ("principal_id", "certificate_der", "label")
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    CERTIFICATE_DER_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    principal_id: str
    certificate_der: bytes
    label: str
    def __init__(self, principal_id: _Optional[str] = ..., certificate_der: _Optional[bytes] = ..., label: _Optional[str] = ...) -> None: ...

class AddPrincipalCredentialResponse(_message.Message):
    __slots__ = ("credential",)
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    credential: PrincipalCredential
    def __init__(self, credential: _Optional[_Union[PrincipalCredential, _Mapping]] = ...) -> None: ...

class ListPrincipalCredentialsRequest(_message.Message):
    __slots__ = ("principal_id",)
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    principal_id: str
    def __init__(self, principal_id: _Optional[str] = ...) -> None: ...

class ListPrincipalCredentialsResponse(_message.Message):
    __slots__ = ("credentials",)
    CREDENTIALS_FIELD_NUMBER: _ClassVar[int]
    credentials: _containers.RepeatedCompositeFieldContainer[PrincipalCredential]
    def __init__(self, credentials: _Optional[_Iterable[_Union[PrincipalCredential, _Mapping]]] = ...) -> None: ...

class RevokePrincipalCredentialRequest(_message.Message):
    __slots__ = ("credential_id",)
    CREDENTIAL_ID_FIELD_NUMBER: _ClassVar[int]
    credential_id: str
    def __init__(self, credential_id: _Optional[str] = ...) -> None: ...

class RevokePrincipalCredentialResponse(_message.Message):
    __slots__ = ("credential",)
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    credential: PrincipalCredential
    def __init__(self, credential: _Optional[_Union[PrincipalCredential, _Mapping]] = ...) -> None: ...

class GrantRoleBindingRequest(_message.Message):
    __slots__ = ("principal_id", "scope_type", "namespace", "role")
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    SCOPE_TYPE_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    principal_id: str
    scope_type: AccessScopeType
    namespace: str
    role: AccessRole
    def __init__(self, principal_id: _Optional[str] = ..., scope_type: _Optional[_Union[AccessScopeType, str]] = ..., namespace: _Optional[str] = ..., role: _Optional[_Union[AccessRole, str]] = ...) -> None: ...

class GrantRoleBindingResponse(_message.Message):
    __slots__ = ("binding",)
    BINDING_FIELD_NUMBER: _ClassVar[int]
    binding: RoleBinding
    def __init__(self, binding: _Optional[_Union[RoleBinding, _Mapping]] = ...) -> None: ...

class ListRoleBindingsRequest(_message.Message):
    __slots__ = ("principal_id", "namespace", "include_revoked")
    PRINCIPAL_ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    INCLUDE_REVOKED_FIELD_NUMBER: _ClassVar[int]
    principal_id: str
    namespace: str
    include_revoked: bool
    def __init__(self, principal_id: _Optional[str] = ..., namespace: _Optional[str] = ..., include_revoked: _Optional[bool] = ...) -> None: ...

class ListRoleBindingsResponse(_message.Message):
    __slots__ = ("bindings",)
    BINDINGS_FIELD_NUMBER: _ClassVar[int]
    bindings: _containers.RepeatedCompositeFieldContainer[RoleBinding]
    def __init__(self, bindings: _Optional[_Iterable[_Union[RoleBinding, _Mapping]]] = ...) -> None: ...

class RevokeRoleBindingRequest(_message.Message):
    __slots__ = ("binding_id",)
    BINDING_ID_FIELD_NUMBER: _ClassVar[int]
    binding_id: str
    def __init__(self, binding_id: _Optional[str] = ...) -> None: ...

class RevokeRoleBindingResponse(_message.Message):
    __slots__ = ("binding",)
    BINDING_FIELD_NUMBER: _ClassVar[int]
    binding: RoleBinding
    def __init__(self, binding: _Optional[_Union[RoleBinding, _Mapping]] = ...) -> None: ...
