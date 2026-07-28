"""Async gateway-backed client for node sandbox execution APIs."""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator, Callable

import grpc

from axern.node.sandbox.v1 import node_pb2, node_pb2_grpc
from axern_sdk._internal.errors import sandbox_rpc_error
from axern_sdk.async_client import AsyncAxernClient
from axern_sdk.errors import SandboxConnectionError
from axern_sdk.node.async_browser_client import AsyncNodeSandboxBrowserMixin
from axern_sdk.node.async_capability_client import AsyncNodeSandboxCapabilityMixin
from axern_sdk.node.commands import exec_argv
from axern_sdk.node.async_file_client import AsyncNodeSandboxFileMixin
from axern_sdk.node.async_process import AsyncSandboxProcess
from axern_sdk.node.async_computer_use_client import AsyncNodeSandboxComputerUseMixin
from axern_sdk.node.models import ExecCommand, ExecResult, ExecStreamEvent, ImageProcessMount
from axern_sdk.node.protocol import exec_spec, image_process_spec, text_exec_result


class AsyncNodeSandboxClient(AsyncNodeSandboxCapabilityMixin, AsyncNodeSandboxBrowserMixin, AsyncNodeSandboxComputerUseMixin, AsyncNodeSandboxFileMixin):
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
        lease_ttl_seconds: int = 60,
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
            lease_ttl_seconds=lease_ttl_seconds,
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
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> AsyncIterator[ExecStreamEvent]:
        argv = exec_argv(command, shell=shell)
        stdin = b"" if input is None else input.encode(encoding, errors=errors) if isinstance(input, str) else input
        process = await self.process(
            argv,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        async for event in self._process_events_with_input(process, stdin):
            yield event

    async def exec_image(
        self,
        image: str,
        command: ExecCommand,
        *,
        env: dict[str, str] | None = None,
        cwd: str = "",
        timeout_seconds: int = 0,
        user: str = "",
        tty: bool = False,
        check: bool = False,
        text: bool = False,
        encoding: str = "utf-8",
        errors: str = "strict",
        shell: bool | None = None,
        mounts: list[ImageProcessMount] | tuple[ImageProcessMount, ...] | None = None,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> ExecResult:
        argv = exec_argv(command, shell=shell)

        def request_factory() -> node_pb2.ExecImageRequest:
            return node_pb2.ExecImageRequest(
                allocation_id=self._allocation_id,
                spec=image_process_spec(
                    image,
                    argv,
                    env=env,
                    cwd=cwd,
                    timeout_seconds=timeout_seconds,
                    user=user,
                    tty=tty,
                    mounts=mounts,
                ),
            )

        response = await self._call_unary(
            "sandbox exec image",
            "ExecImage",
            request_factory,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        result = ExecResult(
            exit_code=response.exit_code,
            stdout=bytes(response.stdout),
            stderr=bytes(response.stderr),
            stdout_truncated=bool(response.stdout_truncated),
            stderr_truncated=bool(response.stderr_truncated),
        )
        if text:
            result = text_exec_result(result, encoding=encoding, errors=errors)
        if check:
            result.raise_for_status(argv)
        return result

    async def process(
        self,
        command: ExecCommand,
        *,
        env: dict[str, str] | None = None,
        cwd: str = "",
        timeout_seconds: int = 0,
        user: str = "",
        tty: bool = False,
        shell: bool | None = None,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
        ) -> AsyncSandboxProcess:
        argv = exec_argv(command, shell=shell)
        channel = self._gateway_channel()
        try:
            call = node_pb2_grpc.NodeSandboxStub(channel).Process(timeout=rpc_timeout)
            await call.write(
                node_pb2.ProcessRequest(
                    open=node_pb2.ProcessOpen(
                        allocation_id=self._allocation_id,
                        spec=exec_spec(argv, env=env, cwd=cwd, timeout_seconds=timeout_seconds, user=user, tty=tty),
                    )
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

    async def process_image(
        self,
        image: str,
        command: ExecCommand,
        *,
        env: dict[str, str] | None = None,
        cwd: str = "",
        timeout_seconds: int = 0,
        user: str = "",
        tty: bool = False,
        shell: bool | None = None,
        mounts: list[ImageProcessMount] | tuple[ImageProcessMount, ...] | None = None,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
        ) -> AsyncSandboxProcess:
        argv = exec_argv(command, shell=shell)
        channel = self._gateway_channel()
        try:
            call = node_pb2_grpc.NodeSandboxStub(channel).ProcessImage(timeout=rpc_timeout)
            await call.write(
                node_pb2.ProcessImageRequest(
                    open=node_pb2.ProcessImageOpen(
                        allocation_id=self._allocation_id,
                        spec=image_process_spec(
                            image,
                            argv,
                            env=env,
                            cwd=cwd,
                            timeout_seconds=timeout_seconds,
                            user=user,
                            tty=tty,
                            mounts=mounts,
                        ),
                    )
                )
            )
            first = await call.read()
            aio_eof = getattr(grpc.aio, "EOF", object())
            if first is aio_eof:
                raise SandboxConnectionError("sandbox image process stream ended before ready")
            if first.WhichOneof("payload") == "ready":
                return AsyncSandboxProcess(
                    channel=channel,
                    call=call,
                    request_type=node_pb2.ProcessImageRequest,
                    close_channel=False,
                )
            return AsyncSandboxProcess(
                channel=channel,
                call=call,
                prefetched=[first],
                request_type=node_pb2.ProcessImageRequest,
                close_channel=False,
            )
        except grpc.aio.AioRpcError as exc:
            raise sandbox_rpc_error(exc, operation="sandbox image process", allocation_id=self._allocation_id) from exc

    async def _call_unary(
        self,
        operation: str,
        method_name: str,
        request_factory: Callable[[], object],
        *,
        lease_ttl_seconds: int,
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
        lease_ttl_seconds: int,
        rpc_timeout: float | None,
    ) -> ExecResult:
        process = await self.process(
            argv,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        stdout = bytearray()
        stderr = bytearray()
        exit_code: int | None = None
        async for event in self._process_events_with_input(process, input or b""):
            if event.stream == "stdout":
                stdout.extend(event.data)
            elif event.stream == "stderr":
                stderr.extend(event.data)
            elif event.exit_code is not None:
                exit_code = event.exit_code
        if exit_code is None:
            raise SandboxConnectionError("sandbox process ended without exit status")
        return ExecResult(exit_code=exit_code, stdout=bytes(stdout), stderr=bytes(stderr))

    async def _process_events_with_input(
        self,
        process: AsyncSandboxProcess,
        input: bytes,
    ) -> AsyncIterator[ExecStreamEvent]:
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
