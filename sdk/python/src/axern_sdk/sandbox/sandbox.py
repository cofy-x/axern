"""High-level programmable sandbox abstraction for the Axern Python SDK."""

from __future__ import annotations

import time
from collections.abc import Callable, Iterable, Iterator

from axern.control.tunnel.v1 import tunnel_pb2
from axern_sdk._internal.resources import ResourceQuantity
from axern_sdk.client import AxernClient
from axern_sdk.errors import SandboxNotStartedError, SandboxTimeoutError
from axern_sdk.models import VolumeMount
from axern_sdk.node import (
    ExecCommand,
    ExecResult,
    ExecStreamEvent,
    ImageProcessMount,
    NodeSandboxClient,
    SandboxProcess,
)
from axern_sdk.sandbox.browser import SandboxBrowserMixin
from axern_sdk.sandbox.capabilities import SandboxCapabilityMixin
from axern_sdk.sandbox.computer_use import SandboxComputerUseMixin
from axern_sdk.sandbox.files import SandboxFileMixin
from axern_sdk.sandbox.lifecycle import wait_ready_replica, wait_service_deleted
from axern_sdk.sandbox.renewal import TunnelRenewal
from axern_sdk.sandbox.types import DEFAULT_SANDBOX_ARGV, SandboxMetadata, SandboxState, _validate_source
from axern_sdk.tunnel import ConnectorConfig, TunnelConnector


