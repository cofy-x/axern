import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PlatformCapability(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PLATFORM_CAPABILITY_UNSPECIFIED: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_PORT_FORWARDING: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_NETWORK_BRIDGE: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_NETWORK_BPFNET: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST: _ClassVar[PlatformCapability]
    PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST: _ClassVar[PlatformCapability]

class CapabilityState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CAPABILITY_STATE_UNSPECIFIED: _ClassVar[CapabilityState]
    CAPABILITY_STATE_AVAILABLE: _ClassVar[CapabilityState]
    CAPABILITY_STATE_DEGRADED: _ClassVar[CapabilityState]
    CAPABILITY_STATE_UNAVAILABLE: _ClassVar[CapabilityState]
    CAPABILITY_STATE_UNKNOWN: _ClassVar[CapabilityState]

class CapabilityProvider(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CAPABILITY_PROVIDER_UNSPECIFIED: _ClassVar[CapabilityProvider]
    CAPABILITY_PROVIDER_CONFIG: _ClassVar[CapabilityProvider]
    CAPABILITY_PROVIDER_HOST_CGROUP: _ClassVar[CapabilityProvider]
    CAPABILITY_PROVIDER_FILESTORE: _ClassVar[CapabilityProvider]
    CAPABILITY_PROVIDER_EROFS_PROBE: _ClassVar[CapabilityProvider]
    CAPABILITY_PROVIDER_RUNC_SELF_TEST: _ClassVar[CapabilityProvider]
    CAPABILITY_PROVIDER_RUNSC_SELF_TEST: _ClassVar[CapabilityProvider]
    CAPABILITY_PROVIDER_NETWORK_HEALTH: _ClassVar[CapabilityProvider]
    CAPABILITY_PROVIDER_DERIVED: _ClassVar[CapabilityProvider]

class CapabilityValidityScope(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CAPABILITY_VALIDITY_SCOPE_UNSPECIFIED: _ClassVar[CapabilityValidityScope]
    CAPABILITY_VALIDITY_SCOPE_CONFIG_STATIC: _ClassVar[CapabilityValidityScope]
    CAPABILITY_VALIDITY_SCOPE_BOOT: _ClassVar[CapabilityValidityScope]
    CAPABILITY_VALIDITY_SCOPE_MOUNT: _ClassVar[CapabilityValidityScope]
    CAPABILITY_VALIDITY_SCOPE_RUNTIME: _ClassVar[CapabilityValidityScope]
    CAPABILITY_VALIDITY_SCOPE_REFRESHABLE: _ClassVar[CapabilityValidityScope]

class CapabilityReasonCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CAPABILITY_REASON_CODE_UNSPECIFIED: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_AVAILABLE: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_DISABLED: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_PROBE_FAILED: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_PROBE_ERROR: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_EXPIRED: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_IDENTITY_CHANGED: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_RECOVERY_PENDING: _ClassVar[CapabilityReasonCode]
    CAPABILITY_REASON_CODE_ENFORCEMENT_LOST: _ClassVar[CapabilityReasonCode]

class CapabilityLossPolicy(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CAPABILITY_LOSS_POLICY_UNSPECIFIED: _ClassVar[CapabilityLossPolicy]
    CAPABILITY_LOSS_POLICY_ADMISSION_ONLY: _ClassVar[CapabilityLossPolicy]
    CAPABILITY_LOSS_POLICY_DEGRADE: _ClassVar[CapabilityLossPolicy]
    CAPABILITY_LOSS_POLICY_FAIL_STOP: _ClassVar[CapabilityLossPolicy]

class CapabilityConditionState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CAPABILITY_CONDITION_STATE_UNSPECIFIED: _ClassVar[CapabilityConditionState]
    CAPABILITY_CONDITION_STATE_HEALTHY: _ClassVar[CapabilityConditionState]
    CAPABILITY_CONDITION_STATE_DEGRADED: _ClassVar[CapabilityConditionState]
    CAPABILITY_CONDITION_STATE_FAILED: _ClassVar[CapabilityConditionState]
    CAPABILITY_CONDITION_STATE_UNKNOWN: _ClassVar[CapabilityConditionState]
PLATFORM_CAPABILITY_UNSPECIFIED: PlatformCapability
PLATFORM_CAPABILITY_PORT_FORWARDING: PlatformCapability
PLATFORM_CAPABILITY_NETWORK_BRIDGE: PlatformCapability
PLATFORM_CAPABILITY_NETWORK_BPFNET: PlatformCapability
PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER: PlatformCapability
PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT: PlatformCapability
PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT: PlatformCapability
PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER: PlatformCapability
PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA: PlatformCapability
PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT: PlatformCapability
PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT: PlatformCapability
PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS: PlatformCapability
PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST: PlatformCapability
PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST: PlatformCapability
PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST: PlatformCapability
PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST: PlatformCapability
CAPABILITY_STATE_UNSPECIFIED: CapabilityState
CAPABILITY_STATE_AVAILABLE: CapabilityState
CAPABILITY_STATE_DEGRADED: CapabilityState
CAPABILITY_STATE_UNAVAILABLE: CapabilityState
CAPABILITY_STATE_UNKNOWN: CapabilityState
CAPABILITY_PROVIDER_UNSPECIFIED: CapabilityProvider
CAPABILITY_PROVIDER_CONFIG: CapabilityProvider
CAPABILITY_PROVIDER_HOST_CGROUP: CapabilityProvider
CAPABILITY_PROVIDER_FILESTORE: CapabilityProvider
CAPABILITY_PROVIDER_EROFS_PROBE: CapabilityProvider
CAPABILITY_PROVIDER_RUNC_SELF_TEST: CapabilityProvider
CAPABILITY_PROVIDER_RUNSC_SELF_TEST: CapabilityProvider
CAPABILITY_PROVIDER_NETWORK_HEALTH: CapabilityProvider
CAPABILITY_PROVIDER_DERIVED: CapabilityProvider
CAPABILITY_VALIDITY_SCOPE_UNSPECIFIED: CapabilityValidityScope
CAPABILITY_VALIDITY_SCOPE_CONFIG_STATIC: CapabilityValidityScope
CAPABILITY_VALIDITY_SCOPE_BOOT: CapabilityValidityScope
CAPABILITY_VALIDITY_SCOPE_MOUNT: CapabilityValidityScope
CAPABILITY_VALIDITY_SCOPE_RUNTIME: CapabilityValidityScope
CAPABILITY_VALIDITY_SCOPE_REFRESHABLE: CapabilityValidityScope
CAPABILITY_REASON_CODE_UNSPECIFIED: CapabilityReasonCode
CAPABILITY_REASON_CODE_AVAILABLE: CapabilityReasonCode
CAPABILITY_REASON_CODE_DISABLED: CapabilityReasonCode
CAPABILITY_REASON_CODE_PROBE_FAILED: CapabilityReasonCode
CAPABILITY_REASON_CODE_PROBE_ERROR: CapabilityReasonCode
CAPABILITY_REASON_CODE_EXPIRED: CapabilityReasonCode
CAPABILITY_REASON_CODE_IDENTITY_CHANGED: CapabilityReasonCode
CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE: CapabilityReasonCode
CAPABILITY_REASON_CODE_RECOVERY_PENDING: CapabilityReasonCode
CAPABILITY_REASON_CODE_ENFORCEMENT_LOST: CapabilityReasonCode
CAPABILITY_LOSS_POLICY_UNSPECIFIED: CapabilityLossPolicy
CAPABILITY_LOSS_POLICY_ADMISSION_ONLY: CapabilityLossPolicy
CAPABILITY_LOSS_POLICY_DEGRADE: CapabilityLossPolicy
CAPABILITY_LOSS_POLICY_FAIL_STOP: CapabilityLossPolicy
CAPABILITY_CONDITION_STATE_UNSPECIFIED: CapabilityConditionState
CAPABILITY_CONDITION_STATE_HEALTHY: CapabilityConditionState
CAPABILITY_CONDITION_STATE_DEGRADED: CapabilityConditionState
CAPABILITY_CONDITION_STATE_FAILED: CapabilityConditionState
CAPABILITY_CONDITION_STATE_UNKNOWN: CapabilityConditionState

class ExtensionCapability(_message.Message):
    __slots__ = ("name", "value")
    NAME_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    name: str
    value: str
    def __init__(self, name: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...

class CapabilityKey(_message.Message):
    __slots__ = ("platform", "extension")
    PLATFORM_FIELD_NUMBER: _ClassVar[int]
    EXTENSION_FIELD_NUMBER: _ClassVar[int]
    platform: PlatformCapability
    extension: ExtensionCapability
    def __init__(self, platform: _Optional[_Union[PlatformCapability, str]] = ..., extension: _Optional[_Union[ExtensionCapability, _Mapping]] = ...) -> None: ...

class CapabilityEvidence(_message.Message):
    __slots__ = ("evidence_id", "boot_id", "mount_identity", "runtime_name", "runtime_binary_digest", "config_digest")
    EVIDENCE_ID_FIELD_NUMBER: _ClassVar[int]
    BOOT_ID_FIELD_NUMBER: _ClassVar[int]
    MOUNT_IDENTITY_FIELD_NUMBER: _ClassVar[int]
    RUNTIME_NAME_FIELD_NUMBER: _ClassVar[int]
    RUNTIME_BINARY_DIGEST_FIELD_NUMBER: _ClassVar[int]
    CONFIG_DIGEST_FIELD_NUMBER: _ClassVar[int]
    evidence_id: str
    boot_id: str
    mount_identity: str
    runtime_name: str
    runtime_binary_digest: str
    config_digest: str
    def __init__(self, evidence_id: _Optional[str] = ..., boot_id: _Optional[str] = ..., mount_identity: _Optional[str] = ..., runtime_name: _Optional[str] = ..., runtime_binary_digest: _Optional[str] = ..., config_digest: _Optional[str] = ...) -> None: ...

class CapabilityEvidenceReference(_message.Message):
    __slots__ = ("key", "evidence_id", "evidence")
    KEY_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_ID_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    key: CapabilityKey
    evidence_id: str
    evidence: CapabilityEvidence
    def __init__(self, key: _Optional[_Union[CapabilityKey, _Mapping]] = ..., evidence_id: _Optional[str] = ..., evidence: _Optional[_Union[CapabilityEvidence, _Mapping]] = ...) -> None: ...

class CapabilityObservation(_message.Message):
    __slots__ = ("key", "state", "provider", "validity_scope", "observed_at", "valid_until", "evidence", "dependencies", "reason_code", "reason")
    KEY_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    PROVIDER_FIELD_NUMBER: _ClassVar[int]
    VALIDITY_SCOPE_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_AT_FIELD_NUMBER: _ClassVar[int]
    VALID_UNTIL_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    DEPENDENCIES_FIELD_NUMBER: _ClassVar[int]
    REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    key: CapabilityKey
    state: CapabilityState
    provider: CapabilityProvider
    validity_scope: CapabilityValidityScope
    observed_at: _timestamp_pb2.Timestamp
    valid_until: _timestamp_pb2.Timestamp
    evidence: CapabilityEvidence
    dependencies: _containers.RepeatedCompositeFieldContainer[CapabilityEvidenceReference]
    reason_code: CapabilityReasonCode
    reason: str
    def __init__(self, key: _Optional[_Union[CapabilityKey, _Mapping]] = ..., state: _Optional[_Union[CapabilityState, str]] = ..., provider: _Optional[_Union[CapabilityProvider, str]] = ..., validity_scope: _Optional[_Union[CapabilityValidityScope, str]] = ..., observed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., valid_until: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., evidence: _Optional[_Union[CapabilityEvidence, _Mapping]] = ..., dependencies: _Optional[_Iterable[_Union[CapabilityEvidenceReference, _Mapping]]] = ..., reason_code: _Optional[_Union[CapabilityReasonCode, str]] = ..., reason: _Optional[str] = ...) -> None: ...

class CapabilitySnapshot(_message.Message):
    __slots__ = ("node_instance_id", "sequence", "snapshot_id", "collected_at", "observations")
    NODE_INSTANCE_ID_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    SNAPSHOT_ID_FIELD_NUMBER: _ClassVar[int]
    COLLECTED_AT_FIELD_NUMBER: _ClassVar[int]
    OBSERVATIONS_FIELD_NUMBER: _ClassVar[int]
    node_instance_id: str
    sequence: int
    snapshot_id: str
    collected_at: _timestamp_pb2.Timestamp
    observations: _containers.RepeatedCompositeFieldContainer[CapabilityObservation]
    def __init__(self, node_instance_id: _Optional[str] = ..., sequence: _Optional[int] = ..., snapshot_id: _Optional[str] = ..., collected_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., observations: _Optional[_Iterable[_Union[CapabilityObservation, _Mapping]]] = ...) -> None: ...

class ExtensionCapabilityRequirement(_message.Message):
    __slots__ = ("capability",)
    CAPABILITY_FIELD_NUMBER: _ClassVar[int]
    capability: ExtensionCapability
    def __init__(self, capability: _Optional[_Union[ExtensionCapability, _Mapping]] = ...) -> None: ...

class CapabilityDependency(_message.Message):
    __slots__ = ("key", "loss_policy", "selected_evidence", "dependency_evidence")
    KEY_FIELD_NUMBER: _ClassVar[int]
    LOSS_POLICY_FIELD_NUMBER: _ClassVar[int]
    SELECTED_EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    DEPENDENCY_EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    key: CapabilityKey
    loss_policy: CapabilityLossPolicy
    selected_evidence: CapabilityEvidence
    dependency_evidence: _containers.RepeatedCompositeFieldContainer[CapabilityEvidenceReference]
    def __init__(self, key: _Optional[_Union[CapabilityKey, _Mapping]] = ..., loss_policy: _Optional[_Union[CapabilityLossPolicy, str]] = ..., selected_evidence: _Optional[_Union[CapabilityEvidence, _Mapping]] = ..., dependency_evidence: _Optional[_Iterable[_Union[CapabilityEvidenceReference, _Mapping]]] = ...) -> None: ...

class CapabilityDependencySet(_message.Message):
    __slots__ = ("dependencies",)
    DEPENDENCIES_FIELD_NUMBER: _ClassVar[int]
    dependencies: _containers.RepeatedCompositeFieldContainer[CapabilityDependency]
    def __init__(self, dependencies: _Optional[_Iterable[_Union[CapabilityDependency, _Mapping]]] = ...) -> None: ...

class CapabilityCondition(_message.Message):
    __slots__ = ("key", "state", "reason_code", "message", "observed_at", "evidence")
    KEY_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    REASON_CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    OBSERVED_AT_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    key: CapabilityKey
    state: CapabilityConditionState
    reason_code: CapabilityReasonCode
    message: str
    observed_at: _timestamp_pb2.Timestamp
    evidence: CapabilityEvidence
    def __init__(self, key: _Optional[_Union[CapabilityKey, _Mapping]] = ..., state: _Optional[_Union[CapabilityConditionState, str]] = ..., reason_code: _Optional[_Union[CapabilityReasonCode, str]] = ..., message: _Optional[str] = ..., observed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., evidence: _Optional[_Union[CapabilityEvidence, _Mapping]] = ...) -> None: ...

class CapabilityConditionSet(_message.Message):
    __slots__ = ("conditions",)
    CONDITIONS_FIELD_NUMBER: _ClassVar[int]
    conditions: _containers.RepeatedCompositeFieldContainer[CapabilityCondition]
    def __init__(self, conditions: _Optional[_Iterable[_Union[CapabilityCondition, _Mapping]]] = ...) -> None: ...
