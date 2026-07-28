"""Raw TCP tunnel connector used by the high-level Sandbox SDK."""

from __future__ import annotations

import threading

import grpc

from axern.control.tunnel.v1 import tunnel_pb2 as control_tunnel_pb2
from axern.tunnel.v1 import tunnel_pb2, tunnel_pb2_grpc
from axern_sdk._internal.channel import relay_channel
from axern_sdk.tunnel.config import ConnectorConfig, _GatewayTransport
from axern_sdk.tunnel.frames import _FrameQueue
from axern_sdk.tunnel.streams import _ConnectorState


class TunnelConnector:
    """Connects one Axern tunnel session to a local TCP upstream."""

    def __init__(
        self,
        *,
        session: control_tunnel_pb2.TunnelSession,
        client_token: str,
        local_target: str,
        transport: _GatewayTransport,
        connector_config: ConnectorConfig | None = None,
    ) -> None:
        self._session = session
        self._client_token = client_token
        self._local_target = local_target
        self._transport = transport
        self._connector_config = connector_config or ConnectorConfig()
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self._error: BaseException | None = None
        self._active_channel: grpc.Channel | None = None
        self._active_frames: _FrameQueue | None = None
        self._active_lock = threading.Lock()

    @property
    def error(self) -> BaseException | None:
        return self._error

    def start(self) -> None:
        if self._thread is not None:
            return
        self._thread = threading.Thread(target=self.run, name=f"axern-tunnel-{self._session.session_id}", daemon=True)
        self._thread.start()

    def stop(self, timeout: float = 5.0) -> None:
        self._stop.set()
        with self._active_lock:
            if self._active_frames is not None:
                self._active_frames.close()
            if self._active_channel is not None:
                self._active_channel.close()
        if self._thread is not None:
            self._thread.join(timeout=timeout)

    def run(self) -> None:
        backoff = 1.0
        while not self._stop.is_set():
            try:
                self._run_once()
                if not self._stop.is_set():
                    backoff = self._sleep_before_reconnect(backoff)
            except grpc.RpcError as exc:
                if _terminal_rpc_error(exc):
                    self._error = exc
                    self._stop.set()
                    return
                backoff = self._sleep_before_reconnect(backoff)
            except BaseException as exc:
                self._error = exc
                self._stop.set()
                return

    def _run_once(self) -> None:
        target = self._session.client_edge_target or self._session.edge_target
        if not target:
            raise ValueError("tunnel session does not include a relay target")
        channel = relay_channel(
            target,
            insecure=self._transport.insecure,
            tls_ca_cert=self._transport.tls_ca_cert or None,
            tls_cert=self._transport.tls_cert or None,
            tls_key=self._transport.tls_key or None,
            server_name=self._transport.server_name or None,
            proxy_mode=self._transport.proxy_mode,
        )
        frames = _FrameQueue(self._stop)
        frames.put(
            tunnel_pb2.TunnelFrame(
                peer_open=tunnel_pb2.PeerOpen(
                    session_id=self._session.session_id,
                    peer_kind=control_tunnel_pb2.TUNNEL_PEER_KIND_CLIENT,
                    token=self._client_token,
                )
            )
        )
        state = _ConnectorState(
            frames=frames,
            local_target=self._local_target,
            stop=self._stop,
            config=self._connector_config,
        )
        try:
            with self._active_lock:
                self._active_channel = channel
                self._active_frames = frames
            responses = tunnel_pb2_grpc.TunnelRelayStub(channel).ConnectPeer(iter(frames))
            state.run(responses)
        finally:
            with self._active_lock:
                if self._active_channel is channel:
                    self._active_channel = None
                if self._active_frames is frames:
                    self._active_frames = None
            frames.close()
            state.close_all()
            channel.close()

    def _sleep_before_reconnect(self, backoff: float) -> float:
        if self._stop.wait(backoff):
            return backoff
        return min(backoff * 2, 10.0)


def _terminal_rpc_error(exc: grpc.RpcError) -> bool:
    return exc.code() in (grpc.StatusCode.PERMISSION_DENIED, grpc.StatusCode.UNAUTHENTICATED, grpc.StatusCode.NOT_FOUND)
