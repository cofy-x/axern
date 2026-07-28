"""Async tunnel session renewal lifecycle for SDK sandboxes."""

from __future__ import annotations

import asyncio

import grpc

from axern_sdk.async_client import AsyncAxernClient
from axern_sdk.sandbox.renewal import _renew_interval_seconds


class AsyncTunnelRenewal:
    def __init__(
        self,
        *,
        client: AsyncAxernClient,
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
        self._task: asyncio.Task[None] | None = None
        self._error: BaseException | None = None

    @property
    def error(self) -> BaseException | None:
        return self._error

    def start(self) -> None:
        if self._task is not None:
            return
        self._error = None
        self._task = asyncio.create_task(self._run(), name=f"axern-tunnel-renew-{self._session_id}")

    async def stop(self) -> None:
        if self._task is None:
            return
        self._task.cancel()
        try:
            await self._task
        except asyncio.CancelledError:
            pass
        self._task = None

    async def _run(self) -> None:
        interval = self._interval_seconds
        if interval is None:
            interval = _renew_interval_seconds(self._ttl_seconds)
        while True:
            await asyncio.sleep(interval)
            try:
                await self._client.renew_tunnel_session(
                    self._session_id,
                    self._client_token,
                    ttl_seconds=self._ttl_seconds,
                    timeout=30.0,
                )
            except grpc.aio.AioRpcError as exc:
                if exc.code() in (
                    grpc.StatusCode.PERMISSION_DENIED,
                    grpc.StatusCode.UNAUTHENTICATED,
                    grpc.StatusCode.NOT_FOUND,
                    grpc.StatusCode.FAILED_PRECONDITION,
                ):
                    self._error = exc
                    return
            except asyncio.CancelledError:
                raise
            except BaseException as exc:
                self._error = exc
                return
