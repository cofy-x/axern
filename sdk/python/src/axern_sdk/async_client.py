"""Async V1 control-plane client for environments, runs, and tunnels."""

from __future__ import annotations

import asyncio
import hashlib
import os
from collections.abc import AsyncGenerator, Iterable
from typing import Awaitable, BinaryIO, TypeVar

import grpc
from google.protobuf import duration_pb2

from axern.control.common.v1 import common_pb2
from axern.control.environment.v1 import environment_pb2, environment_pb2_grpc
from axern.control.run.v1 import run_pb2, run_pb2_grpc
from axern.node.sandbox.v1 import node_pb2, node_pb2_grpc
from axern.control.tunnel.v1 import tunnel_pb2, tunnel_pb2_grpc
from axern_sdk._internal.channel import async_control_channel
from axern_sdk._internal.errors import sandbox_rpc_error
from axern_sdk._internal.resources import ResourceQuantity
from axern_sdk._internal.specs import environment_spec, execution_projections
from axern_sdk.context import load_context
from axern_sdk.errors import SandboxLifecycleError, SandboxTimeoutError
from axern_sdk.network_policy import NetworkPolicy
from axern_sdk.client import (
    DEFAULT_ENDPOINT,
    _declared_output_protos,
    _extension_capability_requirements,
    _resource_spec,
    _run_is_terminal,
    _is_transient_stream_code,
    _STREAM_RETRY_MAX_SECONDS,
    _STREAM_RETRY_MIN_SECONDS,
    _sealed_output,
    _write_all,
)
from axern_sdk.models import (
    DeclaredOutput,
    ImageMount,
    SealedOutput,
    SecretEnvVar,
    SecretFile,
)
from axern_sdk.tunnel.config import _GatewayTransport


_T = TypeVar("_T")


async def _control_rpc(
    operation: str, call: Awaitable[_T], *, allocation_id: str | None = None
) -> _T:
    try:
        return await call
    except grpc.RpcError as exc:
        raise sandbox_rpc_error(
            exc, operation=operation, allocation_id=allocation_id
        ) from exc


