"""High-level programmable sandbox abstraction for the Axern Python SDK."""

from __future__ import annotations

import time
from collections.abc import Callable, Iterable, Iterator

from axern.control.tunnel.v1 import tunnel_pb2
from axern_sdk._internal.resources import ResourceQuantity
from axern_sdk.client import AxernClient
from axern_sdk.errors import SandboxNotStartedError, SandboxTimeoutError
from axern_sdk.node import (
    ExecCommand,
    ExecResult,
    ProcessEvent,
    AllocationClient,
    SandboxProcess,
)
from axern_sdk.sandbox.capabilities import SandboxCapabilityMixin
from axern_sdk.sandbox.computer_use import SandboxComputerUseMixin
from axern_sdk.sandbox.files import SandboxFileMixin
from axern_sdk.sandbox.lifecycle import wait_running_run
from axern_sdk.network_policy import NetworkPolicy
from axern_sdk.models import DeclaredOutput, ImageMount, SecretEnvVar, SecretFile
from axern_sdk.sandbox.renewal import TunnelRenewal
from axern_sdk.sandbox.types import (
    DEFAULT_SANDBOX_ARGV,
    SandboxMetadata,
    SandboxState,
    _validate_source,
)
from axern_sdk.tunnel import ConnectorConfig, TunnelConnector


