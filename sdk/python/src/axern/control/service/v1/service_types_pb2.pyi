import datetime

from axern.control.common.v1 import common_pb2 as _common_pb2
from google.protobuf import duration_pb2 as _duration_pb2
from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ServiceStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SERVICE_STATUS_UNSPECIFIED: _ClassVar[ServiceStatus]
    SERVICE_STATUS_RECONCILING: _ClassVar[ServiceStatus]
    SERVICE_STATUS_READY: _ClassVar[ServiceStatus]
    SERVICE_STATUS_DEGRADED: _ClassVar[ServiceStatus]
    SERVICE_STATUS_FAILED: _ClassVar[ServiceStatus]
    SERVICE_STATUS_DELETING: _ClassVar[ServiceStatus]
    SERVICE_STATUS_DELETED: _ClassVar[ServiceStatus]

class ServiceVolumeDisposition(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SERVICE_VOLUME_DISPOSITION_UNSPECIFIED: _ClassVar[ServiceVolumeDisposition]
    SERVICE_VOLUME_DISPOSITION_RETAIN: _ClassVar[ServiceVolumeDisposition]
    SERVICE_VOLUME_DISPOSITION_DELETE: _ClassVar[ServiceVolumeDisposition]

class ServiceDeletionPhase(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SERVICE_DELETION_PHASE_UNSPECIFIED: _ClassVar[ServiceDeletionPhase]
    SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS: _ClassVar[ServiceDeletionPhase]
    SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES: _ClassVar[ServiceDeletionPhase]
    SERVICE_DELETION_PHASE_COMPLETE: _ClassVar[ServiceDeletionPhase]

class ServiceRolloutPhase(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SERVICE_ROLLOUT_PHASE_UNSPECIFIED: _ClassVar[ServiceRolloutPhase]
    SERVICE_ROLLOUT_PHASE_ADMITTING_REPLACEMENT: _ClassVar[ServiceRolloutPhase]
    SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY: _ClassVar[ServiceRolloutPhase]
    SERVICE_ROLLOUT_PHASE_DRAINING_OUTDATED: _ClassVar[ServiceRolloutPhase]
    SERVICE_ROLLOUT_PHASE_BLOCKED: _ClassVar[ServiceRolloutPhase]

class ServiceAutoscalingAction(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SERVICE_AUTOSCALING_ACTION_UNSPECIFIED: _ClassVar[ServiceAutoscalingAction]
    SERVICE_AUTOSCALING_ACTION_SCALED_UP: _ClassVar[ServiceAutoscalingAction]
    SERVICE_AUTOSCALING_ACTION_SCALED_DOWN: _ClassVar[ServiceAutoscalingAction]
    SERVICE_AUTOSCALING_ACTION_NO_CHANGE: _ClassVar[ServiceAutoscalingAction]

class HttpProbeScheme(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    HTTP_PROBE_SCHEME_UNSPECIFIED: _ClassVar[HttpProbeScheme]
    HTTP_PROBE_SCHEME_HTTP: _ClassVar[HttpProbeScheme]
    HTTP_PROBE_SCHEME_HTTPS: _ClassVar[HttpProbeScheme]
SERVICE_STATUS_UNSPECIFIED: ServiceStatus
SERVICE_STATUS_RECONCILING: ServiceStatus
SERVICE_STATUS_READY: ServiceStatus
SERVICE_STATUS_DEGRADED: ServiceStatus
SERVICE_STATUS_FAILED: ServiceStatus
SERVICE_STATUS_DELETING: ServiceStatus
SERVICE_STATUS_DELETED: ServiceStatus
SERVICE_VOLUME_DISPOSITION_UNSPECIFIED: ServiceVolumeDisposition
SERVICE_VOLUME_DISPOSITION_RETAIN: ServiceVolumeDisposition
SERVICE_VOLUME_DISPOSITION_DELETE: ServiceVolumeDisposition
SERVICE_DELETION_PHASE_UNSPECIFIED: ServiceDeletionPhase
SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS: ServiceDeletionPhase
SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES: ServiceDeletionPhase
SERVICE_DELETION_PHASE_COMPLETE: ServiceDeletionPhase
SERVICE_ROLLOUT_PHASE_UNSPECIFIED: ServiceRolloutPhase
SERVICE_ROLLOUT_PHASE_ADMITTING_REPLACEMENT: ServiceRolloutPhase
SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY: ServiceRolloutPhase
SERVICE_ROLLOUT_PHASE_DRAINING_OUTDATED: ServiceRolloutPhase
SERVICE_ROLLOUT_PHASE_BLOCKED: ServiceRolloutPhase
SERVICE_AUTOSCALING_ACTION_UNSPECIFIED: ServiceAutoscalingAction
SERVICE_AUTOSCALING_ACTION_SCALED_UP: ServiceAutoscalingAction
SERVICE_AUTOSCALING_ACTION_SCALED_DOWN: ServiceAutoscalingAction
SERVICE_AUTOSCALING_ACTION_NO_CHANGE: ServiceAutoscalingAction
HTTP_PROBE_SCHEME_UNSPECIFIED: HttpProbeScheme
HTTP_PROBE_SCHEME_HTTP: HttpProbeScheme
HTTP_PROBE_SCHEME_HTTPS: HttpProbeScheme

class ServiceDeletionStatus(_message.Message):
    __slots__ = ("phase", "volume_disposition", "claim_ids", "message", "completed_at")
    PHASE_FIELD_NUMBER: _ClassVar[int]
    VOLUME_DISPOSITION_FIELD_NUMBER: _ClassVar[int]
    CLAIM_IDS_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    COMPLETED_AT_FIELD_NUMBER: _ClassVar[int]
    phase: ServiceDeletionPhase
    volume_disposition: ServiceVolumeDisposition
    claim_ids: _containers.RepeatedScalarFieldContainer[str]
    message: str
    completed_at: _timestamp_pb2.Timestamp
    def __init__(self, phase: _Optional[_Union[ServiceDeletionPhase, str]] = ..., volume_disposition: _Optional[_Union[ServiceVolumeDisposition, str]] = ..., claim_ids: _Optional[_Iterable[str]] = ..., message: _Optional[str] = ..., completed_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class ServiceProbe(_message.Message):
    __slots__ = ("http", "tcp", "initial_delay", "period", "timeout", "success_threshold", "failure_threshold")
    HTTP_FIELD_NUMBER: _ClassVar[int]
    TCP_FIELD_NUMBER: _ClassVar[int]
    INITIAL_DELAY_FIELD_NUMBER: _ClassVar[int]
    PERIOD_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_THRESHOLD_FIELD_NUMBER: _ClassVar[int]
    FAILURE_THRESHOLD_FIELD_NUMBER: _ClassVar[int]
    http: HttpProbe
    tcp: TcpProbe
    initial_delay: _duration_pb2.Duration
    period: _duration_pb2.Duration
    timeout: _duration_pb2.Duration
    success_threshold: int
    failure_threshold: int
    def __init__(self, http: _Optional[_Union[HttpProbe, _Mapping]] = ..., tcp: _Optional[_Union[TcpProbe, _Mapping]] = ..., initial_delay: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., period: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., timeout: _Optional[_Union[datetime.timedelta, _duration_pb2.Duration, _Mapping]] = ..., success_threshold: _Optional[int] = ..., failure_threshold: _Optional[int] = ...) -> None: ...

class HttpProbe(_message.Message):
    __slots__ = ("port", "path", "scheme")
    PORT_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    SCHEME_FIELD_NUMBER: _ClassVar[int]
    port: int
    path: str
    scheme: HttpProbeScheme
    def __init__(self, port: _Optional[int] = ..., path: _Optional[str] = ..., scheme: _Optional[_Union[HttpProbeScheme, str]] = ...) -> None: ...

class TcpProbe(_message.Message):
    __slots__ = ("port",)
    PORT_FIELD_NUMBER: _ClassVar[int]
    port: int
    def __init__(self, port: _Optional[int] = ...) -> None: ...

class ServiceAutoscalingSchedule(_message.Message):
    __slots__ = ("name", "cron_utc", "replicas")
    NAME_FIELD_NUMBER: _ClassVar[int]
    CRON_UTC_FIELD_NUMBER: _ClassVar[int]
    REPLICAS_FIELD_NUMBER: _ClassVar[int]
    name: str
    cron_utc: str
    replicas: int
    def __init__(self, name: _Optional[str] = ..., cron_utc: _Optional[str] = ..., replicas: _Optional[int] = ...) -> None: ...

class ServiceAutoscalingPolicy(_message.Message):
    __slots__ = ("min_replicas", "max_replicas", "schedules")
    MIN_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    MAX_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    SCHEDULES_FIELD_NUMBER: _ClassVar[int]
    min_replicas: int
    max_replicas: int
    schedules: _containers.RepeatedCompositeFieldContainer[ServiceAutoscalingSchedule]
    def __init__(self, min_replicas: _Optional[int] = ..., max_replicas: _Optional[int] = ..., schedules: _Optional[_Iterable[_Union[ServiceAutoscalingSchedule, _Mapping]]] = ...) -> None: ...

class ServiceAutoscalingStatus(_message.Message):
    __slots__ = ("current_desired_replicas", "effective_min_replicas", "effective_max_replicas", "active_schedule_name", "active_schedule_replicas", "last_evaluated_at", "last_action", "message")
    CURRENT_DESIRED_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    EFFECTIVE_MIN_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    EFFECTIVE_MAX_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_SCHEDULE_NAME_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_SCHEDULE_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    LAST_EVALUATED_AT_FIELD_NUMBER: _ClassVar[int]
    LAST_ACTION_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    current_desired_replicas: int
    effective_min_replicas: int
    effective_max_replicas: int
    active_schedule_name: str
    active_schedule_replicas: int
    last_evaluated_at: _timestamp_pb2.Timestamp
    last_action: ServiceAutoscalingAction
    message: str
    def __init__(self, current_desired_replicas: _Optional[int] = ..., effective_min_replicas: _Optional[int] = ..., effective_max_replicas: _Optional[int] = ..., active_schedule_name: _Optional[str] = ..., active_schedule_replicas: _Optional[int] = ..., last_evaluated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., last_action: _Optional[_Union[ServiceAutoscalingAction, str]] = ..., message: _Optional[str] = ...) -> None: ...

class Service(_message.Message):
    __slots__ = ("id", "namespace", "environment_id", "replicas", "ready_replicas", "unhealthy_replicas", "rollout_policy", "rollout_status", "status", "config", "allocation_ids", "labels", "version", "created_at", "updated_at", "message", "readiness_probe", "liveness_probe", "autoscaling_policy", "autoscaling_status", "diagnostic_code", "deletion_status")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    REPLICAS_FIELD_NUMBER: _ClassVar[int]
    READY_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    UNHEALTHY_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    ROLLOUT_POLICY_FIELD_NUMBER: _ClassVar[int]
    ROLLOUT_STATUS_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    ALLOCATION_IDS_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    READINESS_PROBE_FIELD_NUMBER: _ClassVar[int]
    LIVENESS_PROBE_FIELD_NUMBER: _ClassVar[int]
    AUTOSCALING_POLICY_FIELD_NUMBER: _ClassVar[int]
    AUTOSCALING_STATUS_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_CODE_FIELD_NUMBER: _ClassVar[int]
    DELETION_STATUS_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    environment_id: str
    replicas: int
    ready_replicas: int
    unhealthy_replicas: int
    rollout_policy: ServiceRolloutPolicy
    rollout_status: ServiceRolloutStatus
    status: ServiceStatus
    config: _common_pb2.ExecutionConfig
    allocation_ids: _containers.RepeatedScalarFieldContainer[str]
    labels: _containers.ScalarMap[str, str]
    version: int
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    message: str
    readiness_probe: ServiceProbe
    liveness_probe: ServiceProbe
    autoscaling_policy: ServiceAutoscalingPolicy
    autoscaling_status: ServiceAutoscalingStatus
    diagnostic_code: _common_pb2.WorkloadDiagnosticCode
    deletion_status: ServiceDeletionStatus
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., environment_id: _Optional[str] = ..., replicas: _Optional[int] = ..., ready_replicas: _Optional[int] = ..., unhealthy_replicas: _Optional[int] = ..., rollout_policy: _Optional[_Union[ServiceRolloutPolicy, _Mapping]] = ..., rollout_status: _Optional[_Union[ServiceRolloutStatus, _Mapping]] = ..., status: _Optional[_Union[ServiceStatus, str]] = ..., config: _Optional[_Union[_common_pb2.ExecutionConfig, _Mapping]] = ..., allocation_ids: _Optional[_Iterable[str]] = ..., labels: _Optional[_Mapping[str, str]] = ..., version: _Optional[int] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., message: _Optional[str] = ..., readiness_probe: _Optional[_Union[ServiceProbe, _Mapping]] = ..., liveness_probe: _Optional[_Union[ServiceProbe, _Mapping]] = ..., autoscaling_policy: _Optional[_Union[ServiceAutoscalingPolicy, _Mapping]] = ..., autoscaling_status: _Optional[_Union[ServiceAutoscalingStatus, _Mapping]] = ..., diagnostic_code: _Optional[_Union[_common_pb2.WorkloadDiagnosticCode, str]] = ..., deletion_status: _Optional[_Union[ServiceDeletionStatus, _Mapping]] = ...) -> None: ...

class ServiceListFilter(_message.Message):
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
    statuses: _containers.RepeatedScalarFieldContainer[ServiceStatus]
    labels: _containers.ScalarMap[str, str]
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., statuses: _Optional[_Iterable[_Union[ServiceStatus, str]]] = ..., labels: _Optional[_Mapping[str, str]] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class ServiceRolloutPolicy(_message.Message):
    __slots__ = ("max_surge", "max_unavailable")
    MAX_SURGE_FIELD_NUMBER: _ClassVar[int]
    MAX_UNAVAILABLE_FIELD_NUMBER: _ClassVar[int]
    max_surge: int
    max_unavailable: int
    def __init__(self, max_surge: _Optional[int] = ..., max_unavailable: _Optional[int] = ...) -> None: ...

class ServiceRolloutStatus(_message.Message):
    __slots__ = ("in_progress", "current_replicas", "updated_ready_replicas", "outdated_replicas", "phase", "diagnostic_code", "diagnostic_message")
    IN_PROGRESS_FIELD_NUMBER: _ClassVar[int]
    CURRENT_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    UPDATED_READY_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    OUTDATED_REPLICAS_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_CODE_FIELD_NUMBER: _ClassVar[int]
    DIAGNOSTIC_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    in_progress: bool
    current_replicas: int
    updated_ready_replicas: int
    outdated_replicas: int
    phase: ServiceRolloutPhase
    diagnostic_code: _common_pb2.WorkloadDiagnosticCode
    diagnostic_message: str
    def __init__(self, in_progress: _Optional[bool] = ..., current_replicas: _Optional[int] = ..., updated_ready_replicas: _Optional[int] = ..., outdated_replicas: _Optional[int] = ..., phase: _Optional[_Union[ServiceRolloutPhase, str]] = ..., diagnostic_code: _Optional[_Union[_common_pb2.WorkloadDiagnosticCode, str]] = ..., diagnostic_message: _Optional[str] = ...) -> None: ...
