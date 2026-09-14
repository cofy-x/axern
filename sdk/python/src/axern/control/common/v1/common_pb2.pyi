from axern.control.capability.v1 import capability_pb2 as _capability_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class NetworkMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    NETWORK_MODE_UNSPECIFIED: _ClassVar[NetworkMode]
    NETWORK_MODE_DEFAULT: _ClassVar[NetworkMode]
    NETWORK_MODE_ISOLATED: _ClassVar[NetworkMode]
    NETWORK_MODE_HOST: _ClassVar[NetworkMode]

class EgressProtocol(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    EGRESS_PROTOCOL_UNSPECIFIED: _ClassVar[EgressProtocol]
    EGRESS_PROTOCOL_TCP: _ClassVar[EgressProtocol]
    EGRESS_PROTOCOL_UDP: _ClassVar[EgressProtocol]

class AllocationLifecycleState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ALLOCATION_LIFECYCLE_STATE_UNSPECIFIED: _ClassVar[AllocationLifecycleState]
    ALLOCATION_LIFECYCLE_STATE_BOUND: _ClassVar[AllocationLifecycleState]
    ALLOCATION_LIFECYCLE_STATE_STARTING: _ClassVar[AllocationLifecycleState]
    ALLOCATION_LIFECYCLE_STATE_ACTIVE: _ClassVar[AllocationLifecycleState]
    ALLOCATION_LIFECYCLE_STATE_STOPPED: _ClassVar[AllocationLifecycleState]
    ALLOCATION_LIFECYCLE_STATE_RELEASING: _ClassVar[AllocationLifecycleState]
    ALLOCATION_LIFECYCLE_STATE_RELEASED: _ClassVar[AllocationLifecycleState]

class WorkloadDiagnosticCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_IMAGE_RESOLUTION_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_NODE_SELECTION_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_PROCESS_EXITED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED: _ClassVar[WorkloadDiagnosticCode]
    WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED: _ClassVar[WorkloadDiagnosticCode]
NETWORK_MODE_UNSPECIFIED: NetworkMode
NETWORK_MODE_DEFAULT: NetworkMode
NETWORK_MODE_ISOLATED: NetworkMode
NETWORK_MODE_HOST: NetworkMode
EGRESS_PROTOCOL_UNSPECIFIED: EgressProtocol
EGRESS_PROTOCOL_TCP: EgressProtocol
EGRESS_PROTOCOL_UDP: EgressProtocol
ALLOCATION_LIFECYCLE_STATE_UNSPECIFIED: AllocationLifecycleState
ALLOCATION_LIFECYCLE_STATE_BOUND: AllocationLifecycleState
ALLOCATION_LIFECYCLE_STATE_STARTING: AllocationLifecycleState
ALLOCATION_LIFECYCLE_STATE_ACTIVE: AllocationLifecycleState
ALLOCATION_LIFECYCLE_STATE_STOPPED: AllocationLifecycleState
ALLOCATION_LIFECYCLE_STATE_RELEASING: AllocationLifecycleState
ALLOCATION_LIFECYCLE_STATE_RELEASED: AllocationLifecycleState
WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_IMAGE_RESOLUTION_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_NODE_SELECTION_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_PROCESS_EXITED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED: WorkloadDiagnosticCode
WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED: WorkloadDiagnosticCode

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

class NetworkSpec(_message.Message):
    __slots__ = ("mode", "egress_policy")
    MODE_FIELD_NUMBER: _ClassVar[int]
    EGRESS_POLICY_FIELD_NUMBER: _ClassVar[int]
    mode: NetworkMode
    egress_policy: NetworkEgressPolicy
    def __init__(self, mode: _Optional[_Union[NetworkMode, str]] = ..., egress_policy: _Optional[_Union[NetworkEgressPolicy, _Mapping]] = ...) -> None: ...

class NetworkEgressPolicy(_message.Message):
    __slots__ = ("strict", "dns_deny")
    STRICT_FIELD_NUMBER: _ClassVar[int]
    DNS_DENY_FIELD_NUMBER: _ClassVar[int]
    strict: StrictEgressPolicy
    dns_deny: DnsDenyPolicy
    def __init__(self, strict: _Optional[_Union[StrictEgressPolicy, _Mapping]] = ..., dns_deny: _Optional[_Union[DnsDenyPolicy, _Mapping]] = ...) -> None: ...

class StrictEgressPolicy(_message.Message):
    __slots__ = ("allowed_domains", "allowed_cidrs")
    ALLOWED_DOMAINS_FIELD_NUMBER: _ClassVar[int]
    ALLOWED_CIDRS_FIELD_NUMBER: _ClassVar[int]
    allowed_domains: _containers.RepeatedScalarFieldContainer[str]
    allowed_cidrs: _containers.RepeatedCompositeFieldContainer[CIDREgressRule]
    def __init__(self, allowed_domains: _Optional[_Iterable[str]] = ..., allowed_cidrs: _Optional[_Iterable[_Union[CIDREgressRule, _Mapping]]] = ...) -> None: ...

class DnsDenyPolicy(_message.Message):
    __slots__ = ("denied_domains",)
    DENIED_DOMAINS_FIELD_NUMBER: _ClassVar[int]
    denied_domains: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, denied_domains: _Optional[_Iterable[str]] = ...) -> None: ...