class Sandbox(SandboxCapabilityMixin, SandboxComputerUseMixin, SandboxFileMixin):
    """Run allocation-backed Axern sandbox with optional reverse TCP tunnel."""

    def __init__(
        self,
        *,
        client: AxernClient,
        image: str = "",
        registry_credential_id: str = "",
        environment_id: str = "",
        namespace: str = "default",
        argv: list[str] | None = None,
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
        upstream: str = "",
        remote_port: int | None = None,
        connector: ConnectorConfig | None = None,
        ready_timeout_seconds: float = 180.0,
        tunnel_ttl_seconds: float = 300.0,
        connector_ready_timeout_seconds: float = 15.0,
        labels: dict[str, str] | None = None,
        _connector_factory: Callable[..., TunnelConnector] = TunnelConnector,
        _node_client_factory: Callable[..., AllocationClient] = AllocationClient,
        _renew_interval_seconds: float | None = None,
    ) -> None:
        _validate_source(image=image, environment_id=environment_id)
        self._client = client
        self._image = image
        self._registry_credential_id = registry_credential_id
        self._environment_id = environment_id
        self._namespace = namespace
        self._argv = list(argv or DEFAULT_SANDBOX_ARGV)
        self._env = dict(env or {})
        self._cwd = cwd
        self._network_policy = network_policy
        self._request_cpu = request_cpu
        self._request_memory = request_memory
        self._request_ephemeral_storage = request_ephemeral_storage
        self._limit_cpu = limit_cpu
        self._limit_memory = limit_memory
        self._limit_ephemeral_storage = limit_ephemeral_storage
        self._extension_capabilities = dict(extension_capabilities or {})
        self._image_mounts = list(image_mounts or ())
        self._secret_env = list(secret_env or ())
        self._secret_files = list(secret_files or ())
        self._declared_outputs = list(declared_outputs or ())
        self._upstream = upstream
        self._remote_port = remote_port
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
        self._created_run_id = ""
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
    def run_id(self) -> str:
        return self.state.run_id

    @property
    def allocation_id(self) -> str:
        return self.state.allocation_id

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
            run_id=state.run_id,
            allocation_id=state.allocation_id,
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
            run = self._client.create_run(
                environment_id=environment_id,
                argv=self._argv,
                env=self._env,
                cwd=self._cwd,
                network_policy=self._network_policy,
                request_cpu=self._request_cpu,
                request_memory=self._request_memory,
                request_ephemeral_storage=self._request_ephemeral_storage,
                limit_cpu=self._limit_cpu,
                limit_memory=self._limit_memory,
                limit_ephemeral_storage=self._limit_ephemeral_storage,
                extension_capabilities=self._extension_capabilities,
                image_mounts=self._image_mounts,
                secret_env=self._secret_env,
                secret_files=self._secret_files,
                declared_outputs=self._declared_outputs,
                namespace=self._namespace,
                labels=self._labels,
            )
            self._created_run_id = run.id
            run = wait_running_run(
                self._client,
                run_id=run.id,
                timeout_seconds=self._ready_timeout_seconds,
            )

            if self._upstream:
                tunnel = self._client.create_tunnel_session(
                    allocation_id=run.allocation_id,
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
                    client=self._client,
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
                run_id=run.id,
                allocation_id=run.allocation_id,
                tunnel_session_id=tunnel_session_id,
                bound_addr=bound_addr,
            )
            self._started_at_ns = time.time_ns()
            return self
        except Exception as start_error:
            try:
                self.close()
            except Exception as cleanup_error:
                raise ExceptionGroup(
                    "sandbox start and cleanup failed", [start_error, cleanup_error]
                ) from None
            raise

    def close(self) -> None:
        errors: list[Exception] = []
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
                self._client.revoke_tunnel_session(
                    tunnel_session_id, reason="sandbox closed", timeout=10.0
                )
            except Exception as error:
                errors.append(error)
            self._created_tunnel_session_id = ""
        self._tunnel_client_token = ""
        if self._created_run_id:
            try:
                self._client.cancel_run(self._created_run_id, timeout=30.0)
            except Exception as error:
                errors.append(error)
            self._created_run_id = ""
        if self._created_environment and self._created_environment_id:
            try:
                self._client.delete_environment(
                    self._created_environment_id, timeout=30.0
                )
            except Exception as error:
                errors.append(error)
            self._created_environment = False
            self._created_environment_id = ""
        self._state = None
        self._started_at_ns = 0
        if errors:
            raise ExceptionGroup("sandbox cleanup failed", errors)

    def _resolve_environment(self) -> str:
        if self._environment_id:
            return self._environment_id
        environment = self._client.create_environment(
            namespace=self._namespace,
            image_ref=self._image,
            registry_credential_id=self._registry_credential_id,
            labels=self._labels,
        )
        self._created_environment = True
        self._created_environment_id = environment.id
        return environment.id

    def _wait_client_connected(self, session_id: str) -> None:
        deadline = time.monotonic() + self._connector_ready_timeout_seconds
        while time.monotonic() < deadline:
            if self._renewal is not None and self._renewal.error is not None:
                raise RuntimeError(
                    f"tunnel renew failed: {self._renewal.error}"
                ) from self._renewal.error
            if self._connector is not None and self._connector.error is not None:
                raise RuntimeError(
                    f"tunnel connector failed: {self._connector.error}"
                ) from self._connector.error
            events = self._client.list_tunnel_events(session_id, limit=50)
            if any(
                event.event_type
                == tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED
                for event in events
            ):
                return
            time.sleep(0.25)
        raise SandboxTimeoutError(
            f"tunnel client peer did not connect within {self._connector_ready_timeout_seconds}s"
        )

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
        rpc_timeout: float | None = None,
    ) -> Iterator[ProcessEvent]:
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
        initial_cols: int = 0,
        initial_rows: int = 0,
        shell: bool | None = None,
        rpc_timeout: float | None = None,
    ) -> SandboxProcess:
        return self._node_client().process(
            command,
            env=env,
            cwd=cwd,
            timeout_seconds=timeout_seconds,
            user=user,
            tty=tty,
            initial_cols=initial_cols,
            initial_rows=initial_rows,
            shell=shell,
            rpc_timeout=rpc_timeout,
        )

    def _node_client(self) -> AllocationClient:
        if self._state is None:
            raise SandboxNotStartedError("sandbox is not active")
        return self._node_client_factory(
            client=self._client,
            allocation_id=self._state.allocation_id,
        )
