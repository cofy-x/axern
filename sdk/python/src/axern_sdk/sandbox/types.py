"""Shared data types for the high-level sandbox API."""

from __future__ import annotations

from dataclasses import dataclass

from axern_sdk.errors import SandboxValidationError


DEFAULT_SANDBOX_ARGV = ["sh", "-lc", "sleep 2147483647"]


@dataclass(frozen=True, slots=True)
class SandboxState:
    """Stable identifiers for an active SDK sandbox."""

    environment_id: str
    run_id: str
    allocation_id: str
    tunnel_session_id: str
    bound_addr: str


@dataclass(frozen=True, slots=True)
class SandboxMetadata:
    """Diagnostic metadata for an active SDK sandbox."""

    environment_id: str
    run_id: str
    allocation_id: str
    tunnel_session_id: str
    bound_addr: str
    started_at_ns: int
    labels: dict[str, str]


def _validate_source(*, image: str, environment_id: str) -> None:
    sources = [bool(image), bool(environment_id)]
    if sum(sources) != 1:
        raise SandboxValidationError("pass exactly one of image or environment_id")
