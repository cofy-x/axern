import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class VolumeBackend(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VOLUME_BACKEND_UNSPECIFIED: _ClassVar[VolumeBackend]
    VOLUME_BACKEND_LOCAL: _ClassVar[VolumeBackend]
    VOLUME_BACKEND_KUBERNETES_PVC: _ClassVar[VolumeBackend]
    VOLUME_BACKEND_NFS: _ClassVar[VolumeBackend]
    VOLUME_BACKEND_OBJECT_STORE_DATASET: _ClassVar[VolumeBackend]

class VolumeAccessMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VOLUME_ACCESS_MODE_UNSPECIFIED: _ClassVar[VolumeAccessMode]
    VOLUME_ACCESS_MODE_READ_WRITE_ONCE: _ClassVar[VolumeAccessMode]
    VOLUME_ACCESS_MODE_READ_ONLY_MANY: _ClassVar[VolumeAccessMode]
    VOLUME_ACCESS_MODE_READ_WRITE_MANY: _ClassVar[VolumeAccessMode]

class VolumeReclaimPolicy(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VOLUME_RECLAIM_POLICY_UNSPECIFIED: _ClassVar[VolumeReclaimPolicy]
    VOLUME_RECLAIM_POLICY_RETAIN: _ClassVar[VolumeReclaimPolicy]
    VOLUME_RECLAIM_POLICY_DELETE: _ClassVar[VolumeReclaimPolicy]

class VolumeBindingScope(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VOLUME_BINDING_SCOPE_UNSPECIFIED: _ClassVar[VolumeBindingScope]
    VOLUME_BINDING_SCOPE_SERVICE: _ClassVar[VolumeBindingScope]
    VOLUME_BINDING_SCOPE_ALLOCATION: _ClassVar[VolumeBindingScope]
    VOLUME_BINDING_SCOPE_EXTERNAL: _ClassVar[VolumeBindingScope]

class VolumeConsistencyProfile(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VOLUME_CONSISTENCY_PROFILE_UNSPECIFIED: _ClassVar[VolumeConsistencyProfile]
    VOLUME_CONSISTENCY_PROFILE_POSIX: _ClassVar[VolumeConsistencyProfile]
    VOLUME_CONSISTENCY_PROFILE_SHARED_FILESYSTEM: _ClassVar[VolumeConsistencyProfile]
    VOLUME_CONSISTENCY_PROFILE_OBJECT_STORE: _ClassVar[VolumeConsistencyProfile]
    VOLUME_CONSISTENCY_PROFILE_CACHE: _ClassVar[VolumeConsistencyProfile]

class VolumeStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    VOLUME_STATUS_UNSPECIFIED: _ClassVar[VolumeStatus]
    VOLUME_STATUS_PENDING: _ClassVar[VolumeStatus]
    VOLUME_STATUS_BOUND: _ClassVar[VolumeStatus]
    VOLUME_STATUS_AVAILABLE: _ClassVar[VolumeStatus]
    VOLUME_STATUS_DELETING: _ClassVar[VolumeStatus]
    VOLUME_STATUS_DELETED: _ClassVar[VolumeStatus]
    VOLUME_STATUS_FAILED: _ClassVar[VolumeStatus]
    VOLUME_STATUS_PUBLISHING: _ClassVar[VolumeStatus]
    VOLUME_STATUS_PUBLISHED: _ClassVar[VolumeStatus]
    VOLUME_STATUS_RELEASING: _ClassVar[VolumeStatus]
VOLUME_BACKEND_UNSPECIFIED: VolumeBackend
VOLUME_BACKEND_LOCAL: VolumeBackend
VOLUME_BACKEND_KUBERNETES_PVC: VolumeBackend
VOLUME_BACKEND_NFS: VolumeBackend
VOLUME_BACKEND_OBJECT_STORE_DATASET: VolumeBackend
VOLUME_ACCESS_MODE_UNSPECIFIED: VolumeAccessMode
VOLUME_ACCESS_MODE_READ_WRITE_ONCE: VolumeAccessMode
VOLUME_ACCESS_MODE_READ_ONLY_MANY: VolumeAccessMode
VOLUME_ACCESS_MODE_READ_WRITE_MANY: VolumeAccessMode
VOLUME_RECLAIM_POLICY_UNSPECIFIED: VolumeReclaimPolicy
VOLUME_RECLAIM_POLICY_RETAIN: VolumeReclaimPolicy
VOLUME_RECLAIM_POLICY_DELETE: VolumeReclaimPolicy
VOLUME_BINDING_SCOPE_UNSPECIFIED: VolumeBindingScope
VOLUME_BINDING_SCOPE_SERVICE: VolumeBindingScope
VOLUME_BINDING_SCOPE_ALLOCATION: VolumeBindingScope
VOLUME_BINDING_SCOPE_EXTERNAL: VolumeBindingScope
VOLUME_CONSISTENCY_PROFILE_UNSPECIFIED: VolumeConsistencyProfile
VOLUME_CONSISTENCY_PROFILE_POSIX: VolumeConsistencyProfile
VOLUME_CONSISTENCY_PROFILE_SHARED_FILESYSTEM: VolumeConsistencyProfile
VOLUME_CONSISTENCY_PROFILE_OBJECT_STORE: VolumeConsistencyProfile
VOLUME_CONSISTENCY_PROFILE_CACHE: VolumeConsistencyProfile
VOLUME_STATUS_UNSPECIFIED: VolumeStatus
VOLUME_STATUS_PENDING: VolumeStatus
VOLUME_STATUS_BOUND: VolumeStatus
VOLUME_STATUS_AVAILABLE: VolumeStatus
VOLUME_STATUS_DELETING: VolumeStatus
VOLUME_STATUS_DELETED: VolumeStatus
VOLUME_STATUS_FAILED: VolumeStatus
VOLUME_STATUS_PUBLISHING: VolumeStatus
VOLUME_STATUS_PUBLISHED: VolumeStatus
VOLUME_STATUS_RELEASING: VolumeStatus

class VolumeCapacity(_message.Message):
    __slots__ = ("size_bytes",)
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    size_bytes: int
    def __init__(self, size_bytes: _Optional[int] = ...) -> None: ...

class VolumeTopology(_message.Message):
    __slots__ = ("cluster", "zone", "node_id")
    CLUSTER_FIELD_NUMBER: _ClassVar[int]
    ZONE_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    cluster: str
    zone: str
    node_id: str
    def __init__(self, cluster: _Optional[str] = ..., zone: _Optional[str] = ..., node_id: _Optional[str] = ...) -> None: ...

class VolumeRuntimeCompatibility(_message.Message):
    __slots__ = ("supports_runc", "supports_runsc", "requires_privileged_node_mount")
    SUPPORTS_RUNC_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_RUNSC_FIELD_NUMBER: _ClassVar[int]
    REQUIRES_PRIVILEGED_NODE_MOUNT_FIELD_NUMBER: _ClassVar[int]
    supports_runc: bool
    supports_runsc: bool
    requires_privileged_node_mount: bool
    def __init__(self, supports_runc: _Optional[bool] = ..., supports_runsc: _Optional[bool] = ..., requires_privileged_node_mount: _Optional[bool] = ...) -> None: ...

class VolumeClass(_message.Message):
    __slots__ = ("name", "backend", "access_modes", "default_reclaim_policy", "consistency_profile", "runtime_compatibility", "parameters", "created_at", "updated_at")
    class ParametersEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    BACKEND_FIELD_NUMBER: _ClassVar[int]
    ACCESS_MODES_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_RECLAIM_POLICY_FIELD_NUMBER: _ClassVar[int]
    CONSISTENCY_PROFILE_FIELD_NUMBER: _ClassVar[int]
    RUNTIME_COMPATIBILITY_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    name: str
    backend: VolumeBackend
    access_modes: _containers.RepeatedScalarFieldContainer[VolumeAccessMode]
    default_reclaim_policy: VolumeReclaimPolicy
    consistency_profile: VolumeConsistencyProfile
    runtime_compatibility: VolumeRuntimeCompatibility
    parameters: _containers.ScalarMap[str, str]
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, name: _Optional[str] = ..., backend: _Optional[_Union[VolumeBackend, str]] = ..., access_modes: _Optional[_Iterable[_Union[VolumeAccessMode, str]]] = ..., default_reclaim_policy: _Optional[_Union[VolumeReclaimPolicy, str]] = ..., consistency_profile: _Optional[_Union[VolumeConsistencyProfile, str]] = ..., runtime_compatibility: _Optional[_Union[VolumeRuntimeCompatibility, _Mapping]] = ..., parameters: _Optional[_Mapping[str, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class VolumeClaim(_message.Message):
    __slots__ = ("id", "namespace", "name", "class_name", "requested_capacity", "access_mode", "reclaim_policy", "binding_scope", "status", "topology", "parameters", "labels", "version", "created_at", "updated_at", "message", "owner_type", "owner_id", "backend_handle", "reclaim_attempt", "next_reclaim_at", "reclaim_lease_token", "reclaim_lease_until")
    class ParametersEntry(_message.Message):
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
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    CLASS_NAME_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_CAPACITY_FIELD_NUMBER: _ClassVar[int]
    ACCESS_MODE_FIELD_NUMBER: _ClassVar[int]
    RECLAIM_POLICY_FIELD_NUMBER: _ClassVar[int]
    BINDING_SCOPE_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    TOPOLOGY_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    OWNER_TYPE_FIELD_NUMBER: _ClassVar[int]
    OWNER_ID_FIELD_NUMBER: _ClassVar[int]
    BACKEND_HANDLE_FIELD_NUMBER: _ClassVar[int]
    RECLAIM_ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    NEXT_RECLAIM_AT_FIELD_NUMBER: _ClassVar[int]
    RECLAIM_LEASE_TOKEN_FIELD_NUMBER: _ClassVar[int]
    RECLAIM_LEASE_UNTIL_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    name: str
    class_name: str
    requested_capacity: VolumeCapacity
    access_mode: VolumeAccessMode
    reclaim_policy: VolumeReclaimPolicy
    binding_scope: VolumeBindingScope
    status: VolumeStatus
    topology: VolumeTopology
    parameters: _containers.ScalarMap[str, str]
    labels: _containers.ScalarMap[str, str]
    version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    message: str
    owner_type: str
    owner_id: str
    backend_handle: str
    reclaim_attempt: int
    next_reclaim_at: _timestamp_pb2.Timestamp
    reclaim_lease_token: str
    reclaim_lease_until: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., name: _Optional[str] = ..., class_name: _Optional[str] = ..., requested_capacity: _Optional[_Union[VolumeCapacity, _Mapping]] = ..., access_mode: _Optional[_Union[VolumeAccessMode, str]] = ..., reclaim_policy: _Optional[_Union[VolumeReclaimPolicy, str]] = ..., binding_scope: _Optional[_Union[VolumeBindingScope, str]] = ..., status: _Optional[_Union[VolumeStatus, str]] = ..., topology: _Optional[_Union[VolumeTopology, _Mapping]] = ..., parameters: _Optional[_Mapping[str, str]] = ..., labels: _Optional[_Mapping[str, str]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., message: _Optional[str] = ..., owner_type: _Optional[str] = ..., owner_id: _Optional[str] = ..., backend_handle: _Optional[str] = ..., reclaim_attempt: _Optional[int] = ..., next_reclaim_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., reclaim_lease_token: _Optional[str] = ..., reclaim_lease_until: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class VolumeMount(_message.Message):
    __slots__ = ("claim_name", "target", "readonly", "options")
    CLAIM_NAME_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    READONLY_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    claim_name: str
    target: str
    readonly: bool
    options: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, claim_name: _Optional[str] = ..., target: _Optional[str] = ..., readonly: _Optional[bool] = ..., options: _Optional[_Iterable[str]] = ...) -> None: ...

class VolumeClaimListFilter(_message.Message):
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
    statuses: _containers.RepeatedScalarFieldContainer[VolumeStatus]
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., statuses: _Optional[_Iterable[_Union[VolumeStatus, str]]] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...
