"""Protocol conversion helpers for node sandbox RPCs."""

from __future__ import annotations

from collections.abc import Iterable, Iterator

from axern.common.file.v1 import file_pb2
from axern.node.sandbox.v1 import node_pb2
from axern_sdk.node.models import BrowserStatus, ExecResult, ImageProcessMount, SandboxFileInfo, SandboxFileKind, workspace_mount


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


def image_process_spec(
    image: str,
    argv: list[str],
    *,
    env: dict[str, str] | None,
    cwd: str,
    timeout_seconds: int,
    user: str,
    tty: bool,
    mounts: Iterable[ImageProcessMount] | None,
) -> node_pb2.ImageProcessSpec:
    return node_pb2.ImageProcessSpec(
        image=image,
        argv=list(argv),
        env=dict(env or {}),
        cwd=cwd,
        timeout_seconds=timeout_seconds,
        user=user,
        tty=tty,
        mounts=image_process_mounts(mounts),
    )


def image_process_mounts(mounts: Iterable[ImageProcessMount] | None) -> list[node_pb2.ImageProcessMount]:
    if mounts is None:
        mounts = (workspace_mount(),)
    return [
        node_pb2.ImageProcessMount(
            sandbox_path=mount.sandbox_path,
            target_path=mount.target_path,
            readonly=mount.readonly,
            options=list(mount.options),
        )
        for mount in mounts
    ]


def exec_stream_requests(
    *,
    allocation_id: str,
    spec: node_pb2.ExecSpec,
    input: bytes,
) -> Iterator[node_pb2.ExecStreamRequest]:
    yield node_pb2.ExecStreamRequest(
        open=node_pb2.ExecStreamOpen(
            allocation_id=allocation_id,
            spec=spec,
        )
    )
    chunk_size = 32 * 1024
    for offset in range(0, len(input), chunk_size):
        yield node_pb2.ExecStreamRequest(stdin=input[offset : offset + chunk_size])
    yield node_pb2.ExecStreamRequest(close_stdin=True)


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


def browser_status(response: node_pb2.BrowserStatusResponse) -> BrowserStatus:
    return BrowserStatus(
        available=bool(response.available),
        command=response.command,
        running=bool(response.running),
        pid=response.pid,
        url=response.url,
        reason=response.reason,
    )


def text_exec_result(result: ExecResult, *, encoding: str, errors: str) -> ExecResult:
    return ExecResult(
        exit_code=result.exit_code,
        stdout=result.stdout_text(encoding=encoding, errors=errors),
        stderr=result.stderr_text(encoding=encoding, errors=errors),
        stdout_truncated=result.stdout_truncated,
        stderr_truncated=result.stderr_truncated,
    )
