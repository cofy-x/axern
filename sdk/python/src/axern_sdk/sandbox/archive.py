"""Local archive helpers for sandbox directory transfer."""

from __future__ import annotations

import os
import posixpath
import tarfile
from collections.abc import Iterator
from pathlib import Path
from tempfile import NamedTemporaryFile

DEFAULT_ARCHIVE_CHUNK_SIZE = 1024 * 1024


def create_directory_archive(source: Path) -> Path:
    if source.is_symlink():
        raise ValueError(f"local directory symlinks are not supported: {source}")
    if not source.is_dir():
        raise NotADirectoryError(str(source))
    tmp = NamedTemporaryFile(prefix="axern-upload-", suffix=".tar", delete=False)
    tmp_path = Path(tmp.name)
    tmp.close()
    try:
        with tarfile.open(tmp_path, "w") as archive:
            archive.add(source, arcname=".", recursive=False)
            for root, dirnames, filenames in os.walk(source):
                root_path = Path(root)
                for name in sorted(dirnames):
                    path = root_path / name
                    _reject_symlink(path)
                    archive.add(path, arcname=_archive_name(source, path), recursive=False)
                for name in sorted(filenames):
                    path = root_path / name
                    _reject_symlink(path)
                    if not path.is_file():
                        raise ValueError(f"local path kind is not supported: {path}")
                    archive.add(path, arcname=_archive_name(source, path), recursive=False)
    except Exception:
        _remove_file(tmp_path)
        raise
    return tmp_path


def archive_chunks(path: Path, *, chunk_size: int = DEFAULT_ARCHIVE_CHUNK_SIZE) -> Iterator[bytes]:
    with path.open("rb") as archive:
        while True:
            chunk = archive.read(chunk_size)
            if not chunk:
                break
            yield chunk


def safe_extract_directory_archive(archive_path: Path, target: Path, *, overwrite: bool) -> None:
    if target.exists():
        if not target.is_dir():
            raise NotADirectoryError(str(target))
        _reject_existing_symlinks(target)
        if not overwrite and any(target.iterdir()):
            raise FileExistsError(str(target))
    else:
        target.mkdir(parents=True)

    with tarfile.open(archive_path, "r") as archive:
        for member in archive:
            relative = _safe_member_path(member)
            destination = target.joinpath(*relative.split("/")) if relative else target
            _reject_symlink_path(target, destination)
            if member.isdir():
                destination.mkdir(parents=True, exist_ok=True)
                continue
            if member.isfile():
                destination.parent.mkdir(parents=True, exist_ok=True)
                extracted = archive.extractfile(member)
                if extracted is None:
                    raise ValueError(f"archive file entry has no content: {member.name}")
                with extracted, destination.open("wb") as output:
                    while True:
                        chunk = extracted.read(DEFAULT_ARCHIVE_CHUNK_SIZE)
                        if not chunk:
                            break
                        output.write(chunk)
                os.chmod(destination, member.mode & 0o777)
                continue
            raise ValueError(f"archive entry kind is not supported: {member.name}")


def _archive_name(source: Path, path: Path) -> str:
    return path.relative_to(source).as_posix()


def _safe_member_path(member: tarfile.TarInfo) -> str:
    name = member.name
    if member.issym() or member.islnk():
        raise ValueError(f"archive links are not supported: {name}")
    if not name:
        raise ValueError("archive entry path is required")
    if name.startswith("/"):
        raise ValueError(f"archive entry is absolute: {name}")
    if ".." in name.split("/"):
        raise ValueError(f"archive entry escapes target directory: {name}")
    cleaned = posixpath.normpath(name)
    if cleaned == ".":
        return ""
    if cleaned == "..":
        raise ValueError(f"archive entry escapes target directory: {name}")
    return cleaned


def _reject_symlink(path: Path) -> None:
    if path.is_symlink():
        raise ValueError(f"local symlinks are not supported: {path}")


def _reject_existing_symlinks(root: Path) -> None:
    if root.is_symlink():
        raise ValueError(f"local symlinks are not supported: {root}")
    for path in root.rglob("*"):
        if path.is_symlink():
            raise ValueError(f"local symlinks are not supported: {path}")


def _reject_symlink_path(root: Path, destination: Path) -> None:
    current = root
    if current.is_symlink():
        raise ValueError(f"local symlinks are not supported: {current}")
    try:
        relative = destination.relative_to(root)
    except ValueError as exc:
        raise ValueError(f"archive destination escapes target directory: {destination}") from exc
    for part in relative.parts:
        current = current / part
        if current.is_symlink():
            raise ValueError(f"local symlinks are not supported: {current}")


def _remove_file(path: Path) -> None:
    try:
        path.unlink()
    except FileNotFoundError:
        pass
