"""Async attached sandbox process primitives."""

from __future__ import annotations

from contextlib import suppress
from dataclasses import dataclass
from typing import Any

import grpc

from axern.node.sandbox.v1 import node_pb2
from axern_sdk.errors import SandboxConnectionError
from axern_sdk.node.models import ExecStreamEvent


@dataclass(frozen=True, slots=True)
class AsyncProcessResult:
    """Exit result for an attached async sandbox process."""

    exit_code: int
    message: str = ""


class AsyncSandboxProcess:
    """Async attached process backed by the node sandbox Process stream."""

    def __init__(
        self,
        *,
        channel: Any,
        call: Any,
        prefetched: list[object] | None = None,
        request_type: type[Any] = node_pb2.ProcessRequest,
        close_channel: bool = True,
    ) -> None:
        self._channel = channel
        self._call = call
        self._prefetched = list(prefetched or [])
        self._request_type = request_type
        self._close_channel = close_channel
        self._exit: AsyncProcessResult | None = None
        self._closed = False

    async def __aenter__(self) -> "AsyncSandboxProcess":
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        await self.close()

    async def write(self, data: bytes | str, *, encoding: str = "utf-8", errors: str = "strict") -> None:
        if isinstance(data, str):
            data = data.encode(encoding, errors=errors)
        await self._send(self._request_type(stdin=data))

    async def close_stdin(self) -> None:
        await self._send(self._request_type(close_stdin=True))

    async def resize(self, cols: int, rows: int) -> None:
        await self._send(self._request_type(resize=node_pb2.TerminalResize(cols=cols, rows=rows)))

    async def terminate(self) -> None:
        await self.signal("TERM")

    async def kill(self) -> None:
        await self.signal("KILL")

    async def signal(self, signal: str) -> None:
        await self._send(self._request_type(signal=node_pb2.ProcessSignal(signal=signal)))

    async def events(self):
        exit_seen = self._exit is not None
        aio_eof = getattr(grpc.aio, "EOF", object())
        try:
            while self._prefetched:
                event = self._event_from_response(self._prefetched.pop(0))
                if event is not None:
                    if event.exit_code is not None:
                        exit_seen = True
                    yield event
            while True:
                response = await self._call.read()
                if response is aio_eof:
                    break
                event = self._event_from_response(response)
                if event is not None:
                    if event.exit_code is not None:
                        exit_seen = True
                    yield event
        finally:
            with suppress(Exception):
                await self._done_writing()
            if self._close_channel:
                with suppress(Exception):
                    await self._channel.close()
        if not exit_seen:
            raise SandboxConnectionError("sandbox process stream ended without exit status")

    async def wait(self) -> AsyncProcessResult:
        if self._exit is not None:
            return self._exit
        async for _ in self.events():
            pass
        if self._exit is None:
            raise SandboxConnectionError("sandbox process stream ended without exit status")
        return self._exit

    @property
    def returncode(self) -> int | None:
        return None if self._exit is None else self._exit.exit_code

    async def close(self) -> None:
        if self._exit is None:
            try:
                await self.terminate()
            except Exception:
                pass
        await self._done_writing()
        if self._close_channel:
            await self._channel.close()

    async def _send(self, request: object) -> None:
        if self._closed:
            raise RuntimeError("sandbox process is closed")
        await self._call.write(request)

    def _event_from_response(self, response) -> ExecStreamEvent | None:
        payload = response.WhichOneof("payload")
        if payload == "stdout":
            return ExecStreamEvent(stream="stdout", data=bytes(response.stdout))
        if payload == "stderr":
            return ExecStreamEvent(stream="stderr", data=bytes(response.stderr))
        if payload == "exit":
            self._exit = AsyncProcessResult(exit_code=response.exit.exit_code, message=response.exit.message)
            return ExecStreamEvent(stream="exit", exit_code=response.exit.exit_code, message=response.exit.message)
        return None

    async def _done_writing(self) -> None:
        if not self._closed:
            self._closed = True
            await self._call.done_writing()
