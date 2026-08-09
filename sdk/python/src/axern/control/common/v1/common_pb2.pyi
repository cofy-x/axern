import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from axern.control.capability.v1 import capability_pb2 as _capability_pb2
from axern.control.storage.v1 import storage_types_pb2 as _storage_types_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PortProtocol(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PORT_PROTOCOL_UNSPECIFIED: _ClassVar[PortProtocol]
    PORT_PROTOCOL_TCP: _ClassVar[PortProtocol]
    PORT_PROTOCOL_UDP: _ClassVar[PortProtocol]

class NetworkMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    NETWORK_MODE_UNSPECIFIED: _ClassVar[NetworkMode]
    NETWORK_MODE_DEFAULT: _ClassVar[NetworkMode]
    NETWORK_MODE_ISOLATED: _ClassVar[NetworkMode]
    NETWORK_MODE_HOST: _ClassVar[NetworkMode]

class AllocationStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ALLOCATION_STATUS_UNSPECIFIED: _ClassVar[AllocationStatus]
    ALLOCATION_STATUS_RESERVED: _ClassVar[AllocationStatus]
    ALLOCATION_STATUS_BOUND: _ClassVar[AllocationStatus]
    ALLOCATION_STATUS_STARTING: _ClassVar[AllocationStatus]
    ALLOCATION_STATUS_RUNNING: _ClassVar[AllocationStatus]
    ALLOCATION_STATUS_EXITED: _ClassVar[AllocationStatus]
    ALLOCATION_STATUS_FAILED: _ClassVar[AllocationStatus]
    ALLOCATION_STATUS_RELEASING: _ClassVar[AllocationStatus]
    ALLOCATION_STATUS_RELEASED: _ClassVar[AllocationStatus]

class WorkloadDiagnosticCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_IMAGE_RESOLUTION_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_NODE_SELECTION_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_PROCESS_EXITED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_LIVENESS_PROBE_FAILED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_STORAGE_TOPOLOGY_UNSATISFIED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_STORAGE_RESERVE_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_VOLUME_PUBLISH_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_VOLUME_RELEASE_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_VOLUME_SPEC_CONFLICT: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST: _ClassVar[WorkloadDiagnosticCode]

class LeaseType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    LEASE_TYPE_UNSPECIFIED: _ClassVar[LeaseType]
    LEASE_TYPE_RUN: _ClassVar[LeaseType]
    LEASE_TYPE_SERVICE: _ClassVar[LeaseType]
    LEASE_TYPE_INVOKE: _ClassVar[LeaseType]
PORT_PROTOCOL_UNSPECIFIED: PortProtocol
PORT_PROTOCOL_TCP: PortProtocol
PORT_PROTOCOL_UDP: PortProtocol
NETWORK_MODE_UNSPECIFIED: NetworkMode
NETWORK_MODE_DEFAULT: NetworkMode
NETWORK_MODE_ISOLATED: NetworkMode
NETWORK_MODE_HOST: NetworkMode
ALLOCATION_STATUS_UNSPECIFIED: AllocationStatus
ALLOCATION_STATUS_RESERVED: AllocationStatus
ALLOCATION_STATUS_BOUND: AllocationStatus
ALLOCATION_STATUS_STARTING: AllocationStatus
ALLOCATION_STATUS_RUNNING: AllocationStatus
ALLOCATION_STATUS_EXITED: AllocationStatus
ALLOCATION_STATUS_FAILED: AllocationStatus
ALLOCATION_STATUS_RELEASING: AllocationStatus
ALLOCATION_STATUS_RELEASED: AllocationStatus
WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_IMAGE_RESOLUTION_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_NODE_SELECTION_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_PROCESS_EXITED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_LIVENESS_PROBE_FAILED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_STORAGE_TOPOLOGY_UNSATISFIED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_STORAGE_RESERVE_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_VOLUME_PUBLISH_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_VOLUME_RELEASE_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_VOLUME_SPEC_CONFLICT: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST: WorkloadDiagnosticCode
LEASE_TYPE_UNSPECIFIED: LeaseType
LEASE_TYPE_RUN: LeaseType
LEASE_TYPE_SERVICE: LeaseType
LEASE_TYPE_INVOKE: LeaseType

class ResourceQuantity(_message.Message):
    __slots__ = ("cpu_milli", "memory_bytes", "ephemeral_storage_bytes")
    CPU_MILLI_FIELD_NUMBER: _ClassVar[int]
    MEMORY_BYTES_FIELD_NUMBER: _ClassVar[int]
    EPHEMERAL_STORAGE_BYTES_FIELD_NUMBER: _ClassVar[int]
    cpu_milli: int
    memory_bytes: int
    ephemeral_storage_bytes: int
    def __init__(self, cpu_milli: _Optional[int] = ..., memory_bytes: _Optional[int] = ..., ephemeral_storage_bytes: _Optional[int] = ...) -> None: ...

class ResourceSpec(_message.Message):
    __slots__ = ("requests", "limits")
    REQUESTS_FIELD_NUMBER: _ClassVar[int]
    LIMITS_FIELD_NUMBER: _ClassVar[int]
    requests: ResourceQuantity
    limits: ResourceQuantity
    def __init__(self, requests: _Optional[_Union[ResourceQuantity, _Mapping]] = ..., limits: _Optional[_Union[ResourceQuantity, _Mapping]] = ...) -> None: ...

class PortSpec(_message.Message):
    __slots__ = ("name", "protocol", "container_port", "host_port")
    NAME_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_FIELD_NUMBER: _ClassVar[int]
    CONTAINER_PORT_FIELD_NUMBER: _ClassVar[int]
    HOST_PORT_FIELD_NUMBER: _ClassVar[int]
    name: str
    protocol: PortProtocol
    container_port: int
    host_port: int
    def __init__(self, name: _Optional[str] = ..., protocol: _Optional[_Union[PortProtocol, str]] = ..., container_port: _Optional[int] = ..., host_port: _Optional[int] = ...) -> None: ...

class NetworkSpec(_message.Message):
    __slots__ = ("mode",)
    MODE_FIELD_NUMBER: _ClassVar[int]
    mode: NetworkMode
    def __init__(self, mode: _Optional[_Union[NetworkMode, str]] = ...) -> None: ...

class PlacementConstraints(_message.Message):
    __slots__ = ("node_selector",)
    class NodeSelectorEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NODE_SELECTOR_FIELD_NUMBER: _ClassVar[int]
    node_selector: _containers.ScalarMap[str, str]
    def __init__(self, node_selector: _Optional[_Mapping[str, str]] = ...) -> None: ...

class SecretEnvVar(_message.Message):
    __slots__ = ("name", "secret_id", "key", "optional")
    NAME_FIELD_NUMBER: _ClassVar[int]
    SECRET_ID_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    OPTIONAL_FIELD_NUMBER: _ClassVar[int]
    name: str
    secret_id: str
    key: str
    optional: bool
    def __init__(self, name: _Optional[str] = ..., secret_id: _Optional[str] = ..., key: _Optional[str] = ..., optional: _Optional[bool] = ...) -> None: ...

class SecretFile(_message.Message):
    __slots__ = ("path", "secret_id", "key", "mode", "optional")
    PATH_FIELD_NUMBER: _ClassVar[int]
    SECRET_ID_FIELD_NUMBER: _ClassVar[int]
    KEY_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    OPTIONAL_FIELD_NUMBER: _ClassVar[int]
    path: str
    secret_id: str
    key: str
    mode: int
    optional: bool
    def __init__(self, path: _Optional[str] = ..., secret_id: _Optional[str] = ..., key: _Optional[str] = ..., mode: _Optional[int] = ..., optional: _Optional[bool] = ...) -> None: ...

class ServiceVolumeMount(_message.Message):
    __slots__ = ("name", "target", "readonly", "options", "reclaim_policy")
    NAME_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    READONLY_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    RECLAIM_POLICY_FIELD_NUMBER: _ClassVar[int]
    name: str
    target: str
    readonly: bool
    options: _containers.RepeatedScalarFieldContainer[str]
    reclaim_policy: _storage_types_pb2.VolumeReclaimPolicy
    def __init__(self, name: _Optional[str] = ..., target: _Optional[str] = ..., readonly: _Optional[bool] = ..., options: _Optional[_Iterable[str]] = ..., reclaim_policy: _Optional[_Union[_storage_types_pb2.VolumeReclaimPolicy, str]] = ...) -> None: ...

class ImageMount(_message.Message):
    __slots__ = ("image", "target", "readonly")
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    READONLY_FIELD_NUMBER: _ClassVar[int]
    image: str
    target: str
    readonly: bool
    def __init__(self, image: _Optional[str] = ..., target: _Optional[str] = ..., readonly: _Optional[bool] = ...) -> None: ...

class WorkspaceImageVariant(_message.Message):
    __slots__ = ("format", "image")
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    format: str
    image: str
    def __init__(self, format: _Optional[str] = ..., image: _Optional[str] = ...) -> None: ...

class WorkspaceImageSource(_message.Message):
    __slots__ = ("variants", "source_path", "target")
    VARIANTS_FIELD_NUMBER: _ClassVar[int]
    SOURCE_PATH_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    variants: _containers.RepeatedCompositeFieldContainer[WorkspaceImageVariant]
    source_path: str
    target: str
    def __init__(self, variants: _Optional[_Iterable[_Union[WorkspaceImageVariant, _Mapping]]] = ..., source_path: _Optional[str] = ..., target: _Optional[str] = ...) -> None: ...

class WorkspacePreparationFacts(_message.Message):
    __slots__ = ("payload_format", "payload_digest", "cache_hit", "image_resolve_ms", "image_pull_ms", "cow_prepare_ms")
    PAYLOAD_FORMAT_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_DIGEST_FIELD_NUMBER: _ClassVar[int]
    CACHE_HIT_FIELD_NUMBER: _ClassVar[int]
    IMAGE_RESOLVE_MS_FIELD_NUMBER: _ClassVar[int]
    IMAGE_PULL_MS_FIELD_NUMBER: _ClassVar[int]
    COW_PREPARE_MS_FIELD_NUMBER: _ClassVar[int]
    payload_format: str
    payload_digest: str
    cache_hit: bool
    image_resolve_ms: int
    image_pull_ms: int
    cow_prepare_ms: int
    def __init__(self, payload_format: _Optional[str] = ..., payload_digest: _Optional[str] = ..., cache_hit: _Optional[bool] = ..., image_resolve_ms: _Optional[int] = ..., image_pull_ms: _Optional[int] = ..., cow_prepare_ms: _Optional[int] = ...) -> None: ...

class ExecutionConfig(_message.Message):
    __slots__ = ("argv", "env", "cwd", "resources", "ports", "network", "extension_capability_requirements", "placement", "secret_env", "secret_files", "volume_mounts", "runtime_class", "image_mounts", "workspace_image")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ARGV_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    CWD_FIELD_NUMBER: _ClassVar[int]
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    PORTS_FIELD_NUMBER: _ClassVar[int]
    NETWORK_FIELD_NUMBER: _ClassVar[int]
    EXTENSION_CAPABILITY_REQUIREMENTS_FIELD_NUMBER: _ClassVar[int]
    PLACEMENT_FIELD_NUMBER: _ClassVar[int]
    SECRET_ENV_FIELD_NUMBER: _ClassVar[int]
    SECRET_FILES_FIELD_NUMBER: _ClassVar[int]
    VOLUME_MOUNTS_FIELD_NUMBER: _ClassVar[int]
    RUNTIME_CLASS_FIELD_NUMBER: _ClassVar[int]
    IMAGE_MOUNTS_FIELD_NUMBER: _ClassVar[int]
    WORKSPACE_IMAGE_FIELD_NUMBER: _ClassVar[int]
    argv: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    cwd: str
    resources: ResourceSpec
    ports: _containers.RepeatedCompositeFieldContainer[PortSpec]
    network: NetworkSpec
    extension_capability_requirements: _containers.RepeatedCompositeFieldContainer[_capability_pb2.ExtensionCapabilityRequirement]
    placement: PlacementConstraints
    secret_env: _containers.RepeatedCompositeFieldContainer[SecretEnvVar]
    secret_files: _containers.RepeatedCompositeFieldContainer[SecretFile]
    volume_mounts: _containers.RepeatedCompositeFieldContainer[ServiceVolumeMount]
    runtime_class: str
    image_mounts: _containers.RepeatedCompositeFieldContainer[ImageMount]
    workspace_image: WorkspaceImageSource
    def __init__(self, argv: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., cwd: _Optional[str] = ..., resources: _Optional[_Union[ResourceSpec, _Mapping]] = ..., ports: _Optional[_Iterable[_Union[PortSpec, _Mapping]]] = ..., network: _Optional[_Union[NetworkSpec, _Mapping]] = ..., extension_capability_requirements: _Optional[_Iterable[_Union[_capability_pb2.ExtensionCapabilityRequirement, _Mapping]]] = ..., placement: _Optional[_Union[PlacementConstraints, _Mapping]] = ..., secret_env: _Optional[_Iterable[_Union[SecretEnvVar, _Mapping]]] = ..., secret_files: _Optional[_Iterable[_Union[SecretFile, _Mapping]]] = ..., volume_mounts: _Optional[_Iterable[_Union[ServiceVolumeMount, _Mapping]]] = ..., runtime_class: _Optional[str] = ..., image_mounts: _Optional[_Iterable[_Union[ImageMount, _Mapping]]] = ..., workspace_image: _Optional[_Union[WorkspaceImageSource, _Mapping]] = ...) -> None: ...

class ExecutionLease(_message.Message):
    __slots__ = ("lease_id", "allocation_id", "node_id", "attempt", "lease_type", "plaintext_token", "revision", "expires_at", "revoked", "node_target", "validation_token_hash")
    LEASE_ID_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    LEASE_TYPE_FIELD_NUMBER: _ClassVar[int]
    PLAINTEXT_TOKEN_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    REVOKED_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    VALIDATION_TOKEN_HASH_FIELD_NUMBER: _ClassVar[int]
    lease_id: str
    allocation_id: str
    node_id: str
    attempt: int
    lease_type: LeaseType
    plaintext_token: str
    revision: int
    expires_at: _timestamp_pb2.Timestamp
    revoked: bool
    node_target: str
    validation_token_hash: str
    def __init__(self, lease_id: _Optional[str] = ..., allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., attempt: _Optional[int] = ..., lease_type: _Optional[_Union[LeaseType, str]] = ..., plaintext_token: _Optional[str] = ..., revision: _Optional[int] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., revoked: _Optional[bool] = ..., node_target: _Optional[str] = ..., validation_token_hash: _Optional[str] = ...) -> None: ...
