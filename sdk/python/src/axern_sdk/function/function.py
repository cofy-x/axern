"""High-level Function handle for parsed Axern Function sources."""

from __future__ import annotations

import hashlib
import io
import json
import stat
import tarfile
import time
from collections.abc import Iterator
from pathlib import Path
from typing import Any, cast

from google.protobuf import duration_pb2

from axern.control.common.v1 import common_pb2
from axern.control.environment.v1 import environment_pb2
from axern.control.function.v1 import function_pb2, function_types_pb2
from axern_sdk._internal.resources import cpu_milli, memory_bytes
from axern_sdk.client import _extension_capability_requirements
from axern_sdk.function.manifest import load_function_spec
from axern_sdk.function.models import (
    FunctionInvocationError,
    FunctionInvocationResult,
    FunctionPackage,
    FunctionSpec,
)

FUNCTION_BUNDLE_MEDIA_TYPE = "application/vnd.axern.function.tar"
FUNCTION_BUNDLE_CHUNK_SIZE = 1024 * 1024

_INVOCATION_STATUS_NAMES = {
    v: k.removeprefix("FUNCTION_INVOCATION_STATUS_").lower()
    for k, v in function_types_pb2.FunctionInvocationStatus.items()
}


def _decode_invocation(
    invocation: function_types_pb2.FunctionInvocation,
) -> FunctionInvocationResult:
    status_name = _INVOCATION_STATUS_NAMES.get(invocation.status, "unknown")

    value = None
    if invocation.result and invocation.result.data:
        content_type = invocation.result.content_type or "application/json"
        if "json" in content_type:
            value = json.loads(invocation.result.data.decode("utf-8"))
        else:
            value = invocation.result.data

    error = None
    if invocation.error and (invocation.error.code or invocation.error.message):
        error = FunctionInvocationError(
            code=invocation.error.code,
            message=invocation.error.message,
            type=invocation.error.type,
            stack_trace=invocation.error.stack_trace,
            details=dict(invocation.error.details),
        )

    duration = 0.0
    if invocation.duration:
        duration = invocation.duration.seconds + invocation.duration.nanos / 1e9

    def _ts(ts: Any) -> str:
        if ts and ts.seconds:
            return ts.ToJsonString()
        return ""

    return FunctionInvocationResult(
        invocation_id=invocation.id,
        function_id=invocation.function_id,
        function_name=invocation.function_name,
        namespace=invocation.namespace,
        revision_id=invocation.revision_id,
        status=status_name,
        request_id=invocation.request_id,
        value=value,
        error=error,
        duration_seconds=duration,
        created_at=_ts(invocation.created_at),
        started_at=_ts(invocation.started_at),
        completed_at=_ts(invocation.completed_at),
    )


