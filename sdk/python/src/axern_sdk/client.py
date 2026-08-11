"""V1 control-plane client for environments, runs, services, and functions."""

from __future__ import annotations

import os
import time
from collections.abc import Generator, Iterable

import grpc
from google.protobuf import duration_pb2

from axern.control.admin.v1 import (
    node_pb2_grpc as admin_node_pb2_grpc,
    service_pb2 as admin_service_pb2,
    service_pb2_grpc as admin_service_pb2_grpc,
)
from axern.control.capability.v1 import capability_pb2
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
from axern_sdk._internal.channel import control_channel
from axern_sdk._internal.resources import ResourceQuantity, cpu_milli, memory_bytes
from axern_sdk._internal.specs import environment_spec
from axern_sdk.context import load_context
from axern_sdk.models import HTTPProbe, ServiceProbe, TCPProbe, VolumeMount
from axern_sdk.tunnel.config import _GatewayTransport


DEFAULT_ENDPOINT = "127.0.0.1:25000"
_SERVICE_WATCH_RETRY_MIN_SECONDS = 0.1
_SERVICE_WATCH_RETRY_MAX_SECONDS = 2.0

ServiceVolumeMountInput = VolumeMount | common_pb2.ServiceVolumeMount
ServiceProbeInput = ServiceProbe | service_types_pb2.ServiceProbe


def _extension_capability_requirements(
    values: dict[str, str] | None,
) -> list[capability_pb2.ExtensionCapabilityRequirement]:
    return [
        capability_pb2.ExtensionCapabilityRequirement(
            capability=capability_pb2.ExtensionCapability(name=name, value=value),
        )
        for name, value in sorted((values or {}).items())
    ]


def _is_transient_service_read_code(code: grpc.StatusCode) -> bool:
    return code in {grpc.StatusCode.UNAVAILABLE, grpc.StatusCode.DEADLINE_EXCEEDED}


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
    request_ephemeral_storage_bytes = memory_bytes("request_ephemeral_storage", request_ephemeral_storage)
    limit_cpu_milli = cpu_milli("limit_cpu", limit_cpu)
    limit_memory_bytes = memory_bytes("limit_memory", limit_memory)
    limit_ephemeral_storage_bytes = memory_bytes("limit_ephemeral_storage", limit_ephemeral_storage)

    resources = common_pb2.ResourceSpec()
    if request_cpu_milli > 0 or request_memory_bytes > 0 or request_ephemeral_storage_bytes > 0:
        resources.requests.cpu_milli = request_cpu_milli
        resources.requests.memory_bytes = request_memory_bytes
        resources.requests.ephemeral_storage_bytes = request_ephemeral_storage_bytes
    if limit_cpu_milli > 0 or limit_memory_bytes > 0 or limit_ephemeral_storage_bytes > 0:
        resources.limits.cpu_milli = limit_cpu_milli
        resources.limits.memory_bytes = limit_memory_bytes
        resources.limits.ephemeral_storage_bytes = limit_ephemeral_storage_bytes
    if not resources.HasField("requests") and not resources.HasField("limits"):
        return None
    return resources


def _service_volume_mounts(
    volume_mounts: Iterable[ServiceVolumeMountInput] | None = None,
) -> list[common_pb2.ServiceVolumeMount]:
    if volume_mounts is None:
        return []
    out: list[common_pb2.ServiceVolumeMount] = []
    for mount in volume_mounts:
        if isinstance(mount, common_pb2.ServiceVolumeMount):
            out.append(common_pb2.ServiceVolumeMount(
                name=mount.name,
                target=mount.target,
                readonly=mount.readonly,
                options=list(mount.options),
            ))
            continue
        if isinstance(mount, VolumeMount):
            out.append(common_pb2.ServiceVolumeMount(
                name=mount.name,
                target=mount.target,
                readonly=mount.readonly,
                options=list(mount.options),
            ))
            continue
        raise TypeError(f"unsupported volume mount type: {type(mount).__name__}")
    return out


def _service_probe(probe: ServiceProbeInput | None = None) -> service_types_pb2.ServiceProbe | None:
    if probe is None:
        return None
    if isinstance(probe, service_types_pb2.ServiceProbe):
        cloned = service_types_pb2.ServiceProbe()
        cloned.CopyFrom(probe)
        return cloned
    if not isinstance(probe, ServiceProbe):
        raise TypeError(f"unsupported service probe type: {type(probe).__name__}")
    out = service_types_pb2.ServiceProbe(
        success_threshold=probe.success_threshold,
        failure_threshold=probe.failure_threshold,
    )
    if probe.initial_delay > 0:
        out.initial_delay.CopyFrom(_duration(probe.initial_delay))
    if probe.period > 0:
        out.period.CopyFrom(_duration(probe.period))
    if probe.timeout > 0:
        out.timeout.CopyFrom(_duration(probe.timeout))
    if probe.http is not None:
        out.http.CopyFrom(_http_probe(probe.http))
    elif probe.tcp is not None:
        out.tcp.CopyFrom(_tcp_probe(probe.tcp))
    return out


