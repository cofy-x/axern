"""Strict loader for ``axern/v1`` Function resource specs."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any, Mapping

import yaml

from axern_sdk._internal.resources import cpu_milli, memory_bytes
from axern_sdk.function.models import FunctionResources, FunctionScaling, FunctionSource, FunctionSpec, FunctionWorkerSource
from axern_sdk.models import ImageMount, SecretEnvVar, SecretFile, VolumeMount

_NAME_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$")
_PYTHON_REF_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+$")


def load_function_spec(path: str | Path) -> FunctionSpec:
    """Load and validate an ``axern/v1`` Function YAML or JSON spec."""

    spec_path = Path(path).expanduser().resolve()
    if not spec_path.is_file():
        raise ValueError(f"function resource spec does not exist: {spec_path}")
    try:
        raw = json.loads(spec_path.read_text(encoding="utf-8")) if spec_path.suffix.lower() == ".json" else yaml.safe_load(spec_path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, yaml.YAMLError) as exc:
        raise ValueError(f"invalid function resource spec: {exc}") from exc

    envelope = _object("resource", raw)
    _reject_unknown("resource", envelope, {"api_version", "kind", "metadata", "spec"})
    if envelope.get("api_version") != "axern/v1":
        raise ValueError("api_version must be 'axern/v1'")
    if envelope.get("kind") != "Function":
        raise ValueError("kind must be 'Function'")

    metadata = _object("metadata", envelope.get("metadata"))
    _reject_unknown("metadata", metadata, {"name", "namespace", "labels"})
    name = _required_string(metadata, "name", "metadata")
    if not _NAME_RE.fullmatch(name):
        raise ValueError("metadata.name contains unsupported characters")
    namespace = _optional_string(metadata, "namespace", "metadata") or "default"
    labels = _string_map("metadata.labels", metadata.get("labels"))

    spec = _object("spec", envelope.get("spec"))
    _reject_unknown(spec=spec)
    source_environment = _object("spec.source", spec.get("source"))
    _reject_unknown("spec.source", source_environment, {"environment", "template", "template_version", "image", "registry_credential_id", "rootfs_readonly"})
    if sum(bool(_optional_string(source_environment, key, "spec.source")) for key in ("environment", "template", "image")) != 1:
        raise ValueError("spec.source must select exactly one of environment, template, or image")
    worker_source = FunctionWorkerSource(
        environment_id=_optional_string(source_environment, "environment", "spec.source"),
        template=_optional_string(source_environment, "template", "spec.source"),
        template_version=_optional_string(source_environment, "template_version", "spec.source"),
        image=_optional_string(source_environment, "image", "spec.source"),
        registry_credential_id=_optional_string(source_environment, "registry_credential_id", "spec.source"),
        rootfs_readonly=source_environment.get("rootfs_readonly", False),
    )
    if not isinstance(worker_source.rootfs_readonly, bool):
        raise ValueError("spec.source.rootfs_readonly must be a boolean")
    if not worker_source.template and worker_source.template_version:
        raise ValueError("spec.source.template_version requires template")
    if not worker_source.image and (worker_source.registry_credential_id or worker_source.rootfs_readonly):
        raise ValueError("spec.source registry_credential_id and rootfs_readonly require image")

    function = _object("spec.function", spec.get("function"))
    _reject_unknown("spec.function", function, {"runtime", "handler", "initializer", "source", "timeout_seconds", "scaling"})
    runtime = _required_string(function, "runtime", "spec.function")
    handler = _required_string(function, "handler", "spec.function")
    _validate_python_ref("spec.function.handler", handler)
    initializer = _optional_string(function, "initializer", "spec.function")
    if initializer:
        _validate_python_ref("spec.function.initializer", initializer)

    source_value = _required_string(function, "source", "spec.function")
    source_path = Path(source_value)
    if source_path.is_absolute() or ".." in source_path.parts:
        raise ValueError("spec.function.source must be a relative path below the spec directory")
    source_dir = (spec_path.parent / source_path).resolve()
    if spec_path.parent == source_dir or spec_path.parent not in source_dir.parents:
        raise ValueError("spec.function.source must stay below the spec directory")
    if not source_dir.is_dir():
        raise ValueError(f"spec.function.source is not a directory: {source_value}")

    timeout_seconds = _integer(function, "timeout_seconds", 60, "spec.function")
    if timeout_seconds <= 0:
        raise ValueError("spec.function.timeout_seconds must be greater than 0")

    resources = _resources(spec.get("resources"))
    scaling = _scaling(function.get("scaling"))
    env = _string_map("spec.env", spec.get("env"))
    secret_env = _secret_env(spec.get("secret_env"))
    secret_files = _secret_files(spec.get("secret_files"))
    volumes = _volumes(spec.get("volumes"))
    image_mounts = _image_mounts(spec.get("image_mounts"))

    return FunctionSpec(
        name=name,
        runtime=runtime,
        handler=handler,
        namespace=namespace,
        labels=labels,
        worker_source=worker_source,
        initializer=initializer,
        source=FunctionSource(root=source_dir.relative_to(spec_path.parent).as_posix()),
        timeout_seconds=timeout_seconds,
        resources=resources,
        scaling=scaling,
        env=env,
        secret_env=secret_env,
        secret_files=secret_files,
        volumes=volumes,
        image_mounts=image_mounts,
        root_dir=spec_path.parent,
        manifest_path=spec_path,
    )


def _reject_unknown(label: str = "spec", data: Mapping[str, Any] | None = None, allowed: set[str] | None = None, *, spec: Mapping[str, Any] | None = None) -> None:
    if spec is not None:
        data = spec
        allowed = {"source", "command", "runtime_class", "resources", "function", "env", "secret_env", "secret_files", "volumes", "image_mounts"}
    assert data is not None and allowed is not None
    unknown = sorted(set(data) - allowed)
    if unknown:
        raise ValueError(f"{label} contains unsupported field(s): {', '.join(unknown)}")


def _resources(value: Any) -> FunctionResources:
    if value is None:
        return FunctionResources()
    data = _object("spec.resources", value)
    _reject_unknown("spec.resources", data, {"requests", "limits"})
    requests = _quantity("spec.resources.requests", data.get("requests"))
    limits = _quantity("spec.resources.limits", data.get("limits"))
    result = FunctionResources(
        request_cpu=requests.get("cpu", ""),
        request_memory=requests.get("memory", ""),
        request_ephemeral_storage=requests.get("ephemeral_storage", ""),
        limit_cpu=limits.get("cpu", ""),
        limit_memory=limits.get("memory", ""),
        limit_ephemeral_storage=limits.get("ephemeral_storage", ""),
    )
    if result.request_cpu:
        cpu_milli("spec.resources.requests.cpu", result.request_cpu)
    if result.request_memory:
        memory_bytes("spec.resources.requests.memory", result.request_memory)
    if result.request_ephemeral_storage:
        memory_bytes("spec.resources.requests.ephemeral_storage", result.request_ephemeral_storage)
    if result.limit_cpu:
        cpu_milli("spec.resources.limits.cpu", result.limit_cpu)
    if result.limit_memory:
        memory_bytes("spec.resources.limits.memory", result.limit_memory)
    if result.limit_ephemeral_storage:
        memory_bytes("spec.resources.limits.ephemeral_storage", result.limit_ephemeral_storage)
    return result


def _quantity(label: str, value: Any) -> Mapping[str, str]:
    if value is None:
        return {}
    data = _object(label, value)
    _reject_unknown(label, data, {"cpu", "memory", "ephemeral_storage"})
    return {key: _optional_string(data, key, label) for key in ("cpu", "memory", "ephemeral_storage")}


def _scaling(value: Any) -> FunctionScaling:
    if value is None:
        return FunctionScaling()
    data = _object("spec.function.scaling", value)
    _reject_unknown("spec.function.scaling", data, {"min_replicas", "max_replicas", "concurrency", "idle_timeout"})
    idle = _optional_string(data, "idle_timeout", "spec.function.scaling")
    idle_seconds = _duration_seconds("spec.function.scaling.idle_timeout", idle) if idle else 300
    result = FunctionScaling(
        min_replicas=_integer(data, "min_replicas", 0, "spec.function.scaling"),
        max_replicas=_integer(data, "max_replicas", 1, "spec.function.scaling"),
        concurrency=_integer(data, "concurrency", 1, "spec.function.scaling"),
        idle_seconds=idle_seconds,
    )
    if result.min_replicas < 0 or result.max_replicas < result.min_replicas or result.concurrency <= 0:
        raise ValueError("spec.function.scaling values are invalid")
    return result


def _duration_seconds(label: str, value: str) -> int:
    match = re.fullmatch(r"([0-9]+)(s|m|h)", value)
    if not match:
        raise ValueError(f"{label} must use s, m, or h units")
    multiplier = {"s": 1, "m": 60, "h": 3600}[match.group(2)]
    return int(match.group(1)) * multiplier


def _volumes(value: Any) -> tuple[VolumeMount, ...]:
    if value is None:
        return ()
    if not isinstance(value, list):
        raise ValueError("spec.volumes must be a list")
    result: list[VolumeMount] = []
    names: set[str] = set()
    targets: set[str] = set()
    for index, item in enumerate(value):
        label = f"spec.volumes[{index}]"
        data = _object(label, item)
        _reject_unknown(label, data, {"name", "target", "readonly", "options"})
        name = _required_string(data, "name", label)
        target = _required_string(data, "target", label)
        if name in names or target in targets or not target.startswith("/") or target == "/" or ".." in Path(target).parts:
            raise ValueError(f"{label} has an invalid or duplicate name/target")
        readonly = data.get("readonly", False)
        options = data.get("options", [])
        if not isinstance(readonly, bool) or not isinstance(options, list) or not all(isinstance(item, str) for item in options):
            raise ValueError(f"{label} readonly/options are invalid")
        names.add(name)
        targets.add(target)
        result.append(VolumeMount(name=name, target=target, readonly=readonly, options=tuple(options)))
    return tuple(result)


def _secret_env(value: Any) -> tuple[SecretEnvVar, ...]:
    if value is None:
        return ()
    if not isinstance(value, list):
        raise ValueError("spec.secret_env must be a list")
    result: list[SecretEnvVar] = []
    names: set[str] = set()
    for index, item in enumerate(value):
        label = f"spec.secret_env[{index}]"
        data = _object(label, item)
        _reject_unknown(label, data, {"name", "secret_id", "key", "optional"})
        name = _required_string(data, "name", label)
        optional = data.get("optional", False)
        if name in names or not isinstance(optional, bool):
            raise ValueError(f"{label} has an invalid or duplicate name")
        names.add(name)
        result.append(SecretEnvVar(name, _required_string(data, "secret_id", label), _required_string(data, "key", label), optional))
    return tuple(result)


def _secret_files(value: Any) -> tuple[SecretFile, ...]:
    if value is None:
        return ()
    if not isinstance(value, list):
        raise ValueError("spec.secret_files must be a list")
    result: list[SecretFile] = []
    paths: set[str] = set()
    for index, item in enumerate(value):
        label = f"spec.secret_files[{index}]"
        data = _object(label, item)
        _reject_unknown(label, data, {"path", "secret_id", "key", "mode", "optional"})
        path = _required_string(data, "path", label)
        optional = data.get("optional", False)
        mode_text = _optional_string(data, "mode", label)
        try:
            mode = int(mode_text, 8) if mode_text else 0
        except ValueError as exc:
            raise ValueError(f"{label}.mode must be an octal permission string") from exc
        if not _valid_target(path) or path in paths or not isinstance(optional, bool) or mode > 0o777:
            raise ValueError(f"{label} has invalid path, mode, or optional value")
        paths.add(path)
        result.append(SecretFile(path, _required_string(data, "secret_id", label), _required_string(data, "key", label), mode, optional))
    return tuple(result)


def _image_mounts(value: Any) -> tuple[ImageMount, ...]:
    if value is None:
        return ()
    if not isinstance(value, list):
        raise ValueError("spec.image_mounts must be a list")
    result: list[ImageMount] = []
    targets: set[str] = set()
    for index, item in enumerate(value):
        label = f"spec.image_mounts[{index}]"
        data = _object(label, item)
        _reject_unknown(label, data, {"image", "target"})
        image = _required_string(data, "image", label)
        target = _required_string(data, "target", label)
        if not _valid_target(target) or target in targets:
            raise ValueError(f"{label} has an invalid or duplicate target")
        targets.add(target)
        result.append(ImageMount(image, target))
    return tuple(result)


def _valid_target(value: str) -> bool:
    path = Path(value)
    return path.is_absolute() and value != "/" and ".." not in path.parts


def _object(label: str, value: Any) -> Mapping[str, Any]:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        raise ValueError(f"{label} must be an object")
    return value


def _required_string(data: Mapping[str, Any], key: str, label: str) -> str:
    value = _optional_string(data, key, label)
    if not value:
        raise ValueError(f"{label}.{key} is required")
    return value


def _optional_string(data: Mapping[str, Any], key: str, label: str) -> str:
    value = data.get(key, "")
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError(f"{label}.{key} must be a string")
    return value.strip()


def _integer(data: Mapping[str, Any], key: str, default: int, label: str) -> int:
    value = data.get(key, default)
    if not isinstance(value, int) or isinstance(value, bool):
        raise ValueError(f"{label}.{key} must be an integer")
    return value


def _string_map(label: str, value: Any) -> dict[str, str]:
    if value is None:
        return {}
    data = _object(label, value)
    if not all(key.strip() and isinstance(item, str) for key, item in data.items()):
        raise ValueError(f"{label} must map non-empty names to strings")
    return dict(data)


def _validate_python_ref(label: str, value: str) -> None:
    if not _PYTHON_REF_RE.fullmatch(value):
        raise ValueError(f"{label} must be a Python module.callable reference")
