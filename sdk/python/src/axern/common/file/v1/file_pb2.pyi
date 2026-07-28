from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SandboxFileKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SANDBOX_FILE_KIND_UNSPECIFIED: _ClassVar[SandboxFileKind]
    SANDBOX_FILE_KIND_FILE: _ClassVar[SandboxFileKind]
    SANDBOX_FILE_KIND_DIRECTORY: _ClassVar[SandboxFileKind]
    SANDBOX_FILE_KIND_SYMLINK: _ClassVar[SandboxFileKind]
    SANDBOX_FILE_KIND_OTHER: _ClassVar[SandboxFileKind]

class SandboxArchiveFormat(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SANDBOX_ARCHIVE_FORMAT_UNSPECIFIED: _ClassVar[SandboxArchiveFormat]
    SANDBOX_ARCHIVE_FORMAT_TAR: _ClassVar[SandboxArchiveFormat]

class SandboxArchiveSymlinkPolicy(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SANDBOX_ARCHIVE_SYMLINK_POLICY_UNSPECIFIED: _ClassVar[SandboxArchiveSymlinkPolicy]
    SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT: _ClassVar[SandboxArchiveSymlinkPolicy]
SANDBOX_FILE_KIND_UNSPECIFIED: SandboxFileKind
SANDBOX_FILE_KIND_FILE: SandboxFileKind
SANDBOX_FILE_KIND_DIRECTORY: SandboxFileKind
SANDBOX_FILE_KIND_SYMLINK: SandboxFileKind
SANDBOX_FILE_KIND_OTHER: SandboxFileKind
SANDBOX_ARCHIVE_FORMAT_UNSPECIFIED: SandboxArchiveFormat
SANDBOX_ARCHIVE_FORMAT_TAR: SandboxArchiveFormat
SANDBOX_ARCHIVE_SYMLINK_POLICY_UNSPECIFIED: SandboxArchiveSymlinkPolicy
SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT: SandboxArchiveSymlinkPolicy

class SandboxFileInfo(_message.Message):
    __slots__ = ("path", "kind", "size", "mode", "mtime_ns")
    PATH_FIELD_NUMBER: _ClassVar[int]
    KIND_FIELD_NUMBER: _ClassVar[int]
    SIZE_FIELD_NUMBER: _ClassVar[int]
    MODE_FIELD_NUMBER: _ClassVar[int]
    MTIME_NS_FIELD_NUMBER: _ClassVar[int]
    path: str
    kind: SandboxFileKind
    size: int
    mode: int
    mtime_ns: int
    def __init__(self, path: _Optional[str] = ..., kind: _Optional[_Union[SandboxFileKind, str]] = ..., size: _Optional[int] = ..., mode: _Optional[int] = ..., mtime_ns: _Optional[int] = ...) -> None: ...
