"""Synchronous sandbox file and archive helpers."""

from __future__ import annotations

from os import PathLike
from pathlib import Path
from tempfile import NamedTemporaryFile
from typing import Protocol

from axern_sdk.node import NodeSandboxClient, SandboxFileInfo
from axern_sdk.sandbox.archive import archive_chunks, create_directory_archive, safe_extract_directory_archive


class _HasNodeClient(Protocol):
    def _node_client(self) -> NodeSandboxClient: ...
    def read_bytes(self, path: str, *, timeout_seconds: int = 30) -> bytes: ...
    def read_text(
        self,
        path: str,
        *,
        encoding: str = "utf-8",
        errors: str = "strict",
        timeout_seconds: int = 30,
    ) -> str: ...
    def write_bytes(
        self,
        path: str,
        data: bytes,
        *,
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None: ...
    def write_text(
        self,
        path: str,
        data: str,
        *,
        encoding: str = "utf-8",
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None: ...


class SandboxFileMixin:
    """File and archive operations for a started sandbox."""

    def read_file(
        self: _HasNodeClient,
        path: str,
        *,
        encoding: str | None = "utf-8",
        timeout_seconds: int = 30,
    ) -> str | bytes:
        if encoding is None:
            return self.read_bytes(path, timeout_seconds=timeout_seconds)
        return self.read_text(path, encoding=encoding, timeout_seconds=timeout_seconds)

    def read_bytes(self: _HasNodeClient, path: str, *, timeout_seconds: int = 30) -> bytes:
        return self._node_client().read_file(path, rpc_timeout=timeout_seconds)

    def read_text(
        self: _HasNodeClient,
        path: str,
        *,
        encoding: str = "utf-8",
        errors: str = "strict",
        timeout_seconds: int = 30,
    ) -> str:
        return self.read_bytes(path, timeout_seconds=timeout_seconds).decode(encoding, errors=errors)

    def write_file(
        self: _HasNodeClient,
        path: str,
        data: bytes | str,
        *,
        encoding: str = "utf-8",
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        if isinstance(data, str):
            self.write_text(
                path,
                data,
                encoding=encoding,
                create_parents=create_parents,
                timeout_seconds=timeout_seconds,
            )
            return
        self.write_bytes(path, data, create_parents=create_parents, timeout_seconds=timeout_seconds)

    def write_bytes(
        self: _HasNodeClient,
        path: str,
        data: bytes,
        *,
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        self._node_client().write_file(path, data, create_parents=create_parents, rpc_timeout=timeout_seconds)

    def write_text(
        self: _HasNodeClient,
        path: str,
        data: str,
        *,
        encoding: str = "utf-8",
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        self.write_bytes(
            path,
            data.encode(encoding),
            create_parents=create_parents,
            timeout_seconds=timeout_seconds,
        )

    def upload_file(
        self: _HasNodeClient,
        local_path: str | PathLike[str],
        remote_path: str,
        *,
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        data = Path(local_path).read_bytes()
        self.write_bytes(remote_path, data, create_parents=create_parents, timeout_seconds=timeout_seconds)

    def download_file(
        self: _HasNodeClient,
        remote_path: str,
        local_path: str | PathLike[str],
        *,
        create_parents: bool = True,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        target = Path(local_path)
        if target.exists() and not overwrite:
            raise FileExistsError(str(target))
        if create_parents:
            target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(self.read_bytes(remote_path, timeout_seconds=timeout_seconds))

    def upload_dir(
        self: _HasNodeClient,
        local_path: str | PathLike[str],
        remote_path: str,
        *,
        create_parents: bool = True,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        archive_path = create_directory_archive(Path(local_path))
        try:
            self._node_client().upload_archive(
                remote_path,
                lambda: archive_chunks(archive_path),
                create_parents=create_parents,
                overwrite=overwrite,
                rpc_timeout=timeout_seconds,
            )
        finally:
            archive_path.unlink(missing_ok=True)

    def download_dir(
        self: _HasNodeClient,
        remote_path: str,
        local_path: str | PathLike[str],
        *,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        with NamedTemporaryFile(prefix="axern-download-", suffix=".tar", delete=False) as archive:
            archive_path = Path(archive.name)
            self._node_client().download_archive(remote_path, archive.write, rpc_timeout=timeout_seconds)
        try:
            safe_extract_directory_archive(archive_path, Path(local_path), overwrite=overwrite)
        finally:
            archive_path.unlink(missing_ok=True)

    def exists(self: _HasNodeClient, path: str, *, timeout_seconds: int = 30) -> bool:
        return self._node_client().exists(path, rpc_timeout=timeout_seconds)

    def stat(self: _HasNodeClient, path: str, *, timeout_seconds: int = 30) -> SandboxFileInfo:
        return self._node_client().stat_file(path, rpc_timeout=timeout_seconds)

    def list_dir(self: _HasNodeClient, path: str, *, timeout_seconds: int = 30) -> list[SandboxFileInfo]:
        return self._node_client().list_dir(path, rpc_timeout=timeout_seconds)

    def mkdir(self: _HasNodeClient, path: str, *, parents: bool = True, timeout_seconds: int = 30) -> None:
        self._node_client().mkdir(path, parents=parents, rpc_timeout=timeout_seconds)

    def remove(
        self: _HasNodeClient,
        path: str,
        *,
        recursive: bool = False,
        force: bool = False,
        timeout_seconds: int = 30,
    ) -> None:
        self._node_client().remove(path, recursive=recursive, force=force, rpc_timeout=timeout_seconds)

    def copy(
        self: _HasNodeClient,
        src_path: str,
        dst_path: str,
        *,
        recursive: bool = False,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        self._node_client().copy(src_path, dst_path, recursive=recursive, overwrite=overwrite, rpc_timeout=timeout_seconds)

    def move(
        self: _HasNodeClient,
        src_path: str,
        dst_path: str,
        *,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        self._node_client().move(src_path, dst_path, overwrite=overwrite, rpc_timeout=timeout_seconds)

    def chmod(
        self: _HasNodeClient,
        path: str,
        mode: int,
        *,
        recursive: bool = False,
        timeout_seconds: int = 30,
    ) -> None:
        self._node_client().chmod(path, mode, recursive=recursive, rpc_timeout=timeout_seconds)

    def touch(
        self: _HasNodeClient,
        path: str,
        *,
        create: bool = True,
        mtime_ns: int = 0,
        timeout_seconds: int = 30,
    ) -> None:
        self._node_client().touch(path, create=create, mtime_ns=mtime_ns, rpc_timeout=timeout_seconds)
