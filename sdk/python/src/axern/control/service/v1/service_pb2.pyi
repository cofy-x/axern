from axern.control.common.v1 import common_pb2 as _common_pb2
from axern.control.service.v1 import service_event_pb2 as _service_event_pb2
from axern.control.service.v1 import service_replica_pb2 as _service_replica_pb2
from axern.control.service.v1 import service_types_pb2 as _service_types_pb2
from google.protobuf import field_mask_pb2 as _field_mask_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CreateServiceRequest(_message.Message):
    __slots__ = ("namespace", "environment_id", "replicas", "config", "labels", "rollout_policy", "readiness_probe", "liveness_probe", "autoscaling_policy")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    REPLICAS_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    ROLLOUT_POLICY_FIELD_NUMBER: _ClassVar[int]
    READINESS_PROBE_FIELD_NUMBER: _ClassVar[int]
    LIVENESS_PROBE_FIELD_NUMBER: _ClassVar[int]
    AUTOSCALING_POLICY_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    environment_id: str
    replicas: int
    config: _common_pb2.ExecutionConfig
    labels: _containers.ScalarMap[str, str]
    rollout_policy: _service_types_pb2.ServiceRolloutPolicy
    readiness_probe: _service_types_pb2.ServiceProbe
    liveness_probe: _service_types_pb2.ServiceProbe
    autoscaling_policy: _service_types_pb2.ServiceAutoscalingPolicy
    def __init__(self, namespace: _Optional[str] = ..., environment_id: _Optional[str] = ..., replicas: _Optional[int] = ..., config: _Optional[_Union[_common_pb2.ExecutionConfig, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., rollout_policy: _Optional[_Union[_service_types_pb2.ServiceRolloutPolicy, _Mapping]] = ..., readiness_probe: _Optional[_Union[_service_types_pb2.ServiceProbe, _Mapping]] = ..., liveness_probe: _Optional[_Union[_service_types_pb2.ServiceProbe, _Mapping]] = ..., autoscaling_policy: _Optional[_Union[_service_types_pb2.ServiceAutoscalingPolicy, _Mapping]] = ...) -> None: ...

class CreateServiceResponse(_message.Message):
    __slots__ = ("service",)
    SERVICE_FIELD_NUMBER: _ClassVar[int]
    service: _service_types_pb2.Service
    def __init__(self, service: _Optional[_Union[_service_types_pb2.Service, _Mapping]] = ...) -> None: ...

class GetServiceRequest(_message.Message):
    __slots__ = ("service_id",)
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    def __init__(self, service_id: _Optional[str] = ...) -> None: ...

class GetServiceResponse(_message.Message):
    __slots__ = ("service",)
    SERVICE_FIELD_NUMBER: _ClassVar[int]
    service: _service_types_pb2.Service
    def __init__(self, service: _Optional[_Union[_service_types_pb2.Service, _Mapping]] = ...) -> None: ...

class WatchServiceRequest(_message.Message):
    __slots__ = ("service_id", "after_version")
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    AFTER_VERSION_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    after_version: int
    def __init__(self, service_id: _Optional[str] = ..., after_version: _Optional[int] = ...) -> None: ...

class WatchServiceResponse(_message.Message):
    __slots__ = ("service",)
    SERVICE_FIELD_NUMBER: _ClassVar[int]
    service: _service_types_pb2.Service
    def __init__(self, service: _Optional[_Union[_service_types_pb2.Service, _Mapping]] = ...) -> None: ...

class ListServicesRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: _service_types_pb2.ServiceListFilter
    def __init__(self, filter: _Optional[_Union[_service_types_pb2.ServiceListFilter, _Mapping]] = ...) -> None: ...

class ListServicesResponse(_message.Message):
    __slots__ = ("services", "next_cursor")
    SERVICES_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    services: _containers.RepeatedCompositeFieldContainer[_service_types_pb2.Service]
    next_cursor: str
    def __init__(self, services: _Optional[_Iterable[_Union[_service_types_pb2.Service, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class UpdateServiceRequest(_message.Message):
    __slots__ = ("service_id", "expected_version", "replicas", "config", "labels", "update_mask", "rollout_policy", "environment_id", "readiness_probe", "liveness_probe", "autoscaling_policy")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    REPLICAS_FIELD_NUMBER: _ClassVar[int]
    CONFIG_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    UPDATE_MASK_FIELD_NUMBER: _ClassVar[int]
    ROLLOUT_POLICY_FIELD_NUMBER: _ClassVar[int]
    ENVIRONMENT_ID_FIELD_NUMBER: _ClassVar[int]
    READINESS_PROBE_FIELD_NUMBER: _ClassVar[int]
    LIVENESS_PROBE_FIELD_NUMBER: _ClassVar[int]
    AUTOSCALING_POLICY_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    expected_version: int
    replicas: int
    config: _common_pb2.ExecutionConfig
    labels: _containers.ScalarMap[str, str]
    update_mask: _field_mask_pb2.FieldMask
    rollout_policy: _service_types_pb2.ServiceRolloutPolicy
    environment_id: str
    readiness_probe: _service_types_pb2.ServiceProbe
    liveness_probe: _service_types_pb2.ServiceProbe
    autoscaling_policy: _service_types_pb2.ServiceAutoscalingPolicy
    def __init__(self, service_id: _Optional[str] = ..., expected_version: _Optional[int] = ..., replicas: _Optional[int] = ..., config: _Optional[_Union[_common_pb2.ExecutionConfig, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., update_mask: _Optional[_Union[_field_mask_pb2.FieldMask, _Mapping]] = ..., rollout_policy: _Optional[_Union[_service_types_pb2.ServiceRolloutPolicy, _Mapping]] = ..., environment_id: _Optional[str] = ..., readiness_probe: _Optional[_Union[_service_types_pb2.ServiceProbe, _Mapping]] = ..., liveness_probe: _Optional[_Union[_service_types_pb2.ServiceProbe, _Mapping]] = ..., autoscaling_policy: _Optional[_Union[_service_types_pb2.ServiceAutoscalingPolicy, _Mapping]] = ...) -> None: ...

class UpdateServiceResponse(_message.Message):
    __slots__ = ("service",)
    SERVICE_FIELD_NUMBER: _ClassVar[int]
    service: _service_types_pb2.Service
    def __init__(self, service: _Optional[_Union[_service_types_pb2.Service, _Mapping]] = ...) -> None: ...

class DeleteServiceRequest(_message.Message):
    __slots__ = ("service_id", "expected_version", "require_suspended", "volume_disposition")
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    REQUIRE_SUSPENDED_FIELD_NUMBER: _ClassVar[int]
    VOLUME_DISPOSITION_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    expected_version: int
    require_suspended: bool
    volume_disposition: _service_types_pb2.ServiceVolumeDisposition
    def __init__(self, service_id: _Optional[str] = ..., expected_version: _Optional[int] = ..., require_suspended: _Optional[bool] = ..., volume_disposition: _Optional[_Union[_service_types_pb2.ServiceVolumeDisposition, str]] = ...) -> None: ...

class DeleteServiceResponse(_message.Message):
    __slots__ = ("service",)
    SERVICE_FIELD_NUMBER: _ClassVar[int]
    service: _service_types_pb2.Service
    def __init__(self, service: _Optional[_Union[_service_types_pb2.Service, _Mapping]] = ...) -> None: ...
