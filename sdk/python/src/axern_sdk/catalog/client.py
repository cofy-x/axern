"""Catalog client for official Axern runtime templates."""

from __future__ import annotations

import grpc

from axern.control.catalog.v1 import catalog_pb2, catalog_pb2_grpc

from axern_sdk._internal.channel import control_channel
from axern_sdk.catalog.models import (
    MountSpec,
    OciImageDescriptor,
    RuntimeBaselinePolicy,
    RuntimeCapabilities,
    RuntimeCapabilityPolicy,
    RuntimeExecutionProfile,
    RuntimeNetworkNamespacePolicy,
    RuntimeResourcePolicy,
    RuntimeTemplate,
)


class CatalogClient:
    """Read-only client for runtime catalog lookups."""

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
        self._client = catalog_pb2_grpc.RuntimeCatalogStub(self._channel)

    def close(self) -> None:
        if self._owns_channel:
            self._channel.close()

    def list_runtime_templates(
        self,
        *,
        namespace: str = "",
        version: str = "",
        language: str = "",
    ) -> list[RuntimeTemplate]:
        response = self._client.ListRuntimeTemplates(
            catalog_pb2.ListRuntimeTemplatesRequest(
                namespace=namespace,
                version=version,
                language=language,
            )
        )
        return [_runtime_template_from_proto(template) for template in response.runtime_templates]

    def get_runtime_template(self, runtime_id: str, *, version: str = "") -> RuntimeTemplate:
        response = self._client.GetRuntimeTemplate(catalog_pb2.GetRuntimeTemplateRequest(id=runtime_id, version=version))
        return _runtime_template_from_proto(response.runtime_template)


def _runtime_template_from_proto(template: catalog_pb2.RuntimeTemplate) -> RuntimeTemplate:
    capabilities = RuntimeCapabilities(
        supports_exec=template.capabilities.supports_exec,
        supports_exec_stream=template.capabilities.supports_exec_stream,
        supports_long_lived_process=template.capabilities.supports_long_lived_process,
        supports_ports=template.capabilities.supports_ports,
        supports_computer_use=template.capabilities.supports_computer_use,
    )
    mounts = tuple(
        MountSpec(
            type=mount.type,
            source=mount.source,
            target=mount.target,
            options=tuple(mount.options),
        )
        for mount in template.mounts
    )
    return RuntimeTemplate(
        id=template.id,
        rootfs_readonly=template.rootfs_readonly,
        image_default_argv=tuple(template.image_default_argv),
        default_cwd=template.default_cwd,
        default_env=dict(template.default_env),
        mounts=mounts,
        capabilities=capabilities,
        language=template.language,
        language_version=template.language_version,
        description=template.description,
        version=template.version,
        image_descriptor=OciImageDescriptor(
            digest=template.image_descriptor.digest,
            media_type=template.image_descriptor.media_type,
            size_bytes=template.image_descriptor.size_bytes,
            annotations=dict(template.image_descriptor.annotations),
        ),
        warm_policy=template.warm_policy,
        cache_policy=template.cache_policy,
        execution_profile=_runtime_execution_profile_from_proto(template.execution_profile),
    )


def _runtime_execution_profile_from_proto(profile: catalog_pb2.RuntimeExecutionProfile) -> RuntimeExecutionProfile:
    return RuntimeExecutionProfile(
        runtime_baseline=RuntimeBaselinePolicy(
            capabilities=tuple(profile.runtime_baseline.capabilities),
            no_file_limit=profile.runtime_baseline.no_file_limit,
        ),
        capabilities=RuntimeCapabilityPolicy(
            annotation_key=profile.capabilities.annotation_key,
            include_ambient=profile.capabilities.include_ambient
            if profile.capabilities.HasField("include_ambient")
            else None,
        ),
        network_namespace=RuntimeNetworkNamespacePolicy(
            annotation_key=profile.network_namespace.annotation_key,
        ),
        resources=RuntimeResourcePolicy(
            ignore_annotation_keys=tuple(profile.resources.ignore_annotation_keys),
        ),
    )
