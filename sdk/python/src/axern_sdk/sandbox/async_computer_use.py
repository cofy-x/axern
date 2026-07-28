"""Async sandbox computer-use helpers."""

from __future__ import annotations

from typing import Protocol

from axern_sdk.node import (
    AsyncNodeSandboxClient,
    ComputerUseDisplay,
    ComputerUseRegion,
    ComputerUseScreenshot,
    ComputerUseStatus,
)


class _HasAsyncNodeClient(Protocol):
    def _node_client(self) -> AsyncNodeSandboxClient: ...


class AsyncSandboxComputerUseMixin:
    """Async computer-use operations for a started sandbox."""

    async def computer_use_status(self: _HasAsyncNodeClient, *, timeout_seconds: int = 30) -> ComputerUseStatus:
        return await self._node_client().computer_use_status(rpc_timeout=timeout_seconds)

    async def computer_use_screenshot(
        self: _HasAsyncNodeClient,
        *,
        show_cursor: bool = False,
        region: ComputerUseRegion | None = None,
        format: str = "",
        quality: int = 0,
        scale: float = 0,
        timeout_seconds: int = 30,
    ) -> ComputerUseScreenshot:
        return await self._node_client().computer_use_screenshot(
            show_cursor=show_cursor,
            region=region,
            format=format,
            quality=quality,
            scale=scale,
            rpc_timeout=timeout_seconds,
        )

    async def computer_use_display(self: _HasAsyncNodeClient, *, timeout_seconds: int = 30) -> ComputerUseDisplay:
        return await self._node_client().computer_use_display(rpc_timeout=timeout_seconds)

    async def computer_use_mouse(
        self: _HasAsyncNodeClient,
        *,
        action: str = "click",
        x: int = 0,
        y: int = 0,
        to_x: int = 0,
        to_y: int = 0,
        button: str = "",
        direction: str = "",
        amount: int = 0,
        timeout_seconds: int = 30,
    ) -> None:
        await self._node_client().computer_use_mouse(
            action=action,
            x=x,
            y=y,
            to_x=to_x,
            to_y=to_y,
            button=button,
            direction=direction,
            amount=amount,
            rpc_timeout=timeout_seconds,
        )

    async def computer_use_keyboard(
        self: _HasAsyncNodeClient,
        *,
        text: str = "",
        key: str = "",
        keys: list[str] | tuple[str, ...] = (),
        delay_ms: int = 0,
        timeout_seconds: int = 30,
    ) -> None:
        await self._node_client().computer_use_keyboard(
            text=text,
            key=key,
            keys=keys,
            delay_ms=delay_ms,
            rpc_timeout=timeout_seconds,
        )
