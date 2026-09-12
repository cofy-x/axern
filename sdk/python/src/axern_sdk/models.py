"""Shared user-facing models for the Axern Python SDK."""

from __future__ import annotations

from dataclasses import dataclass


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
