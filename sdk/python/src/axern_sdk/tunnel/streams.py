"""Local TCP stream state for Axern tunnel connectors."""

from __future__ import annotations

import socket
import threading
import time
from typing import Iterator

from axern.tunnel.v1 import tunnel_pb2
from axern_sdk.tunnel.config import ConnectorConfig
from axern_sdk.tunnel.frames import _FrameQueue


class _ConnectorState:
    def __init__(
        self,
        *,
        frames: _FrameQueue,
        local_target: str,
        stop: threading.Event,
        config: ConnectorConfig,
    ) -> None:
        self._frames = frames
        self._local_target = local_target
        self._stop = stop
        self._config = config
        self._lock = threading.Lock()
        self._conns: dict[int, socket.socket] = {}
        self._last_seen = time.monotonic()

    def run(self, responses: Iterator[tunnel_pb2.TunnelFrame]) -> None:
        heartbeat = threading.Thread(target=self._heartbeat_loop, name="axern-tunnel-heartbeat", daemon=True)
        heartbeat.start()
        for frame in responses:
            if self._stop.is_set():
                return
            self._last_seen = time.monotonic()
            self._handle_frame(frame)

    def _handle_frame(self, frame: tunnel_pb2.TunnelFrame) -> None:
        payload = frame.WhichOneof("payload")
        if payload == "ping":
            self._frames.put(tunnel_pb2.TunnelFrame(pong=tunnel_pb2.Pong(id=frame.ping.id)))
        elif payload == "pong":
            return
        elif payload == "stream_open":
            self._open_local(frame.stream_open.stream_id)
        elif payload == "stream_data":
            self._write_local(frame.stream_data.stream_id, frame.stream_data.data)
        elif payload == "stream_close":
            self._close_local(frame.stream_close.stream_id)

    def _open_local(self, stream_id: int) -> None:
        with self._lock:
            if self._config.max_streams > 0 and len(self._conns) >= self._config.max_streams:
                self._send_stream_close(stream_id, "connector max streams reached")
                return
        try:
            conn = socket.create_connection(
                _split_host_port(self._local_target),
                timeout=self._config.local_connect_timeout_seconds,
            )
        except OSError as exc:
            self._send_stream_close(stream_id, str(exc))
            return
        with self._lock:
            old = self._conns.get(stream_id)
            self._conns[stream_id] = conn
        if old is not None:
            old.close()
        threading.Thread(target=self._copy_local_to_relay, args=(stream_id, conn), daemon=True).start()

    def _write_local(self, stream_id: int, data: bytes) -> None:
        if not data:
            return
        with self._lock:
            conn = self._conns.get(stream_id)
        if conn is None:
            return
        try:
            conn.sendall(data)
        except OSError:
            self._close_local(stream_id)

    def _copy_local_to_relay(self, stream_id: int, conn: socket.socket) -> None:
        try:
            while not self._stop.is_set():
                data = conn.recv(32 * 1024)
                if not data:
                    self._send_stream_close(stream_id)
                    return
                self._frames.put(tunnel_pb2.TunnelFrame(stream_data=tunnel_pb2.StreamData(stream_id=stream_id, data=data)))
        except OSError as exc:
            self._send_stream_close(stream_id, str(exc))
        finally:
            self._close_local_conn(stream_id, conn)

    def _heartbeat_loop(self) -> None:
        if self._config.ping_interval_seconds <= 0:
            return
        while not self._stop.wait(self._config.ping_interval_seconds):
            if time.monotonic() - self._last_seen > self._config.pong_timeout_seconds:
                self._stop.set()
                self._frames.close()
                return
            self._frames.put(tunnel_pb2.TunnelFrame(ping=tunnel_pb2.Ping(id=str(time.time_ns()))))

    def _send_stream_close(self, stream_id: int, error: str = "") -> None:
        self._frames.put(tunnel_pb2.TunnelFrame(stream_close=tunnel_pb2.StreamClose(stream_id=stream_id, error=error)))

    def _close_local(self, stream_id: int) -> None:
        with self._lock:
            conn = self._conns.pop(stream_id, None)
        if conn is not None:
            conn.close()

    def _close_local_conn(self, stream_id: int, conn: socket.socket) -> None:
        with self._lock:
            if self._conns.get(stream_id) is conn:
                self._conns.pop(stream_id, None)
        conn.close()

    def close_all(self) -> None:
        with self._lock:
            conns = list(self._conns.values())
            self._conns.clear()
        for conn in conns:
            conn.close()


def _split_host_port(target: str) -> tuple[str, int]:
    host, port = target.rsplit(":", 1)
    return host, int(port)
