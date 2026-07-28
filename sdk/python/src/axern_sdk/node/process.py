"""Attached sandbox process primitives."""

from __future__ import annotations

import queue
from collections.abc import Iterator
from contextlib import suppress
from dataclasses import dataclass
from typing import Any

import grpc

from axern.node.sandbox.v1 import node_pb2
from axern_sdk.errors import SandboxConnectionError
from axern_sdk.node.models import ExecStreamEvent


@dataclass(frozen=True, slots=True)
class ProcessResult:
    """Exit result for an attached sandbox process."""

    exit_code: int
    message: str = ""


class SandboxProcess:
    """Attached process backed by the node sandbox Process stream."""

    def __init__(
        self,
        *,
        channel: grpc.Channel,
        responses,
        requests: "queue.Queue[object | None]",
        prefetched: list[object] | None = None,
        request_type: type[Any] = node_pb2.ProcessRequest,
        close_channel: bool = True,
    ) -> None:
        self._channel = channel
        self._responses = responses
        self._requests = requests
        self._prefetched = list(prefetched or [])
        self._request_type = request_type
        self._close_channel = close_channel
        self._exit: ProcessResult | None = None
        self._closed = False

    def __enter__(self) -> "SandboxProcess":
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        self.close()

    def write(self, data: bytes | str, *, encoding: str = "utf-8", errors: str = "strict") -> None:
        if isinstance(data, str):
            data = data.encode(encoding, errors=errors)
        self._send(self._request_type(stdin=data))

    def close_stdin(self) -> None:
        self._send(self._request_type(close_stdin=True))

    def resize(self, cols: int, rows: int) -> None:
        self._send(self._request_type(resize=node_pb2.TerminalResize(cols=cols, rows=rows)))

    def terminate(self) -> None:
        self.signal("TERM")

    def kill(self) -> None:
        self.signal("KILL")

    def signal(self, signal: str) -> None:
        self._send(self._request_type(signal=node_pb2.ProcessSignal(signal=signal)))

    def events(self) -> Iterator[ExecStreamEvent]:
        exit_seen = self._exit is not None
        try:
            while self._prefetched:
                event = self._event_from_response(self._prefetched.pop(0))
                if event is not None:
                    if event.exit_code is not None:
                        exit_seen = True
                    yield event
            for response in self._responses:
                event = self._event_from_response(response)
                if event is not None:
                    if event.exit_code is not None:
                        exit_seen = True
                    yield event
        finally:
            self._close_requests()
            if self._close_channel:
                with suppress(Exception):
                    self._channel.close()
        if not exit_seen:
            raise SandboxConnectionError("sandbox process stream ended without exit status")

    def _close_owned_channel(self) -> None:
        if self._close_channel:
            with suppress(Exception):
                self._channel.close()

    def wait(self) -> ProcessResult:
        if self._exit is not None:
            return self._exit
        for _ in self.events():
            pass
        if self._exit is None:
            raise SandboxConnectionError("sandbox process stream ended without exit status")
        return self._exit

    @property
    def returncode(self) -> int | None:
        return None if self._exit is None else self._exit.exit_code

    def close(self) -> None:
        if self._exit is None:
            try:
                self.terminate()
            except Exception:
                pass
        self._close_requests()
        self._close_owned_channel()

    def _send(self, request: object) -> None:
        if self._closed:
            raise RuntimeError("sandbox process is closed")
        self._requests.put(request)

    def _event_from_response(self, response) -> ExecStreamEvent | None:
        payload = response.WhichOneof("payload")
        if payload == "stdout":
            return ExecStreamEvent(stream="stdout", data=bytes(response.stdout))
        if payload == "stderr":
            return ExecStreamEvent(stream="stderr", data=bytes(response.stderr))
        if payload == "exit":
            self._exit = ProcessResult(exit_code=response.exit.exit_code, message=response.exit.message)
            return ExecStreamEvent(stream="exit", exit_code=response.exit.exit_code, message=response.exit.message)
        return None

    def _close_requests(self) -> None:
        if not self._closed:
            self._closed = True
            self._requests.put(None)


def process_request_iterator(
    open_request,
    requests: "queue.Queue[object | None]",
) -> Iterator[object]:
    yield open_request
    while True:
        request = requests.get()
        if request is None:
            return
        yield request
