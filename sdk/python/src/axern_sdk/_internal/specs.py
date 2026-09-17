"""Shared request spec builders for SDK clients."""

from __future__ import annotations

from collections.abc import Iterable

from axern.control.common.v1 import common_pb2
from axern.control.environment.v1 import environment_pb2
from axern_sdk.models import ImageMount, SecretEnvVar, SecretFile


def environment_spec(
    *,
    namespace: str,
    template_id: str,
    template_version: str,
    image_ref: str,
    registry_credential_id: str,
    rootfs_readonly: bool,
) -> environment_pb2.EnvironmentSpec:
    if image_ref and (template_id or template_version):
        raise ValueError(
            "image_ref cannot be combined with template_id or template_version"
        )
    if not image_ref and not template_id:
        raise ValueError("template_id or image_ref is required")
    spec = environment_pb2.EnvironmentSpec(namespace=namespace)
    if image_ref:
        spec.image.CopyFrom(
            environment_pb2.EnvironmentImageSource(
                ref=image_ref,
                registry_credential_id=registry_credential_id,
                rootfs_readonly=rootfs_readonly,
            )
        )
    else:
        spec.template_id = template_id
        spec.template_version = template_version
    return spec


def execution_projections(
    *,
    image_mounts: Iterable[ImageMount] | None,
    secret_env: Iterable[SecretEnvVar] | None,
    secret_files: Iterable[SecretFile] | None,
) -> tuple[
    list[common_pb2.ImageMount],
    list[common_pb2.SecretEnvVar],
    list[common_pb2.SecretFile],
]:
    """Convert public immutable Run projections to their wire contract."""

    return (
        [
            common_pb2.ImageMount(image=value.image, target=value.target)
            for value in image_mounts or ()
        ],
        [
            common_pb2.SecretEnvVar(
                name=value.name,
                secret_id=value.secret_id,
                key=value.key,
                optional=value.optional,
            )
            for value in secret_env or ()
        ],
        [
            common_pb2.SecretFile(
                path=value.path,
                secret_id=value.secret_id,
                key=value.key,
                mode=value.mode,
                optional=value.optional,
            )
            for value in secret_files or ()
        ],
    )
