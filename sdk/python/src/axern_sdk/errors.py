"""Axern Python SDK exceptions."""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from axern_sdk.node.models import ExecResult


class AxernError(RuntimeError):
    """Base exception for Axern SDK failures."""


class AxernTimeoutError(AxernError, TimeoutError):
    """Raised when an SDK operation exceeds its timeout."""


class SandboxValidationError(AxernError, ValueError):
    """Raised when public sandbox options violate the SDK contract."""


class SandboxError(AxernError):
    """Base exception for sandbox lifecycle and execution failures."""


class SandboxNotStartedError(SandboxError):
    """Raised when an operation requires an active sandbox."""


class SandboxLifecycleError(SandboxError):
    """Raised when sandbox start, readiness, or cleanup fails."""


@dataclass(frozen=True)
class SandboxCapabilityErrorInfo:
    """Structured sandbox capability/provider detail parsed from an RPC error."""

    capability: str = ""
    provider: str = ""
    provider_state: str = ""
    reason: str = ""
    missing_dependencies: tuple[str, ...] = ()


class SandboxRpcError(SandboxError):
    """Raised when a sandbox RPC fails with a non-timeout gRPC status."""

    def __init__(
        self,
        *,
        operation: str,
        code: str,
        details: str,
        retryable: bool = False,
        allocation_id: str | None = None,
    ) -> None:
        self.operation = operation
        self.code = code
        self.details = details
        self.retryable = retryable
        self.allocation_id = allocation_id
        self.capability: SandboxCapabilityErrorInfo | None = sandbox_capability_error_info(details)
        super().__init__(f"{operation} failed with {code}: {details}")


class SandboxConnectionError(SandboxError):
    """Raised when the SDK cannot reach sandbox control or node APIs."""

    def __init__(
        self,
        message: str | None = None,
        *,
        operation: str = "",
        code: str = "",
        details: str = "",
        retryable: bool = True,
        allocation_id: str | None = None,
    ) -> None:
        self.operation = operation
        self.code = code
        self.details = details
        self.retryable = retryable
        self.allocation_id = allocation_id
        super().__init__(message or f"{operation} failed with {code}: {details}")


class SandboxCancelledError(SandboxConnectionError):
    """Raised when a sandbox RPC is cancelled before completion."""


class SandboxNotFoundError(SandboxRpcError):
    """Raised when a requested sandbox resource does not exist."""


class SandboxPermissionError(SandboxRpcError):
    """Raised when sandbox credentials lack permission for an operation."""


class SandboxPreconditionError(SandboxRpcError):
    """Raised when a sandbox operation is invalid for the resource state."""


class SandboxTimeoutError(SandboxError, AxernTimeoutError):
    """Raised when a sandbox operation exceeds its timeout."""

    def __init__(
        self,
        message: str,
        *,
        operation: str = "",
        details: str = "",
        retryable: bool = True,
        allocation_id: str | None = None,
    ) -> None:
        self.operation = operation
        self.code = "DEADLINE_EXCEEDED"
        self.details = details
        self.retryable = retryable
        self.allocation_id = allocation_id
        super().__init__(message)


class SandboxExecutionError(SandboxError):
    """Raised when sandbox process execution fails."""


class SandboxExecError(SandboxExecutionError):
    """Raised when a checked sandbox command exits unsuccessfully."""

    def __init__(self, *, argv: list[str], result: ExecResult) -> None:
        self.argv = list(argv)
        self.result = result
        super().__init__(f"sandbox command exited with code {result.exit_code}: {self.argv!r}")


def sandbox_capability_error_info(details: str) -> SandboxCapabilityErrorInfo | None:
    """Return sandbox capability/provider information from an Axern RPC detail string."""

    if "sandboxd" not in details and "provider" not in details:
        return None

    capability = _first_match(details, r"sandboxd ([a-z_][a-z0-9_]*) capability unavailable")
    if not capability:
        capability = _first_match(details, r"sandboxd ([a-z_][a-z0-9_]*) [a-z-]+ failed")

    provider = _first_match(details, r"provider=([^\s;]+)")
    provider_state = _first_match(details, r"state=([^\s;]+)")
    if not _is_sandbox_provider_state(provider_state):
        provider_state = ""
    reason = _first_match(details, r'reason="([^"]*)"')

    provider_detail = re.search(r"\b([a-z_][a-z0-9_]*) provider (available|degraded|unavailable): ([^;]+)", details)
    if provider_detail:
        provider = provider or provider_detail.group(1)
        provider_state = provider_state or provider_detail.group(2)
        reason = reason or provider_detail.group(3).strip()
    missing_dependencies = _missing_dependency_details(details)
    if not any((capability, provider, provider_state, reason, missing_dependencies)):
        return None
    return SandboxCapabilityErrorInfo(
        capability=capability,
        provider=provider,
        provider_state=provider_state,
        reason=reason,
        missing_dependencies=missing_dependencies,
    )


def _first_match(value: str, pattern: str) -> str:
    match = re.search(pattern, value)
    if match is None:
        return ""
    return match.group(1).strip()


def _is_sandbox_provider_state(value: str) -> bool:
    return value in {"available", "degraded", "unavailable"}


def _missing_dependency_details(details: str) -> tuple[str, ...]:
    match = re.search(r"missing dependencies: ([^;]+)", details)
    if match is not None:
        return tuple(part.strip() for part in match.group(1).split(",") if part.strip())

    match = re.search(r"dependencies=([^:]+)", details)
    if match is None:
        return ()
    return tuple(
        part.strip()
        for part in match.group(1).split(",")
        if part.strip() and "=unavailable" in part
    )
