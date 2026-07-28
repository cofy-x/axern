"""Async node sandbox capability RPC mixin."""

from __future__ import annotations

from collections.abc import Callable
from typing import TYPE_CHECKING, Any

from axern.node.sandbox.v1 import node_pb2
from axern_sdk.node.capability_protocol import capability_status
from axern_sdk.node.models import CapabilityStatus


class AsyncNodeSandboxCapabilityMixin:
    """Capability RPCs for ``AsyncNodeSandboxClient``."""

    if TYPE_CHECKING:
        _allocation_id: str

        async def _call_unary(
            self,
            operation: str,
            method_name: str,
            request_factory: Callable[[], object],
            *,
            lease_ttl_seconds: int,
            rpc_timeout: float | None,
        ) -> Any: ...

    async def capability_status(
        self,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> CapabilityStatus:
        response = await self._call_unary(
            "sandbox capability status",
            "CapabilityStatus",
            lambda: node_pb2.CapabilityStatusRequest(allocation_id=self._allocation_id),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return capability_status(response)
