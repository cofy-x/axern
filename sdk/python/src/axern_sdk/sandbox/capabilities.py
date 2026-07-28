"""Synchronous sandbox capability helpers."""

from __future__ import annotations

from typing import Protocol

from axern_sdk.node import CapabilityStatus, NodeSandboxClient


class _HasNodeClient(Protocol):
    def _node_client(self) -> NodeSandboxClient: ...


class SandboxCapabilityMixin:
    """Capability operations for a started sandbox."""

    def capability_status(self: _HasNodeClient, *, timeout_seconds: int = 30) -> CapabilityStatus:
        return self._node_client().capability_status(rpc_timeout=timeout_seconds)
