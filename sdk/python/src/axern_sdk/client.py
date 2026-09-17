"""V1 control-plane client for environments, runs, and tunnels."""

from __future__ import annotations

import os
import time
import hashlib
from collections.abc import Generator, Iterable
from typing import BinaryIO, Callable, TypeVar

import grpc
from google.protobuf import duration_pb2

from axern.control.capability.v1 import capability_pb2
from axern.control.common.v1 import common_pb2
from axern.control.environment.v1 import environment_pb2, environment_pb2_grpc
from axern.control.run.v1 import run_pb2, run_pb2_grpc
from axern.node.sandbox.v1 import node_pb2, node_pb2_grpc
from axern.control.tunnel.v1 import tunnel_pb2, tunnel_pb2_grpc
from axern_sdk._internal.channel import control_channel
from axern_sdk._internal.errors import sandbox_rpc_error
from axern_sdk._internal.resources import ResourceQuantity, cpu_milli, memory_bytes
from axern_sdk._internal.specs import environment_spec
from axern_sdk.context import load_context
from axern_sdk.errors import SandboxLifecycleError, SandboxTimeoutError
from axern_sdk.network_policy import NetworkPolicy
from axern_sdk.models import DeclaredOutput, DeclaredOutputFormat, SealedOutput
from axern_sdk.tunnel.config import _GatewayTransport


DEFAULT_ENDPOINT = "127.0.0.1:25000"
_STREAM_RETRY_MIN_SECONDS = 0.1
_STREAM_RETRY_MAX_SECONDS = 2.0
_T = TypeVar("_T")


def _control_rpc(
    operation: str, call: Callable[[], _T], *, allocation_id: str | None = None
) -> _T:
    try:
        return call()
    except grpc.RpcError as exc:
        raise sandbox_rpc_error(
            exc, operation=operation, allocation_id=allocation_id
        ) from exc


def _write_all(destination: BinaryIO, data: bytes) -> None:
    remaining = memoryview(data)
    while remaining:
        written = destination.write(remaining)
        if written is None:
            return
        if written <= 0:
            raise OSError("sealed output destination made no write progress")
        remaining = remaining[written:]


def _extension_capability_requirements(
    values: dict[str, str] | None,
) -> list[capability_pb2.ExtensionCapabilityRequirement]:
    return [
        capability_pb2.ExtensionCapabilityRequirement(
            capability=capability_pb2.ExtensionCapability(name=name, value=value),
        )
        for name, value in sorted((values or {}).items())
    ]


def _is_transient_stream_code(code: grpc.StatusCode) -> bool:
    return code in {grpc.StatusCode.UNAVAILABLE, grpc.StatusCode.DEADLINE_EXCEEDED}


def _run_is_terminal(run: run_pb2.Run) -> bool:
    return run.status in {
        run_pb2.RUN_STATUS_SUCCEEDED,
        run_pb2.RUN_STATUS_FAILED,
        run_pb2.RUN_STATUS_CANCELLED,
    }


def _resource_spec(
    *,
    request_cpu: ResourceQuantity = "",
    request_memory: ResourceQuantity = "",
    request_ephemeral_storage: ResourceQuantity = "",
    limit_cpu: ResourceQuantity = "",
    limit_memory: ResourceQuantity = "",
    limit_ephemeral_storage: ResourceQuantity = "",
) -> common_pb2.ResourceSpec | None:
    request_cpu_milli = cpu_milli("request_cpu", request_cpu)
    request_memory_bytes = memory_bytes("request_memory", request_memory)
    request_ephemeral_storage_bytes = memory_bytes(
        "request_ephemeral_storage", request_ephemeral_storage
    )
    limit_cpu_milli = cpu_milli("limit_cpu", limit_cpu)
    limit_memory_bytes = memory_bytes("limit_memory", limit_memory)
    limit_ephemeral_storage_bytes = memory_bytes(
        "limit_ephemeral_storage", limit_ephemeral_storage
    )

    resources = common_pb2.ResourceSpec()
    if (
        request_cpu_milli > 0
        or request_memory_bytes > 0
        or request_ephemeral_storage_bytes > 0
    ):
        resources.requests.cpu_milli = request_cpu_milli
        resources.requests.memory_bytes = request_memory_bytes
        resources.requests.ephemeral_storage_bytes = request_ephemeral_storage_bytes
    if (
        limit_cpu_milli > 0
        or limit_memory_bytes > 0
        or limit_ephemeral_storage_bytes > 0
    ):
        resources.limits.cpu_milli = limit_cpu_milli
        resources.limits.memory_bytes = limit_memory_bytes
        resources.limits.ephemeral_storage_bytes = limit_ephemeral_storage_bytes
    if not resources.HasField("requests") and not resources.HasField("limits"):
        return None
    return resources


