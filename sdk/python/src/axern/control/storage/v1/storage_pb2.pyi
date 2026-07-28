from axern.control.storage.v1 import storage_types_pb2 as _storage_types_pb2
from google.protobuf import field_mask_pb2 as _field_mask_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class CreateVolumeClassRequest(_message.Message):
    __slots__ = ("name", "backend", "access_modes", "default_reclaim_policy", "consistency_profile", "runtime_compatibility", "parameters")
    class ParametersEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAME_FIELD_NUMBER: _ClassVar[int]
    BACKEND_FIELD_NUMBER: _ClassVar[int]
    ACCESS_MODES_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_RECLAIM_POLICY_FIELD_NUMBER: _ClassVar[int]
    CONSISTENCY_PROFILE_FIELD_NUMBER: _ClassVar[int]
    RUNTIME_COMPATIBILITY_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_FIELD_NUMBER: _ClassVar[int]
    name: str
    backend: _storage_types_pb2.VolumeBackend
    access_modes: _containers.RepeatedScalarFieldContainer[_storage_types_pb2.VolumeAccessMode]
    default_reclaim_policy: _storage_types_pb2.VolumeReclaimPolicy
    consistency_profile: _storage_types_pb2.VolumeConsistencyProfile
    runtime_compatibility: _storage_types_pb2.VolumeRuntimeCompatibility
    parameters: _containers.ScalarMap[str, str]
    def __init__(self, name: _Optional[str] = ..., backend: _Optional[_Union[_storage_types_pb2.VolumeBackend, str]] = ..., access_modes: _Optional[_Iterable[_Union[_storage_types_pb2.VolumeAccessMode, str]]] = ..., default_reclaim_policy: _Optional[_Union[_storage_types_pb2.VolumeReclaimPolicy, str]] = ..., consistency_profile: _Optional[_Union[_storage_types_pb2.VolumeConsistencyProfile, str]] = ..., runtime_compatibility: _Optional[_Union[_storage_types_pb2.VolumeRuntimeCompatibility, _Mapping]] = ..., parameters: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateVolumeClassResponse(_message.Message):
    __slots__ = ("volume_class",)
    VOLUME_CLASS_FIELD_NUMBER: _ClassVar[int]
    volume_class: _storage_types_pb2.VolumeClass
    def __init__(self, volume_class: _Optional[_Union[_storage_types_pb2.VolumeClass, _Mapping]] = ...) -> None: ...

class GetVolumeClassRequest(_message.Message):
    __slots__ = ("name",)
    NAME_FIELD_NUMBER: _ClassVar[int]
    name: str
    def __init__(self, name: _Optional[str] = ...) -> None: ...

class GetVolumeClassResponse(_message.Message):
    __slots__ = ("volume_class",)
    VOLUME_CLASS_FIELD_NUMBER: _ClassVar[int]
    volume_class: _storage_types_pb2.VolumeClass
    def __init__(self, volume_class: _Optional[_Union[_storage_types_pb2.VolumeClass, _Mapping]] = ...) -> None: ...

class ListVolumeClassesRequest(_message.Message):
    __slots__ = ("cursor", "page_size")
    CURSOR_FIELD_NUMBER: _ClassVar[int]
    PAGE_SIZE_FIELD_NUMBER: _ClassVar[int]
    cursor: str
    page_size: int
    def __init__(self, cursor: _Optional[str] = ..., page_size: _Optional[int] = ...) -> None: ...

class ListVolumeClassesResponse(_message.Message):
    __slots__ = ("volume_classes", "next_cursor")
    VOLUME_CLASSES_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    volume_classes: _containers.RepeatedCompositeFieldContainer[_storage_types_pb2.VolumeClass]
    next_cursor: str
    def __init__(self, volume_classes: _Optional[_Iterable[_Union[_storage_types_pb2.VolumeClass, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class CreateVolumeClaimRequest(_message.Message):
    __slots__ = ("namespace", "name", "class_name", "requested_capacity", "access_mode", "reclaim_policy", "binding_scope", "parameters", "labels")
    class ParametersEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    CLASS_NAME_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_CAPACITY_FIELD_NUMBER: _ClassVar[int]
    ACCESS_MODE_FIELD_NUMBER: _ClassVar[int]
    RECLAIM_POLICY_FIELD_NUMBER: _ClassVar[int]
    BINDING_SCOPE_FIELD_NUMBER: _ClassVar[int]
    PARAMETERS_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    class_name: str
    requested_capacity: _storage_types_pb2.VolumeCapacity
    access_mode: _storage_types_pb2.VolumeAccessMode
    reclaim_policy: _storage_types_pb2.VolumeReclaimPolicy
    binding_scope: _storage_types_pb2.VolumeBindingScope
    parameters: _containers.ScalarMap[str, str]
    labels: _containers.ScalarMap[str, str]
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., class_name: _Optional[str] = ..., requested_capacity: _Optional[_Union[_storage_types_pb2.VolumeCapacity, _Mapping]] = ..., access_mode: _Optional[_Union[_storage_types_pb2.VolumeAccessMode, str]] = ..., reclaim_policy: _Optional[_Union[_storage_types_pb2.VolumeReclaimPolicy, str]] = ..., binding_scope: _Optional[_Union[_storage_types_pb2.VolumeBindingScope, str]] = ..., parameters: _Optional[_Mapping[str, str]] = ..., labels: _Optional[_Mapping[str, str]] = ...) -> None: ...

class CreateVolumeClaimResponse(_message.Message):
    __slots__ = ("volume_claim",)
    VOLUME_CLAIM_FIELD_NUMBER: _ClassVar[int]
    volume_claim: _storage_types_pb2.VolumeClaim
    def __init__(self, volume_claim: _Optional[_Union[_storage_types_pb2.VolumeClaim, _Mapping]] = ...) -> None: ...

class GetVolumeClaimRequest(_message.Message):
    __slots__ = ("namespace", "name")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class GetVolumeClaimResponse(_message.Message):
    __slots__ = ("volume_claim",)
    VOLUME_CLAIM_FIELD_NUMBER: _ClassVar[int]
    volume_claim: _storage_types_pb2.VolumeClaim
    def __init__(self, volume_claim: _Optional[_Union[_storage_types_pb2.VolumeClaim, _Mapping]] = ...) -> None: ...

class ListVolumeClaimsRequest(_message.Message):
    __slots__ = ("filter",)
    FILTER_FIELD_NUMBER: _ClassVar[int]
    filter: _storage_types_pb2.VolumeClaimListFilter
    def __init__(self, filter: _Optional[_Union[_storage_types_pb2.VolumeClaimListFilter, _Mapping]] = ...) -> None: ...

class ListVolumeClaimsResponse(_message.Message):
    __slots__ = ("volume_claims", "next_cursor")
    VOLUME_CLAIMS_FIELD_NUMBER: _ClassVar[int]
    NEXT_CURSOR_FIELD_NUMBER: _ClassVar[int]
    volume_claims: _containers.RepeatedCompositeFieldContainer[_storage_types_pb2.VolumeClaim]
    next_cursor: str
    def __init__(self, volume_claims: _Optional[_Iterable[_Union[_storage_types_pb2.VolumeClaim, _Mapping]]] = ..., next_cursor: _Optional[str] = ...) -> None: ...

class UpdateVolumeClaimRequest(_message.Message):
    __slots__ = ("namespace", "name", "expected_version", "requested_capacity", "labels", "update_mask")
    class LabelsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_VERSION_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_CAPACITY_FIELD_NUMBER: _ClassVar[int]
    LABELS_FIELD_NUMBER: _ClassVar[int]
    UPDATE_MASK_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    expected_version: int
    requested_capacity: _storage_types_pb2.VolumeCapacity
    labels: _containers.ScalarMap[str, str]
    update_mask: _field_mask_pb2.FieldMask
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ..., expected_version: _Optional[int] = ..., requested_capacity: _Optional[_Union[_storage_types_pb2.VolumeCapacity, _Mapping]] = ..., labels: _Optional[_Mapping[str, str]] = ..., update_mask: _Optional[_Union[_field_mask_pb2.FieldMask, _Mapping]] = ...) -> None: ...

class UpdateVolumeClaimResponse(_message.Message):
    __slots__ = ("volume_claim",)
    VOLUME_CLAIM_FIELD_NUMBER: _ClassVar[int]
    volume_claim: _storage_types_pb2.VolumeClaim
    def __init__(self, volume_claim: _Optional[_Union[_storage_types_pb2.VolumeClaim, _Mapping]] = ...) -> None: ...

class DeleteVolumeClaimRequest(_message.Message):
    __slots__ = ("namespace", "name")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    name: str
    def __init__(self, namespace: _Optional[str] = ..., name: _Optional[str] = ...) -> None: ...

class DeleteVolumeClaimResponse(_message.Message):
    __slots__ = ("volume_claim",)
    VOLUME_CLAIM_FIELD_NUMBER: _ClassVar[int]
    volume_claim: _storage_types_pb2.VolumeClaim
    def __init__(self, volume_claim: _Optional[_Union[_storage_types_pb2.VolumeClaim, _Mapping]] = ...) -> None: ...
