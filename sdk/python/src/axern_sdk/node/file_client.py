"""Synchronous node sandbox file RPC mixin."""

from __future__ import annotations

from collections.abc import Callable, Iterator
from typing import TYPE_CHECKING, Any

import grpc

from axern.common.file.v1 import file_pb2
from axern.node.sandbox.v1 import node_pb2, node_pb2_grpc
from axern_sdk._internal.errors import sandbox_rpc_error
from axern_sdk.node.models import SandboxFileInfo
from axern_sdk.node.protocol import file_info


class NodeSandboxFileMixin:
    """File and archive RPCs for ``NodeSandboxClient``."""

    if TYPE_CHECKING:
        _allocation_id: str

        def _call_unary(
            self,
            operation: str,
            method_name: str,
            request_factory: Callable[[], object],
            *,
            lease_ttl_seconds: int,
            rpc_timeout: float | None,
        ) -> Any: ...

        def _gateway_channel(self) -> grpc.Channel: ...

    def stat_file(
        self,
        path: str,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> SandboxFileInfo:
        if not path:
            raise ValueError("path is required")
        response = self._call_unary(
            "sandbox stat file",
            "StatFile",
            lambda: node_pb2.StatFileRequest(allocation_id=self._allocation_id, path=path),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return file_info(response.info)

    def list_dir(
        self,
        path: str,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> list[SandboxFileInfo]:
        if not path:
            raise ValueError("path is required")
        response = self._call_unary(
            "sandbox list directory",
            "ListDir",
            lambda: node_pb2.ListDirRequest(allocation_id=self._allocation_id, path=path),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return [file_info(entry) for entry in response.entries]

    def read_file(
        self,
        path: str,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> bytes:
        if not path:
            raise ValueError("path is required")
        response = self._call_unary(
            "sandbox read file",
            "ReadFile",
            lambda: node_pb2.ReadFileRequest(allocation_id=self._allocation_id, path=path),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return bytes(response.data)

    def write_file(
        self,
        path: str,
        data: bytes,
        *,
        create_parents: bool = True,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not path:
            raise ValueError("path is required")
        self._call_unary(
            "sandbox write file",
            "WriteFile",
            lambda: node_pb2.WriteFileRequest(
                allocation_id=self._allocation_id,
                path=path,
                data=data,
                create_parents=create_parents,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    def mkdir(
        self,
        path: str,
        *,
        parents: bool = True,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not path:
            raise ValueError("path is required")
        self._call_unary(
            "sandbox make directory",
            "Mkdir",
            lambda: node_pb2.MkdirRequest(
                allocation_id=self._allocation_id,
                path=path,
                parents=parents,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    def remove(
        self,
        path: str,
        *,
        recursive: bool = False,
        force: bool = False,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not path:
            raise ValueError("path is required")
        self._call_unary(
            "sandbox remove",
            "Remove",
            lambda: node_pb2.RemoveRequest(
                allocation_id=self._allocation_id,
                path=path,
                recursive=recursive,
                force=force,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    def exists(
        self,
        path: str,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> bool:
        if not path:
            raise ValueError("path is required")
        response = self._call_unary(
            "sandbox exists",
            "Exists",
            lambda: node_pb2.ExistsRequest(allocation_id=self._allocation_id, path=path),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return bool(response.exists)

    def copy(
        self,
        src_path: str,
        dst_path: str,
        *,
        recursive: bool = False,
        overwrite: bool = True,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not src_path or not dst_path:
            raise ValueError("src_path and dst_path are required")
        self._call_unary(
            "sandbox copy",
            "Copy",
            lambda: node_pb2.CopyRequest(
                allocation_id=self._allocation_id,
                src_path=src_path,
                dst_path=dst_path,
                recursive=recursive,
                overwrite=overwrite,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    def move(
        self,
        src_path: str,
        dst_path: str,
        *,
        overwrite: bool = True,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not src_path or not dst_path:
            raise ValueError("src_path and dst_path are required")
        self._call_unary(
            "sandbox move",
            "Move",
            lambda: node_pb2.MoveRequest(
                allocation_id=self._allocation_id,
                src_path=src_path,
                dst_path=dst_path,
                overwrite=overwrite,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    def chmod(
        self,
        path: str,
        mode: int,
        *,
        recursive: bool = False,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not path:
            raise ValueError("path is required")
        self._call_unary(
            "sandbox chmod",
            "Chmod",
            lambda: node_pb2.ChmodRequest(
                allocation_id=self._allocation_id,
                path=path,
                mode=mode,
                recursive=recursive,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    def touch(
        self,
        path: str,
        *,
        create: bool = True,
        mtime_ns: int = 0,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not path:
            raise ValueError("path is required")
        self._call_unary(
            "sandbox touch",
            "Touch",
            lambda: node_pb2.TouchRequest(
                allocation_id=self._allocation_id,
                path=path,
                create=create,
                mtime_ns=mtime_ns,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    def upload_archive(
        self,
        path: str,
        chunk_factory: Callable[[], Iterator[bytes]],
        *,
        create_parents: bool = True,
        overwrite: bool = True,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not path:
            raise ValueError("path is required")
        try:
            node_pb2_grpc.NodeSandboxStub(self._gateway_channel()).UploadArchive(
                self._upload_archive_requests(
                    path=path,
                    chunks=chunk_factory(),
                    create_parents=create_parents,
                    overwrite=overwrite,
                ),
                timeout=rpc_timeout,
            )
        except grpc.RpcError as exc:
            raise sandbox_rpc_error(exc, operation="sandbox upload archive", allocation_id=self._allocation_id) from exc

    def download_archive(
        self,
        path: str,
        writer: Callable[[bytes], object],
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        if not path:
            raise ValueError("path is required")
        try:
            responses = node_pb2_grpc.NodeSandboxStub(self._gateway_channel()).DownloadArchive(
                node_pb2.DownloadArchiveRequest(
                    allocation_id=self._allocation_id,
                    path=path,
                    format=file_pb2.SANDBOX_ARCHIVE_FORMAT_TAR,
                    symlink_policy=file_pb2.SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
                ),
                timeout=rpc_timeout,
            )
            for response in responses:
                if response.chunk:
                    writer(bytes(response.chunk))
        except grpc.RpcError as exc:
            raise sandbox_rpc_error(exc, operation="sandbox download archive", allocation_id=self._allocation_id) from exc

    def _upload_archive_requests(
        self,
        *,
        path: str,
        chunks: Iterator[bytes],
        create_parents: bool,
        overwrite: bool,
    ) -> Iterator[node_pb2.UploadArchiveRequest]:
        yield node_pb2.UploadArchiveRequest(
            open=node_pb2.UploadArchiveOpen(
                allocation_id=self._allocation_id,
                path=path,
                format=file_pb2.SANDBOX_ARCHIVE_FORMAT_TAR,
                create_parents=create_parents,
                overwrite=overwrite,
                symlink_policy=file_pb2.SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT,
            )
        )
        for chunk in chunks:
            if chunk:
                yield node_pb2.UploadArchiveRequest(chunk=chunk)