def _declared_output_protos(
    values: Iterable[DeclaredOutput] | None,
) -> list[common_pb2.DeclaredOutput]:
    formats = {
        DeclaredOutputFormat.FILE: common_pb2.DECLARED_OUTPUT_FORMAT_FILE,
        DeclaredOutputFormat.TAR: common_pb2.DECLARED_OUTPUT_FORMAT_TAR,
    }
    return [
        common_pb2.DeclaredOutput(
            path=value.path, format=formats[value.format], media_type=value.media_type
        )
        for value in values or ()
    ]


def _sealed_output(value: node_pb2.SealedOutput) -> SealedOutput:
    formats = {
        common_pb2.DECLARED_OUTPUT_FORMAT_FILE: DeclaredOutputFormat.FILE,
        common_pb2.DECLARED_OUTPUT_FORMAT_TAR: DeclaredOutputFormat.TAR,
    }
    status = (
        node_pb2.SealedOutputStatus.Name(value.status)
        .removeprefix("SEALED_OUTPUT_STATUS_")
        .lower()
    )
    return SealedOutput(
        output_id=value.output_id,
        path=value.path,
        size_bytes=value.size_bytes,
        sha256=value.sha256,
        media_type=value.media_type,
        format=formats.get(value.format),
        status=status,
        reason=value.reason,
        sealed_at=value.sealed_at.ToDatetime() if value.HasField("sealed_at") else None,
        expires_at=value.expires_at.ToDatetime()
        if value.HasField("expires_at")
        else None,
    )


