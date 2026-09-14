"""Catalog client for Axern environment templates."""

from __future__ import annotations

import grpc

from axern.control.catalog.v1 import catalog_pb2, catalog_pb2_grpc

from axern_sdk._internal.channel import control_channel
from axern_sdk.catalog.models import (
    MountSpec,
    OciImageDescriptor,
    OciBaselinePolicy,
    EnvironmentTemplateCapabilities,
    OciExecutionProfile,
    OciNetworkNamespacePolicy,
    OciResourcePolicy,
    ResolvedEnvironmentSpec,
    EnvironmentTemplate,
)


class EnvironmentCatalogClient:
    """Read-only client for environment catalog lookups."""

    def __init__(
        self,
        target: str,
        *,
        channel: grpc.Channel | None = None,
        tls_ca_cert: str | None = None,
        tls_cert: str | None = None,
        tls_key: str | None = None,
        tls_server_name: str | None = None,
        proxy_mode: str = "env",
    ) -> None:
        self._owns_channel = channel is None
        self._channel = channel or control_channel(
            target,
            tls_ca_cert=tls_ca_cert,
            tls_cert=tls_cert,
            tls_key=tls_key,
            tls_server_name=tls_server_name,
            proxy_mode=proxy_mode,
        )
        self._client = catalog_pb2_grpc.EnvironmentCatalogStub(self._channel)

    def close(self) -> None:
        if self._owns_channel:
            self._channel.close()

    def list_environment_templates(
        self,
        *,
        version: str = "",
        language: str = "",
    ) -> list[EnvironmentTemplate]:
        response = self._client.ListEnvironmentTemplates(
            catalog_pb2.ListEnvironmentTemplatesRequest(
                version=version,
                language=language,
            )
        )
        return [_environment_template_from_proto(template) for template in response.environment_templates]

    def get_environment_template(self, environment_id: str, *, version: str = "") -> EnvironmentTemplate:
        response = self._client.GetEnvironmentTemplate(catalog_pb2.GetEnvironmentTemplateRequest(id=environment_id, version=version))
        return _environment_template_from_proto(response.environment_template)


def _environment_template_from_proto(template: catalog_pb2.EnvironmentTemplate) -> EnvironmentTemplate:
    capabilities = EnvironmentTemplateCapabilities(
        supports_exec=template.capabilities.supports_exec,
        supports_exec_stream=template.capabilities.supports_exec_stream,
        supports_long_lived_process=template.capabilities.supports_long_lived_process,
        supports_ports=template.capabilities.supports_ports,
        supports_computer_use=template.capabilities.supports_computer_use,
    )
    resolved = template.resolved_spec
    mounts = tuple(
        MountSpec(
            type=mount.type,
            source=mount.source,
            target=mount.target,
            options=tuple(mount.options),
        )
        for mount in resolved.mounts
    )
    return EnvironmentTemplate(
        id=template.id,
        capabilities=capabilities,
        language=template.language,
        language_version=template.language_version,
        description=template.description,
        version=template.version,
        resolved_spec=ResolvedEnvironmentSpec(
            rootfs_readonly=resolved.rootfs_readonly,
            image_default_argv=tuple(resolved.image_default_argv),
            default_cwd=resolved.default_cwd,
            default_env=dict(resolved.default_env),
            mounts=mounts,
            image_descriptor=OciImageDescriptor(
                digest=resolved.image_descriptor.digest,
                media_type=resolved.image_descriptor.media_type,
                size_bytes=resolved.image_descriptor.size_bytes,
                annotations=dict(resolved.image_descriptor.annotations),
            ),
            execution_profile=_oci_execution_profile_from_proto(resolved.execution_profile),
        ),
    )


def _oci_execution_profile_from_proto(profile: catalog_pb2.OciExecutionProfile) -> OciExecutionProfile:
    return OciExecutionProfile(
        baseline=OciBaselinePolicy(
            capabilities=tuple(profile.baseline.capabilities),
            no_file_limit=profile.baseline.no_file_limit,
        ),
        network_namespace=OciNetworkNamespacePolicy(
            annotation_key=profile.network_namespace.annotation_key,
        ),
        resources=OciResourcePolicy(
            ignore_annotation_keys=tuple(profile.resources.ignore_annotation_keys),
        ),
    )
