"""Protocol conversion helpers for node sandbox RPCs."""

from __future__ import annotations

from axern.common.file.v1 import file_pb2
from axern.node.sandbox.v1 import node_pb2
from axern_sdk.node.models import ExecResult, SandboxFileInfo, SandboxFileKind


def exec_spec(
    argv: list[str],
    *,
    env: dict[str, str] | None,
    cwd: str,
    timeout_seconds: int,
    user: str,
    tty: bool,
) -> node_pb2.ExecSpec:
    return node_pb2.ExecSpec(
        argv=list(argv),
        env=dict(env or {}),
        cwd=cwd,
        timeout_seconds=timeout_seconds,
        user=user,
        tty=tty,
    )

def file_info(info: file_pb2.SandboxFileInfo) -> SandboxFileInfo:
    return SandboxFileInfo(
        path=info.path,
        kind=file_kind(info.kind),
        size=info.size,
        mode=info.mode,
        mtime_ns=info.mtime_ns,
    )


def file_kind(kind: int) -> SandboxFileKind:
    if kind == file_pb2.SANDBOX_FILE_KIND_FILE:
        return SandboxFileKind.FILE
    if kind == file_pb2.SANDBOX_FILE_KIND_DIRECTORY:
        return SandboxFileKind.DIRECTORY
    if kind == file_pb2.SANDBOX_FILE_KIND_SYMLINK:
        return SandboxFileKind.SYMLINK
    if kind == file_pb2.SANDBOX_FILE_KIND_OTHER:
        return SandboxFileKind.OTHER
    return SandboxFileKind.UNSPECIFIED


def text_exec_result(result: ExecResult, *, encoding: str, errors: str) -> ExecResult:
    return ExecResult(
        exit_code=result.exit_code,
        stdout=result.stdout_text(encoding=encoding, errors=errors),
        stderr=result.stderr_text(encoding=encoding, errors=errors),
        stdout_truncated=result.stdout_truncated,
        stderr_truncated=result.stderr_truncated,
    )