class AxernClient:
    """Aggregated V1 client for the control-plane API."""

    def __init__(
        self,
        target: str,
        *,
        channel: grpc.Channel | None = None,
        tls_ca_cert: str | None = None,
        tls_cert: str | None = None,
        tls_key: str | None = None,
        tls_server_name: str | None = None,
        proxy_mode: str = "env",
    ) -> None:
        self._owns_channel = channel is None
        self._tls_ca_cert = tls_ca_cert or ""
        self._tls_cert = tls_cert or ""
        self._tls_key = tls_key or ""
        self._tls_server_name = tls_server_name or ""
        self._proxy_mode = proxy_mode
        self._channel = channel or control_channel(
            target,
            tls_ca_cert=self._tls_ca_cert,
            tls_cert=self._tls_cert,
            tls_key=self._tls_key,
            tls_server_name=self._tls_server_name,
            proxy_mode=self._proxy_mode,
        )
        self.environments = environment_pb2_grpc.EnvironmentControlStub(self._channel)
        self.runs = run_pb2_grpc.RunControlStub(self._channel)
        self.tunnels = tunnel_pb2_grpc.TunnelControlStub(self._channel)

    @classmethod
    def from_env(
        cls,
        *,
        target: str | None = None,
        channel: grpc.Channel | None = None,
        tls_ca_cert: str | None = None,
        tls_cert: str | None = None,
        tls_key: str | None = None,
        tls_server_name: str | None = None,
        proxy_mode: str | None = None,
    ) -> "AxernClient":
        """Create a control-plane client from Axern SDK environment variables."""

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
        cls, path: str, name: str = "", *, channel: grpc.Channel | None = None
    ) -> "AxernClient":
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

    def close(self) -> None:
        if self._owns_channel:
            self._channel.close()

    def _gateway_transport(self) -> _GatewayTransport:
        """Return tunnel transport inherited from this gateway client."""

        return _GatewayTransport(
            insecure=not any((self._tls_ca_cert, self._tls_cert, self._tls_key)),
            tls_ca_cert=self._tls_ca_cert,
            tls_cert=self._tls_cert,
            tls_key=self._tls_key,
            server_name=self._tls_server_name,
            proxy_mode=self._proxy_mode,
        )

    def create_environment(
        self,
        *,
        template_id: str = "",
        namespace: str = "default",
        template_version: str = "",
        image_ref: str = "",
        registry_credential_id: str = "",
        rootfs_readonly: bool = False,
        labels: dict[str, str] | None = None,
        timeout: float | None = 30.0,
    ) -> environment_pb2.Environment:
        spec = environment_spec(
            namespace=namespace,
            template_id=template_id,
            template_version=template_version,
            image_ref=image_ref,
            registry_credential_id=registry_credential_id,
            rootfs_readonly=rootfs_readonly,
        )
        response = _control_rpc(
            "create environment",
            lambda: self.environments.CreateEnvironment(
                environment_pb2.CreateEnvironmentRequest(
                    spec=spec, labels=dict(labels or {})
                ),
                timeout=timeout,
            ),
        )
        return response.environment

    def delete_environment(
        self,
        environment_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> environment_pb2.Environment:
        response = _control_rpc(
            "delete environment",
            lambda: self.environments.DeleteEnvironment(
                environment_pb2.DeleteEnvironmentRequest(environment_id=environment_id),
                timeout=timeout,
            ),
        )
        return response.environment

    def get_environment(
        self,
        environment_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> environment_pb2.Environment:
        response = _control_rpc(
            "get environment",
            lambda: self.environments.GetEnvironment(
                environment_pb2.GetEnvironmentRequest(environment_id=environment_id),
                timeout=timeout,
            ),
        )
        return response.environment

    def list_environments(
        self,
        *,
        namespace: str = "",
        labels: dict[str, str] | None = None,
        cursor: str = "",
        page_size: int = 0,
        timeout: float | None = 30.0,
    ) -> environment_pb2.ListEnvironmentsResponse:
        return _control_rpc(
            "list environments",
            lambda: self.environments.ListEnvironments(
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

    def create_run(
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
        declared_outputs: Iterable[DeclaredOutput] | None = None,
        labels: dict[str, str] | None = None,
        timeout: float | None = 120.0,
    ) -> run_pb2.Run:
        response = _control_rpc(
            "create run",
            lambda: self.runs.CreateRun(
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
                        declared_outputs=_declared_output_protos(declared_outputs),
                    ),
                    labels=dict(labels or {}),
                ),
                timeout=timeout,
            ),
        )
        return response.run

    def get_sealed_output_manifest(
        self,
        run_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> list[SealedOutput]:
        """Return immutable declared-output metadata for a terminal Run."""

        run = self.get_run(run_id, timeout=timeout)
        if not run.allocation_id:
            raise RuntimeError(f"run {run_id} has no allocation")
        response = _control_rpc(
            "get sealed output manifest",
            lambda: node_pb2_grpc.NodeSandboxStub(
                self._channel
            ).GetSealedOutputManifest(
                node_pb2.GetSealedOutputManifestRequest(
                    allocation_id=run.allocation_id
                ),
                timeout=timeout,
            ),
            allocation_id=run.allocation_id,
        )
        return [_sealed_output(value) for value in response.outputs]

    def get_run(self, run_id: str, *, timeout: float | None = 30.0) -> run_pb2.Run:
        return _control_rpc(
            "get run",
            lambda: self.runs.GetRun(
                run_pb2.GetRunRequest(run_id=run_id), timeout=timeout
            ),
        ).run

    def list_runs(
        self,
        *,
        namespace: str = "",
        statuses: Iterable[run_pb2.RunStatus | str] | None = None,
        labels: dict[str, str] | None = None,
        cursor: str = "",
        page_size: int = 0,
        timeout: float | None = 30.0,
    ) -> run_pb2.ListRunsResponse:
        return _control_rpc(
            "list runs",
            lambda: self.runs.ListRuns(
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

    def wait_run(self, run_id: str, *, timeout: float | None = None) -> run_pb2.Run:
        deadline = None if timeout is None else time.monotonic() + timeout
        run = self.get_run(run_id, timeout=timeout)
        if _run_is_terminal(run):
            return run
        remaining = None if deadline is None else max(0.0, deadline - time.monotonic())
        for updated in self.watch_run(
            run_id, after_version=run.version, timeout=remaining
        ):
            if _run_is_terminal(updated):
                return updated
        raise SandboxLifecycleError(
            f"run {run_id} watch ended before a terminal state"
        )

    def allocation(self, allocation_id: str):
        """Recover Allocation-scoped operations from a persisted public identity."""

        if not allocation_id.strip():
            raise ValueError("allocation_id is required")
        from axern_sdk.node import AllocationClient

        return AllocationClient(client=self, allocation_id=allocation_id)

    def allocation_for_run(self, run_id: str, *, timeout: float | None = 30.0):
        """Resolve a Run's current Allocation without exposing Node routing."""

        run = self.get_run(run_id, timeout=timeout)
        if not run.allocation_id:
            raise RuntimeError(f"run {run_id} has no allocation")
        return self.allocation(run.allocation_id)

    def download_sealed_output(
        self,
        run_id: str,
        output_id: str,
        destination: BinaryIO,
        *,
        offset: int = 0,
        timeout: float | None = None,
    ) -> SealedOutput:
        """Stream one sealed output and verify size and digest for full downloads."""

        if offset < 0:
            raise ValueError("offset must be non-negative")
        outputs = self.get_sealed_output_manifest(run_id, timeout=timeout)
        selected = next(
            (value for value in outputs if value.output_id == output_id), None
        )
        if selected is None or selected.status != "available":
            raise RuntimeError(f"sealed output {output_id} is not available")
        run = self.get_run(run_id, timeout=timeout)
        digest = hashlib.sha256()
        position = offset
        call = node_pb2_grpc.NodeSandboxStub(self._channel).DownloadSealedOutput(
            node_pb2.DownloadSealedOutputRequest(
                allocation_id=run.allocation_id,
                output_id=output_id,
                offset=offset,
            ),
            timeout=timeout,
        )
        try:
            for response in call:
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

    def cancel_run(self, run_id: str, *, timeout: float | None = 30.0) -> run_pb2.Run:
        """Cancel a run and release its allocation."""

        if not run_id.strip():
            raise ValueError("run_id is required")
        response = _control_rpc(
            "cancel run",
            lambda: self.runs.CancelRun(
                run_pb2.CancelRunRequest(run_id=run_id), timeout=timeout
            ),
        )
        return response.run

    def watch_run(
        self,
        run_id: str,
        *,
        after_version: int = 0,
        timeout: float | None = None,
    ) -> Generator[run_pb2.Run, None, None]:
        """Yield newer run snapshots and resume transient disconnects by version."""

        if not run_id.strip():
            raise ValueError("run_id is required")
        if after_version < 0:
            raise ValueError("after_version must be non-negative")
        deadline = None if timeout is None else time.monotonic() + timeout
        version = after_version
        retry_delay = _STREAM_RETRY_MIN_SECONDS
        while True:
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise SandboxTimeoutError(
                    f"run {run_id} watch timed out", operation="watch run"
                )
            call = self.runs.WatchRun(
                run_pb2.WatchRunRequest(run_id=run_id, after_version=version),
                timeout=remaining,
            )
            try:
                for response in call:
                    if not response.HasField("run") or response.run.version <= version:
                        continue
                    version = response.run.version
                    retry_delay = _STREAM_RETRY_MIN_SECONDS
                    yield response.run
                return
            except grpc.RpcError as exc:
                if not _is_transient_stream_code(exc.code()):
                    raise sandbox_rpc_error(exc, operation="watch run") from exc
            finally:
                call.cancel()
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise SandboxTimeoutError(
                    f"run {run_id} watch timed out", operation="watch run"
                )
            time.sleep(
                retry_delay if remaining is None else min(retry_delay, remaining)
            )
            retry_delay = min(retry_delay * 2, _STREAM_RETRY_MAX_SECONDS)

    def read_run_output(
        self,
        run_id: str,
        *,
        cursor: str = "",
        follow: bool = False,
        timeout: float | None = None,
    ) -> Generator[node_pb2.ReadOutputResponse, None, None]:
        """Yield Allocation-local output through the Run output_expires_at deadline."""

        if not run_id.strip():
            raise ValueError("run_id is required")
        run = self.get_run(run_id, timeout=timeout)
        if not run.allocation_id:
            raise RuntimeError(f"run {run_id} output is not available yet")
        next_cursor = cursor
        deadline = None if timeout is None else time.monotonic() + timeout
        retry_delay = _STREAM_RETRY_MIN_SECONDS
        not_found_since: float | None = None
        while True:
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise SandboxTimeoutError(
                    f"run {run_id} output read timed out",
                    operation="read run output",
                    allocation_id=run.allocation_id,
                )
            call = node_pb2_grpc.NodeSandboxStub(self._channel).ReadOutput(
                node_pb2.ReadOutputRequest(
                    allocation_id=run.allocation_id, cursor=next_cursor, follow=follow
                ),
                timeout=remaining,
            )
            try:
                for event in call:
                    next_cursor = event.next_cursor
                    retry_delay = _STREAM_RETRY_MIN_SECONDS
                    not_found_since = None
                    yield event
                return
            except grpc.RpcError as exc:
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
                    not_found_since = not_found_since or time.monotonic()
                    if time.monotonic() - not_found_since >= 30:
                        raise sandbox_rpc_error(
                            exc,
                            operation="read run output",
                            allocation_id=run.allocation_id,
                        ) from exc
            finally:
                call.cancel()
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise SandboxTimeoutError(
                    f"run {run_id} output read timed out",
                    operation="read run output",
                    allocation_id=run.allocation_id,
                )
            time.sleep(
                retry_delay if remaining is None else min(retry_delay, remaining)
            )
            retry_delay = min(retry_delay * 2, _STREAM_RETRY_MAX_SECONDS)

    def create_tunnel_session(
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
        return self.tunnels.CreateTunnelSession(request, timeout=timeout)

    def get_tunnel_session(
        self,
        session_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> tunnel_pb2.TunnelSession:
        response = self.tunnels.GetTunnelSession(
            tunnel_pb2.GetTunnelSessionRequest(session_id=session_id),
            timeout=timeout,
        )
        return response.session

    def list_tunnel_events(
        self,
        session_id: str,
        *,
        limit: int = 50,
        timeout: float | None = 30.0,
    ) -> list[tunnel_pb2.TunnelSessionEvent]:
        response = self.tunnels.ListTunnelSessionEvents(
            tunnel_pb2.ListTunnelSessionEventsRequest(
                session_id=session_id, limit=limit
            ),
            timeout=timeout,
        )
        return list(response.events)

    def inspect_tunnel_session(
        self,
        session_id: str,
        *,
        event_limit: int = 50,
        timeout: float | None = 30.0,
    ) -> tunnel_pb2.InspectTunnelSessionResponse:
        return self.tunnels.InspectTunnelSession(
            tunnel_pb2.InspectTunnelSessionRequest(
                session_id=session_id, event_limit=event_limit
            ),
            timeout=timeout,
        )

    def renew_tunnel_session(
        self,
        session_id: str,
        client_token: str,
        *,
        ttl_seconds: float = 300.0,
        timeout: float | None = 30.0,
    ) -> tunnel_pb2.TunnelSession:
        response = self.tunnels.RenewTunnelSession(
            tunnel_pb2.RenewTunnelSessionRequest(
                session_id=session_id,
                client_token=client_token,
                ttl=duration_pb2.Duration(seconds=int(ttl_seconds)),
            ),
            timeout=timeout,
        )
        return response.session

    def revoke_tunnel_session(
        self,
        session_id: str,
        *,
        reason: str = "client disconnected",
        timeout: float | None = 30.0,
    ) -> tunnel_pb2.TunnelSession:
        response = self.tunnels.RevokeTunnelSession(
            tunnel_pb2.RevokeTunnelSessionRequest(session_id=session_id, reason=reason),
            timeout=timeout,
        )
        return response.session