def _duration(seconds: float) -> duration_pb2.Duration:
    milliseconds = round(seconds * 1000)
    whole_seconds, remaining_milliseconds = divmod(milliseconds, 1000)
    return duration_pb2.Duration(
        seconds=whole_seconds,
        nanos=remaining_milliseconds * 1_000_000,
    )


def _http_probe(probe: HTTPProbe) -> service_types_pb2.HttpProbe:
    scheme = service_types_pb2.HTTP_PROBE_SCHEME_HTTP
    if probe.scheme == "https":
        scheme = service_types_pb2.HTTP_PROBE_SCHEME_HTTPS
    elif probe.scheme != "http":
        raise ValueError("HTTPProbe.scheme must be 'http' or 'https'")
    return service_types_pb2.HttpProbe(port=probe.port, path=probe.path, scheme=scheme)


def _tcp_probe(probe: TCPProbe) -> service_types_pb2.TcpProbe:
    return service_types_pb2.TcpProbe(port=probe.port)


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
        self.services = service_pb2_grpc.ServiceControlStub(self._channel)
        self.node_admin = admin_node_pb2_grpc.NodeAdminStub(self._channel)
        self.service_admin = admin_service_pb2_grpc.ServiceAdminStub(self._channel)
        self.functions = function_pb2_grpc.FunctionControlStub(self._channel)
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
            tls_server_name=tls_server_name or os.getenv("AXERN_TLS_SERVER_NAME") or None,
            proxy_mode=proxy_mode or os.getenv("AXERN_PROXY_MODE", "env"),
        )

    @classmethod
    def from_context(cls, path: str, name: str = "", *, channel: grpc.Channel | None = None) -> "AxernClient":
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
        response = self.environments.CreateEnvironment(
            environment_pb2.CreateEnvironmentRequest(
                spec=spec,
                labels=dict(labels or {}),
            ),
            timeout=timeout,
        )
        return response.environment

    def delete_environment(
        self,
        environment_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> environment_pb2.Environment:
        response = self.environments.DeleteEnvironment(
            environment_pb2.DeleteEnvironmentRequest(environment_id=environment_id),
            timeout=timeout,
        )
        return response.environment

    def create_run(
        self,
        *,
        environment_id: str,
        argv: list[str] | None = None,
        namespace: str = "default",
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
        labels: dict[str, str] | None = None,
        timeout: float | None = 120.0,
    ) -> run_pb2.Run:
        response = self.runs.CreateRun(
            run_pb2.CreateRunRequest(
                namespace=namespace,
                environment_id=environment_id,
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
                ),
                labels=dict(labels or {}),
            ),
            timeout=timeout,
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
        retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
        while True:
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"run {run_id} watch timed out")
            call = self.runs.WatchRun(
                run_pb2.WatchRunRequest(run_id=run_id, after_version=version),
                timeout=remaining,
            )
            try:
                for response in call:
                    if not response.HasField("run") or response.run.version <= version:
                        continue
                    version = response.run.version
                    retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
                    yield response.run
                return
            except grpc.RpcError as exc:
                if not _is_transient_service_read_code(exc.code()):
                    raise
            finally:
                call.cancel()
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"run {run_id} watch timed out")
            time.sleep(retry_delay if remaining is None else min(retry_delay, remaining))
            retry_delay = min(retry_delay * 2, _SERVICE_WATCH_RETRY_MAX_SECONDS)

    def read_run_output(
        self,
        run_id: str,
        *,
        cursor: str = "",
        follow: bool = False,
        timeout: float | None = None,
    ) -> Generator[node_pb2.ReadOutputResponse, None, None]:
        """Yield stdout/stderr events, resuming transient disconnects by cursor."""

        if not run_id.strip():
            raise ValueError("run_id is required")
        run = self.runs.GetRun(run_pb2.GetRunRequest(run_id=run_id), timeout=timeout).run
        if not run.allocation_id:
            raise RuntimeError(f"run {run_id} output is not available yet")
        next_cursor = cursor
        deadline = None if timeout is None else time.monotonic() + timeout
        retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
        not_found_since: float | None = None
        while True:
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"run {run_id} output read timed out")
            call = node_pb2_grpc.NodeSandboxStub(self._channel).ReadOutput(
                node_pb2.ReadOutputRequest(allocation_id=run.allocation_id, cursor=next_cursor, follow=follow),
                timeout=remaining,
            )
            try:
                for event in call:
                    next_cursor = event.next_cursor
                    retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
                    not_found_since = None
                    yield event
                return
            except grpc.RpcError as exc:
                startup_not_found = exc.code() == grpc.StatusCode.NOT_FOUND
                if not follow or (not _is_transient_service_read_code(exc.code()) and not startup_not_found):
                    raise
                if startup_not_found:
                    not_found_since = not_found_since or time.monotonic()
                    if time.monotonic() - not_found_since >= 30:
                        raise
            finally:
                call.cancel()
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"run {run_id} output read timed out")
            time.sleep(retry_delay if remaining is None else min(retry_delay, remaining))
            retry_delay = min(retry_delay * 2, _SERVICE_WATCH_RETRY_MAX_SECONDS)

    def create_service(
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
        response = self.services.CreateService(
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

    def get_service(
        self,
        service_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> service_types_pb2.Service:
        service_id = service_id.strip()
        if not service_id:
            raise ValueError("service_id is required")
        response = self.services.GetService(
            service_pb2.GetServiceRequest(service_id=service_id),
            timeout=timeout,
        )
        return response.service

    def list_services(
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
        deadline = None if timeout is None else time.monotonic() + timeout
        cursor = ""
        services: list[service_types_pb2.Service] = []
        while True:
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise TimeoutError("list services timed out")
            response = self.services.ListServices(
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

    def list_service_events(
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
        response = self.services.ListServiceEvents(
            service_event_pb2.ListServiceEventsRequest(service_id=service_id, limit=limit),
            timeout=timeout,
        )
        return list(response.events)

    def list_service_replicas(
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
        deadline = None if timeout is None else time.monotonic() + timeout
        retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
        last_error: grpc.RpcError | None = None
        while True:
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                if last_error is not None:
                    raise last_error
                raise TimeoutError(f"list replicas for service {service_id} timed out")
            try:
                response = self.services.ListServiceReplicas(request, timeout=remaining)
                return list(response.replicas)
            except grpc.RpcError as exc:
                if not _is_transient_service_read_code(exc.code()):
                    raise
                last_error = exc
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                if last_error is not None:
                    raise last_error
                raise TimeoutError(f"list replicas for service {service_id} timed out")
            sleep_seconds = retry_delay if remaining is None else min(retry_delay, remaining)
            time.sleep(sleep_seconds)
            retry_delay = min(retry_delay * 2, _SERVICE_WATCH_RETRY_MAX_SECONDS)

    def watch_service(
        self,
        service_id: str,
        *,
        after_version: int = 0,
        timeout: float | None = None,
    ) -> Generator[service_types_pb2.Service, None, None]:
        """Yield monotonically newer service snapshots and resume transient disconnects."""

        if not service_id.strip():
            raise ValueError("service_id is required")
        if after_version < 0:
            raise ValueError("after_version must be non-negative")
        deadline = None if timeout is None else time.monotonic() + timeout
        last_version = after_version
        retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
        while True:
            remaining = None if deadline is None else deadline - time.monotonic()
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
                for response in call:
                    if not response.HasField("service"):
                        raise RuntimeError("WatchService response did not include a service")
                    service = response.service
                    if service.version <= last_version:
                        continue
                    last_version = service.version
                    retry_delay = _SERVICE_WATCH_RETRY_MIN_SECONDS
                    yield service
            except grpc.RpcError as exc:
                if not _is_transient_service_read_code(exc.code()):
                    raise
            finally:
                call.cancel()

            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                raise TimeoutError(f"service {service_id} watch timed out")
            sleep_seconds = retry_delay if remaining is None else min(retry_delay, remaining)
            time.sleep(sleep_seconds)
            retry_delay = min(retry_delay * 2, _SERVICE_WATCH_RETRY_MAX_SECONDS)

    def delete_service(
        self,
        service_id: str,
        *,
        timeout: float | None = 30.0,
    ) -> service_types_pb2.Service:
        response = self.services.DeleteService(
            service_pb2.DeleteServiceRequest(service_id=service_id),
            timeout=timeout,
        )
        return response.service

    def admin_purge_service(
        self,
        service_id: str,
        *,
        operator_reason: str,
        timeout: float | None = 30.0,
    ) -> str:
        if not operator_reason.strip():
            raise ValueError("operator_reason is required")
        response = self.service_admin.PurgeService(
            admin_service_pb2.PurgeServiceRequest(
                service_id=service_id,
                operator_reason=operator_reason,
            ),
            timeout=timeout,
        )
        return response.service_id

    def create_tunnel_session(
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
            tunnel_pb2.ListTunnelSessionEventsRequest(session_id=session_id, limit=limit),
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
            tunnel_pb2.InspectTunnelSessionRequest(session_id=session_id, event_limit=event_limit),
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
