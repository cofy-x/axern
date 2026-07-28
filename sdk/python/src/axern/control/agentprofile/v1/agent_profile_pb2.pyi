import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AgentProvider(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_PROVIDER_UNSPECIFIED: _ClassVar[AgentProvider]
    AGENT_PROVIDER_OPENAI: _ClassVar[AgentProvider]
    AGENT_PROVIDER_ANTHROPIC: _ClassVar[AgentProvider]

class AgentWireApi(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_WIRE_API_UNSPECIFIED: _ClassVar[AgentWireApi]
    AGENT_WIRE_API_OPENAI_RESPONSES: _ClassVar[AgentWireApi]
    AGENT_WIRE_API_ANTHROPIC_MESSAGES: _ClassVar[AgentWireApi]

class ProfileCheckKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PROFILE_CHECK_KIND_UNSPECIFIED: _ClassVar[ProfileCheckKind]
    PROFILE_CHECK_KIND_CONFIGURATION: _ClassVar[ProfileCheckKind]
    PROFILE_CHECK_KIND_CREDENTIAL: _ClassVar[ProfileCheckKind]
    PROFILE_CHECK_KIND_WORKER_CAPABILITY: _ClassVar[ProfileCheckKind]
    PROFILE_CHECK_KIND_PROVIDER_AUTHENTICATION: _ClassVar[ProfileCheckKind]
    PROFILE_CHECK_KIND_WIRE_API: _ClassVar[ProfileCheckKind]
    PROFILE_CHECK_KIND_MODEL: _ClassVar[ProfileCheckKind]
    PROFILE_CHECK_KIND_LATENCY: _ClassVar[ProfileCheckKind]

class ProfileCheckStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PROFILE_CHECK_STATUS_UNSPECIFIED: _ClassVar[ProfileCheckStatus]
    PROFILE_CHECK_STATUS_PASS: _ClassVar[ProfileCheckStatus]
    PROFILE_CHECK_STATUS_WARN: _ClassVar[ProfileCheckStatus]
    PROFILE_CHECK_STATUS_FAIL: _ClassVar[ProfileCheckStatus]
AGENT_PROVIDER_UNSPECIFIED: AgentProvider
AGENT_PROVIDER_OPENAI: AgentProvider
AGENT_PROVIDER_ANTHROPIC: AgentProvider
AGENT_WIRE_API_UNSPECIFIED: AgentWireApi
AGENT_WIRE_API_OPENAI_RESPONSES: AgentWireApi
AGENT_WIRE_API_ANTHROPIC_MESSAGES: AgentWireApi
PROFILE_CHECK_KIND_UNSPECIFIED: ProfileCheckKind
PROFILE_CHECK_KIND_CONFIGURATION: ProfileCheckKind
PROFILE_CHECK_KIND_CREDENTIAL: ProfileCheckKind
PROFILE_CHECK_KIND_WORKER_CAPABILITY: ProfileCheckKind
PROFILE_CHECK_KIND_PROVIDER_AUTHENTICATION: ProfileCheckKind
PROFILE_CHECK_KIND_WIRE_API: ProfileCheckKind
PROFILE_CHECK_KIND_MODEL: ProfileCheckKind
PROFILE_CHECK_KIND_LATENCY: ProfileCheckKind
PROFILE_CHECK_STATUS_UNSPECIFIED: ProfileCheckStatus
PROFILE_CHECK_STATUS_PASS: ProfileCheckStatus
PROFILE_CHECK_STATUS_WARN: ProfileCheckStatus
PROFILE_CHECK_STATUS_FAIL: ProfileCheckStatus

class AgentProfileSpec(_message.Message):
    __slots__ = ("agent", "provider", "wire_api", "base_url", "max_concurrency")
    AGENT_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    WIRE_API_FIELD_NUMBER: _ClassVar[int]
    BASE_URL_FIELD_NUMBER: _ClassVar[int]
    MAX_CONCURRENCY_FIELD_NUMBER: _ClassVar[int]
    agent: str
    provider: AgentProvider
    wire_api: AgentWireApi
    base_url: str
    max_concurrency: int
    def __init__(self, agent: _Optional[str] = ..., provider: _Optional[_Union[AgentProvider, str]] = ..., wire_api: _Optional[_Union[AgentWireApi, str]] = ..., base_url: _Optional[str] = ..., max_concurrency: _Optional[int] = ...) -> None: ...

class AgentProfile(_message.Message):
    __slots__ = ("id", "namespace", "name", "spec", "labels", "version", "credential_version", "created_at", "updated_at")
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
    SPEC_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    name: str
    spec: AgentProfileSpec
    labels: _containers.ScalarMap[str, str]
    version: int
    credential_version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ..., spec: _Optional[_Union[AgentProfileSpec, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., version: _Optional[int] = ..., credential_version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class AgentProfileListFilter(_message.Message):
    __slots__ = ("namespace", "agent", "provider", "labels", "cursor", "page_size")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    agent: str
    provider: AgentProvider
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., agent: _Optional[str] = ..., provider: _Optional[_Union[AgentProvider, str]] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class CreateAgentProfileRequest(_message.Message):
    __slots__ = ("namespace", "name", "spec", "labels", "credential", "idempotency_key")
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
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    spec: AgentProfileSpec
    labels: _containers.ScalarMap[str, str]
    credential: bytes
    idempotency_key: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., spec: _Optional[_Union[AgentProfileSpec, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., credential: _Optional[bytes] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class CreateAgentProfileResponse(_message.Message):
    __slots__ = ("profile",)
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    profile: AgentProfile
    def __init__(self, profile: _Optional[_Union[AgentProfile, _Mapping]] = ...) -> None: ...

class GetAgentProfileRequest(_message.Message):
    __slots__ = ("namespace", "name")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class GetAgentProfileResponse(_message.Message):
    __slots__ = ("profile",)
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    profile: AgentProfile
    def __init__(self, profile: _Optional[_Union[AgentProfile, _Mapping]] = ...) -> None: ...

class ListAgentProfilesRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: AgentProfileListFilter
    def __init__(self, filter: _Optional[_Union[AgentProfileListFilter, _Mapping]] = ...) -> None: ...

class ListAgentProfilesResponse(_message.Message):
    __slots__ = ("profiles", "next_cursor")
    PROFILES_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    profiles: _containers.RepeatedCompositeFieldContainer[AgentProfile]
    next_cursor: str
    def __init__(self, profiles: _Optional[_Iterable[_Union[AgentProfile, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class AgentProfilePatch(_message.Message):
    __slots__ = ("base_url", "max_concurrency", "labels", "replace_labels")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    BASE_URL_FIELD_NUMBER: _ClassVar[int]
    MAX_CONCURRENCY_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    REPLACE_LABELS_FIELD_NUMBER: _ClassVar[int]
    base_url: str
    max_concurrency: int
    labels: _containers.ScalarMap[str, str]
    replace_labels: bool
    def __init__(self, base_url: _Optional[str] = ..., max_concurrency: _Optional[int] = ..., labels: _Optional[_Mapping[str, str]] = ..., replace_labels: _Optional[bool] = ...) -> None: ...

class UpdateAgentProfileRequest(_message.Message):
    __slots__ = ("namespace", "name", "patch", "expected_version", "idempotency_key")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    PATCH_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    patch: AgentProfilePatch
    expected_version: int
    idempotency_key: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., patch: _Optional[_Union[AgentProfilePatch, _Mapping]] = ..., expected_version: _Optional[int] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class UpdateAgentProfileResponse(_message.Message):
    __slots__ = ("profile",)
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    profile: AgentProfile
    def __init__(self, profile: _Optional[_Union[AgentProfile, _Mapping]] = ...) -> None: ...

class RotateAgentProfileCredentialRequest(_message.Message):
    __slots__ = ("namespace", "name", "credential", "expected_version", "idempotency_key")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    credential: bytes
    expected_version: int
    idempotency_key: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., credential: _Optional[bytes] = ..., expected_version: _Optional[int] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class RotateAgentProfileCredentialResponse(_message.Message):
    __slots__ = ("profile",)
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    profile: AgentProfile
    def __init__(self, profile: _Optional[_Union[AgentProfile, _Mapping]] = ...) -> None: ...

class ProfileCheck(_message.Message):
    __slots__ = ("kind", "status", "code", "message", "retryable", "latency_ms")
    KIND_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    RETRYABLE_FIELD_NUMBER: _ClassVar[int]
    LATENCY_MS_FIELD_NUMBER: _ClassVar[int]
    kind: ProfileCheckKind
    status: ProfileCheckStatus
    code: str
    message: str
    retryable: bool
    latency_ms: int
    def __init__(self, kind: _Optional[_Union[ProfileCheckKind, str]] = ..., status: _Optional[_Union[ProfileCheckStatus, str]] = ..., code: _Optional[str] = ..., message: _Optional[str] = ..., retryable: _Optional[bool] = ..., latency_ms: _Optional[int] = ...) -> None: ...

class DoctorAgentProfileRequest(_message.Message):
    __slots__ = ("namespace", "name", "model")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    model: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., model: _Optional[str] = ...) -> None: ...

class DoctorAgentProfileResponse(_message.Message):
    __slots__ = ("profile", "checks", "healthy")
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    CHECKS_FIELD_NUMBER: _ClassVar[int]
    HEALTHY_FIELD_NUMBER: _ClassVar[int]
    profile: AgentProfile
    checks: _containers.RepeatedCompositeFieldContainer[ProfileCheck]
    healthy: bool
    def __init__(self, profile: _Optional[_Union[AgentProfile, _Mapping]] = ..., checks: _Optional[_Iterable[_Union[ProfileCheck, _Mapping]]] = ..., healthy: _Optional[bool] = ...) -> None: ...

class DeleteAgentProfileRequest(_message.Message):
    __slots__ = ("namespace", "name", "expected_version")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    expected_version: int
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., expected_version: _Optional[int] = ...) -> None: ...

class DeleteAgentProfileResponse(_message.Message):
    __slots__ = ("profile",)
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    profile: AgentProfile
    def __init__(self, profile: _Optional[_Union[AgentProfile, _Mapping]] = ...) -> None: ...
