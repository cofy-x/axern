"""Async sandbox file and archive helpers."""

from __future__ import annotations

import asyncio
from os import PathLike
from pathlib import Path
from tempfile import NamedTemporaryFile
from typing import Protocol

from axern_sdk.node import AsyncNodeSandboxClient, SandboxFileInfo
from axern_sdk.sandbox.archive import archive_chunks, create_directory_archive, safe_extract_directory_archive


class _HasAsyncNodeClient(Protocol):
    def _node_client(self) -> AsyncNodeSandboxClient: ...
    async def read_bytes(self, path: str, *, timeout_seconds: int = 30) -> bytes: ...
    async def read_text(
        self,
        path: str,
        *,
        encoding: str = "utf-8",
        errors: str = "strict",
        timeout_seconds: int = 30,
    ) -> str: ...
    async def write_bytes(
        self,
        path: str,
        data: bytes,
        *,
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None: ...
    async def write_text(
        self,
        path: str,
        data: str,
        *,
        encoding: str = "utf-8",
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None: ...


class AsyncSandboxFileMixin:
    """Async file and archive operations for a started sandbox."""

    async def read_file(
        self: _HasAsyncNodeClient,
        path: str,
        *,
        encoding: str | None = "utf-8",
        timeout_seconds: int = 30,
    ) -> str | bytes:
        if encoding is None:
            return await self.read_bytes(path, timeout_seconds=timeout_seconds)
        return await self.read_text(path, encoding=encoding, timeout_seconds=timeout_seconds)

    async def read_bytes(self: _HasAsyncNodeClient, path: str, *, timeout_seconds: int = 30) -> bytes:
        return await self._node_client().read_file(path, rpc_timeout=timeout_seconds)

    async def read_text(
        self: _HasAsyncNodeClient,
        path: str,
        *,
        encoding: str = "utf-8",
        errors: str = "strict",
        timeout_seconds: int = 30,
    ) -> str:
        return (await self.read_bytes(path, timeout_seconds=timeout_seconds)).decode(encoding, errors=errors)

    async def write_file(
        self: _HasAsyncNodeClient,
        path: str,
        data: bytes | str,
        *,
        encoding: str = "utf-8",
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        if isinstance(data, str):
            await self.write_text(
                path,
                data,
                encoding=encoding,
                create_parents=create_parents,
                timeout_seconds=timeout_seconds,
            )
            return
        await self.write_bytes(path, data, create_parents=create_parents, timeout_seconds=timeout_seconds)

    async def write_bytes(
        self: _HasAsyncNodeClient,
        path: str,
        data: bytes,
        *,
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        await self._node_client().write_file(path, data, create_parents=create_parents, rpc_timeout=timeout_seconds)

    async def write_text(
        self: _HasAsyncNodeClient,
        path: str,
        data: str,
        *,
        encoding: str = "utf-8",
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        await self.write_bytes(
            path,
            data.encode(encoding),
            create_parents=create_parents,
            timeout_seconds=timeout_seconds,
        )

    async def upload_file(
        self: _HasAsyncNodeClient,
        local_path: str | PathLike[str],
        remote_path: str,
        *,
        create_parents: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        data = await asyncio.to_thread(Path(local_path).read_bytes)
        await self.write_bytes(remote_path, data, create_parents=create_parents, timeout_seconds=timeout_seconds)

    async def download_file(
        self: _HasAsyncNodeClient,
        remote_path: str,
        local_path: str | PathLike[str],
        *,
        create_parents: bool = True,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        target = Path(local_path)
        if await asyncio.to_thread(target.exists) and not overwrite:
            raise FileExistsError(str(target))
        if create_parents:
            await asyncio.to_thread(target.parent.mkdir, parents=True, exist_ok=True)
        data = await self.read_bytes(remote_path, timeout_seconds=timeout_seconds)
        await asyncio.to_thread(target.write_bytes, data)

    async def upload_dir(
        self: _HasAsyncNodeClient,
        local_path: str | PathLike[str],
        remote_path: str,
        *,
        create_parents: bool = True,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        archive_path = await asyncio.to_thread(create_directory_archive, Path(local_path))
        try:
            await self._node_client().upload_archive(
                remote_path,
                lambda: archive_chunks(archive_path),
                create_parents=create_parents,
                overwrite=overwrite,
                rpc_timeout=timeout_seconds,
            )
        finally:
            await asyncio.to_thread(archive_path.unlink, missing_ok=True)

    async def download_dir(
        self: _HasAsyncNodeClient,
        remote_path: str,
        local_path: str | PathLike[str],
        *,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        with NamedTemporaryFile(prefix="axern-download-", suffix=".tar", delete=False) as archive:
            archive_path = Path(archive.name)
            await self._node_client().download_archive(remote_path, archive.write, rpc_timeout=timeout_seconds)
        try:
            await asyncio.to_thread(safe_extract_directory_archive, archive_path, Path(local_path), overwrite=overwrite)
        finally:
            await asyncio.to_thread(archive_path.unlink, missing_ok=True)

    async def exists(self: _HasAsyncNodeClient, path: str, *, timeout_seconds: int = 30) -> bool:
        return await self._node_client().exists(path, rpc_timeout=timeout_seconds)

    async def stat(self: _HasAsyncNodeClient, path: str, *, timeout_seconds: int = 30) -> SandboxFileInfo:
        return await self._node_client().stat_file(path, rpc_timeout=timeout_seconds)

    async def list_dir(self: _HasAsyncNodeClient, path: str, *, timeout_seconds: int = 30) -> list[SandboxFileInfo]:
        return await self._node_client().list_dir(path, rpc_timeout=timeout_seconds)

    async def mkdir(self: _HasAsyncNodeClient, path: str, *, parents: bool = True, timeout_seconds: int = 30) -> None:
        await self._node_client().mkdir(path, parents=parents, rpc_timeout=timeout_seconds)

    async def remove(
        self: _HasAsyncNodeClient,
        path: str,
        *,
        recursive: bool = False,
        force: bool = False,
        timeout_seconds: int = 30,
    ) -> None:
        await self._node_client().remove(path, recursive=recursive, force=force, rpc_timeout=timeout_seconds)

    async def copy(
        self: _HasAsyncNodeClient,
        src_path: str,
        dst_path: str,
        *,
        recursive: bool = False,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        await self._node_client().copy(src_path, dst_path, recursive=recursive, overwrite=overwrite, rpc_timeout=timeout_seconds)

    async def move(
        self: _HasAsyncNodeClient,
        src_path: str,
        dst_path: str,
        *,
        overwrite: bool = True,
        timeout_seconds: int = 30,
    ) -> None:
        await self._node_client().move(src_path, dst_path, overwrite=overwrite, rpc_timeout=timeout_seconds)

    async def chmod(
        self: _HasAsyncNodeClient,
        path: str,
        mode: int,
        *,
        recursive: bool = False,
        timeout_seconds: int = 30,
    ) -> None:
        await self._node_client().chmod(path, mode, recursive=recursive, rpc_timeout=timeout_seconds)

    async def touch(
        self: _HasAsyncNodeClient,
        path: str,
        *,
        create: bool = True,
        mtime_ns: int = 0,
        timeout_seconds: int = 30,
    ) -> None:
        await self._node_client().touch(path, create=create, mtime_ns=mtime_ns, rpc_timeout=timeout_seconds)
