from axern.control.common.v1 import common_pb2 as _common_pb2
from axern.control.service.v1 import service_types_pb2 as _service_types_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class AllocationAccessPurpose(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ALLOCATION_ACCESS_PURPOSE_UNSPECIFIED: _ClassVar[AllocationAccessPurpose]
    ALLOCATION_ACCESS_PURPOSE_INTERACTIVE: _ClassVar[AllocationAccessPurpose]
    ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT: _ClassVar[AllocationAccessPurpose]
ALLOCATION_ACCESS_PURPOSE_UNSPECIFIED: AllocationAccessPurpose
ALLOCATION_ACCESS_PURPOSE_INTERACTIVE: AllocationAccessPurpose
ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT: AllocationAccessPurpose

class ResolveServiceRouteRequest(_message.Message):
    __slots__ = ("namespace", "service_id", "port_ref", "ttl_seconds")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    PORT_REF_FIELD_NUMBER: _ClassVar[int]
    TTL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    service_id: str
    port_ref: str
    ttl_seconds: int
    def __init__(self, namespace: _Optional[str] = ..., service_id: _Optional[str] = ..., port_ref: _Optional[str] = ..., ttl_seconds: _Optional[int] = ...) -> None: ...

class ServiceRoutePort(_message.Message):
    __slots__ = ("name", "protocol", "container_port")
    NAME_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_FIELD_NUMBER: _ClassVar[int]
    CONTAINER_PORT_FIELD_NUMBER: _ClassVar[int]
    name: str
    protocol: _common_pb2.PortProtocol
    container_port: int
    def __init__(self, name: _Optional[str] = ..., protocol: _Optional[_Union[_common_pb2.PortProtocol, str]] = ..., container_port: _Optional[int] = ...) -> None: ...

class ServiceRouteEndpoint(_message.Message):
    __slots__ = ("allocation_id", "node_id", "node_target", "attempt", "container_port", "protocol", "ready", "lease")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    CONTAINER_PORT_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_FIELD_NUMBER: _ClassVar[int]
    READY_FIELD_NUMBER: _ClassVar[int]
    LEASE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    node_id: str
    node_target: str
    attempt: int
    container_port: int
    protocol: _common_pb2.PortProtocol
    ready: bool
    lease: _common_pb2.ExecutionLease
    def __init__(self, allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ..., node_target: _Optional[str] = ..., attempt: _Optional[int] = ..., container_port: _Optional[int] = ..., protocol: _Optional[_Union[_common_pb2.PortProtocol, str]] = ..., ready: _Optional[bool] = ..., lease: _Optional[_Union[_common_pb2.ExecutionLease, _Mapping]] = ...) -> None: ...

class ResolveServiceRouteResponse(_message.Message):
    __slots__ = ("service_id", "namespace", "service_status", "port", "endpoints")
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    SERVICE_STATUS_FIELD_NUMBER: _ClassVar[int]
    PORT_FIELD_NUMBER: _ClassVar[int]
    ENDPOINTS_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    namespace: str
    service_status: _service_types_pb2.ServiceStatus
    port: ServiceRoutePort
    endpoints: _containers.RepeatedCompositeFieldContainer[ServiceRouteEndpoint]
    def __init__(self, service_id: _Optional[str] = ..., namespace: _Optional[str] = ..., service_status: _Optional[_Union[_service_types_pb2.ServiceStatus, str]] = ..., port: _Optional[_Union[ServiceRoutePort, _Mapping]] = ..., endpoints: _Optional[_Iterable[_Union[ServiceRouteEndpoint, _Mapping]]] = ...) -> None: ...

class ResolveAllocationTerminalRequest(_message.Message):
    __slots__ = ("allocation_id", "ttl_seconds", "client_certificate_fingerprint", "rollout_execution_lease", "purpose")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    TTL_SECONDS_FIELD_NUMBER: _ClassVar[int]
    CLIENT_CERTIFICATE_FINGERPRINT_FIELD_NUMBER: _ClassVar[int]
    ROLLOUT_EXECUTION_LEASE_FIELD_NUMBER: _ClassVar[int]
    PURPOSE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    ttl_seconds: int
    client_certificate_fingerprint: str
    rollout_execution_lease: str
    purpose: AllocationAccessPurpose
    def __init__(self, allocation_id: _Optional[str] = ..., ttl_seconds: _Optional[int] = ..., client_certificate_fingerprint: _Optional[str] = ..., rollout_execution_lease: _Optional[str] = ..., purpose: _Optional[_Union[AllocationAccessPurpose, str]] = ...) -> None: ...

class ResolveAllocationTerminalResponse(_message.Message):
    __slots__ = ("allocation_id", "owner_type", "owner_id", "node_id", "node_target", "attempt", "lease")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    OWNER_TYPE_FIELD_NUMBER: _ClassVar[int]
    OWNER_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_TARGET_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    LEASE_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    owner_type: str
    owner_id: str
    node_id: str
    node_target: str
    attempt: int
    lease: _common_pb2.ExecutionLease
    def __init__(self, allocation_id: _Optional[str] = ..., owner_type: _Optional[str] = ..., owner_id: _Optional[str] = ..., node_id: _Optional[str] = ..., node_target: _Optional[str] = ..., attempt: _Optional[int] = ..., lease: _Optional[_Union[_common_pb2.ExecutionLease, _Mapping]] = ...) -> None: ...

class ResolveTunnelRelayTargetRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class ResolveTunnelRelayTargetResponse(_message.Message):
    __slots__ = ("node_edge_target",)
    NODE_EDGE_TARGET_FIELD_NUMBER: _ClassVar[int]
    node_edge_target: str
    def __init__(self, node_edge_target: _Optional[str] = ...) -> None: ...

class ResolveServiceReplicaTargetsRequest(_message.Message):
    __slots__ = ("service_id",)
    SERVICE_ID_FIELD_NUMBER: _ClassVar[int]
    service_id: str
    def __init__(self, service_id: _Optional[str] = ...) -> None: ...

class ServiceReplicaTarget(_message.Message):
    __slots__ = ("allocation_id", "node_id")
    ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    allocation_id: str
    node_id: str
    def __init__(self, allocation_id: _Optional[str] = ..., node_id: _Optional[str] = ...) -> None: ...

class ResolveServiceReplicaTargetsResponse(_message.Message):
    __slots__ = ("replicas",)
    REPLICAS_FIELD_NUMBER: _ClassVar[int]
    replicas: _containers.RepeatedCompositeFieldContainer[ServiceReplicaTarget]
    def __init__(self, replicas: _Optional[_Iterable[_Union[ServiceReplicaTarget, _Mapping]]] = ...) -> None: ...
