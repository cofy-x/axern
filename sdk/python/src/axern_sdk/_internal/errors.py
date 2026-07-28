"""Internal exception mapping helpers."""

from __future__ import annotations

import grpc

from axern_sdk.errors import (
    SandboxCancelledError,
    SandboxConnectionError,
    SandboxNotFoundError,
    SandboxPermissionError,
    SandboxPreconditionError,
    SandboxRpcError,
    SandboxTimeoutError,
)


def sandbox_rpc_error(exc: grpc.RpcError, *, operation: str, allocation_id: str | None = None) -> Exception:
    code = exc.code()
    details = exc.details() or ""
    if code == grpc.StatusCode.DEADLINE_EXCEEDED:
        return SandboxTimeoutError(
            f"{operation} timed out: {details}",
            operation=operation,
            details=details,
            allocation_id=allocation_id,
        )
    if code == grpc.StatusCode.NOT_FOUND:
        return SandboxNotFoundError(operation=operation, code=code.name, details=details, allocation_id=allocation_id)
    if code == grpc.StatusCode.FAILED_PRECONDITION:
        return SandboxPreconditionError(operation=operation, code=code.name, details=details, allocation_id=allocation_id)
    if code == grpc.StatusCode.CANCELLED:
        return SandboxCancelledError(
            f"{operation} was cancelled: {details}",
            operation=operation,
            code=code.name,
            details=details,
            retryable=False,
            allocation_id=allocation_id,
        )
    if code == grpc.StatusCode.UNAVAILABLE:
        return SandboxConnectionError(
            f"{operation} failed with {code.name}: {details}",
            operation=operation,
            code=code.name,
            details=details,
            retryable=True,
            allocation_id=allocation_id,
        )
    if code in {grpc.StatusCode.PERMISSION_DENIED, grpc.StatusCode.UNAUTHENTICATED}:
        return SandboxPermissionError(
            operation=operation,
            code=code.name,
            details=details,
            retryable=False,
            allocation_id=allocation_id,
        )
    return SandboxRpcError(operation=operation, code=code.name, details=details, allocation_id=allocation_id)
