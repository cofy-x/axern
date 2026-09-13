from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EnvironmentMount(_message.Message):
    __slots__ = ("type", "source", "target", "options")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    TARGET_FIELD_NUMBER: _ClassVar[int]
    OPTIONS_FIELD_NUMBER: _ClassVar[int]
    type: str
    source: str
    target: str
    options: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, type: _Optional[str] = ..., source: _Optional[str] = ..., target: _Optional[str] = ..., options: _Optional[_Iterable[str]] = ...) -> None: ...

class EnvironmentTemplateCapabilities(_message.Message):
    __slots__ = ("supports_exec", "supports_exec_stream", "supports_long_lived_process", "supports_ports", "supports_computer_use")
    SUPPORTS_EXEC_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_EXEC_STREAM_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_LONG_LIVED_PROCESS_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_PORTS_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_COMPUTER_USE_FIELD_NUMBER: _ClassVar[int]
    supports_exec: bool
    supports_exec_stream: bool
    supports_long_lived_process: bool
    supports_ports: bool
    supports_computer_use: bool
    def __init__(self, supports_exec: _Optional[bool] = ..., supports_exec_stream: _Optional[bool] = ..., supports_long_lived_process: _Optional[bool] = ..., supports_ports: _Optional[bool] = ..., supports_computer_use: _Optional[bool] = ...) -> None: ...

class OciImageDescriptor(_message.Message):
    __slots__ = ("digest", "media_type", "size_bytes", "annotations")
    class AnnotationsEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    DIGEST_FIELD_NUMBER: _ClassVar[int]
    MEDIA_TYPE_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    ANNOTATIONS_FIELD_NUMBER: _ClassVar[int]
    digest: str
    media_type: str
    size_bytes: int
    annotations: _containers.ScalarMap[str, str]
    def __init__(self, digest: _Optional[str] = ..., media_type: _Optional[str] = ..., size_bytes: _Optional[int] = ..., annotations: _Optional[_Mapping[str, str]] = ...) -> None: ...

class OciBaselinePolicy(_message.Message):
    __slots__ = ("capabilities", "no_file_limit")
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    NO_FILE_LIMIT_FIELD_NUMBER: _ClassVar[int]
    capabilities: _containers.RepeatedScalarFieldContainer[str]
    no_file_limit: int
    def __init__(self, capabilities: _Optional[_Iterable[str]] = ..., no_file_limit: _Optional[int] = ...) -> None: ...

class OciNetworkNamespacePolicy(_message.Message):
    __slots__ = ("annotation_key",)
    ANNOTATION_KEY_FIELD_NUMBER: _ClassVar[int]
    annotation_key: str
    def __init__(self, annotation_key: _Optional[str] = ...) -> None: ...

class OciResourcePolicy(_message.Message):
    __slots__ = ("ignore_annotation_keys",)
    IGNORE_ANNOTATION_KEYS_FIELD_NUMBER: _ClassVar[int]
    ignore_annotation_keys: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, ignore_annotation_keys: _Optional[_Iterable[str]] = ...) -> None: ...

class OciExecutionProfile(_message.Message):
    __slots__ = ("baseline", "network_namespace", "resources")
    BASELINE_FIELD_NUMBER: _ClassVar[int]
    NETWORK_NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    baseline: OciBaselinePolicy
    network_namespace: OciNetworkNamespacePolicy
    resources: OciResourcePolicy
    def __init__(self, baseline: _Optional[_Union[OciBaselinePolicy, _Mapping]] = ..., network_namespace: _Optional[_Union[OciNetworkNamespacePolicy, _Mapping]] = ..., resources: _Optional[_Union[OciResourcePolicy, _Mapping]] = ...) -> None: ...

class EnvironmentTemplate(_message.Message):
    __slots__ = ("id", "rootfs_readonly", "image_default_argv", "default_cwd", "default_env", "mounts", "capabilities", "language", "language_version", "description", "version", "image_descriptor", "warm_policy", "cache_policy", "execution_profile")
    class DefaultEnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    ID_FIELD_NUMBER: _ClassVar[int]
    ROOTFS_READONLY_FIELD_NUMBER: _ClassVar[int]
    IMAGE_DEFAULT_ARGV_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_CWD_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_ENV_FIELD_NUMBER: _ClassVar[int]
    MOUNTS_FIELD_NUMBER: _ClassVar[int]
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_VERSION_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    IMAGE_DESCRIPTOR_FIELD_NUMBER: _ClassVar[int]
    WARM_POLICY_FIELD_NUMBER: _ClassVar[int]
    CACHE_POLICY_FIELD_NUMBER: _ClassVar[int]
    EXECUTION_PROFILE_FIELD_NUMBER: _ClassVar[int]
    id: str
    rootfs_readonly: bool
    image_default_argv: _containers.RepeatedScalarFieldContainer[str]
    default_cwd: str
    default_env: _containers.ScalarMap[str, str]
    mounts: _containers.RepeatedCompositeFieldContainer[EnvironmentMount]
    capabilities: EnvironmentTemplateCapabilities
    language: str
    language_version: str
    description: str
    version: str
    image_descriptor: OciImageDescriptor
    warm_policy: str
    cache_policy: str
    execution_profile: OciExecutionProfile
    def __init__(self, id: _Optional[str] = ..., rootfs_readonly: _Optional[bool] = ..., image_default_argv: _Optional[_Iterable[str]] = ..., default_cwd: _Optional[str] = ..., default_env: _Optional[_Mapping[str, str]] = ..., mounts: _Optional[_Iterable[_Union[EnvironmentMount, _Mapping]]] = ..., capabilities: _Optional[_Union[EnvironmentTemplateCapabilities, _Mapping]] = ..., language: _Optional[str] = ..., language_version: _Optional[str] = ..., description: _Optional[str] = ..., version: _Optional[str] = ..., image_descriptor: _Optional[_Union[OciImageDescriptor, _Mapping]] = ..., warm_policy: _Optional[str] = ..., cache_policy: _Optional[str] = ..., execution_profile: _Optional[_Union[OciExecutionProfile, _Mapping]] = ...) -> None: ...

class ListEnvironmentTemplatesRequest(_message.Message):
    __slots__ = ("namespace", "version", "language")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    version: str
    language: str
    def __init__(self, namespace: _Optional[str] = ..., version: _Optional[str] = ..., language: _Optional[str] = ...) -> None: ...

class ListEnvironmentTemplatesResponse(_message.Message):
    __slots__ = ("environment_templates",)
    ENVIRONMENT_TEMPLATES_FIELD_NUMBER: _ClassVar[int]
    environment_templates: _containers.RepeatedCompositeFieldContainer[EnvironmentTemplate]
    def __init__(self, environment_templates: _Optional[_Iterable[_Union[EnvironmentTemplate, _Mapping]]] = ...) -> None: ...

class GetEnvironmentTemplateRequest(_message.Message):
    __slots__ = ("id", "version")
    ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    id: str
    version: str
    def __init__(self, id: _Optional[str] = ..., version: _Optional[str] = ...) -> None: ...

class GetEnvironmentTemplateResponse(_message.Message):
    __slots__ = ("environment_template",)
    ENVIRONMENT_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    environment_template: EnvironmentTemplate
    def __init__(self, environment_template: _Optional[_Union[EnvironmentTemplate, _Mapping]] = ...) -> None: ...
