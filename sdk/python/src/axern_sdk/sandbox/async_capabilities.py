"""Async sandbox capability helpers."""

from __future__ import annotations

from typing import Protocol

from axern_sdk.node import AsyncAllocationClient, CapabilityStatus


class _HasAsyncNodeClient(Protocol):
    def _node_client(self) -> AsyncAllocationClient: ...


class AsyncSandboxCapabilityMixin:
    """Capability operations for a started sandbox."""

    async def capability_status(self: _HasAsyncNodeClient, *, timeout_seconds: int = 30) -> CapabilityStatus:
        return await self._node_client().capability_status(rpc_timeout=timeout_seconds)
