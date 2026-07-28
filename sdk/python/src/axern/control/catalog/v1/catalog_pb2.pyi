from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RuntimeMount(_message.Message):
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

class RuntimeTemplateCapabilities(_message.Message):
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

class RuntimeBaselinePolicy(_message.Message):
    __slots__ = ("capabilities", "no_file_limit")
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    NO_FILE_LIMIT_FIELD_NUMBER: _ClassVar[int]
    capabilities: _containers.RepeatedScalarFieldContainer[str]
    no_file_limit: int
    def __init__(self, capabilities: _Optional[_Iterable[str]] = ..., no_file_limit: _Optional[int] = ...) -> None: ...

class RuntimeCapabilityPolicy(_message.Message):
    __slots__ = ("annotation_key", "include_ambient")
    ANNOTATION_KEY_FIELD_NUMBER: _ClassVar[int]
    INCLUDE_AMBIENT_FIELD_NUMBER: _ClassVar[int]
    annotation_key: str
    include_ambient: bool
    def __init__(self, annotation_key: _Optional[str] = ..., include_ambient: _Optional[bool] = ...) -> None: ...

class RuntimeNetworkNamespacePolicy(_message.Message):
    __slots__ = ("annotation_key",)
    ANNOTATION_KEY_FIELD_NUMBER: _ClassVar[int]
    annotation_key: str
    def __init__(self, annotation_key: _Optional[str] = ...) -> None: ...

class RuntimeResourcePolicy(_message.Message):
    __slots__ = ("ignore_annotation_keys",)
    IGNORE_ANNOTATION_KEYS_FIELD_NUMBER: _ClassVar[int]
    ignore_annotation_keys: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, ignore_annotation_keys: _Optional[_Iterable[str]] = ...) -> None: ...

class RuntimeExecutionProfile(_message.Message):
    __slots__ = ("runtime_baseline", "capabilities", "network_namespace", "resources")
    RUNTIME_BASELINE_FIELD_NUMBER: _ClassVar[int]
    CAPABILITIES_FIELD_NUMBER: _ClassVar[int]
    NETWORK_NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    runtime_baseline: RuntimeBaselinePolicy
    capabilities: RuntimeCapabilityPolicy
    network_namespace: RuntimeNetworkNamespacePolicy
    resources: RuntimeResourcePolicy
    def __init__(self, runtime_baseline: _Optional[_Union[RuntimeBaselinePolicy, _Mapping]] = ..., capabilities: _Optional[_Union[RuntimeCapabilityPolicy, _Mapping]] = ..., network_namespace: _Optional[_Union[RuntimeNetworkNamespacePolicy, _Mapping]] = ..., resources: _Optional[_Union[RuntimeResourcePolicy, _Mapping]] = ...) -> None: ...

class RuntimeTemplate(_message.Message):
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
    mounts: _containers.RepeatedCompositeFieldContainer[RuntimeMount]
    capabilities: RuntimeTemplateCapabilities
    language: str
    language_version: str
    description: str
    version: str
    image_descriptor: OciImageDescriptor
    warm_policy: str
    cache_policy: str
    execution_profile: RuntimeExecutionProfile
    def __init__(self, id: _Optional[str] = ..., rootfs_readonly: _Optional[bool] = ..., image_default_argv: _Optional[_Iterable[str]] = ..., default_cwd: _Optional[str] = ..., default_env: _Optional[_Mapping[str, str]] = ..., mounts: _Optional[_Iterable[_Union[RuntimeMount, _Mapping]]] = ..., capabilities: _Optional[_Union[RuntimeTemplateCapabilities, _Mapping]] = ..., language: _Optional[str] = ..., language_version: _Optional[str] = ..., description: _Optional[str] = ..., version: _Optional[str] = ..., image_descriptor: _Optional[_Union[OciImageDescriptor, _Mapping]] = ..., warm_policy: _Optional[str] = ..., cache_policy: _Optional[str] = ..., execution_profile: _Optional[_Union[RuntimeExecutionProfile, _Mapping]] = ...) -> None: ...

class ListRuntimeTemplatesRequest(_message.Message):
    __slots__ = ("namespace", "version", "language")
    NAMESPACE_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    LANGUAGE_FIELD_NUMBER: _ClassVar[int]
    namespace: str
    version: str
    language: str
    def __init__(self, namespace: _Optional[str] = ..., version: _Optional[str] = ..., language: _Optional[str] = ...) -> None: ...

class ListRuntimeTemplatesResponse(_message.Message):
    __slots__ = ("runtime_templates",)
    RUNTIME_TEMPLATES_FIELD_NUMBER: _ClassVar[int]
    runtime_templates: _containers.RepeatedCompositeFieldContainer[RuntimeTemplate]
    def __init__(self, runtime_templates: _Optional[_Iterable[_Union[RuntimeTemplate, _Mapping]]] = ...) -> None: ...

class GetRuntimeTemplateRequest(_message.Message):
    __slots__ = ("id", "version")
    ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    id: str
    version: str
    def __init__(self, id: _Optional[str] = ..., version: _Optional[str] = ...) -> None: ...

class GetRuntimeTemplateResponse(_message.Message):
    __slots__ = ("runtime_template",)
    RUNTIME_TEMPLATE_FIELD_NUMBER: _ClassVar[int]
    runtime_template: RuntimeTemplate
    def __init__(self, runtime_template: _Optional[_Union[RuntimeTemplate, _Mapping]] = ...) -> None: ...

class AgentBundle(_message.Message):
    __slots__ = ("id", "version", "image_descriptor", "binary_path", "description")
    ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    IMAGE_DESCRIPTOR_FIELD_NUMBER: _ClassVar[int]
    BINARY_PATH_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    id: str
    version: str
    image_descriptor: OciImageDescriptor
    binary_path: str
    description: str
    def __init__(self, id: _Optional[str] = ..., version: _Optional[str] = ..., image_descriptor: _Optional[_Union[OciImageDescriptor, _Mapping]] = ..., binary_path: _Optional[str] = ..., description: _Optional[str] = ...) -> None: ...

class ListAgentBundlesRequest(_message.Message):
    __slots__ = ("version",)
    VERSION_FIELD_NUMBER: _ClassVar[int]
    version: str
    def __init__(self, version: _Optional[str] = ...) -> None: ...

class ListAgentBundlesResponse(_message.Message):
    __slots__ = ("agent_bundles",)
    AGENT_BUNDLES_FIELD_NUMBER: _ClassVar[int]
    agent_bundles: _containers.RepeatedCompositeFieldContainer[AgentBundle]
    def __init__(self, agent_bundles: _Optional[_Iterable[_Union[AgentBundle, _Mapping]]] = ...) -> None: ...

class GetAgentBundleRequest(_message.Message):
    __slots__ = ("id", "version")
    ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    id: str
    version: str
    def __init__(self, id: _Optional[str] = ..., version: _Optional[str] = ...) -> None: ...

class GetAgentBundleResponse(_message.Message):
    __slots__ = ("agent_bundle",)
    AGENT_BUNDLE_FIELD_NUMBER: _ClassVar[int]
    agent_bundle: AgentBundle
    def __init__(self, agent_bundle: _Optional[_Union[AgentBundle, _Mapping]] = ...) -> None: ...