class AsyncAxernClient:
    """Async aggregated V1 client for the control-plane API."""

    def __init__(
        self,
        target: str,
        *,
        channel: grpc.aio.Channel | None = None,
        tls_ca_cert: str | None = None,
        tls_cert: str | None = None,
        tls_key: str | None = None,
        tls_server_name: str | None = None,
        proxy_mode: str = "env",
    ) -> None:
        self._owns_channel = channel is None
        self._target = target
        self._tls_ca_cert = tls_ca_cert
        self._tls_cert = tls_cert
        self._tls_key = tls_key
        self._tls_server_name = tls_server_name
        self._proxy_mode = proxy_mode
        self._channel: grpc.aio.Channel | None = channel
        self._loop: asyncio.AbstractEventLoop | None = None
        self._environments: environment_pb2_grpc.EnvironmentControlStub | None = None
        self._runs: run_pb2_grpc.RunControlStub | None = None
        self._tunnels: tunnel_pb2_grpc.TunnelControlStub | None = None

    async def close(self) -> None:
        if self._owns_channel and self._channel is not None:
            await self._channel.close()
        if self._owns_channel:
            self._channel = None
            self._loop = None
            self._environments = None
            self._runs = None
            self._tunnels = None

    async def __aenter__(self) -> "AsyncAxernClient":
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        await self.close()

    @classmethod
    def from_env(
        cls,
        *,
        target: str | None = None,
        channel: grpc.aio.Channel | None = None,
        tls_ca_cert: str | None = None,
        tls_cert: str | None = None,
        tls_key: str | None = None,
        tls_server_name: str | None = None,
        proxy_mode: str | None = None,
    ) -> "AsyncAxernClient":
        """Create an async control-plane client from Axern SDK environment variables."""

        return cls(
            target or os.getenv("AXERN_ENDPOINT", DEFAULT_ENDPOINT),
            channel=channel,
            tls_ca_cert=tls_ca_cert or os.getenv("AXERN_TLS_CA_CERT") or None,
            tls_cert=tls_cert or os.getenv("AXERN_TLS_CERT") or None,
            tls_key=tls_key or os.getenv("AXERN_TLS_KEY") or None,
            tls_server_name=tls_server_name
            or os.getenv("AXERN_TLS_SERVER_NAME")
            or None,
            proxy_mode=proxy_mode or os.getenv("AXERN_PROXY_MODE", "env"),
        )

    @classmethod
    def from_context(
        cls, path: str, name: str = "", *, channel: grpc.aio.Channel | None = None
    ) -> "AsyncAxernClient":
        context = load_context(path, name)
        return cls(
            context.endpoint,
            channel=channel,
            tls_ca_cert=context.tls.ca_cert,
            tls_cert=context.tls.cert,
            tls_key=context.tls.key,
            tls_server_name=context.tls.server_name,
            proxy_mode=context.proxy_mode,
        )

    @property
    def environments(self) -> environment_pb2_grpc.EnvironmentControlStub:
        self._ensure_channel()
        assert self._environments is not None
        return self._environments

    @property
    def runs(self) -> run_pb2_grpc.RunControlStub:
        self._ensure_channel()
        assert self._runs is not None
        return self._runs

    @property
    def tunnels(self) -> tunnel_pb2_grpc.TunnelControlStub:
        self._ensure_channel()
        assert self._tunnels is not None
        return self._tunnels

    def _ensure_channel(self) -> None:
        loop = asyncio.get_running_loop()
        if self._channel is not None:
            if self._loop is not None and self._loop is not loop:
                raise RuntimeError(
                    "AsyncAxernClient is bound to a different asyncio event loop"
                )
            if self._loop is None:
                self._loop = loop
            if self._environments is None:
                self._environments = environment_pb2_grpc.EnvironmentControlStub(
                    self._channel
                )
                self._runs = run_pb2_grpc.RunControlStub(self._channel)
                self._tunnels = tunnel_pb2_grpc.TunnelControlStub(self._channel)
            return
        self._loop = loop
        self._channel = async_control_channel(
            self._target,
            tls_ca_cert=self._tls_ca_cert,
            tls_cert=self._tls_cert,
            tls_key=self._tls_key,
            tls_server_name=self._tls_server_name,
            proxy_mode=self._proxy_mode,
        )
        self._environments = environment_pb2_grpc.EnvironmentControlStub(self._channel)
        self._runs = run_pb2_grpc.RunControlStub(self._channel)
        self._tunnels = tunnel_pb2_grpc.TunnelControlStub(self._channel)

    def _gateway_transport(self) -> _GatewayTransport:
        """Return tunnel transport inherited from this gateway client."""

        return _GatewayTransport(
            insecure=not any((self._tls_ca_cert, self._tls_cert, self._tls_key)),
            tls_ca_cert=self._tls_ca_cert or "",
            tls_cert=self._tls_cert or "",
            tls_key=self._tls_key or "",
            server_name=self._tls_server_name or "",
            proxy_mode=self._proxy_mode,
        )

    async def create_environment(
        self,
        *,
        namespace: str = "default",
        image_ref: str = "",
        registry_credential_id: str = "",
        rootfs_readonly: bool = False,
        labels: dict[str, str] | None = None,
        timeout: float | None = 30.0,
    ) -> environment_pb2.Environment:
        spec = environment_spec(
            namespace=namespace,
            image_ref=image_ref,
            registry_credential_id=registry_credential_id,
            rootfs_readonly=rootfs_readonly,
        )
        response = await _control_rpc(
            "create environment",
            self.environments.CreateEnvironment(
                environment_pb2.CreateEnvironmentRequest(
                    spec=spec, labels=dict(labels or {})
                ),
                timeout=timeout,
            ),
        )
        return response.environment

    async def get_environment(
        self,
        environment_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> environment_pb2.Environment:
        response = await _control_rpc(
            "get environment",
            self.environments.GetEnvironment(
                environment_pb2.GetEnvironmentRequest(environment_id=environment_id),
                timeout=timeout,
            ),
        )
        return response.environment

    async def list_environments(
        self,
        *,
        namespace: str = "",
        labels: dict[str, str] | None = None,
        cursor: str = "",
        page_size: int = 0,
        timeout: float | None = 30.0,
    ) -> environment_pb2.ListEnvironmentsResponse:
        return await _control_rpc(
            "list environments",
            self.environments.ListEnvironments(
                environment_pb2.ListEnvironmentsRequest(
                    filter=environment_pb2.ListFilter(
                        namespace=namespace,
                        labels=dict(labels or {}),
                        cursor=cursor,
                        page_size=page_size,
                    )
                ),
                timeout=timeout,
            ),
        )

    async def watch_run(
        self,
        run_id: str,
        *,
        after_version: int = 0,
        timeout: float | None = None,
    ) -> AsyncGenerator[run_pb2.Run, None]:
        """Yield newer run snapshots and resume transient disconnects by version."""

        if not run_id.strip():
            raise ValueError("run_id is required")
        if after_version < 0:
            raise ValueError("after_version must be non-negative")
        deadline = (
            None if timeout is None else asyncio.get_running_loop().time() + timeout
        )
        version = after_version
        retry_delay = _STREAM_RETRY_MIN_SECONDS
        while True:
            remaining = (
                None
                if deadline is None
                else deadline - asyncio.get_running_loop().time()
            )
            if remaining is not None and remaining <= 0:
                raise SandboxTimeoutError(
                    f"run {run_id} watch timed out", operation="watch run"
                )
            call = self.runs.WatchRun(
                run_pb2.WatchRunRequest(run_id=run_id, after_version=version),
                timeout=remaining,
            )
            try:
                async for response in call:
                    if not response.HasField("run") or response.run.version <= version:
                        continue
                    version = response.run.version
                    retry_delay = _STREAM_RETRY_MIN_SECONDS
                    yield response.run
                return
            except grpc.aio.AioRpcError as exc:
                if not _is_transient_stream_code(exc.code()):
                    raise sandbox_rpc_error(exc, operation="watch run") from exc
            finally:
                call.cancel()
            remaining = (
                None
                if deadline is None
                else deadline - asyncio.get_running_loop().time()
            )
            if remaining is not None and remaining <= 0:
                raise SandboxTimeoutError(
                    f"run {run_id} watch timed out", operation="watch run"
                )
            await asyncio.sleep(
                retry_delay if remaining is None else min(retry_delay, remaining)
            )
            retry_delay = min(retry_delay * 2, _STREAM_RETRY_MAX_SECONDS)

    async def create_run(
        self,
        *,
        environment_id: str,
        argv: list[str] | None = None,
        namespace: str = "default",
        env: dict[str, str] | None = None,
        cwd: str = "",
        network_policy: NetworkPolicy | None = None,
        request_cpu: ResourceQuantity = "",
        request_memory: ResourceQuantity = "",
        request_ephemeral_storage: ResourceQuantity = "",
        limit_cpu: ResourceQuantity = "",
        limit_memory: ResourceQuantity = "",
        limit_ephemeral_storage: ResourceQuantity = "",
        extension_capabilities: dict[str, str] | None = None,
        image_mounts: Iterable[ImageMount] | None = None,
        secret_env: Iterable[SecretEnvVar] | None = None,
        secret_files: Iterable[SecretFile] | None = None,
        declared_outputs: Iterable[DeclaredOutput] | None = None,
        rootfs_snapshot: bool = False,
        labels: dict[str, str] | None = None,
        timeout: float | None = 120.0,
    ) -> run_pb2.Run:
        image_mount_protos, secret_env_protos, secret_file_protos = (
            execution_projections(
                image_mounts=image_mounts,
                secret_env=secret_env,
                secret_files=secret_files,
            )
        )
        response = await _control_rpc(
            "create run",
            self.runs.CreateRun(
                run_pb2.CreateRunRequest(
                    namespace=namespace,
                    environment_id=environment_id,
                    config=common_pb2.ExecutionConfig(
                        argv=list(argv or []),
                        env=dict(env or {}),
                        cwd=cwd,
                        network=(
                            common_pb2.NetworkSpec(
                                egress_policy=network_policy._to_proto()
                            )
                            if network_policy is not None
                            else None
                        ),
                        resources=_resource_spec(
                            request_cpu=request_cpu,
                            request_memory=request_memory,
                            request_ephemeral_storage=request_ephemeral_storage,
                            limit_cpu=limit_cpu,
                            limit_memory=limit_memory,
                            limit_ephemeral_storage=limit_ephemeral_storage,
                        ),
                        extension_capability_requirements=_extension_capability_requirements(
                            extension_capabilities
                        ),
                        image_mounts=image_mount_protos,
                        secret_env=secret_env_protos,
                        secret_files=secret_file_protos,
                        declared_outputs=_declared_output_protos(declared_outputs),
                        rootfs_snapshot=(
                            common_pb2.RootfsSnapshot() if rootfs_snapshot else None
                        ),
                    ),
                    labels=dict(labels or {}),
                ),
                timeout=timeout,
            ),
        )
        return response.run

    async def wait_rootfs_snapshot(
        self, run_id: str, *, timeout: float | None = None
    ) -> run_pb2.RootfsSnapshotResult:
        loop = asyncio.get_running_loop()
        deadline = None if timeout is None else loop.time() + timeout
        run = await self.get_run(run_id, timeout=timeout)

        def result(current: run_pb2.Run) -> run_pb2.RootfsSnapshotResult | None:
            if not current.HasField("rootfs_snapshot"):
                raise SandboxLifecycleError(
                    f"run {run_id} did not request a rootfs snapshot"
                )
            if current.rootfs_snapshot.status == run_pb2.ROOTFS_SNAPSHOT_STATUS_READY:
                return current.rootfs_snapshot
            if current.rootfs_snapshot.status == run_pb2.ROOTFS_SNAPSHOT_STATUS_FAILED:
                raise SandboxLifecycleError(
                    f"run {run_id} rootfs snapshot failed: "
                    f"{current.rootfs_snapshot.message}"
                )
            return None

        if snapshot := result(run):
            return snapshot
        remaining = None if deadline is None else max(0.0, deadline - loop.time())
        async for updated in self.watch_run(
            run_id, after_version=run.version, timeout=remaining
        ):
            if snapshot := result(updated):
                return snapshot
        raise SandboxLifecycleError(
            f"run {run_id} watch ended before the rootfs snapshot was finalized"
        )

    async def get_sealed_output_manifest(
        self,
        run_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> list[SealedOutput]:
        run = await self.get_run(run_id, timeout=timeout)
        if not run.allocation_id:
            raise RuntimeError(f"run {run_id} has no allocation")
        channel = self._channel
        assert channel is not None
        response = await _control_rpc(
            "get sealed output manifest",
            node_pb2_grpc.NodeSandboxStub(channel).GetSealedOutputManifest(
                node_pb2.GetSealedOutputManifestRequest(
                    allocation_id=run.allocation_id
                ),
                timeout=timeout,
            ),
            allocation_id=run.allocation_id,
        )
        return [_sealed_output(value) for value in response.outputs]

    async def get_run(
        self, run_id: str, *, timeout: float | None = 30.0
    ) -> run_pb2.Run:
        return (
            await _control_rpc(
                "get run",
                self.runs.GetRun(run_pb2.GetRunRequest(run_id=run_id), timeout=timeout),
            )
        ).run

    async def list_runs(
        self,
        *,
        namespace: str = "",
        statuses: Iterable[run_pb2.RunStatus | str] | None = None,
        labels: dict[str, str] | None = None,
        cursor: str = "",
        page_size: int = 0,
        timeout: float | None = 30.0,
    ) -> run_pb2.ListRunsResponse:
        return await _control_rpc(
            "list runs",
            self.runs.ListRuns(
                run_pb2.ListRunsRequest(
                    filter=run_pb2.RunListFilter(
                        namespace=namespace,
                        statuses=list(statuses or ()),
                        labels=dict(labels or {}),
                        cursor=cursor,
                        page_size=page_size,
                    )
                ),
                timeout=timeout,
            ),
        )

    async def wait_run(
        self, run_id: str, *, timeout: float | None = None
    ) -> run_pb2.Run:
        loop = asyncio.get_running_loop()
        deadline = None if timeout is None else loop.time() + timeout
        run = await self.get_run(run_id, timeout=timeout)
        if _run_is_terminal(run):
            return run
        remaining = None if deadline is None else max(0.0, deadline - loop.time())
        async for updated in self.watch_run(
            run_id, after_version=run.version, timeout=remaining
        ):
            if _run_is_terminal(updated):
                return updated
        raise SandboxLifecycleError(f"run {run_id} watch ended before a terminal state")

    def allocation(self, allocation_id: str):
        """Recover Allocation-scoped operations from a persisted public identity."""

        if not allocation_id.strip():
            raise ValueError("allocation_id is required")
        from axern_sdk.node import AsyncAllocationClient

        return AsyncAllocationClient(client=self, allocation_id=allocation_id)

    async def allocation_for_run(self, run_id: str, *, timeout: float | None = 30.0):
        """Resolve a Run's current Allocation without exposing Node routing."""

        run = await self.get_run(run_id, timeout=timeout)
        if not run.allocation_id:
            raise RuntimeError(f"run {run_id} has no allocation")
        return self.allocation(run.allocation_id)

    async def download_sealed_output(
        self,
        run_id: str,
        output_id: str,
        destination: BinaryIO,
        *,
        offset: int = 0,
        timeout: float | None = None,
    ) -> SealedOutput:
        if offset < 0:
            raise ValueError("offset must be non-negative")
        outputs = await self.get_sealed_output_manifest(run_id, timeout=timeout)
        selected = next(
            (value for value in outputs if value.output_id == output_id), None
        )
        if selected is None or selected.status != "available":
            raise RuntimeError(f"sealed output {output_id} is not available")
        run = await self.get_run(run_id, timeout=timeout)
        channel = self._channel
        assert channel is not None
        call = node_pb2_grpc.NodeSandboxStub(channel).DownloadSealedOutput(
            node_pb2.DownloadSealedOutputRequest(
                allocation_id=run.allocation_id, output_id=output_id, offset=offset
            ),
            timeout=timeout,
        )
        digest = hashlib.sha256()
        position = offset
        try:
            async for response in call:
                if response.next_offset != position + len(response.data):
                    raise RuntimeError("sealed output returned a non-contiguous offset")
                _write_all(destination, response.data)
                if offset == 0:
                    digest.update(response.data)
                position = response.next_offset
                if response.eof:
                    break
        except grpc.RpcError as exc:
            raise sandbox_rpc_error(
                exc,
                operation="download sealed output",
                allocation_id=run.allocation_id,
            ) from exc
        finally:
            call.cancel()
        if position != selected.size_bytes:
            raise RuntimeError("sealed output size does not match its manifest")
        if offset == 0 and digest.hexdigest() != selected.sha256:
            raise RuntimeError("sealed output digest does not match its manifest")
        return selected

    async def cancel_run(
        self, run_id: str, *, timeout: float | None = 30.0
    ) -> run_pb2.Run:
        """Cancel a run and release its allocation."""

        if not run_id.strip():
            raise ValueError("run_id is required")
        response = await _control_rpc(
            "cancel run",
            self.runs.CancelRun(
                run_pb2.CancelRunRequest(run_id=run_id), timeout=timeout
            ),
        )
        return response.run

    async def read_run_output(
        self,
        run_id: str,
        *,
        cursor: str = "",
        follow: bool = False,
        timeout: float | None = None,
    ) -> AsyncGenerator[node_pb2.ReadOutputResponse, None]:
        """Yield Allocation-local output through the Run output_expires_at deadline."""

        if not run_id.strip():
            raise ValueError("run_id is required")
        run = await self.get_run(run_id, timeout=timeout)
        if not run.allocation_id:
            raise RuntimeError(f"run {run_id} output is not available yet")
        next_cursor = cursor
        channel = self._channel
        assert channel is not None
        deadline = (
            None if timeout is None else asyncio.get_running_loop().time() + timeout
        )
        retry_delay = _STREAM_RETRY_MIN_SECONDS
        not_found_since: float | None = None
        while True:
            remaining = (
                None
                if deadline is None
                else deadline - asyncio.get_running_loop().time()
            )
            if remaining is not None and remaining <= 0:
                raise SandboxTimeoutError(
                    f"run {run_id} output read timed out",
                    operation="read run output",
                    allocation_id=run.allocation_id,
                )
            call = node_pb2_grpc.NodeSandboxStub(channel).ReadOutput(
                node_pb2.ReadOutputRequest(
                    allocation_id=run.allocation_id, cursor=next_cursor, follow=follow
                ),
                timeout=remaining,
            )
            try:
                async for event in call:
                    next_cursor = event.next_cursor
                    retry_delay = _STREAM_RETRY_MIN_SECONDS
                    not_found_since = None
                    yield event
                return
            except grpc.aio.AioRpcError as exc:
                startup_not_found = exc.code() == grpc.StatusCode.NOT_FOUND
                if not follow or (
                    not _is_transient_stream_code(exc.code()) and not startup_not_found
                ):
                    raise sandbox_rpc_error(
                        exc,
                        operation="read run output",
                        allocation_id=run.allocation_id,
                    ) from exc
                if startup_not_found:
                    not_found_since = (
                        not_found_since or asyncio.get_running_loop().time()
                    )
                    if asyncio.get_running_loop().time() - not_found_since >= 30:
                        raise sandbox_rpc_error(
                            exc,
                            operation="read run output",
                            allocation_id=run.allocation_id,
                        ) from exc
            finally:
                call.cancel()
            remaining = (
                None
                if deadline is None
                else deadline - asyncio.get_running_loop().time()
            )
            if remaining is not None and remaining <= 0:
                raise SandboxTimeoutError(
                    f"run {run_id} output read timed out",
                    operation="read run output",
                    allocation_id=run.allocation_id,
                )
            await asyncio.sleep(
                retry_delay if remaining is None else min(retry_delay, remaining)
            )
            retry_delay = min(retry_delay * 2, _STREAM_RETRY_MAX_SECONDS)

    async def delete_environment(
        self,
        environment_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> environment_pb2.Environment:
        response = await _control_rpc(
            "delete environment",
            self.environments.DeleteEnvironment(
                environment_pb2.DeleteEnvironmentRequest(environment_id=environment_id),
                timeout=timeout,
            ),
        )
        return response.environment

    async def create_tunnel_session(
        self,
        *,
        allocation_id: str,
        remote_port: int | None = None,
        ttl_seconds: float = 300.0,
        wait_ready: bool = True,
        ready_timeout_seconds: float = 60.0,
        timeout: float | None = 90.0,
    ) -> tunnel_pb2.CreateTunnelSessionResponse:
        request = tunnel_pb2.CreateTunnelSessionRequest(
            allocation_id=allocation_id,
            ttl=duration_pb2.Duration(seconds=int(ttl_seconds)),
            wait_ready=wait_ready,
            ready_timeout=duration_pb2.Duration(seconds=int(ready_timeout_seconds)),
        )
        if remote_port is not None:
            request.remote_port = int(remote_port)
        return await self.tunnels.CreateTunnelSession(request, timeout=timeout)

    async def get_tunnel_session(
        self,
        session_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> tunnel_pb2.TunnelSession:
        response = await self.tunnels.GetTunnelSession(
            tunnel_pb2.GetTunnelSessionRequest(session_id=session_id),
            timeout=timeout,
        )
        return response.session

    async def list_tunnel_events(
        self,
        session_id: str,
        *,
        limit: int = 50,
        timeout: float | None = 30.0,
    ) -> list[tunnel_pb2.TunnelSessionEvent]:
        response = await self.tunnels.ListTunnelSessionEvents(
            tunnel_pb2.ListTunnelSessionEventsRequest(
                session_id=session_id, limit=limit
            ),
            timeout=timeout,
        )
        return list(response.events)

    async def renew_tunnel_session(
        self,
        session_id: str,
        client_token: str,
        *,
        ttl_seconds: float = 300.0,
        timeout: float | None = 30.0,
    ) -> tunnel_pb2.TunnelSession:
        response = await self.tunnels.RenewTunnelSession(
            tunnel_pb2.RenewTunnelSessionRequest(
                session_id=session_id,
                client_token=client_token,
                ttl=duration_pb2.Duration(seconds=int(ttl_seconds)),
            ),
            timeout=timeout,
        )
        return response.session

    async def revoke_tunnel_session(
        self,
        session_id: str,
        *,
        reason: str = "client disconnected",
        timeout: float | None = 30.0,
    ) -> tunnel_pb2.TunnelSession:
        response = await self.tunnels.RevokeTunnelSession(
            tunnel_pb2.RevokeTunnelSessionRequest(session_id=session_id, reason=reason),
            timeout=timeout,
        )
        return response.session
