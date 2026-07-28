"""Gateway-backed client for sandbox execution APIs."""

from __future__ import annotations

import queue
from collections.abc import Callable, Iterator

import grpc

from axern.node.sandbox.v1 import node_pb2, node_pb2_grpc
from axern_sdk._internal.errors import sandbox_rpc_error
from axern_sdk.client import AxernClient
from axern_sdk.errors import SandboxConnectionError
from axern_sdk.node.browser_client import NodeSandboxBrowserMixin
from axern_sdk.node.capability_client import NodeSandboxCapabilityMixin
from axern_sdk.node.commands import exec_argv
from axern_sdk.node.computer_use_client import NodeSandboxComputerUseMixin
from axern_sdk.node.file_client import NodeSandboxFileMixin
from axern_sdk.node.models import ExecCommand, ExecResult, ExecStreamEvent, ImageProcessMount
from axern_sdk.node.process import SandboxProcess, process_request_iterator
from axern_sdk.node.protocol import exec_spec, image_process_spec, text_exec_result


class NodeSandboxClient(NodeSandboxCapabilityMixin, NodeSandboxBrowserMixin, NodeSandboxComputerUseMixin, NodeSandboxFileMixin):
    """Executes commands against one allocation through the node sandbox API."""

    def __init__(
        self,
        *,
        client: AxernClient,
        allocation_id: str,
    ) -> None:
        self._client = client
        self._allocation_id = allocation_id

    def exec(
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
        result = self._exec_via_process(
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

    def exec_stream(
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
    ) -> Iterator[ExecStreamEvent]:
        argv = exec_argv(command, shell=shell)
        stdin = b"" if input is None else input.encode(encoding, errors=errors) if isinstance(input, str) else input
        process = self.process(
            argv,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        try:
            if stdin:
                process.write(stdin)
            process.close_stdin()
            yield from process.events()
        except BaseException:
            process.close()
            raise

    def exec_image(
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

        response = self._call_unary(
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

    def process(
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
    ) -> SandboxProcess:
        argv = exec_argv(command, shell=shell)
        channel = self._gateway_channel()
        requests: queue.Queue[object | None] = queue.Queue()
        open_request = node_pb2.ProcessRequest(
            open=node_pb2.ProcessOpen(
                allocation_id=self._allocation_id,
                spec=exec_spec(argv, env=env, cwd=cwd, timeout_seconds=timeout_seconds, user=user, tty=tty),
            )
        )
        try:
            responses = node_pb2_grpc.NodeSandboxStub(channel).Process(
                process_request_iterator(open_request, requests),
                timeout=rpc_timeout,
            )
            try:
                first = next(responses)
            except StopIteration as exc:
                requests.put(None)
                raise SandboxConnectionError("sandbox process stream ended before ready") from exc
            if first.WhichOneof("payload") == "ready":
                return SandboxProcess(channel=channel, responses=responses, requests=requests, close_channel=False)
            return SandboxProcess(channel=channel, responses=responses, requests=requests, prefetched=[first], close_channel=False)
        except grpc.RpcError as exc:
            requests.put(None)
            raise sandbox_rpc_error(exc, operation="sandbox process", allocation_id=self._allocation_id) from exc

    def process_image(
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
    ) -> SandboxProcess:
        argv = exec_argv(command, shell=shell)
        channel = self._gateway_channel()
        requests: queue.Queue[object | None] = queue.Queue()
        open_request = node_pb2.ProcessImageRequest(
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
                )
            )
        )
        try:
            responses = node_pb2_grpc.NodeSandboxStub(channel).ProcessImage(
                process_request_iterator(open_request, requests),
                timeout=rpc_timeout,
            )
            try:
                first = next(responses)
            except StopIteration as exc:
                requests.put(None)
                raise SandboxConnectionError("sandbox image process stream ended before ready") from exc
            if first.WhichOneof("payload") == "ready":
                return SandboxProcess(channel=channel, responses=responses, requests=requests, request_type=node_pb2.ProcessImageRequest, close_channel=False)
            return SandboxProcess(channel=channel, responses=responses, requests=requests, prefetched=[first], request_type=node_pb2.ProcessImageRequest, close_channel=False)
        except grpc.RpcError as exc:
            requests.put(None)
            raise sandbox_rpc_error(exc, operation="sandbox image process", allocation_id=self._allocation_id) from exc

    def _call_unary(
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
            return method(request_factory(), timeout=rpc_timeout)
        except grpc.RpcError as exc:
            raise sandbox_rpc_error(exc, operation=operation, allocation_id=self._allocation_id) from exc

    def _exec_via_process(
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
        process = self.process(
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
        try:
            if input:
                process.write(input)
            process.close_stdin()
            for event in process.events():
                if event.stream == "stdout":
                    stdout.extend(event.data)
                elif event.stream == "stderr":
                    stderr.extend(event.data)
                elif event.exit_code is not None:
                    exit_code = event.exit_code
        except BaseException:
            process.close()
            raise
        if exit_code is None:
            raise SandboxConnectionError("sandbox process ended without exit status")
        return ExecResult(exit_code=exit_code, stdout=bytes(stdout), stderr=bytes(stderr))

    def _gateway_channel(self) -> grpc.Channel:
        return self._client._channel
