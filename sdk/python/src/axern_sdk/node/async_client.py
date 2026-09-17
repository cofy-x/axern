"""Async gateway-backed client for node sandbox execution APIs."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator, Callable

import grpc

from axern.node.sandbox.v1 import node_pb2, node_pb2_grpc
from axern_sdk._internal.errors import sandbox_rpc_error
from axern_sdk.async_client import AsyncAxernClient
from axern_sdk.errors import SandboxConnectionError
from axern_sdk.node.async_capability_client import AsyncAllocationCapabilityMixin
from axern_sdk.node.commands import exec_argv
from axern_sdk.node.async_file_client import AsyncAllocationFileMixin
from axern_sdk.node.async_process import AsyncSandboxProcess
from axern_sdk.node.async_computer_use_client import AsyncAllocationComputerUseMixin
from axern_sdk.node.models import ExecCommand, ExecResult, ProcessEvent
from axern_sdk.node.protocol import exec_spec, text_exec_result

_MAX_COLLECTED_EXEC_OUTPUT_BYTES = 1 << 20


def _process_initial_size(cols: int, rows: int) -> node_pb2.TerminalResize | None:
    if (cols == 0) != (rows == 0):
        raise ValueError("initial_cols and initial_rows must both be zero or both be positive")
    if cols < 0 or rows < 0:
        raise ValueError("initial_cols and initial_rows must both be positive")
    return None if cols == 0 else node_pb2.TerminalResize(cols=cols, rows=rows)


class AsyncAllocationClient(AsyncAllocationCapabilityMixin, AsyncAllocationComputerUseMixin, AsyncAllocationFileMixin):
    """Async command execution client for one sandbox allocation."""

    def __init__(
        self,
        *,
        client: AsyncAxernClient,
        allocation_id: str,
    ) -> None:
        self._client = client
        self._allocation_id = allocation_id

    async def exec(
        self,
        command: ExecCommand,
        *,
        env: dict[str, str] | None = None,
        cwd: str = "",
        timeout_seconds: int = 0,
        user: str = "",
        tty: bool = False,
        input: bytes | str | None = None,
        check: bool = False,
        text: bool = False,
        encoding: str = "utf-8",
        errors: str = "strict",
        shell: bool | None = None,
        rpc_timeout: float | None = None,
    ) -> ExecResult:
        argv = exec_argv(command, shell=shell)
        stdin = None if input is None else input.encode(encoding, errors=errors) if isinstance(input, str) else input
        result = await self._exec_via_process(
            argv,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            input=stdin,
            rpc_timeout=rpc_timeout,
        )
        if text:
            result = text_exec_result(result, encoding=encoding, errors=errors)
        if check:
            result.raise_for_status(argv)
        return result

    async def exec_stream(
        self,
        command: ExecCommand,
        *,
        env: dict[str, str] | None = None,
        cwd: str = "",
        timeout_seconds: int = 0,
        user: str = "",
        tty: bool = False,
        input: bytes | str | None = None,
        encoding: str = "utf-8",
        errors: str = "strict",
        shell: bool | None = None,
        rpc_timeout: float | None = None,
    ) -> AsyncIterator[ProcessEvent]:
        argv = exec_argv(command, shell=shell)
        stdin = b"" if input is None else input.encode(encoding, errors=errors) if isinstance(input, str) else input
        process = await self.process(
            argv,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            rpc_timeout=rpc_timeout,
        )
        async for event in self._process_events_with_input(process, stdin):
            yield event


    async def process(
        self,
        command: ExecCommand,
        *,
        env: dict[str, str] | None = None,
        cwd: str = "",
        timeout_seconds: int = 0,
        user: str = "",
        tty: bool = False,
        initial_cols: int = 0,
        initial_rows: int = 0,
        shell: bool | None = None,
        rpc_timeout: float | None = None,
    ) -> AsyncSandboxProcess:
        argv = exec_argv(command, shell=shell)
        initial_size = _process_initial_size(initial_cols, initial_rows)
        channel = self._gateway_channel()
        try:
            call = node_pb2_grpc.NodeSandboxStub(channel).Process(timeout=rpc_timeout)
            open_payload = node_pb2.ProcessOpen(
                allocation_id=self._allocation_id,
                spec=exec_spec(argv, env=env, cwd=cwd, timeout_seconds=timeout_seconds, user=user, tty=tty),
            )
            if initial_size is not None:
                open_payload.initial_size.CopyFrom(initial_size)
            await call.write(
                node_pb2.ProcessRequest(
                    open=open_payload
                )
            )
            first = await call.read()
            aio_eof = getattr(grpc.aio, "EOF", object())
            if first is aio_eof:
                raise SandboxConnectionError("sandbox process stream ended before ready")
            if first.WhichOneof("payload") == "ready":
                return AsyncSandboxProcess(channel=channel, call=call, close_channel=False)
            return AsyncSandboxProcess(channel=channel, call=call, prefetched=[first], close_channel=False)
        except grpc.aio.AioRpcError as exc:
            raise sandbox_rpc_error(exc, operation="sandbox process", allocation_id=self._allocation_id) from exc

    async def _call_unary(
        self,
        operation: str,
        method_name: str,
        request_factory: Callable[[], object],
        *,
        rpc_timeout: float | None,
    ):
        try:
            method = getattr(node_pb2_grpc.NodeSandboxStub(self._gateway_channel()), method_name)
            return await method(request_factory(), timeout=rpc_timeout)
        except grpc.aio.AioRpcError as exc:
            raise sandbox_rpc_error(exc, operation=operation, allocation_id=self._allocation_id) from exc

    async def _exec_via_process(
        self,
        argv: list[str],
        *,
        env: dict[str, str] | None,
        cwd: str,
        timeout_seconds: int,
        user: str,
        tty: bool,
        input: bytes | None,
        rpc_timeout: float | None,
    ) -> ExecResult:
        process = await self.process(
            argv,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            rpc_timeout=rpc_timeout,
        )
        stdout = bytearray()
        stderr = bytearray()
        exit_code: int | None = None
        stdout_truncated = False
        stderr_truncated = False
        async for event in self._process_events_with_input(process, input or b""):
            if event.stream == "stdout":
                remaining = _MAX_COLLECTED_EXEC_OUTPUT_BYTES - len(stdout)
                stdout.extend(event.data[:remaining])
                stdout_truncated = stdout_truncated or len(event.data) > remaining
            elif event.stream == "stderr":
                remaining = _MAX_COLLECTED_EXEC_OUTPUT_BYTES - len(stderr)
                stderr.extend(event.data[:remaining])
                stderr_truncated = stderr_truncated or len(event.data) > remaining
            elif event.exit_code is not None:
                exit_code = event.exit_code
        if exit_code is None:
            raise SandboxConnectionError("sandbox process ended without exit status")
        return ExecResult(
            exit_code=exit_code,
            stdout=bytes(stdout),
            stderr=bytes(stderr),
            stdout_truncated=stdout_truncated,
            stderr_truncated=stderr_truncated,
        )

    async def _process_events_with_input(
        self,
        process: AsyncSandboxProcess,
        input: bytes,
    ) -> AsyncIterator[ProcessEvent]:
        sender = asyncio.create_task(self._write_process_input(process, input))
        exit_seen = getattr(process, "returncode", None) is not None
        try:
            async for event in process.events():
                if event.exit_code is not None:
                    exit_seen = True
                yield event
            if exit_seen:
                await self._cancel_process_sender(sender)
            else:
                await sender
        except BaseException:
            await self._cancel_process_sender(sender)
            await process.close()
            raise

    async def _write_process_input(self, process: AsyncSandboxProcess, input: bytes) -> None:
        chunk_size = 32 * 1024
        for offset in range(0, len(input), chunk_size):
            await process.write(input[offset : offset + chunk_size])
        await process.close_stdin()

    async def _cancel_process_sender(self, sender: asyncio.Task[None]) -> None:
        if sender.done():
            try:
                await sender
            except (Exception, asyncio.CancelledError):
                pass
            return
        sender.cancel()
        try:
            await sender
        except asyncio.CancelledError:
            pass

    def _gateway_channel(self) -> grpc.aio.Channel:
        channel = self._client._channel
        if channel is None:
            self._client._ensure_channel()
            channel = self._client._channel
        assert channel is not None
        return channel
