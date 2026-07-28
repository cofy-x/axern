"""Shared user-facing models for the Axern Python SDK."""

from __future__ import annotations

import math
from dataclasses import dataclass, field
from typing import Iterable


@dataclass(frozen=True, slots=True)
class HTTPProbe:
    """HTTP service readiness or liveness probe."""

    port: int
    path: str = "/"
    scheme: str = "http"

    def __post_init__(self) -> None:
        object.__setattr__(self, "port", int(self.port))
        object.__setattr__(self, "path", self.path.strip() or "/")
        object.__setattr__(self, "scheme", self.scheme.strip().lower() or "http")


@dataclass(frozen=True, slots=True)
class TCPProbe:
    """TCP service readiness or liveness probe."""

    port: int

    def __post_init__(self) -> None:
        object.__setattr__(self, "port", int(self.port))


@dataclass(frozen=True, slots=True)
class ServiceProbe:
    """Service probe intent for service readiness or liveness."""

    http: HTTPProbe | None = None
    tcp: TCPProbe | None = None
    initial_delay: float = 0.0
    period: float = 0.0
    timeout: float = 0.0
    success_threshold: int = 0
    failure_threshold: int = 0

    def __post_init__(self) -> None:
        if (self.http is None) == (self.tcp is None):
            raise ValueError("ServiceProbe must set exactly one of http or tcp")
        for name in ("initial_delay", "period", "timeout"):
            value = float(getattr(self, name))
            if not math.isfinite(value) or value < 0:
                raise ValueError(f"ServiceProbe.{name} must be a finite non-negative duration in seconds")
            milliseconds = value * 1000
            if not math.isclose(milliseconds, round(milliseconds), rel_tol=0.0, abs_tol=1e-9):
                raise ValueError(f"ServiceProbe.{name} must use whole milliseconds")
            object.__setattr__(self, name, value)
        for name in ("success_threshold", "failure_threshold"):
            raw_value = getattr(self, name)
            if isinstance(raw_value, bool):
                raise ValueError(f"ServiceProbe.{name} must be an integer")
            value = int(raw_value)
            if value != raw_value:
                raise ValueError(f"ServiceProbe.{name} must be an integer")
            if value < 0:
                raise ValueError(f"ServiceProbe.{name} must be non-negative")
            object.__setattr__(self, name, value)


@dataclass(frozen=True, slots=True)
class VolumeMount:
    """Service volume mount intent for workloads and SDK sandboxes."""

    name: str
    target: str
    readonly: bool = False
    options: Iterable[str] = field(default_factory=tuple)

    def __post_init__(self) -> None:
        object.__setattr__(self, "name", self.name.strip())
        object.__setattr__(self, "target", self.target.strip())
        object.__setattr__(
            self,
            "options",
            tuple(option.strip() for option in self.options if option.strip()),
        )


@dataclass(frozen=True, slots=True)
class SecretEnvVar:
    """Projection of one secret key into an environment variable."""

    name: str
    secret_id: str
    key: str
    optional: bool = False


@dataclass(frozen=True, slots=True)
class SecretFile:
    """Projection of one secret key into a file."""

    path: str
    secret_id: str
    key: str
    mode: int = 0
    optional: bool = False


@dataclass(frozen=True, slots=True)
class ImageMount:
    """Read-only image mounted below the workload rootfs."""

    image: str
    target: str
