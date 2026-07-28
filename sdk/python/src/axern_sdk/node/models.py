"""User-facing models for sandbox operations."""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass, field
from enum import StrEnum
from typing import Literal, Self, TypeAlias


ExecCommand: TypeAlias = str | Sequence[str]
ExecOutput: TypeAlias = bytes | str


@dataclass(frozen=True, slots=True)
class ImageProcessMount:
    """Host-backed sandbox path shared into an image-backed process."""

    sandbox_path: str
    target_path: str
    readonly: bool = False
    options: tuple[str, ...] = ()


def workspace_mount(path: str = "/workspace") -> ImageProcessMount:
    """Share a sandbox workspace path at the same path in an image process."""

    return ImageProcessMount(sandbox_path=path, target_path=path)


@dataclass(frozen=True, slots=True)
class ExecResult:
    """Collected result from a sandbox process execution."""

    exit_code: int
    stdout: ExecOutput = b""
    stderr: ExecOutput = b""
    stdout_truncated: bool = False
    stderr_truncated: bool = False

    @property
    def ok(self) -> bool:
        return self.exit_code == 0

    def raise_for_status(self, argv: Sequence[str] | None = None) -> Self:
        """Raise `SandboxExecError` if the command exited unsuccessfully."""

        if not self.ok:
            from axern_sdk.errors import SandboxExecError

            raise SandboxExecError(argv=list(argv or []), result=self)
        return self

    def stdout_text(self, encoding: str = "utf-8", errors: str = "replace") -> str:
        if isinstance(self.stdout, str):
            return self.stdout
        return self.stdout.decode(encoding, errors=errors)

    def stderr_text(self, encoding: str = "utf-8", errors: str = "replace") -> str:
        if isinstance(self.stderr, str):
            return self.stderr
        return self.stderr.decode(encoding, errors=errors)

    def stdout_bytes(self, encoding: str = "utf-8", errors: str = "strict") -> bytes:
        if isinstance(self.stdout, bytes):
            return self.stdout
        return self.stdout.encode(encoding, errors=errors)

    def stderr_bytes(self, encoding: str = "utf-8", errors: str = "strict") -> bytes:
        if isinstance(self.stderr, bytes):
            return self.stderr
        return self.stderr.encode(encoding, errors=errors)


@dataclass(frozen=True, slots=True)
class ExecStreamEvent:
    """One output or exit event from a streamed sandbox execution."""

    stream: Literal["stdout", "stderr", "exit"]
    data: bytes = b""
    exit_code: int | None = None
    message: str = ""

    def text(self, encoding: str = "utf-8", errors: str = "replace") -> str:
        return self.data.decode(encoding, errors=errors)


class SandboxFileKind(StrEnum):
    """Kind of a path inside a sandbox."""

    FILE = "file"
    DIRECTORY = "directory"
    SYMLINK = "symlink"
    OTHER = "other"
    UNSPECIFIED = "unspecified"


@dataclass(frozen=True, slots=True)
class SandboxFileInfo:
    """Metadata for a path inside a sandbox."""

    path: str
    kind: SandboxFileKind
    size: int
    mode: int
    mtime_ns: int


@dataclass(frozen=True, slots=True)
class ComputerUseDependencyStatus:
    """One dependency check reported for sandbox desktop automation."""

    name: str
    available: bool
    reason: str = ""


@dataclass(frozen=True, slots=True)
class ComputerUseStatus:
    """Desktop automation capability status for a sandbox."""

    available: bool
    display: str = ""
    backend: str = ""
    reason: str = ""
    dependencies: tuple[ComputerUseDependencyStatus, ...] = ()


@dataclass(frozen=True, slots=True)
class ComputerUseScreenshot:
    """Screenshot bytes returned by sandbox desktop automation."""

    data: bytes
    content_type: str


@dataclass(frozen=True, slots=True)
class ComputerUseRegion:
    """Rectangular region for desktop screenshots."""

    x: int
    y: int
    width: int
    height: int


@dataclass(frozen=True, slots=True)
class ComputerUseDisplay:
    """Desktop display geometry reported by a sandbox desktop session."""

    display: str
    backend: str
    width: int
    height: int


@dataclass(frozen=True, slots=True)
class BrowserStatus:
    """Browser capability status for a sandbox desktop session."""

    available: bool
    command: str = ""
    running: bool = False
    pid: int = 0
    url: str = ""
    reason: str = ""


@dataclass(frozen=True, slots=True)
class CapabilityDependencyStatus:
    """One dependency check reported by a sandbox capability provider."""

    name: str
    available: bool
    reason: str = ""


@dataclass(frozen=True, slots=True)
class CapabilityProviderStatus:
    """One sandboxd provider and the public capabilities it contributes."""

    name: str
    state: str
    available: bool
    capabilities: tuple[str, ...] = ()
    backend: str = ""
    reason: str = ""
    dependencies: tuple[CapabilityDependencyStatus, ...] = ()


@dataclass(frozen=True, slots=True)
class CapabilityProviderSummary:
    """Aggregate sandbox capability provider counts."""

    total: int = 0
    available: int = 0
    degraded: int = 0
    unavailable: int = 0


@dataclass(frozen=True, slots=True)
class CapabilityStatus:
    """Public sandbox capability status."""

    ready: bool
    capabilities: tuple[str, ...] = ()
    providers: tuple[CapabilityProviderStatus, ...] = ()
    provider_summary: CapabilityProviderSummary = field(default_factory=CapabilityProviderSummary)