class Function:
    """Parsed Function source bound to an Axern client."""

    def __init__(self, *, client: Any, spec: FunctionSpec) -> None:
        self._client = client
        self._spec = spec

    @classmethod
    def from_file(cls, client: Any, path: str | Path) -> "Function":
        """Load an ``axern/v1`` Function resource spec."""

        return cls(client=client, spec=load_function_spec(path))

    @property
    def client(self) -> Any:
        return self._client

    @property
    def spec(self) -> FunctionSpec:
        return self._spec

    @property
    def name(self) -> str:
        return self._spec.name

    def package(self, output_dir: str | Path) -> FunctionPackage:
        """Create a deterministic tar bundle for this function source."""

        output = self._prepare_output_dir(output_dir)
        payload, digest = self._bundle_bytes()
        return self._write_package(output, payload, digest)

    def deploy(
        self,
        *,
        namespace: str | None = None,
        labels: dict[str, str] | None = None,
        storage_uri: str = "",
        output_dir: str | Path | None = None,
        wait_ready: bool = False,
        ready_timeout_seconds: int = 0,
        timeout: float | None = 120.0,
    ) -> function_pb2.DeployFunctionResponse:
        """Package and register this Function through FunctionControl."""

        effective_namespace = namespace or self.spec.namespace
        output = self._prepare_output_dir(output_dir) if output_dir is not None else None
        payload, digest = self._bundle_bytes()
        if output is not None:
            package = self._write_package(output, payload, digest)
            bundle_storage_uri = storage_uri or package.storage_uri
        else:
            package = FunctionPackage(
                digest=digest,
                size_bytes=len(payload),
                media_type=FUNCTION_BUNDLE_MEDIA_TYPE,
                storage_uri=storage_uri or f"axern://function-bundles/{digest.removeprefix('sha256:')}.tar",
            )
            bundle_storage_uri = package.storage_uri

        if not storage_uri:
            uploaded = self.client.functions.UploadFunctionBundle(
                self._upload_bundle_requests(namespace=effective_namespace, package=package, payload=payload),
                timeout=timeout,
            )
            bundle_storage_uri = uploaded.bundle.storage_uri

        request = function_pb2.DeployFunctionRequest(
            namespace=effective_namespace,
            name=self.name,
            spec=self._proto_spec(effective_namespace),
            source=function_types_pb2.FunctionSource(
                bundle=function_types_pb2.FunctionBundleSource(
                    digest=package.digest,
                    media_type=package.media_type,
                    size_bytes=package.size_bytes,
                    storage_uri=bundle_storage_uri,
                )
            ),
            labels=dict(self.spec.labels if labels is None else labels),
        )
        response = self.client.functions.DeployFunction(request, timeout=timeout)

        if wait_ready:
            ready_timeout = ready_timeout_seconds if ready_timeout_seconds > 0 else 180
            self.poll_until_ready(
                response.function.id,
                timeout_seconds=ready_timeout,
            )

        return response

    def poll_until_ready(
        self,
        function_id: str,
        *,
        timeout_seconds: int = 180,
        poll_interval: float = 2.0,
    ) -> function_pb2.GetFunctionResponse:
        """Poll GetFunction until the deployment becomes ready or timeout expires."""

        deadline = time.monotonic() + timeout_seconds
        last: function_pb2.GetFunctionResponse | None = None
        while time.monotonic() < deadline:
            response = self.client.functions.GetFunction(
                function_pb2.GetFunctionRequest(function_id=function_id),
                timeout=30.0,
            )
            if response is None:
                raise RuntimeError("function status response missing")
            current = cast(function_pb2.GetFunctionResponse, response)
            deployment = current.deployment
            function = current.function
            if deployment is None or function is None:
                raise RuntimeError("function status response missing function or deployment")
            if (
                deployment.status == function_types_pb2.FUNCTION_DEPLOYMENT_STATUS_READY
                and deployment.ready_replicas > 0
            ):
                return current
            if function.status == function_types_pb2.FUNCTION_STATUS_FAILED:
                raise RuntimeError(
                    f"function deploy failed: {function.message}"
                )
            last = current
            time.sleep(poll_interval)
        status = (
            function_types_pb2.FunctionDeploymentStatus.Name(last.deployment.status)
            if last is not None and last.deployment is not None
            else "unknown"
        )
        raise TimeoutError(
            f"function did not become ready within {timeout_seconds}s: {status}"
        )

    def _prepare_output_dir(self, output_dir: str | Path) -> Path:
        output = Path(output_dir).expanduser().resolve()
        source_root = (self.spec.root_dir / Path(self.spec.source.root)).resolve()
        if output == source_root or source_root in output.parents:
            raise ValueError("function package output_dir must be outside manifest.source.root")
        return output

    def _write_package(self, output: Path, payload: bytes, digest: str) -> FunctionPackage:
        output.mkdir(parents=True, exist_ok=True)
        filename = f"{self.name}-{digest.removeprefix('sha256:')[:16]}.tar"
        path = output / filename
        path.write_bytes(payload)
        return FunctionPackage(
            digest=digest,
            size_bytes=len(payload),
            media_type=FUNCTION_BUNDLE_MEDIA_TYPE,
            path=path,
            storage_uri=path.resolve().as_uri(),
        )

    def _upload_bundle_requests(
        self,
        *,
        namespace: str,
        package: FunctionPackage,
        payload: bytes,
    ) -> Iterator[function_pb2.UploadFunctionBundleRequest]:
        yield function_pb2.UploadFunctionBundleRequest(
            open=function_pb2.UploadFunctionBundleOpen(
                namespace=namespace,
                name=self.name,
                digest=package.digest,
                media_type=package.media_type,
                size_bytes=package.size_bytes,
            )
        )
        for offset in range(0, len(payload), FUNCTION_BUNDLE_CHUNK_SIZE):
            yield function_pb2.UploadFunctionBundleRequest(chunk=payload[offset : offset + FUNCTION_BUNDLE_CHUNK_SIZE])

    def invoke(
        self,
        payload: Any = None,
        *,
        namespace: str = "default",
        function_id: str = "",
        revision_id: str = "",
        mode: function_types_pb2.FunctionInvocationMode = function_types_pb2.FUNCTION_INVOCATION_MODE_SYNC,
        timeout_seconds: int = 30,
        request_id: str = "",
        labels: dict[str, str] | None = None,
        rpc_timeout: float | None = 120.0,
    ) -> FunctionInvocationResult:
        """Invoke this function and return the decoded result."""

        encoded_payload = function_types_pb2.FunctionPayload()
        if payload is not None:
            encoded_payload = function_types_pb2.FunctionPayload(
                content_type="application/json",
                data=json.dumps(payload).encode("utf-8"),
            )

        request = function_pb2.InvokeFunctionRequest(
            namespace=namespace,
            name=self.name,
            mode=mode,
            payload=encoded_payload,
            timeout=duration_pb2.Duration(seconds=timeout_seconds),
            labels=dict(labels or {}),
        )
        if function_id:
            request.function_id = function_id
        if revision_id:
            request.revision_id = revision_id
        if request_id:
            request.request_id = request_id

        response = self.client.functions.InvokeFunction(request, timeout=rpc_timeout)
        return _decode_invocation(response.invocation)

    def _proto_spec(self, namespace: str | None = None) -> function_types_pb2.FunctionSpec:
        resources = common_pb2.ResourceSpec()
        request_cpu = cpu_milli("manifest.resources.request_cpu", self.spec.resources.request_cpu)
        request_memory = memory_bytes("manifest.resources.request_memory", self.spec.resources.request_memory)
        request_ephemeral_storage = memory_bytes("manifest.resources.request_ephemeral_storage", self.spec.resources.request_ephemeral_storage)
        limit_cpu = cpu_milli("manifest.resources.limit_cpu", self.spec.resources.limit_cpu)
        limit_memory = memory_bytes("manifest.resources.limit_memory", self.spec.resources.limit_memory)
        limit_ephemeral_storage = memory_bytes("manifest.resources.limit_ephemeral_storage", self.spec.resources.limit_ephemeral_storage)
        if request_cpu > 0 or request_memory > 0 or request_ephemeral_storage > 0:
            resources.requests.cpu_milli = request_cpu
            resources.requests.memory_bytes = request_memory
            resources.requests.ephemeral_storage_bytes = request_ephemeral_storage
        if limit_cpu > 0 or limit_memory > 0 or limit_ephemeral_storage > 0:
            resources.limits.cpu_milli = limit_cpu
            resources.limits.memory_bytes = limit_memory
            resources.limits.ephemeral_storage_bytes = limit_ephemeral_storage

        config = common_pb2.ExecutionConfig(
            env=dict(self.spec.env),
            extension_capability_requirements=_extension_capability_requirements(
                dict(self.spec.extension_capabilities),
            ),
            secret_env=[
                common_pb2.SecretEnvVar(name=item.name, secret_id=item.secret_id, key=item.key, optional=item.optional)
                for item in self.spec.secret_env
            ],
            secret_files=[
                common_pb2.SecretFile(path=item.path, secret_id=item.secret_id, key=item.key, mode=item.mode, optional=item.optional)
                for item in self.spec.secret_files
            ],
            volume_mounts=[
                common_pb2.ServiceVolumeMount(
                    name=mount.name,
                    target=mount.target,
                    readonly=mount.readonly,
                    options=list(mount.options),
                )
                for mount in self.spec.volumes
            ],
            image_mounts=[
                common_pb2.ImageMount(image=mount.image, target=mount.target, readonly=True)
                for mount in self.spec.image_mounts
            ],
        )
        if resources.HasField("requests") or resources.HasField("limits"):
            config.resources.CopyFrom(resources)

        proto = function_types_pb2.FunctionSpec(
            runtime=self.spec.runtime,
            handler=self.spec.handler,
            initializer=self.spec.initializer,
            timeout=duration_pb2.Duration(seconds=int(self.spec.timeout_seconds)),
            config=config,
            scaling=function_types_pb2.FunctionScalingSpec(
                min_replicas=self.spec.scaling.min_replicas,
                max_replicas=self.spec.scaling.max_replicas,
                concurrency=self.spec.scaling.concurrency,
                idle_timeout=duration_pb2.Duration(seconds=int(self.spec.scaling.idle_seconds)),
            ),
        )
        worker = self.spec.worker_source
        if worker.environment_id:
            proto.worker_source.environment_id = worker.environment_id
        else:
            environment = environment_pb2.EnvironmentSpec(namespace=namespace or self.spec.namespace)
            if worker.template:
                environment.template_id = worker.template
                environment.template_version = worker.template_version
            else:
                environment.image.CopyFrom(
                    environment_pb2.EnvironmentImageSource(
                        ref=worker.image,
                        registry_credential_id=worker.registry_credential_id,
                        rootfs_readonly=worker.rootfs_readonly,
                    )
                )
            proto.worker_source.environment.CopyFrom(environment)
        return proto

    def _bundle_bytes(self) -> tuple[bytes, str]:
        root = self.spec.root_dir
        source_root = Path(self.spec.source.root)
        paths: list[Path] = []
        seen: set[Path] = set()
        source_path = root / source_root
        for item in sorted(source_path.rglob("*")):
            if item.is_symlink():
                raise ValueError(f"function source contains unsupported symlink: {item.relative_to(root)}")
            if item.is_dir():
                continue
            if not item.is_file():
                raise ValueError(f"function source contains unsupported file type: {item.relative_to(root)}")
            resolved = item.resolve()
            if resolved in seen:
                continue
            seen.add(resolved)
            paths.append(item)

        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode="w", format=tarfile.PAX_FORMAT) as archive:
            for path in paths:
                rel = Path("src", path.relative_to(source_path)).as_posix()
                data = path.read_bytes()
                info = tarfile.TarInfo(rel)
                mode = stat.S_IMODE(path.stat().st_mode)
                info.mode = 0o755 if mode & stat.S_IXUSR else 0o644
                info.size = len(data)
                info.mtime = 0
                info.uid = 0
                info.gid = 0
                info.uname = ""
                info.gname = ""
                archive.addfile(info, io.BytesIO(data))

        payload = buffer.getvalue()
        digest = "sha256:" + hashlib.sha256(payload).hexdigest()
        return payload, digest
