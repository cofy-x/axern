"""User-facing models for the Axern Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Mapping


@dataclass(frozen=True, slots=True)
class MountSpec:
    """Static mount configuration exposed by catalog templates."""

    type: str = "bind"
    source: str = ""
    target: str = ""
    options: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, "options", tuple(self.options))


@dataclass(frozen=True, slots=True)
class RuntimeCapabilities:
    """Capability flags exported by the runtime catalog."""

    supports_exec: bool = False
    supports_exec_stream: bool = False
    supports_long_lived_process: bool = False
    supports_ports: bool = False
    supports_computer_use: bool = False


@dataclass(frozen=True, slots=True)
class RuntimeBaselinePolicy:
    """Managed process baseline for generated runtime specs."""

    capabilities: tuple[str, ...] = ()
    no_file_limit: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, "capabilities", tuple(self.capabilities))


@dataclass(frozen=True, slots=True)
class RuntimeCapabilityPolicy:
    """Annotation-driven Linux capability policy."""

    annotation_key: str = ""
    include_ambient: bool | None = None


@dataclass(frozen=True, slots=True)
class RuntimeNetworkNamespacePolicy:
    """Annotation-driven network namespace policy."""

    annotation_key: str = ""


@dataclass(frozen=True, slots=True)
class RuntimeResourcePolicy:
    """Resource policy for generated runtime specs."""

    ignore_annotation_keys: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, "ignore_annotation_keys", tuple(self.ignore_annotation_keys))


@dataclass(frozen=True, slots=True)
class RuntimeExecutionProfile:
    """Runtime execution policy profile exported by the catalog."""

    runtime_baseline: RuntimeBaselinePolicy = field(default_factory=RuntimeBaselinePolicy)
    capabilities: RuntimeCapabilityPolicy = field(default_factory=RuntimeCapabilityPolicy)
    network_namespace: RuntimeNetworkNamespacePolicy = field(default_factory=RuntimeNetworkNamespacePolicy)
    resources: RuntimeResourcePolicy = field(default_factory=RuntimeResourcePolicy)


@dataclass(frozen=True, slots=True)
class OciImageDescriptor:
    """Digest-pinned OCI image descriptor metadata."""

    digest: str = ""
    media_type: str = ""
    size_bytes: int = 0
    annotations: Mapping[str, str] = field(default_factory=dict)

    def __post_init__(self) -> None:
        object.__setattr__(self, "annotations", dict(self.annotations))


@dataclass(frozen=True, slots=True)
class RuntimeTemplate:
    """Catalog metadata for an official Axern runtime."""

    id: str
    rootfs_readonly: bool = False
    image_default_argv: tuple[str, ...] = ()
    default_cwd: str = "/"
    default_env: Mapping[str, str] = field(default_factory=dict)
    mounts: tuple[MountSpec, ...] = ()
    capabilities: RuntimeCapabilities = field(default_factory=RuntimeCapabilities)
    language: str = ""
    language_version: str = ""
    description: str = ""
    version: str = ""
    image_descriptor: OciImageDescriptor = field(default_factory=OciImageDescriptor)
    warm_policy: str = ""
    cache_policy: str = ""
    execution_profile: RuntimeExecutionProfile = field(default_factory=RuntimeExecutionProfile)

    def __post_init__(self) -> None:
        object.__setattr__(self, "image_default_argv", tuple(self.image_default_argv))
        object.__setattr__(self, "mounts", tuple(self.mounts))
        object.__setattr__(self, "default_env", dict(self.default_env))
