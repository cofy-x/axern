"""Async node sandbox computer-use RPC mixin."""

from __future__ import annotations

from collections.abc import Callable
from typing import TYPE_CHECKING, Any

from axern.node.sandbox.v1 import node_pb2
from axern_sdk.node.computer_use_protocol import (
    computer_use_display,
    computer_use_screenshot,
    computer_use_screenshot_request,
    computer_use_status,
)
from axern_sdk.node.models import (
    ComputerUseDisplay,
    ComputerUseRegion,
    ComputerUseScreenshot,
    ComputerUseStatus,
)


class AsyncNodeSandboxComputerUseMixin:
    """Computer-use RPCs for ``AsyncNodeSandboxClient``."""

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

    async def computer_use_status(
        self,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> ComputerUseStatus:
        response = await self._call_unary(
            "sandbox computer-use status",
            "ComputerUseStatus",
            lambda: node_pb2.ComputerUseStatusRequest(allocation_id=self._allocation_id),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return computer_use_status(response)

    async def computer_use_screenshot(
        self,
        *,
        show_cursor: bool = False,
        region: ComputerUseRegion | None = None,
        format: str = "",
        quality: int = 0,
        scale: float = 0,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> ComputerUseScreenshot:
        response = await self._call_unary(
            "sandbox computer-use screenshot",
            "ComputerUseScreenshot",
            lambda: computer_use_screenshot_request(
                self._allocation_id,
                show_cursor=show_cursor,
                region=region,
                format=format,
                quality=quality,
                scale=scale,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return computer_use_screenshot(response)

    async def computer_use_display(
        self,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> ComputerUseDisplay:
        response = await self._call_unary(
            "sandbox computer-use display",
            "ComputerUseDisplay",
            lambda: node_pb2.ComputerUseDisplayRequest(allocation_id=self._allocation_id),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return computer_use_display(response)

    async def computer_use_mouse(
        self,
        *,
        action: str = "click",
        x: int = 0,
        y: int = 0,
        to_x: int = 0,
        to_y: int = 0,
        button: str = "",
        direction: str = "",
        amount: int = 0,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        await self._call_unary(
            "sandbox computer-use mouse",
            "ComputerUseMouse",
            lambda: node_pb2.ComputerUseMouseRequest(
                allocation_id=self._allocation_id,
                action=action,
                x=x,
                y=y,
                to_x=to_x,
                to_y=to_y,
                button=button,
                direction=direction,
                amount=amount,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )

    async def computer_use_keyboard(
        self,
        *,
        text: str = "",
        key: str = "",
        keys: list[str] | tuple[str, ...] = (),
        delay_ms: int = 0,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> None:
        await self._call_unary(
            "sandbox computer-use keyboard",
            "ComputerUseKeyboard",
            lambda: node_pb2.ComputerUseKeyboardRequest(
                allocation_id=self._allocation_id,
                text=text,
                key=key,
                keys=list(keys),
                delay_ms=delay_ms,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
