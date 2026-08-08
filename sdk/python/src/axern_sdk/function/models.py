"""User-facing Function manifest models."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Mapping

from axern_sdk.models import ImageMount, SecretEnvVar, SecretFile, VolumeMount


@dataclass(frozen=True, slots=True)
class FunctionSource:
    """Source package location for a function."""

    root: str = "src"


@dataclass(frozen=True, slots=True)
class FunctionWorkerSource:
    """Environment used to host function workers."""

    environment_id: str = ""
    template: str = ""
    template_version: str = ""
    image: str = ""
    registry_credential_id: str = ""
    rootfs_readonly: bool = False


@dataclass(frozen=True, slots=True)
class FunctionResources:
    """Resource requests and limits for function workers."""

    request_cpu: str = ""
    request_memory: str = ""
    request_ephemeral_storage: str = ""
    limit_cpu: str = ""
    limit_memory: str = ""
    limit_ephemeral_storage: str = ""


@dataclass(frozen=True, slots=True)
class FunctionScaling:
    """Warm pool and concurrency policy for a function."""

    min_replicas: int = 0
    max_replicas: int = 1
    concurrency: int = 1
    idle_seconds: int = 300


@dataclass(frozen=True, slots=True)
class FunctionSpec:
    """Parsed Axern Function manifest."""

    name: str
    runtime: str
    handler: str
    namespace: str = "default"
    labels: Mapping[str, str] = field(default_factory=dict)
    worker_source: FunctionWorkerSource = field(default_factory=FunctionWorkerSource)
    initializer: str = ""
    source: FunctionSource = field(default_factory=FunctionSource)
    timeout_seconds: int = 60
    resources: FunctionResources = field(default_factory=FunctionResources)
    scaling: FunctionScaling = field(default_factory=FunctionScaling)
    env: Mapping[str, str] = field(default_factory=dict)
    secret_env: tuple[SecretEnvVar, ...] = ()
    secret_files: tuple[SecretFile, ...] = ()
    volumes: tuple[VolumeMount, ...] = ()
    image_mounts: tuple[ImageMount, ...] = ()
    root_dir: Path = field(default_factory=Path)
    manifest_path: Path = field(default_factory=Path)

    def __post_init__(self) -> None:
        object.__setattr__(self, "labels", dict(self.labels))
        object.__setattr__(self, "env", dict(self.env))
        object.__setattr__(self, "secret_env", tuple(self.secret_env))
        object.__setattr__(self, "secret_files", tuple(self.secret_files))
        object.__setattr__(self, "volumes", tuple(self.volumes))
        object.__setattr__(self, "image_mounts", tuple(self.image_mounts))


@dataclass(frozen=True, slots=True)
class FunctionPackage:
    """Packaged function source artifact metadata."""

    digest: str
    size_bytes: int
    media_type: str
    path: Path | None = None
    storage_uri: str = ""


@dataclass(frozen=True, slots=True)
class FunctionInvocationResult:
    """Decoded result from a function invocation."""

    invocation_id: str
    function_id: str
    function_name: str
    namespace: str
    revision_id: str
    status: str
    request_id: str
    value: Any | None = None
    error: "FunctionInvocationError | None" = None
    duration_seconds: float = 0.0
    created_at: str = ""
    started_at: str = ""
    completed_at: str = ""


@dataclass(frozen=True, slots=True)
class FunctionInvocationError:
    """Structured error from a failed function invocation."""

    code: str = ""
    message: str = ""
    type: str = ""
    stack_trace: str = ""
    details: Mapping[str, str] = field(default_factory=dict)
