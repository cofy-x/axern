"""Async V1 control-plane client for environments, services, functions, and tunnels."""

from __future__ import annotations

import asyncio
import os
from collections.abc import AsyncGenerator, Iterable

import grpc
from google.protobuf import duration_pb2

from axern.control.admin.v1 import service_pb2 as admin_service_pb2, service_pb2_grpc as admin_service_pb2_grpc
from axern.control.common.v1 import common_pb2
from axern.control.environment.v1 import environment_pb2, environment_pb2_grpc
from axern.control.function.v1 import function_pb2_grpc
from axern.control.run.v1 import run_pb2, run_pb2_grpc
from axern.node.sandbox.v1 import node_pb2, node_pb2_grpc
from axern.control.service.v1 import (
    service_event_pb2,
    service_pb2,
    service_pb2_grpc,
    service_replica_pb2,
    service_types_pb2,
)
from axern.control.tunnel.v1 import tunnel_pb2, tunnel_pb2_grpc
from axern_sdk._internal.channel import async_control_channel
from axern_sdk._internal.resources import ResourceQuantity
from axern_sdk._internal.specs import environment_spec
from axern_sdk.context import load_context
from axern_sdk.client import (
    DEFAULT_ENDPOINT,
    ServiceProbeInput,
    ServiceVolumeMountInput,
    _extension_capability_requirements,
    _resource_spec,
    _is_transient_service_read_code,
    _service_probe,
    _service_volume_mounts,
    _SERVICE_WATCH_RETRY_MAX_SECONDS,
    _SERVICE_WATCH_RETRY_MIN_SECONDS,
)
from axern_sdk.tunnel.config import _GatewayTransport


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
        self._services: service_pb2_grpc.ServiceControlStub | None = None
        self._service_admin: admin_service_pb2_grpc.ServiceAdminStub | None = None
        self._functions: function_pb2_grpc.FunctionControlStub | None = None
        self._tunnels: tunnel_pb2_grpc.TunnelControlStub | None = None

    async def close(self) -> None:
        if self._owns_channel and self._channel is not None:
            await self._channel.close()
        if self._owns_channel:
            self._channel = None
            self._loop = None
            self._environments = None
            self._runs = None
            self._services = None
            self._service_admin = None
            self._functions = None
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
            tls_server_name=tls_server_name or os.getenv("AXERN_TLS_SERVER_NAME") or None,
            proxy_mode=proxy_mode or os.getenv("AXERN_PROXY_MODE", "env"),
        )

    @classmethod
    def from_context(cls, path: str, name: str = "", *, channel: grpc.aio.Channel | None = None) -> "AsyncAxernClient":
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
    def services(self) -> service_pb2_grpc.ServiceControlStub:
        self._ensure_channel()
        assert self._services is not None
        return self._services

    @property
    def service_admin(self) -> admin_service_pb2_grpc.ServiceAdminStub:
        self._ensure_channel()
        assert self._service_admin is not None
        return self._service_admin

    @property
    def functions(self) -> function_pb2_grpc.FunctionControlStub:
        self._ensure_channel()
        assert self._functions is not None
        return self._functions

    @property
    def tunnels(self) -> tunnel_pb2_grpc.TunnelControlStub:
        self._ensure_channel()
        assert self._tunnels is not None
        return self._tunnels

    def _ensure_channel(self) -> None:
        loop = asyncio.get_running_loop()
        if self._channel is not None:
            if self._loop is not None and self._loop is not loop:
                raise RuntimeError("AsyncAxernClient is bound to a different asyncio event loop")
            if self._loop is None:
                self._loop = loop
            if self._environments is None:
                self._environments = environment_pb2_grpc.EnvironmentControlStub(self._channel)
                self._runs = run_pb2_grpc.RunControlStub(self._channel)
                self._services = service_pb2_grpc.ServiceControlStub(self._channel)
                self._service_admin = admin_service_pb2_grpc.ServiceAdminStub(self._channel)
                self._functions = function_pb2_grpc.FunctionControlStub(self._channel)
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
        self._services = service_pb2_grpc.ServiceControlStub(self._channel)
        self._service_admin = admin_service_pb2_grpc.ServiceAdminStub(self._channel)
        self._functions = function_pb2_grpc.FunctionControlStub(self._channel)
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
        response = await self.environments.CreateEnvironment(
            environment_pb2.CreateEnvironmentRequest(spec=spec, labels=dict(labels or {})),
            timeout=timeout,
        )
        return response.environment

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
        deadline = None if timeout is None else asyncio.get_running_loop().time() + timeout
        version = after_version
        retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
        while True:
            remaining = None if deadline is None else deadline - asyncio.get_running_loop().time()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"run {run_id} watch timed out")
            call = self.runs.WatchRun(
                run_pb2.WatchRunRequest(run_id=run_id, after_version=version),
                timeout=remaining,
            )
            try:
                async for response in call:
                    if not response.HasField("run") or response.run.version <= version:
                        continue
                    version = response.run.version
                    retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
                    yield response.run
                return
            except grpc.aio.AioRpcError as exc:
                if not _is_transient_service_read_code(exc.code()):
                    raise
            finally:
                call.cancel()
            remaining = None if deadline is None else deadline - asyncio.get_running_loop().time()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"run {run_id} watch timed out")
            await asyncio.sleep(retry_delay if remaining is None else min(retry_delay, remaining))
            retry_delay = min(retry_delay * 2, _SERVICE_WATCH_RETRY_MAX_SECONDS)

    async def read_run_output(
        self,
        run_id: str,
        *,
        cursor: str = "",
        follow: bool = False,
        timeout: float | None = None,
    ) -> AsyncGenerator[node_pb2.ReadOutputResponse, None]:
        """Yield stdout/stderr events, resuming transient disconnects by cursor."""

        if not run_id.strip():
            raise ValueError("run_id is required")
        run = (await self.runs.GetRun(run_pb2.GetRunRequest(run_id=run_id), timeout=timeout)).run
        if not run.allocation_id:
            raise RuntimeError(f"run {run_id} output is not available yet")
        next_cursor = cursor
        channel = self._channel
        assert channel is not None
        deadline = None if timeout is None else asyncio.get_running_loop().time() + timeout
        retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
        not_found_since: float | None = None
        while True:
            remaining = None if deadline is None else deadline - asyncio.get_running_loop().time()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"run {run_id} output read timed out")
            call = node_pb2_grpc.NodeSandboxStub(channel).ReadOutput(
                node_pb2.ReadOutputRequest(allocation_id=run.allocation_id, cursor=next_cursor, follow=follow),
                timeout=remaining,
            )
            try:
                async for event in call:
                    next_cursor = event.next_cursor
                    retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
                    not_found_since = None
                    yield event
                return
            except grpc.aio.AioRpcError as exc:
                startup_not_found = exc.code() == grpc.StatusCode.NOT_FOUND
                if not follow or (not _is_transient_service_read_code(exc.code()) and not startup_not_found):
                    raise
                if startup_not_found:
                    not_found_since = not_found_since or asyncio.get_running_loop().time()
                    if asyncio.get_running_loop().time() - not_found_since >= 30:
                        raise
            finally:
                call.cancel()
            remaining = None if deadline is None else deadline - asyncio.get_running_loop().time()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"run {run_id} output read timed out")
            await asyncio.sleep(retry_delay if remaining is None else min(retry_delay, remaining))
            retry_delay = min(retry_delay * 2, _SERVICE_WATCH_RETRY_MAX_SECONDS)

    async def delete_environment(
        self,
        environment_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> environment_pb2.Environment:
        response = await self.environments.DeleteEnvironment(
            environment_pb2.DeleteEnvironmentRequest(environment_id=environment_id),
            timeout=timeout,
        )
        return response.environment

    async def create_service(
        self,
        *,
        environment_id: str,
        replicas: int = 1,
        argv: list[str] | None = None,
        env: dict[str, str] | None = None,
        cwd: str = "",
        runtime_class: str = "",
        request_cpu: ResourceQuantity = "",
        request_memory: ResourceQuantity = "",
        request_ephemeral_storage: ResourceQuantity = "",
        limit_cpu: ResourceQuantity = "",
        limit_memory: ResourceQuantity = "",
        limit_ephemeral_storage: ResourceQuantity = "",
        extension_capabilities: dict[str, str] | None = None,
        node_selector: dict[str, str] | None = None,
        volume_mounts: Iterable[ServiceVolumeMountInput] | None = None,
        readiness_probe: ServiceProbeInput | None = None,
        liveness_probe: ServiceProbeInput | None = None,
        namespace: str = "default",
        labels: dict[str, str] | None = None,
        timeout: float | None = 120.0,
    ) -> service_types_pb2.Service:
        readiness = _service_probe(readiness_probe)
        liveness = _service_probe(liveness_probe)
        response = await self.services.CreateService(
            service_pb2.CreateServiceRequest(
                namespace=namespace,
                environment_id=environment_id,
                replicas=replicas,
                config=common_pb2.ExecutionConfig(
                    argv=list(argv or []),
                    env=dict(env or {}),
                    cwd=cwd,
                    runtime_class=runtime_class,
                    resources=_resource_spec(
                        request_cpu=request_cpu,
                        request_memory=request_memory,
                        request_ephemeral_storage=request_ephemeral_storage,
                        limit_cpu=limit_cpu,
                        limit_memory=limit_memory,
                        limit_ephemeral_storage=limit_ephemeral_storage,
                    ),
                    extension_capability_requirements=_extension_capability_requirements(extension_capabilities),
                    placement=common_pb2.PlacementConstraints(
                        node_selector=dict(node_selector or {}),
                    ),
                    volume_mounts=_service_volume_mounts(volume_mounts),
                ),
                readiness_probe=readiness,
                liveness_probe=liveness,
                labels=dict(labels or {}),
            ),
            timeout=timeout,
        )
        return response.service

    async def get_service(
        self,
        service_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> service_types_pb2.Service:
        service_id = service_id.strip()
        if not service_id:
            raise ValueError("service_id is required")
        response = await self.services.GetService(
            service_pb2.GetServiceRequest(service_id=service_id),
            timeout=timeout,
        )
        return response.service

    async def list_services(
        self,
        *,
        namespace: str = "",
        statuses: Iterable[service_types_pb2.ServiceStatus] | None = None,
        labels: dict[str, str] | None = None,
        page_size: int = 200,
        timeout: float | None = 30.0,
    ) -> list[service_types_pb2.Service]:
        if page_size <= 0:
            raise ValueError("page_size must be positive")
        status_filter = list(statuses or [])
        loop = asyncio.get_running_loop()
        deadline = None if timeout is None else loop.time() + timeout
        cursor = ""
        services: list[service_types_pb2.Service] = []
        while True:
            remaining = None if deadline is None else deadline - loop.time()
            if remaining is not None and remaining <= 0:
                raise TimeoutError("list services timed out")
            response = await self.services.ListServices(
                service_pb2.ListServicesRequest(
                    filter=service_types_pb2.ServiceListFilter(
                        namespace=namespace,
                        statuses=status_filter,
                        labels=dict(labels or {}),
                        cursor=cursor,
                        page_size=page_size,
                    )
                ),
                timeout=remaining,
            )
            services.extend(response.services)
            cursor = response.next_cursor
            if not cursor:
                return services

    async def list_service_events(
        self,
        service_id: str,
        *,
        limit: int = 50,
        timeout: float | None = 30.0,
    ) -> list[service_event_pb2.ServiceEvent]:
        service_id = service_id.strip()
        if not service_id:
            raise ValueError("service_id is required")
        if limit <= 0:
            raise ValueError("limit must be positive")
        response = await self.services.ListServiceEvents(
            service_event_pb2.ListServiceEventsRequest(service_id=service_id, limit=limit),
            timeout=timeout,
        )
        return list(response.events)

    async def list_service_replicas(
        self,
        service_id: str,
        *,
        current_only: bool = True,
        timeout: float | None = 30.0,
    ) -> list[service_replica_pb2.ServiceReplica]:
        view = service_replica_pb2.ServiceReplicaView.SERVICE_REPLICA_VIEW_CURRENT
        if not current_only:
            view = service_replica_pb2.ServiceReplicaView.SERVICE_REPLICA_VIEW_ALL
        request = service_replica_pb2.ListServiceReplicasRequest(
            service_id=service_id,
            filter=service_replica_pb2.ServiceReplicaListFilter(view=view),
        )
        loop = asyncio.get_running_loop()
        deadline = None if timeout is None else loop.time() + timeout
        retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
        last_error: grpc.aio.AioRpcError | None = None
        while True:
            remaining = None if deadline is None else deadline - loop.time()
            if remaining is not None and remaining <= 0:
                if last_error is not None:
                    raise last_error
                raise TimeoutError(f"list replicas for service {service_id} timed out")
            try:
                response = await self.services.ListServiceReplicas(request, timeout=remaining)
                return list(response.replicas)
            except grpc.aio.AioRpcError as exc:
                if not _is_transient_service_read_code(exc.code()):
                    raise
                last_error = exc
            remaining = None if deadline is None else deadline - loop.time()
            if remaining is not None and remaining <= 0:
                if last_error is not None:
                    raise last_error
                raise TimeoutError(f"list replicas for service {service_id} timed out")
            sleep_seconds = retry_delay if remaining is None else min(retry_delay, remaining)
            await asyncio.sleep(sleep_seconds)
            retry_delay = min(retry_delay * 2, _SERVICE_WATCH_RETRY_MAX_SECONDS)

    async def watch_service(
        self,
        service_id: str,
        *,
        after_version: int = 0,
        timeout: float | None = None,
    ) -> AsyncGenerator[service_types_pb2.Service, None]:
        """Yield monotonically newer service snapshots and resume transient disconnects."""

        if not service_id.strip():
            raise ValueError("service_id is required")
        if after_version < 0:
            raise ValueError("after_version must be non-negative")
        loop = asyncio.get_running_loop()
        deadline = None if timeout is None else loop.time() + timeout
        last_version = after_version
        retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
        while True:
            remaining = None if deadline is None else deadline - loop.time()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"service {service_id} watch timed out")
            call = self.services.WatchService(
                service_pb2.WatchServiceRequest(
                    service_id=service_id,
                    after_version=last_version,
                ),
                timeout=remaining,
            )
            try:
                async for response in call:
                    if not response.HasField("service"):
                        raise RuntimeError("WatchService response did not include a service")
                    service = response.service
                    if service.version <= last_version:
                        continue
                    last_version = service.version
                    retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
                    yield service
            except grpc.aio.AioRpcError as exc:
                if not _is_transient_service_read_code(exc.code()):
                    raise
            finally:
                call.cancel()

            remaining = None if deadline is None else deadline - loop.time()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"service {service_id} watch timed out")
            sleep_seconds = retry_delay if remaining is None else min(retry_delay, remaining)
            await asyncio.sleep(sleep_seconds)
            retry_delay = min(retry_delay * 2, _SERVICE_WATCH_RETRY_MAX_SECONDS)

    async def delete_service(
        self,
        service_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> service_types_pb2.Service:
        response = await self.services.DeleteService(
            service_pb2.DeleteServiceRequest(service_id=service_id),
            timeout=timeout,
        )
        return response.service

    async def admin_purge_service(
        self,
        service_id: str,
        *,
        operator_reason: str,
        timeout: float | None = 30.0,
    ) -> str:
        if not operator_reason.strip():
            raise ValueError("operator_reason is required")
        response = await self.service_admin.PurgeService(
            admin_service_pb2.PurgeServiceRequest(
                service_id=service_id,
                operator_reason=operator_reason,
            ),
            timeout=timeout,
        )
        return response.service_id

    async def create_tunnel_session(
        self,
        *,
        allocation_id: str,
        local_target: str,
        remote_port: int | None = None,
        ttl_seconds: float = 300.0,
        wait_ready: bool = True,
        ready_timeout_seconds: float = 60.0,
        timeout: float | None = 90.0,
    ) -> tunnel_pb2.CreateTunnelSessionResponse:
        request = tunnel_pb2.CreateTunnelSessionRequest(
            allocation_id=allocation_id,
            local_target=local_target,
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
            tunnel_pb2.ListTunnelSessionEventsRequest(session_id=session_id, limit=limit),
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