class PortRange(_message.Message):
    __slots__ = ("start", "end")
    START_FIELD_NUMBER: _ClassVar[int]
    END_FIELD_NUMBER: _ClassVar[int]
    start: int
    end: int
    def __init__(self, start: _Optional[int] = ..., end: _Optional[int] = ...) -> None: ...

class CIDREgressRule(_message.Message):
    __slots__ = ("cidr", "protocol", "ports")
    CIDR_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_FIELD_NUMBER: _ClassVar[int]
    PORTS_FIELD_NUMBER: _ClassVar[int]
    cidr: str
    protocol: EgressProtocol
    ports: _containers.RepeatedCompositeFieldContainer[PortRange]
    def __init__(self, cidr: _Optional[str] = ..., protocol: _Optional[_Union[EgressProtocol, str]] = ..., ports: _Optional[_Iterable[_Union[PortRange, _Mapping]]] = ...) -> None: ...

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

class ImageMount(_message.Message):
    __slots__ = ("image", "target", "readonly")
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    READONLY_FIELD_NUMBER: _ClassVar[int]
    image: str
    target: str
    readonly: bool
    def __init__(self, image: _Optional[str] = ..., target: _Optional[str] = ..., readonly: _Optional[bool] = ...) -> None: ...

class ExecutionConfig(_message.Message):
    __slots__ = ("argv", "env", "cwd", "resources", "network", "extension_capability_requirements", "placement", "secret_env", "secret_files", "image_mounts")
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
    NETWORK_FIELD_NUMBER: _ClassVar[int]
    EXTENSION_CAPABILITY_REQUIREMENTS_FIELD_NUMBER: _ClassVar[int]
    PLACEMENT_FIELD_NUMBER: _ClassVar[int]
    SECRET_ENV_FIELD_NUMBER: _ClassVar[int]
    SECRET_FILES_FIELD_NUMBER: _ClassVar[int]
    IMAGE_MOUNTS_FIELD_NUMBER: _ClassVar[int]
    argv: _containers.RepeatedScalarFieldContainer[str]
    env: _containers.ScalarMap[str, str]
    cwd: str
    resources: ResourceSpec
    network: NetworkSpec
    extension_capability_requirements: _containers.RepeatedCompositeFieldContainer[_capability_pb2.ExtensionCapabilityRequirement]
    placement: PlacementConstraints
    secret_env: _containers.RepeatedCompositeFieldContainer[SecretEnvVar]
    secret_files: _containers.RepeatedCompositeFieldContainer[SecretFile]
    image_mounts: _containers.RepeatedCompositeFieldContainer[ImageMount]
    def __init__(self, argv: _Optional[_Iterable[str]] = ..., env: _Optional[_Mapping[str, str]] = ..., cwd: _Optional[str] = ..., resources: _Optional[_Union[ResourceSpec, _Mapping]] = ..., network: _Optional[_Union[NetworkSpec, _Mapping]] = ..., extension_capability_requirements: _Optional[_Iterable[_Union[_capability_pb2.ExtensionCapabilityRequirement, _Mapping]]] = ..., placement: _Optional[_Union[PlacementConstraints, _Mapping]] = ..., secret_env: _Optional[_Iterable[_Union[SecretEnvVar, _Mapping]]] = ..., secret_files: _Optional[_Iterable[_Union[SecretFile, _Mapping]]] = ..., image_mounts: _Optional[_Iterable[_Union[ImageMount, _Mapping]]] = ...) -> None: ...
