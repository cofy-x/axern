import datetime

from google.protobuf import timestamp_pb2 as _timestamp_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class WorkspaceStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    WORKSPACE_STATUS_UNSPECIFIED: _ClassVar[WorkspaceStatus]
    WORKSPACE_STATUS_PROVISIONING: _ClassVar[WorkspaceStatus]
    WORKSPACE_STATUS_READY: _ClassVar[WorkspaceStatus]
    WORKSPACE_STATUS_UNAVAILABLE: _ClassVar[WorkspaceStatus]
    WORKSPACE_STATUS_DELETING: _ClassVar[WorkspaceStatus]
    WORKSPACE_STATUS_DELETED: _ClassVar[WorkspaceStatus]
WORKSPACE_STATUS_UNSPECIFIED: WorkspaceStatus
WORKSPACE_STATUS_PROVISIONING: WorkspaceStatus
WORKSPACE_STATUS_READY: WorkspaceStatus
WORKSPACE_STATUS_UNAVAILABLE: WorkspaceStatus
WORKSPACE_STATUS_DELETING: WorkspaceStatus
WORKSPACE_STATUS_DELETED: WorkspaceStatus

class Workspace(_message.Message):
    __slots__ = ("id", "namespace", "node_id", "status", "capacity_bytes", "inode_limit", "generation", "attached_allocation_id", "used_bytes", "used_inodes", "message", "labels", "created_at", "updated_at")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    CAPACITY_BYTES_FIELD_NUMBER: _ClassVar[int]
    INODE_LIMIT_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    ATTACHED_ALLOCATION_ID_FIELD_NUMBER: _ClassVar[int]
    USED_BYTES_FIELD_NUMBER: _ClassVar[int]
    USED_INODES_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    CREATED_AT_FIELD_NUMBER: _ClassVar[int]
    UPDATED_AT_FIELD_NUMBER: _ClassVar[int]
    id: str
    namespace: str
    node_id: str
    status: WorkspaceStatus
    capacity_bytes: int
    inode_limit: int
    generation: int
    attached_allocation_id: str
    used_bytes: int
    used_inodes: int
    message: str
    labels: _containers.ScalarMap[str, str]
    created_at: _timestamp_pb2.Timestamp
    updated_at: _timestamp_pb2.Timestamp
    def __init__(self, id: _Optional[str] = ..., namespace: _Optional[str] = ..., node_id: _Optional[str] = ..., status: _Optional[_Union[WorkspaceStatus, str]] = ..., capacity_bytes: _Optional[int] = ..., inode_limit: _Optional[int] = ..., generation: _Optional[int] = ..., attached_allocation_id: _Optional[str] = ..., used_bytes: _Optional[int] = ..., used_inodes: _Optional[int] = ..., message: _Optional[str] = ..., labels: _Optional[_Mapping[str, str]] = ..., created_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ..., updated_at: _Optional[_Union[datetime.datetime, _timestamp_pb2.Timestamp, _Mapping]] = ...) -> None: ...

class WorkspaceAttachment(_message.Message):
    __slots__ = ("workspace_id", "node_id", "generation", "target")
    WORKSPACE_ID_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    GENERATION_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    workspace_id: str
    node_id: str
    generation: int
    target: str
    def __init__(self, workspace_id: _Optional[str] = ..., node_id: _Optional[str] = ..., generation: _Optional[int] = ..., target: _Optional[str] = ...) -> None: ...

class CreateWorkspaceRequest(_message.Message):
    __slots__ = ("namespace", "node_id", "capacity_bytes", "inode_limit", "labels")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    CAPACITY_BYTES_FIELD_NUMBER: _ClassVar[int]
    INODE_LIMIT_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    node_id: str
    capacity_bytes: int
    inode_limit: int
    labels: _containers.ScalarMap[str, str]
    def __init__(self, namespace: _Optional[str] = ..., node_id: _Optional[str] = ..., capacity_bytes: _Optional[int] = ..., inode_limit: _Optional[int] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateWorkspaceResponse(_message.Message):
    __slots__ = ("workspace",)
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ...) -> None: ...

class GetWorkspaceRequest(_message.Message):
    __slots__ = ("workspace_id",)
    WORKSPACE_ID_FIELD_NUMBER: _ClassVar[int]
    workspace_id: str
    def __init__(self, workspace_id: _Optional[str] = ...) -> None: ...

class GetWorkspaceResponse(_message.Message):
    __slots__ = ("workspace",)
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ...) -> None: ...

class ListWorkspacesRequest(_message.Message):
    __slots__ = ("namespace", "cursor", "page_size")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    cursor: str
    page_size: int
    def __init__(self, namespace: _Optional[str] = ..., cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class ListWorkspacesResponse(_message.Message):
    __slots__ = ("workspaces", "next_cursor")
    WORKSPACES_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    workspaces: _containers.RepeatedCompositeFieldContainer[Workspace]
    next_cursor: str
    def __init__(self, workspaces: _Optional[_Iterable[_Union[Workspace, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class DeleteWorkspaceRequest(_message.Message):
    __slots__ = ("workspace_id",)
    WORKSPACE_ID_FIELD_NUMBER: _ClassVar[int]
    workspace_id: str
    def __init__(self, workspace_id: _Optional[str] = ...) -> None: ...

class DeleteWorkspaceResponse(_message.Message):
    __slots__ = ("workspace",)
    WORKSPACE_FIELD_NUMBER: _ClassVar[int]
    workspace: Workspace
    def __init__(self, workspace: _Optional[_Union[Workspace, _Mapping]] = ...) -> None: ...
