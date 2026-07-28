import datetime

from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RolloutStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ROLLOUT_STATUS_UNSPECIFIED: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_ACCEPTED: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_PLANNING: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_QUEUED: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_RUNNING: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_CANCELLING: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_COMPLETED: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_FAILED: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_CANCELLED: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_DELETING: _ClassVar[RolloutStatus]
    ROLLOUT_STATUS_READY: _ClassVar[RolloutStatus]

class RolloutStartPolicy(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ROLLOUT_START_POLICY_UNSPECIFIED: _ClassVar[RolloutStartPolicy]
    ROLLOUT_START_POLICY_MANUAL: _ClassVar[RolloutStartPolicy]
    ROLLOUT_START_POLICY_AUTO: _ClassVar[RolloutStartPolicy]

class EpisodeStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    EPISODE_STATUS_UNSPECIFIED: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_PENDING: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_LEASED: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_STARTING: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_AGENT_RUNNING: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_VERIFYING: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_COLLECTING: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_COMPLETED: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_FAILED: _ClassVar[EpisodeStatus]
    EPISODE_STATUS_CANCELLED: _ClassVar[EpisodeStatus]

class FailureClass(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    FAILURE_CLASS_UNSPECIFIED: _ClassVar[FailureClass]
    FAILURE_CLASS_AGENT: _ClassVar[FailureClass]
    FAILURE_CLASS_VERIFIER: _ClassVar[FailureClass]
    FAILURE_CLASS_INFRASTRUCTURE: _ClassVar[FailureClass]
    FAILURE_CLASS_BUDGET: _ClassVar[FailureClass]
    FAILURE_CLASS_METERING: _ClassVar[FailureClass]

class ArtifactStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ARTIFACT_STATUS_UNSPECIFIED: _ClassVar[ArtifactStatus]
    ARTIFACT_STATUS_PENDING: _ClassVar[ArtifactStatus]
    ARTIFACT_STATUS_PRESENT: _ClassVar[ArtifactStatus]
    ARTIFACT_STATUS_MISSING: _ClassVar[ArtifactStatus]
    ARTIFACT_STATUS_FAILED: _ClassVar[ArtifactStatus]
    ARTIFACT_STATUS_DELETED: _ClassVar[ArtifactStatus]

class PreflightCheckKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PREFLIGHT_CHECK_KIND_UNSPECIFIED: _ClassVar[PreflightCheckKind]
    PREFLIGHT_CHECK_KIND_TASK_SET: _ClassVar[PreflightCheckKind]
    PREFLIGHT_CHECK_KIND_SELECTION: _ClassVar[PreflightCheckKind]
    PREFLIGHT_CHECK_KIND_PROFILE: _ClassVar[PreflightCheckKind]
    PREFLIGHT_CHECK_KIND_PROVIDER: _ClassVar[PreflightCheckKind]
    PREFLIGHT_CHECK_KIND_AGENT_BUNDLE: _ClassVar[PreflightCheckKind]
    PREFLIGHT_CHECK_KIND_RUNTIME: _ClassVar[PreflightCheckKind]
    PREFLIGHT_CHECK_KIND_WORKER_CAPABILITY: _ClassVar[PreflightCheckKind]
    PREFLIGHT_CHECK_KIND_BUDGET: _ClassVar[PreflightCheckKind]

class PreflightCheckStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PREFLIGHT_CHECK_STATUS_UNSPECIFIED: _ClassVar[PreflightCheckStatus]
    PREFLIGHT_CHECK_STATUS_PASS: _ClassVar[PreflightCheckStatus]
    PREFLIGHT_CHECK_STATUS_WARN: _ClassVar[PreflightCheckStatus]
    PREFLIGHT_CHECK_STATUS_FAIL: _ClassVar[PreflightCheckStatus]

class DiagnosisClass(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    DIAGNOSIS_CLASS_UNSPECIFIED: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_HEALTHY: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_PLANNING_REJECTED: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_QUEUE_WAITING: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_WORKER_UNAVAILABLE: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_PROFILE_PROVIDER_FAILURE: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_CAPACITY_WAIT: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_TASK_VERIFIER_FAILURE: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_INFRASTRUCTURE_FAILURE: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_BUDGET_EXHAUSTION: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_CANCEL_PENDING: _ClassVar[DiagnosisClass]
    DIAGNOSIS_CLASS_INCOMPLETE_EVIDENCE: _ClassVar[DiagnosisClass]
ROLLOUT_STATUS_UNSPECIFIED: RolloutStatus
ROLLOUT_STATUS_ACCEPTED: RolloutStatus
ROLLOUT_STATUS_PLANNING: RolloutStatus
ROLLOUT_STATUS_QUEUED: RolloutStatus
ROLLOUT_STATUS_RUNNING: RolloutStatus
ROLLOUT_STATUS_CANCELLING: RolloutStatus
ROLLOUT_STATUS_COMPLETED: RolloutStatus
ROLLOUT_STATUS_FAILED: RolloutStatus
ROLLOUT_STATUS_CANCELLED: RolloutStatus
ROLLOUT_STATUS_DELETING: RolloutStatus
ROLLOUT_STATUS_READY: RolloutStatus
ROLLOUT_START_POLICY_UNSPECIFIED: RolloutStartPolicy
ROLLOUT_START_POLICY_MANUAL: RolloutStartPolicy
ROLLOUT_START_POLICY_AUTO: RolloutStartPolicy
EPISODE_STATUS_UNSPECIFIED: EpisodeStatus
EPISODE_STATUS_PENDING: EpisodeStatus
EPISODE_STATUS_LEASED: EpisodeStatus
EPISODE_STATUS_STARTING: EpisodeStatus
EPISODE_STATUS_AGENT_RUNNING: EpisodeStatus
EPISODE_STATUS_VERIFYING: EpisodeStatus
EPISODE_STATUS_COLLECTING: EpisodeStatus
EPISODE_STATUS_COMPLETED: EpisodeStatus
EPISODE_STATUS_FAILED: EpisodeStatus
EPISODE_STATUS_CANCELLED: EpisodeStatus
FAILURE_CLASS_UNSPECIFIED: FailureClass
FAILURE_CLASS_AGENT: FailureClass
FAILURE_CLASS_VERIFIER: FailureClass
FAILURE_CLASS_INFRASTRUCTURE: FailureClass
FAILURE_CLASS_BUDGET: FailureClass
FAILURE_CLASS_METERING: FailureClass
ARTIFACT_STATUS_UNSPECIFIED: ArtifactStatus
ARTIFACT_STATUS_PENDING: ArtifactStatus
ARTIFACT_STATUS_PRESENT: ArtifactStatus
ARTIFACT_STATUS_MISSING: ArtifactStatus
ARTIFACT_STATUS_FAILED: ArtifactStatus
ARTIFACT_STATUS_DELETED: ArtifactStatus
PREFLIGHT_CHECK_KIND_UNSPECIFIED: PreflightCheckKind
PREFLIGHT_CHECK_KIND_TASK_SET: PreflightCheckKind
PREFLIGHT_CHECK_KIND_SELECTION: PreflightCheckKind
PREFLIGHT_CHECK_KIND_PROFILE: PreflightCheckKind
PREFLIGHT_CHECK_KIND_PROVIDER: PreflightCheckKind
PREFLIGHT_CHECK_KIND_AGENT_BUNDLE: PreflightCheckKind
PREFLIGHT_CHECK_KIND_RUNTIME: PreflightCheckKind
PREFLIGHT_CHECK_KIND_WORKER_CAPABILITY: PreflightCheckKind
PREFLIGHT_CHECK_KIND_BUDGET: PreflightCheckKind
PREFLIGHT_CHECK_STATUS_UNSPECIFIED: PreflightCheckStatus
PREFLIGHT_CHECK_STATUS_PASS: PreflightCheckStatus
PREFLIGHT_CHECK_STATUS_WARN: PreflightCheckStatus
PREFLIGHT_CHECK_STATUS_FAIL: PreflightCheckStatus
DIAGNOSIS_CLASS_UNSPECIFIED: DiagnosisClass
DIAGNOSIS_CLASS_HEALTHY: DiagnosisClass
DIAGNOSIS_CLASS_PLANNING_REJECTED: DiagnosisClass
DIAGNOSIS_CLASS_QUEUE_WAITING: DiagnosisClass
DIAGNOSIS_CLASS_WORKER_UNAVAILABLE: DiagnosisClass
DIAGNOSIS_CLASS_PROFILE_PROVIDER_FAILURE: DiagnosisClass
DIAGNOSIS_CLASS_CAPACITY_WAIT: DiagnosisClass
DIAGNOSIS_CLASS_TASK_VERIFIER_FAILURE: DiagnosisClass
DIAGNOSIS_CLASS_INFRASTRUCTURE_FAILURE: DiagnosisClass
DIAGNOSIS_CLASS_BUDGET_EXHAUSTION: DiagnosisClass
DIAGNOSIS_CLASS_CANCEL_PENDING: DiagnosisClass
DIAGNOSIS_CLASS_INCOMPLETE_EVIDENCE: DiagnosisClass

class TaskSetSelection(_message.Message):
    __slots__ = ("task_ids", "limit", "shard_index", "shard_count")
    TASK_IDS_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    SHARD_INDEX_FIELD_NUMBER: _ClassVar[int]
    SHARD_COUNT_FIELD_NUMBER: _ClassVar[int]
    task_ids: _containers.RepeatedScalarFieldContainer[str]
    limit: int
    shard_index: int
    shard_count: int
    def __init__(self, task_ids: _Optional[_Iterable[str]] = ..., limit: _Optional[int] = ..., shard_index: _Optional[int] = ..., shard_count: _Optional[int] = ...) -> None: ...

class RolloutAgent(_message.Message):
    __slots__ = ("name", "image", "profile", "approval_policy", "command")
    NAME_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    PROFILE_FIELD_NUMBER: _ClassVar[int]
    APPROVAL_POLICY_FIELD_NUMBER: _ClassVar[int]
    COMMAND_FIELD_NUMBER: _ClassVar[int]
    name: str
    image: str
    profile: str
    approval_policy: str
    command: str
    def __init__(self, name: _Optional[str] = ..., image: _Optional[str] = ..., profile: _Optional[str] = ..., approval_policy: _Optional[str] = ..., command: _Optional[str] = ...) -> None: ...

class PreflightCheck(_message.Message):
    __slots__ = ("kind", "status", "code", "message", "retryable", "latency_ms")
    KIND_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    RETRYABLE_FIELD_NUMBER: _ClassVar[int]
    LATENCY_MS_FIELD_NUMBER: _ClassVar[int]
    kind: PreflightCheckKind
    status: PreflightCheckStatus
    code: str
    message: str
    retryable: bool
    latency_ms: int
    def __init__(self, kind: _Optional[_Union[PreflightCheckKind, str]] = ..., status: _Optional[_Union[PreflightCheckStatus, str]] = ..., code: _Optional[str] = ..., message: _Optional[str] = ..., retryable: _Optional[bool] = ..., latency_ms: _Optional[int] = ...) -> None: ...

class PayloadVariant(_message.Message):
    __slots__ = ("format", "digest", "media_type")
    FORMAT_FIELD_NUMBER: _ClassVar[int]
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
    format: str
    digest: str
    media_type: str
    def __init__(self, format: _Optional[str] = ..., digest: _Optional[str] = ..., media_type: _Optional[str] = ...) -> None: ...

class PreflightUsage(_message.Message):
    __slots__ = ("input_tokens", "output_tokens", "cost_microusd", "estimated")
    INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_MICROUSD_FIELD_NUMBER: _ClassVar[int]
    ESTIMATED_FIELD_NUMBER: _ClassVar[int]
    input_tokens: int
    output_tokens: int
    cost_microusd: int
    estimated: bool
    def __init__(self, input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., cost_microusd: _Optional[int] = ..., estimated: _Optional[bool] = ...) -> None: ...

class PreflightReport(_message.Message):
    __slots__ = ("source_digest", "descriptor_digest", "task_count", "episode_count", "profile_id", "profile_name", "profile_version", "credential_version", "agent_bundle_digest", "payload_variants", "checks", "warnings", "usage")
    SOURCE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTOR_DIGEST_FIELD_NUMBER: _ClassVar[int]
    TASK_COUNT_FIELD_NUMBER: _ClassVar[int]
    EPISODE_COUNT_FIELD_NUMBER: _ClassVar[int]
    PROFILE_ID_FIELD_NUMBER: _ClassVar[int]
    PROFILE_NAME_FIELD_NUMBER: _ClassVar[int]
    PROFILE_VERSION_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_VERSION_FIELD_NUMBER: _ClassVar[int]
    AGENT_BUNDLE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_VARIANTS_FIELD_NUMBER: _ClassVar[int]
    CHECKS_FIELD_NUMBER: _ClassVar[int]
    WARNINGS_FIELD_NUMBER: _ClassVar[int]
    USAGE_FIELD_NUMBER: _ClassVar[int]
    source_digest: str
    descriptor_digest: str
    task_count: int
    episode_count: int
    profile_id: str
    profile_name: str
    profile_version: int
    credential_version: int
    agent_bundle_digest: str
    payload_variants: _containers.RepeatedCompositeFieldContainer[PayloadVariant]
    checks: _containers.RepeatedCompositeFieldContainer[PreflightCheck]
    warnings: _containers.RepeatedScalarFieldContainer[str]
    usage: PreflightUsage
    def __init__(self, source_digest: _Optional[str] = ..., descriptor_digest: _Optional[str] = ..., task_count: _Optional[int] = ..., episode_count: _Optional[int] = ..., profile_id: _Optional[str] = ..., profile_name: _Optional[str] = ..., profile_version: _Optional[int] = ..., credential_version: _Optional[int] = ..., agent_bundle_digest: _Optional[str] = ..., payload_variants: _Optional[_Iterable[_Union[PayloadVariant, _Mapping]]] = ..., checks: _Optional[_Iterable[_Union[PreflightCheck, _Mapping]]] = ..., warnings: _Optional[_Iterable[str]] = ..., usage: _Optional[_Union[PreflightUsage, _Mapping]] = ...) -> None: ...

class RolloutExecution(_message.Message):
    __slots__ = ("runtime_class", "concurrency", "attempts")
    RUNTIME_CLASS_FIELD_NUMBER: _ClassVar[int]
    CONCURRENCY_FIELD_NUMBER: _ClassVar[int]
    ATTEMPTS_FIELD_NUMBER: _ClassVar[int]
    runtime_class: str
    concurrency: int
    attempts: int
    def __init__(self, runtime_class: _Optional[str] = ..., concurrency: _Optional[int] = ..., attempts: _Optional[int] = ...) -> None: ...

class RolloutBudget(_message.Message):
    __slots__ = ("max_wall_time", "max_episodes", "max_tokens", "max_cost_microusd")
    MAX_WALL_TIME_FIELD_NUMBER: _ClassVar[int]
    MAX_EPISODES_FIELD_NUMBER: _ClassVar[int]
    MAX_TOKENS_FIELD_NUMBER: _ClassVar[int]
    MAX_COST_MICROUSD_FIELD_NUMBER: _ClassVar[int]
    max_wall_time: _duration_pb2.Duration
    max_episodes: int
    max_tokens: int
    max_cost_microusd: int
    def __init__(self, max_wall_time: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., max_episodes: _Optional[int] = ..., max_tokens: _Optional[int] = ..., max_cost_microusd: _Optional[int] = ...) -> None: ...

class RolloutSpec(_message.Message):
    __slots__ = ("task_set_ref", "agent", "model", "execution", "selection", "budget")
    TASK_SET_REF_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_FIELD_NUMBER: _ClassVar[int]
    SELECTION_FIELD_NUMBER: _ClassVar[int]
    BUDGET_FIELD_NUMBER: _ClassVar[int]
    task_set_ref: str
    agent: RolloutAgent
    model: str
    execution: RolloutExecution
    selection: TaskSetSelection
    budget: RolloutBudget
    def __init__(self, task_set_ref: _Optional[str] = ..., agent: _Optional[_Union[RolloutAgent, _Mapping]] = ..., model: _Optional[str] = ..., execution: _Optional[_Union[RolloutExecution, _Mapping]] = ..., selection: _Optional[_Union[TaskSetSelection, _Mapping]] = ..., budget: _Optional[_Union[RolloutBudget, _Mapping]] = ...) -> None: ...

class RolloutSummary(_message.Message):
    __slots__ = ("task_count", "episode_count", "completed_episodes", "failed_episodes", "cancelled_episodes", "passed_episodes", "input_tokens", "cached_input_tokens", "output_tokens", "cost_microusd", "total_duration_ms")
    TASK_COUNT_FIELD_NUMBER: _ClassVar[int]
    EPISODE_COUNT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_EPISODES_FIELD_NUMBER: _ClassVar[int]
    FAILED_EPISODES_FIELD_NUMBER: _ClassVar[int]
    CANCELLED_EPISODES_FIELD_NUMBER: _ClassVar[int]
    PASSED_EPISODES_FIELD_NUMBER: _ClassVar[int]
    INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    CACHED_INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_MICROUSD_FIELD_NUMBER: _ClassVar[int]
    TOTAL_DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    task_count: int
    episode_count: int
    completed_episodes: int
    failed_episodes: int
    cancelled_episodes: int
    passed_episodes: int
    input_tokens: int
    cached_input_tokens: int
    output_tokens: int
    cost_microusd: int
    total_duration_ms: int
    def __init__(self, task_count: _Optional[int] = ..., episode_count: _Optional[int] = ..., completed_episodes: _Optional[int] = ..., failed_episodes: _Optional[int] = ..., cancelled_episodes: _Optional[int] = ..., passed_episodes: _Optional[int] = ..., input_tokens: _Optional[int] = ..., cached_input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., cost_microusd: _Optional[int] = ..., total_duration_ms: _Optional[int] = ...) -> None: ...

class Rollout(_message.Message):
    __slots__ = ("id", "namespace", "spec", "status", "source_digest", "descriptor_digest", "plan_artifact_id", "summary", "message", "labels", "version", "created_at", "started_at", "completed_at", "deadline", "start_policy", "preflight", "failure_class")
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
    STATUS_FIELD_NUMBER: _ClassVar[int]
    SOURCE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTOR_DIGEST_FIELD_NUMBER: _ClassVar[int]
    PLAN_ARTIFACT_ID_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    DEADLINE_FIELD_NUMBER: _ClassVar[int]
    START_POLICY_FIELD_NUMBER: _ClassVar[int]
    PREFLIGHT_FIELD_NUMBER: _ClassVar[int]
    FAILURE_CLASS_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    spec: RolloutSpec
    status: RolloutStatus
    source_digest: str
    descriptor_digest: str
    plan_artifact_id: str
    summary: RolloutSummary
    message: str
    labels: _containers.ScalarMap[str, str]
    version: int
    created_at: _timestamp_pb2.Timestamp
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    deadline: _timestamp_pb2.Timestamp
    start_policy: RolloutStartPolicy
    preflight: PreflightReport
    failure_class: FailureClass
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., spec: _Optional[_Union[RolloutSpec, _Mapping]] = ..., status: _Optional[_Union[RolloutStatus, str]] = ..., source_digest: _Optional[str] = ..., descriptor_digest: _Optional[str] = ..., plan_artifact_id: _Optional[str] = ..., summary: _Optional[_Union[RolloutSummary, _Mapping]] = ..., message: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., deadline: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., start_policy: _Optional[_Union[RolloutStartPolicy, str]] = ..., preflight: _Optional[_Union[PreflightReport, _Mapping]] = ..., failure_class: _Optional[_Union[FailureClass, str]] = ...) -> None: ...

class ExecutionFacts(_message.Message):
    __slots__ = ("payload_format", "payload_digest", "cache_hit", "image_resolve_ms", "image_pull_ms", "cow_prepare_ms", "verifier_materialize_ms", "allocation_id", "node_id", "runtime_class", "agent_bundle_digest")
    PAYLOAD_FORMAT_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_DIGEST_FIELD_NUMBER: _ClassVar[int]
    CACHE_HIT_FIELD_NUMBER: _ClassVar[int]
    IMAGE_RESOLVE_MS_FIELD_NUMBER: _ClassVar[int]
    IMAGE_PULL_MS_FIELD_NUMBER: _ClassVar[int]
    COW_PREPARE_MS_FIELD_NUMBER: _ClassVar[int]
    VERIFIER_MATERIALIZE_MS_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    RUNTIME_CLASS_FIELD_NUMBER: _ClassVar[int]
    AGENT_BUNDLE_DIGEST_FIELD_NUMBER: _ClassVar[int]
    payload_format: str
    payload_digest: str
    cache_hit: bool
    image_resolve_ms: int
    image_pull_ms: int
    cow_prepare_ms: int
    verifier_materialize_ms: int
    allocation_id: str
    node_id: str
    runtime_class: str
    agent_bundle_digest: str
    def __init__(self, payload_format: _Optional[str] = ..., payload_digest: _Optional[str] = ..., cache_hit: _Optional[bool] = ..., image_resolve_ms: _Optional[int] = ..., image_pull_ms: _Optional[int] = ..., cow_prepare_ms: _Optional[int] = ..., verifier_materialize_ms: _Optional[int] = ..., allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., runtime_class: _Optional[str] = ..., agent_bundle_digest: _Optional[str] = ...) -> None: ...

class Episode(_message.Message):
    __slots__ = ("id", "rollout_id", "task_id", "task_digest", "attempt_index", "execution_generation", "status", "failure_class", "passed", "reward", "input_tokens", "cached_input_tokens", "output_tokens", "cost_microusd", "duration_ms", "execution_facts", "artifact_manifest_id", "message", "created_at", "started_at", "completed_at")
    ID_FIELD_NUMBER: _ClassVar[int]
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_DIGEST_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_INDEX_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_CLASS_FIELD_NUMBER: _ClassVar[int]
    PASSED_FIELD_NUMBER: _ClassVar[int]
    REWARD_FIELD_NUMBER: _ClassVar[int]
    INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    CACHED_INPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    OUTPUT_TOKENS_FIELD_NUMBER: _ClassVar[int]
    COST_MICROUSD_FIELD_NUMBER: _ClassVar[int]
    DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_FACTS_FIELD_NUMBER: _ClassVar[int]
    ARTIFACT_MANIFEST_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    STARTED_AT_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    rollout_id: str
    task_id: str
    task_digest: str
    attempt_index: int
    execution_generation: int
    status: EpisodeStatus
    failure_class: FailureClass
    passed: bool
    reward: float
    input_tokens: int
    cached_input_tokens: int
    output_tokens: int
    cost_microusd: int
    duration_ms: int
    execution_facts: ExecutionFacts
    artifact_manifest_id: str
    message: str
    created_at: _timestamp_pb2.Timestamp
    started_at: _timestamp_pb2.Timestamp
    completed_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., rollout_id: _Optional[str] = ..., task_id: _Optional[str] = ..., task_digest: _Optional[str] = ..., attempt_index: _Optional[int] = ..., execution_generation: _Optional[int] = ..., status: _Optional[_Union[EpisodeStatus, str]] = ..., failure_class: _Optional[_Union[FailureClass, str]] = ..., passed: _Optional[bool] = ..., reward: _Optional[float] = ..., input_tokens: _Optional[int] = ..., cached_input_tokens: _Optional[int] = ..., output_tokens: _Optional[int] = ..., cost_microusd: _Optional[int] = ..., duration_ms: _Optional[int] = ..., execution_facts: _Optional[_Union[ExecutionFacts, _Mapping]] = ..., artifact_manifest_id: _Optional[str] = ..., message: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., started_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class RolloutEvent(_message.Message):
    __slots__ = ("rollout_id", "sequence", "episode_id", "type", "phase", "message", "details", "created_at")
    class DetailsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    EPISODE_ID_FIELD_NUMBER: _ClassVar[int]
    TYPE_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    DETAILS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    sequence: int
    episode_id: str
    type: str
    phase: str
    message: str
    details: _containers.ScalarMap[str, str]
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, rollout_id: _Optional[str] = ..., sequence: _Optional[int] = ..., episode_id: _Optional[str] = ..., type: _Optional[str] = ..., phase: _Optional[str] = ..., message: _Optional[str] = ..., details: _Optional[_Mapping[str, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class Artifact(_message.Message):
    __slots__ = ("id", "rollout_id", "episode_id", "execution_generation", "kind", "name", "media_type", "size_bytes", "digest", "status", "message", "created_at")
    ID_FIELD_NUMBER: _ClassVar[int]
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    EPISODE_ID_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_GENERATION_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    rollout_id: str
    episode_id: str
    execution_generation: int
    kind: str
    name: str
    media_type: str
    size_bytes: int
    digest: str
    status: ArtifactStatus
    message: str
    created_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., rollout_id: _Optional[str] = ..., episode_id: _Optional[str] = ..., execution_generation: _Optional[int] = ..., kind: _Optional[str] = ..., name: _Optional[str] = ..., media_type: _Optional[str] = ..., size_bytes: _Optional[int] = ..., digest: _Optional[str] = ..., status: _Optional[_Union[ArtifactStatus, str]] = ..., message: _Optional[str] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class RolloutListFilter(_message.Message):
    __slots__ = ("namespace", "statuses", "task_set_digest", "agent", "model", "labels", "cursor", "page_size")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    STATUSES_FIELD_NUMBER: _ClassVar[int]
    TASK_SET_DIGEST_FIELD_NUMBER: _ClassVar[int]
    AGENT_FIELD_NUMBER: _ClassVar[int]
    MODEL_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    statuses: _containers.RepeatedScalarFieldContainer[RolloutStatus]
    task_set_digest: str
    agent: str
    model: str
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., statuses: _Optional[_Iterable[_Union[RolloutStatus, str]]] = ..., task_set_digest: _Optional[str] = ..., agent: _Optional[str] = ..., model: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class CreateRolloutRequest(_message.Message):
    __slots__ = ("namespace", "spec", "labels", "idempotency_key", "start_policy")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    SPEC_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    START_POLICY_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    spec: RolloutSpec
    labels: _containers.ScalarMap[str, str]
    idempotency_key: str
    start_policy: RolloutStartPolicy
    def __init__(self, namespace: _Optional[str] = ..., spec: _Optional[_Union[RolloutSpec, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., idempotency_key: _Optional[str] = ..., start_policy: _Optional[_Union[RolloutStartPolicy, str]] = ...) -> None: ...

class CreateRolloutResponse(_message.Message):
    __slots__ = ("rollout",)
    ROLLOUT_FIELD_NUMBER: _ClassVar[int]
    rollout: Rollout
    def __init__(self, rollout: _Optional[_Union[Rollout, _Mapping]] = ...) -> None: ...

class GetRolloutRequest(_message.Message):
    __slots__ = ("rollout_id",)
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    def __init__(self, rollout_id: _Optional[str] = ...) -> None: ...

class GetRolloutResponse(_message.Message):
    __slots__ = ("rollout", "episodes")
    ROLLOUT_FIELD_NUMBER: _ClassVar[int]
    EPISODES_FIELD_NUMBER: _ClassVar[int]
    rollout: Rollout
    episodes: _containers.RepeatedCompositeFieldContainer[Episode]
    def __init__(self, rollout: _Optional[_Union[Rollout, _Mapping]] = ..., episodes: _Optional[_Iterable[_Union[Episode, _Mapping]]] = ...) -> None: ...

class ListRolloutsRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: RolloutListFilter
    def __init__(self, filter: _Optional[_Union[RolloutListFilter, _Mapping]] = ...) -> None: ...

class ListRolloutsResponse(_message.Message):
    __slots__ = ("rollouts", "next_cursor")
    ROLLOUTS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    rollouts: _containers.RepeatedCompositeFieldContainer[Rollout]
    next_cursor: str
    def __init__(self, rollouts: _Optional[_Iterable[_Union[Rollout, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class WatchRolloutEventsRequest(_message.Message):
    __slots__ = ("rollout_id", "after_sequence")
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    after_sequence: int
    def __init__(self, rollout_id: _Optional[str] = ..., after_sequence: _Optional[int] = ...) -> None: ...

class WatchRolloutEventsResponse(_message.Message):
    __slots__ = ("events", "current_sequence", "terminal")
    EVENTS_FIELD_NUMBER: _ClassVar[int]
    CURRENT_SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    TERMINAL_FIELD_NUMBER: _ClassVar[int]
    events: _containers.RepeatedCompositeFieldContainer[RolloutEvent]
    current_sequence: int
    terminal: bool
    def __init__(self, events: _Optional[_Iterable[_Union[RolloutEvent, _Mapping]]] = ..., current_sequence: _Optional[int] = ..., terminal: _Optional[bool] = ...) -> None: ...

class CancelRolloutRequest(_message.Message):
    __slots__ = ("rollout_id",)
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    def __init__(self, rollout_id: _Optional[str] = ...) -> None: ...

class CancelRolloutResponse(_message.Message):
    __slots__ = ("rollout",)
    ROLLOUT_FIELD_NUMBER: _ClassVar[int]
    rollout: Rollout
    def __init__(self, rollout: _Optional[_Union[Rollout, _Mapping]] = ...) -> None: ...

class RetryRolloutRequest(_message.Message):
    __slots__ = ("rollout_id",)
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    def __init__(self, rollout_id: _Optional[str] = ...) -> None: ...

class RetryRolloutResponse(_message.Message):
    __slots__ = ("rollout",)
    ROLLOUT_FIELD_NUMBER: _ClassVar[int]
    rollout: Rollout
    def __init__(self, rollout: _Optional[_Union[Rollout, _Mapping]] = ...) -> None: ...

class StartRolloutRequest(_message.Message):
    __slots__ = ("rollout_id", "idempotency_key")
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    idempotency_key: str
    def __init__(self, rollout_id: _Optional[str] = ..., idempotency_key: _Optional[str] = ...) -> None: ...

class StartRolloutResponse(_message.Message):
    __slots__ = ("rollout",)
    ROLLOUT_FIELD_NUMBER: _ClassVar[int]
    rollout: Rollout
    def __init__(self, rollout: _Optional[_Union[Rollout, _Mapping]] = ...) -> None: ...

class DiagnoseRolloutRequest(_message.Message):
    __slots__ = ("rollout_id",)
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    def __init__(self, rollout_id: _Optional[str] = ...) -> None: ...

class DiagnoseRolloutResponse(_message.Message):
    __slots__ = ("rollout", "diagnosis", "code", "summary", "recommended_action", "artifacts")
    ROLLOUT_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSIS_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    SUMMARY_FIELD_NUMBER: _ClassVar[int]
    RECOMMENDED_ACTION_FIELD_NUMBER: _ClassVar[int]
    ARTIFACTS_FIELD_NUMBER: _ClassVar[int]
    rollout: Rollout
    diagnosis: DiagnosisClass
    code: str
    summary: str
    recommended_action: str
    artifacts: _containers.RepeatedCompositeFieldContainer[Artifact]
    def __init__(self, rollout: _Optional[_Union[Rollout, _Mapping]] = ..., diagnosis: _Optional[_Union[DiagnosisClass, str]] = ..., code: _Optional[str] = ..., summary: _Optional[str] = ..., recommended_action: _Optional[str] = ..., artifacts: _Optional[_Iterable[_Union[Artifact, _Mapping]]] = ...) -> None: ...

class TaskComparison(_message.Message):
    __slots__ = ("task_id", "task_digest", "rewards", "passed", "cost_microusd", "comparable", "reason")
    class RewardsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: float
        def __init__(self, key: _Optional[str] = ..., value: _Optional[float] = ...) -> None: ...
    class PassedEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: bool
        def __init__(self, key: _Optional[str] = ..., value: _Optional[bool] = ...) -> None: ...
    class CostMicrousdEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: int
        def __init__(self, key: _Optional[str] = ..., value: _Optional[int] = ...) -> None: ...
    TASK_ID_FIELD_NUMBER: _ClassVar[int]
    TASK_DIGEST_FIELD_NUMBER: _ClassVar[int]
    REWARDS_FIELD_NUMBER: _ClassVar[int]
    PASSED_FIELD_NUMBER: _ClassVar[int]
    COST_MICROUSD_FIELD_NUMBER: _ClassVar[int]
    COMPARABLE_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    task_id: str
    task_digest: str
    rewards: _containers.ScalarMap[str, float]
    passed: _containers.ScalarMap[str, bool]
    cost_microusd: _containers.ScalarMap[str, int]
    comparable: bool
    reason: str
    def __init__(self, task_id: _Optional[str] = ..., task_digest: _Optional[str] = ..., rewards: _Optional[_Mapping[str, float]] = ..., passed: _Optional[_Mapping[str, bool]] = ..., cost_microusd: _Optional[_Mapping[str, int]] = ..., comparable: _Optional[bool] = ..., reason: _Optional[str] = ...) -> None: ...

class CompareRolloutsRequest(_message.Message):
    __slots__ = ("rollout_ids",)
    ROLLOUT_IDS_FIELD_NUMBER: _ClassVar[int]
    rollout_ids: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, rollout_ids: _Optional[_Iterable[str]] = ...) -> None: ...

class CompareRolloutsResponse(_message.Message):
    __slots__ = ("rollouts", "tasks")
    ROLLOUTS_FIELD_NUMBER: _ClassVar[int]
    TASKS_FIELD_NUMBER: _ClassVar[int]
    rollouts: _containers.RepeatedCompositeFieldContainer[Rollout]
    tasks: _containers.RepeatedCompositeFieldContainer[TaskComparison]
    def __init__(self, rollouts: _Optional[_Iterable[_Union[Rollout, _Mapping]]] = ..., tasks: _Optional[_Iterable[_Union[TaskComparison, _Mapping]]] = ...) -> None: ...

class ListArtifactsRequest(_message.Message):
    __slots__ = ("rollout_id", "episode_id")
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    EPISODE_ID_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    episode_id: str
    def __init__(self, rollout_id: _Optional[str] = ..., episode_id: _Optional[str] = ...) -> None: ...

class ListArtifactsResponse(_message.Message):
    __slots__ = ("artifacts",)
    ARTIFACTS_FIELD_NUMBER: _ClassVar[int]
    artifacts: _containers.RepeatedCompositeFieldContainer[Artifact]
    def __init__(self, artifacts: _Optional[_Iterable[_Union[Artifact, _Mapping]]] = ...) -> None: ...

class PrepareArtifactDownloadRequest(_message.Message):
    __slots__ = ("artifact_id",)
    ARTIFACT_ID_FIELD_NUMBER: _ClassVar[int]
    artifact_id: str
    def __init__(self, artifact_id: _Optional[str] = ...) -> None: ...

class PrepareArtifactDownloadResponse(_message.Message):
    __slots__ = ("artifact", "ticket", "expires_at")
    ARTIFACT_FIELD_NUMBER: _ClassVar[int]
    TICKET_FIELD_NUMBER: _ClassVar[int]
    EXPIRES_AT_FIELD_NUMBER: _ClassVar[int]
    artifact: Artifact
    ticket: str
    expires_at: _timestamp_pb2.Timestamp
    def __init__(self, artifact: _Optional[_Union[Artifact, _Mapping]] = ..., ticket: _Optional[str] = ..., expires_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class DeleteRolloutRequest(_message.Message):
    __slots__ = ("rollout_id",)
    ROLLOUT_ID_FIELD_NUMBER: _ClassVar[int]
    rollout_id: str
    def __init__(self, rollout_id: _Optional[str] = ...) -> None: ...

class DeleteRolloutResponse(_message.Message):
    __slots__ = ("rollout",)
    ROLLOUT_FIELD_NUMBER: _ClassVar[int]
    rollout: Rollout
    def __init__(self, rollout: _Optional[_Union[Rollout, _Mapping]] = ...) -> None: ...
