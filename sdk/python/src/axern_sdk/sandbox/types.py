"""Shared data types for the high-level sandbox API."""

from __future__ import annotations

from dataclasses import dataclass

from axern_sdk.errors import SandboxValidationError


DEFAULT_SANDBOX_ARGV = ["sh", "-lc", "sleep 2147483647"]


@dataclass(frozen=True, slots=True)
class SandboxState:
    """Stable identifiers for an active SDK sandbox."""

    environment_id: str
    service_id: str
    allocation_id: str
    attempt: int
    node_id: str
    tunnel_session_id: str
    bound_addr: str


@dataclass(frozen=True, slots=True)
class SandboxMetadata:
    """Diagnostic metadata for an active SDK sandbox."""

    environment_id: str
    service_id: str
    allocation_id: str
    attempt: int
    node_id: str
    runtime_class: str
    tunnel_session_id: str
    bound_addr: str
    started_at_ns: int
    labels: dict[str, str]


def _validate_source(*, image: str, template_id: str, environment_id: str) -> None:
    sources = [bool(image), bool(template_id), bool(environment_id)]
    if sum(sources) != 1:
        raise SandboxValidationError("pass exactly one of image, template_id, or environment_id")
