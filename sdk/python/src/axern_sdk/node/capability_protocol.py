"""Capability protobuf converters shared by sync and async node clients."""

from __future__ import annotations

from axern.node.sandbox.v1 import node_pb2
from axern_sdk.node.models import (
    CapabilityDependencyStatus,
    CapabilityProviderStatus,
    CapabilityProviderSummary,
    CapabilityStatus,
)


def capability_status(response: node_pb2.CapabilityStatusResponse) -> CapabilityStatus:
    return CapabilityStatus(
        ready=bool(response.ready),
        capabilities=tuple(response.capabilities),
        providers=tuple(capability_provider(item) for item in response.providers),
        provider_summary=capability_provider_summary(response.provider_summary),
    )


def capability_provider(response: node_pb2.CapabilityProviderStatus) -> CapabilityProviderStatus:
    return CapabilityProviderStatus(
        name=response.name,
        state=response.state,
        available=bool(response.available),
        capabilities=tuple(response.capabilities),
        backend=response.backend,
        reason=response.reason,
        dependencies=tuple(capability_dependency(item) for item in response.dependencies),
    )


def capability_dependency(response: node_pb2.CapabilityDependencyStatus) -> CapabilityDependencyStatus:
    return CapabilityDependencyStatus(
        name=response.name,
        available=bool(response.available),
        reason=response.reason,
    )


def capability_provider_summary(response: node_pb2.CapabilityProviderSummary) -> CapabilityProviderSummary:
    return CapabilityProviderSummary(
        total=response.total,
        available=response.available,
        degraded=response.degraded,
        unavailable=response.unavailable,
    )
