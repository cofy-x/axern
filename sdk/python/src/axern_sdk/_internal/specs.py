"""Shared request spec builders for SDK clients."""

from __future__ import annotations

from axern.control.environment.v1 import environment_pb2


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
        raise ValueError("image_ref cannot be combined with template_id or template_version")
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
