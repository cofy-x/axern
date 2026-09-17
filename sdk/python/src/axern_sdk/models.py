"""Shared user-facing models for the Axern Python SDK."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum


class DeclaredOutputFormat(StrEnum):
    FILE = "file"
    TAR = "tar"


@dataclass(frozen=True, slots=True)
class DeclaredOutput:
    """One bounded path Axern seals before Allocation filesystem cleanup."""

    path: str
    format: DeclaredOutputFormat
    media_type: str = ""


@dataclass(frozen=True, slots=True)
class SealedOutput:
    output_id: str
    path: str
    size_bytes: int
    sha256: str
    media_type: str
    format: DeclaredOutputFormat | None
    status: str
    reason: str
    sealed_at: datetime | None
    expires_at: datetime | None


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
