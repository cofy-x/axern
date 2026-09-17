from __future__ import annotations

from axern.control.environment.v1 import environment_pb2
from axern.control.run.v1 import run_pb2
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
        self.created_run = None
        self.created_tunnel = None
        self.revoked = []
        self.deleted_environments = []
        self.renewed = []
        self.cancelled = []

    def create_environment(self, **kwargs):
        self.created_environment = kwargs
        return environment_pb2.Environment(id="env-1")

    def _gateway_transport(self) -> _GatewayTransport:
        return _GatewayTransport(insecure=True)

    def create_run(self, **kwargs):
        self.created_run = kwargs
        return run_pb2.Run(id="run-1", allocation_id="alloc-1")

    def watch_run(self, run_id: str, **kwargs):
        del kwargs
        yield run_pb2.Run(
            id=run_id,
            allocation_id="alloc-1",
            version=1,
            status=run_pb2.RUN_STATUS_RUNNING,
        )

    def cancel_run(self, run_id: str, **kwargs):
        self.cancelled.append((run_id, kwargs))
        return run_pb2.Run(id=run_id, status=run_pb2.RUN_STATUS_CANCELLED)

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

    def delete_environment(self, environment_id: str, **kwargs):
        self.deleted_environments.append((environment_id, kwargs))
        return environment_pb2.Environment(id=environment_id)


class _AsyncFakeClient(_FakeClient):
    async def create_environment(self, **kwargs):
        return super().create_environment(**kwargs)

    async def create_run(self, **kwargs):
        return super().create_run(**kwargs)

    async def watch_run(self, run_id: str, **kwargs):
        del kwargs
        yield run_pb2.Run(
            id=run_id,
            allocation_id="alloc-1",
            version=1,
            status=run_pb2.RUN_STATUS_RUNNING,
        )

    async def cancel_run(self, run_id: str, **kwargs):
        return super().cancel_run(run_id, **kwargs)

    async def create_tunnel_session(self, **kwargs):
        return super().create_tunnel_session(**kwargs)

    async def list_tunnel_events(self, session_id: str, **kwargs):
        return super().list_tunnel_events(session_id, **kwargs)

    async def revoke_tunnel_session(self, session_id: str, **kwargs):
        return super().revoke_tunnel_session(session_id, **kwargs)

    async def renew_tunnel_session(self, session_id: str, client_token: str, **kwargs):
        return super().renew_tunnel_session(session_id, client_token, **kwargs)

    async def delete_environment(self, environment_id: str, **kwargs):
        return super().delete_environment(environment_id, **kwargs)
