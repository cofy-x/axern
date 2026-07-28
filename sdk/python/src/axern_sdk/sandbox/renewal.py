"""Tunnel session renewal lifecycle for SDK sandboxes."""

from __future__ import annotations

import threading

import grpc

from axern_sdk.client import AxernClient


class TunnelRenewal:
    def __init__(
        self,
        *,
        client: AxernClient,
        session_id: str,
        client_token: str,
        ttl_seconds: float,
        interval_seconds: float | None = None,
    ) -> None:
        self._client = client
        self._session_id = session_id
        self._client_token = client_token
        self._ttl_seconds = ttl_seconds
        self._interval_seconds = interval_seconds
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None
        self._error: BaseException | None = None

    @property
    def error(self) -> BaseException | None:
        return self._error

    def start(self) -> None:
        if self._thread is not None:
            return
        self._stop.clear()
        self._error = None
        self._thread = threading.Thread(
            target=self._run,
            name=f"axern-tunnel-renew-{self._session_id}",
            daemon=True,
        )
        self._thread.start()

    def stop(self, timeout: float = 5.0) -> None:
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=timeout)
            self._thread = None

    def _run(self) -> None:
        interval = self._interval_seconds
        if interval is None:
            interval = _renew_interval_seconds(self._ttl_seconds)
        while not self._stop.wait(interval):
            try:
                self._client.renew_tunnel_session(
                    self._session_id,
                    self._client_token,
                    ttl_seconds=self._ttl_seconds,
                    timeout=30.0,
                )
            except grpc.RpcError as exc:
                if exc.code() in (
                    grpc.StatusCode.PERMISSION_DENIED,
                    grpc.StatusCode.UNAUTHENTICATED,
                    grpc.StatusCode.NOT_FOUND,
                    grpc.StatusCode.FAILED_PRECONDITION,
                ):
                    self._error = exc
                    self._stop.set()
                    return
            except BaseException as exc:
                self._error = exc
                self._stop.set()
                return


def _renew_interval_seconds(ttl_seconds: float) -> float:
    if ttl_seconds <= 0:
        return 60.0
    return max(1.0, min(ttl_seconds / 2, 60.0))