class Sandbox(SandboxCapabilityMixin, SandboxBrowserMixin, SandboxComputerUseMixin, SandboxFileMixin):
    """Service-backed Axern sandbox with optional reverse TCP tunnel."""

    def __init__(
        self,
        *,
        client: AxernClient,
        image: str = "",
        registry_credential_id: str = "",
        template_id: str = "",
        environment_id: str = "",
        namespace: str = "default",
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
        volumes: Iterable[VolumeMount] | None = None,
        upstream: str = "",
        remote_port: int | None = None,
        connector: ConnectorConfig | None = None,
        ready_timeout_seconds: float = 180.0,
        tunnel_ttl_seconds: float = 300.0,
        connector_ready_timeout_seconds: float = 15.0,
        labels: dict[str, str] | None = None,
        _connector_factory: Callable[..., TunnelConnector] = TunnelConnector,
        _node_client_factory: Callable[..., NodeSandboxClient] = NodeSandboxClient,
        _renew_interval_seconds: float | None = None,
    ) -> None:
        _validate_source(image=image, template_id=template_id, environment_id=environment_id)
        self._client = client
        self._image = image
        self._registry_credential_id = registry_credential_id
        self._template_id = template_id
        self._environment_id = environment_id
        self._namespace = namespace
        self._argv = list(argv or DEFAULT_SANDBOX_ARGV)
        self._env = dict(env or {})
        self._cwd = cwd
        self._runtime_class = runtime_class
        self._request_cpu = request_cpu
        self._request_memory = request_memory
        self._request_ephemeral_storage = request_ephemeral_storage
        self._limit_cpu = limit_cpu
        self._limit_memory = limit_memory
        self._limit_ephemeral_storage = limit_ephemeral_storage
        self._extension_capabilities = dict(extension_capabilities or {})
        self._volumes = tuple(volumes or ())
        self._upstream = upstream
        self._remote_port = remote_port
        self._gateway_transport = client._gateway_transport()
        self._connector_config = connector or ConnectorConfig()
        self._ready_timeout_seconds = ready_timeout_seconds
        self._tunnel_ttl_seconds = tunnel_ttl_seconds
        self._connector_ready_timeout_seconds = connector_ready_timeout_seconds
        self._labels = {"axern.sdk.resource": "sandbox", **dict(labels or {})}
        self._connector_factory = _connector_factory
        self._node_client_factory = _node_client_factory
        self._renew_interval_seconds = _renew_interval_seconds

        self._created_environment = False
        self._created_environment_id = ""
        self._created_service_id = ""
        self._created_tunnel_session_id = ""
        self._tunnel_client_token = ""
        self._state: SandboxState | None = None
        self._started_at_ns = 0
        self._connector: TunnelConnector | None = None
        self._renewal: TunnelRenewal | None = None

    @property
    def state(self) -> SandboxState:
        if self._state is None:
            raise SandboxNotStartedError("sandbox is not active")
        return self._state

    @property
    def environment_id(self) -> str:
        return self.state.environment_id

    @property
    def service_id(self) -> str:
        return self.state.service_id

    @property
    def allocation_id(self) -> str:
        return self.state.allocation_id

    @property
    def node_id(self) -> str:
        return self.state.node_id

    @property
    def attempt(self) -> int:
        return self.state.attempt

    @property
    def tunnel_session_id(self) -> str:
        return self.state.tunnel_session_id

    @property
    def bound_addr(self) -> str:
        return self.state.bound_addr

    @property
    def metadata(self) -> SandboxMetadata:
        state = self.state
        return SandboxMetadata(
            environment_id=state.environment_id,
            service_id=state.service_id,
            allocation_id=state.allocation_id,
            attempt=state.attempt,
            node_id=state.node_id,
            runtime_class=self._runtime_class,
            tunnel_session_id=state.tunnel_session_id,
            bound_addr=state.bound_addr,
            started_at_ns=self._started_at_ns,
            labels=dict(self._labels),
        )

    def __enter__(self) -> "Sandbox":
        self.start()
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        self.close()

    def start(self) -> "Sandbox":
        if self._state is not None:
            return self
        try:
            environment_id = self._resolve_environment()
            service = self._client.create_service(
                environment_id=environment_id,
                replicas=1,
                argv=self._argv,
                env=self._env,
                cwd=self._cwd,
                runtime_class=self._runtime_class,
                request_cpu=self._request_cpu,
                request_memory=self._request_memory,
                request_ephemeral_storage=self._request_ephemeral_storage,
                limit_cpu=self._limit_cpu,
                limit_memory=self._limit_memory,
                limit_ephemeral_storage=self._limit_ephemeral_storage,
                extension_capabilities=self._extension_capabilities,
                volume_mounts=self._volumes,
                namespace=self._namespace,
                labels=self._labels,
            )
            self._created_service_id = service.id
            replica = wait_ready_replica(
                self._client,
                service_id=service.id,
                timeout_seconds=self._ready_timeout_seconds,
            )

            if self._upstream:
                tunnel = self._client.create_tunnel_session(
                    allocation_id=replica.id,
                    local_target=self._upstream,
                    remote_port=self._remote_port,
                    ttl_seconds=self._tunnel_ttl_seconds,
                    wait_ready=True,
                    ready_timeout_seconds=self._ready_timeout_seconds,
                )
                session = tunnel.session
                self._created_tunnel_session_id = session.session_id
                self._tunnel_client_token = tunnel.client_token
                self._renewal = TunnelRenewal(
                    client=self._client,
                    session_id=session.session_id,
                    client_token=tunnel.client_token,
                    ttl_seconds=self._tunnel_ttl_seconds,
                    interval_seconds=self._renew_interval_seconds,
                )
                self._renewal.start()
                self._connector = self._connector_factory(
                    session=session,
                    client_token=tunnel.client_token,
                    local_target=self._upstream,
                    transport=self._gateway_transport,
                    connector_config=self._connector_config,
                )
                self._connector.start()
                self._wait_client_connected(session.session_id)
                tunnel_session_id = session.session_id
                bound_addr = session.bound_addr or f"127.0.0.1:{session.remote_port}"
            else:
                tunnel_session_id = ""
                bound_addr = ""

            self._state = SandboxState(
                environment_id=environment_id,
                service_id=service.id,
                allocation_id=replica.id,
                attempt=replica.attempt,
                node_id=replica.node_id,
                tunnel_session_id=tunnel_session_id,
                bound_addr=bound_addr,
            )
            self._started_at_ns = time.time_ns()
            return self
        except Exception:
            self.close()
            raise

    def close(self) -> None:
        tunnel_session_id = self._created_tunnel_session_id
        if not tunnel_session_id and self._state is not None:
            tunnel_session_id = self._state.tunnel_session_id
        if self._renewal is not None:
            self._renewal.stop()
            self._renewal = None
        if self._connector is not None:
            self._connector.stop()
            self._connector = None
        if tunnel_session_id:
            try:
                self._client.revoke_tunnel_session(tunnel_session_id, reason="sandbox closed", timeout=10.0)
            except Exception:
                pass
            self._created_tunnel_session_id = ""
        if self._created_service_id:
            service_deleted = False
            try:
                self._client.delete_service(self._created_service_id, timeout=30.0)
                service_deleted = True
            except Exception:
                pass
            if service_deleted:
                try:
                    wait_service_deleted(
                        self._client,
                        service_id=self._created_service_id,
                        timeout_seconds=self._ready_timeout_seconds,
                    )
                except Exception:
                    pass
            self._created_service_id = ""
        if self._created_environment and self._created_environment_id:
            try:
                self._client.delete_environment(self._created_environment_id, timeout=30.0)
            except Exception:
                pass
            self._created_environment = False
            self._created_environment_id = ""
        self._state = None
        self._started_at_ns = 0

    def _resolve_environment(self) -> str:
        if self._environment_id:
            return self._environment_id
        if self._image:
            environment = self._client.create_environment(
                namespace=self._namespace,
                image_ref=self._image,
                registry_credential_id=self._registry_credential_id,
                labels=self._labels,
            )
        else:
            environment = self._client.create_environment(
                namespace=self._namespace,
                template_id=self._template_id,
                labels=self._labels,
            )
        self._created_environment = True
        self._created_environment_id = environment.id
        return environment.id

    def _wait_client_connected(self, session_id: str) -> None:
        deadline = time.monotonic() + self._connector_ready_timeout_seconds
        while time.monotonic() < deadline:
            if self._renewal is not None and self._renewal.error is not None:
                raise RuntimeError(f"tunnel renew failed: {self._renewal.error}") from self._renewal.error
            if self._connector is not None and self._connector.error is not None:
                raise RuntimeError(f"tunnel connector failed: {self._connector.error}") from self._connector.error
            events = self._client.list_tunnel_events(session_id, limit=50)
            if any(event.event_type == tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED for event in events):
                return
            time.sleep(0.25)
        raise SandboxTimeoutError(f"tunnel client peer did not connect within {self._connector_ready_timeout_seconds}s")

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
        return self._node_client().exec(
            command,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            input=input,
            check=check,
            text=text,
            encoding=encoding,
            errors=errors,
            shell=shell,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

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
        return self._node_client().exec_stream(
            command,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            input=input,
            encoding=encoding,
            errors=errors,
            shell=shell,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

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
        return self._node_client().process(
            command,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            shell=shell,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

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
        return self._node_client().exec_image(
            image,
            command,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            check=check,
            text=text,
            encoding=encoding,
            errors=errors,
            shell=shell,
            mounts=mounts,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

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
        return self._node_client().process_image(
            image,
            command,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            shell=shell,
            mounts=mounts,
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    def _node_client(self) -> NodeSandboxClient:
        if self._state is None:
            raise SandboxNotStartedError("sandbox is not active")
        return self._node_client_factory(
            client=self._client,
            allocation_id=self._state.allocation_id,
        )
