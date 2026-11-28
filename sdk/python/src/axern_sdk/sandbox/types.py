"""Shared data types for the high-level sandbox API."""

from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager
from dataclasses import dataclass

from axern_sdk.errors import SandboxNotFoundError, SandboxValidationError


DEFAULT_SANDBOX_ARGV = ["sh", "-lc", "sleep 2147483647"]


@dataclass(slots=True)
class _OwnedResource:
    id: str = ""

    @contextmanager
    def releasing(self, errors: list[Exception]) -> Iterator[str]:
        """Retain the acknowledged ID until cleanup succeeds or confirms absence."""
        try:
            yield self.id
        except SandboxNotFoundError:
            pass
        except Exception as error:
            errors.append(error)
            return
        self.id = ""


@dataclass(frozen=True, slots=True)
class SandboxState:
    """Stable identifiers for an active SDK sandbox."""

    environment_id: str
    run_id: str
    allocation_id: str
    tunnel_session_id: str
    bound_addr: str
    workspace_id: str = ""
    workspace_generation: int = 0


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
    workspace_id: str = ""
    workspace_generation: int = 0


def _validate_source(*, image: str, environment_id: str) -> None:
    sources = [bool(image), bool(environment_id)]
    if sum(sources) != 1:
        raise SandboxValidationError("pass exactly one of image or environment_id")
