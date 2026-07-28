from __future__ import annotations

from axern.control.common.v1 import common_pb2
from axern.control.environment.v1 import environment_pb2
from axern.control.service.v1 import service_replica_pb2, service_types_pb2
from axern.control.tunnel.v1 import tunnel_pb2
from axern_sdk.tunnel.config import _GatewayTransport


class _FakeConnector:
    started = False
    stopped = False
    error = None

    def __init__(self, **kwargs) -> None:
        self.kwargs = kwargs

    def start(self) -> None:
        self.started = True

    def stop(self, timeout: float = 5.0) -> None:
        del timeout
        self.stopped = True


class _FakeClient:
    def __init__(self) -> None:
        self.created_environment = None
        self.created_service = None
        self.created_tunnel = None
        self.revoked = []
        self.deleted = []
        self.purged = []
        self.deleted_environments = []
        self.renewed = []

    def create_environment(self, **kwargs):
        self.created_environment = kwargs
        return environment_pb2.Environment(id="env-1")

    def _gateway_transport(self) -> _GatewayTransport:
        return _GatewayTransport(insecure=True)

    def create_service(self, **kwargs):
        self.created_service = kwargs
        return service_types_pb2.Service(id="svc-1")

    def list_service_replicas(self, service_id: str, **kwargs):
        del kwargs
        self.listed_service_id = service_id
        return [
            service_replica_pb2.ServiceReplica(
                id="alloc-1",
                service_id=service_id,
                node_id="node-1",
                attempt=7,
                status=common_pb2.ALLOCATION_STATUS_RUNNING,
                ready=True,
            )
        ]

    def watch_service(self, service_id: str, **kwargs):
        del kwargs
        yield service_types_pb2.Service(
            id=service_id,
            version=1,
            status=service_types_pb2.SERVICE_STATUS_READY,
            ready_replicas=1,
        )

    def create_tunnel_session(self, **kwargs):
        self.created_tunnel = kwargs
        return tunnel_pb2.CreateTunnelSessionResponse(
            session=tunnel_pb2.TunnelSession(
                session_id="tun-1",
                allocation_id=kwargs["allocation_id"],
                remote_port=8786,
                bound_addr="127.0.0.1:8786",
                client_edge_target="127.0.0.1:25000",
            ),
            client_token="client-token",
        )

    def list_tunnel_events(self, session_id: str, **kwargs):
        del kwargs
        return [
            tunnel_pb2.TunnelSessionEvent(
                session_id=session_id,
                event_type=tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED,
            )
        ]

    def revoke_tunnel_session(self, session_id: str, **kwargs):
        self.revoked.append((session_id, kwargs))
        return tunnel_pb2.TunnelSession(session_id=session_id, status=tunnel_pb2.TUNNEL_SESSION_STATUS_REVOKED)

    def renew_tunnel_session(self, session_id: str, client_token: str, **kwargs):
        self.renewed.append((session_id, client_token, kwargs))
        return tunnel_pb2.TunnelSession(session_id=session_id, status=tunnel_pb2.TUNNEL_SESSION_STATUS_RUNNING)

    def delete_service(self, service_id: str, **kwargs):
        self.deleted.append((service_id, kwargs))
        return service_types_pb2.Service(id=service_id)

    def admin_purge_service(self, service_id: str, **kwargs):
        self.purged.append((service_id, kwargs))
        return service_id

    def delete_environment(self, environment_id: str, **kwargs):
        self.deleted_environments.append((environment_id, kwargs))
        return environment_pb2.Environment(id=environment_id)


class _AsyncFakeClient(_FakeClient):
    async def create_environment(self, **kwargs):
        return super().create_environment(**kwargs)

    async def create_service(self, **kwargs):
        return super().create_service(**kwargs)

    async def list_service_replicas(self, service_id: str, **kwargs):
        return super().list_service_replicas(service_id, **kwargs)

    async def watch_service(self, service_id: str, **kwargs):
        del kwargs
        yield service_types_pb2.Service(
            id=service_id,
            version=1,
            status=service_types_pb2.SERVICE_STATUS_READY,
            ready_replicas=1,
        )

    async def create_tunnel_session(self, **kwargs):
        return super().create_tunnel_session(**kwargs)

    async def list_tunnel_events(self, session_id: str, **kwargs):
        return super().list_tunnel_events(session_id, **kwargs)

    async def revoke_tunnel_session(self, session_id: str, **kwargs):
        return super().revoke_tunnel_session(session_id, **kwargs)

    async def renew_tunnel_session(self, session_id: str, client_token: str, **kwargs):
        return super().renew_tunnel_session(session_id, client_token, **kwargs)

    async def delete_service(self, service_id: str, **kwargs):
        return super().delete_service(service_id, **kwargs)

    async def admin_purge_service(self, service_id: str, **kwargs):
        return super().admin_purge_service(service_id, **kwargs)

    async def delete_environment(self, environment_id: str, **kwargs):
        return super().delete_environment(environment_id, **kwargs)
