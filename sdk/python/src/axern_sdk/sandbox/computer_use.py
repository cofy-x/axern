"""Synchronous sandbox computer-use helpers."""

from __future__ import annotations

from typing import Protocol

from axern_sdk.node import (
    ComputerUseDisplay,
    ComputerUseRegion,
    ComputerUseScreenshot,
    ComputerUseStatus,
    NodeSandboxClient,
)


class _HasNodeClient(Protocol):
    def _node_client(self) -> NodeSandboxClient: ...


class SandboxComputerUseMixin:
    """Computer-use operations for a started sandbox."""

    def computer_use_status(self: _HasNodeClient, *, timeout_seconds: int = 30) -> ComputerUseStatus:
        return self._node_client().computer_use_status(rpc_timeout=timeout_seconds)

    def computer_use_screenshot(
        self: _HasNodeClient,
        *,
        show_cursor: bool = False,
        region: ComputerUseRegion | None = None,
        format: str = "",
        quality: int = 0,
        scale: float = 0,
        timeout_seconds: int = 30,
    ) -> ComputerUseScreenshot:
        return self._node_client().computer_use_screenshot(
            show_cursor=show_cursor,
            region=region,
            format=format,
            quality=quality,
            scale=scale,
            rpc_timeout=timeout_seconds,
        )

    def computer_use_display(self: _HasNodeClient, *, timeout_seconds: int = 30) -> ComputerUseDisplay:
        return self._node_client().computer_use_display(rpc_timeout=timeout_seconds)

    def computer_use_mouse(
        self: _HasNodeClient,
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
        self._node_client().computer_use_mouse(
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

    def computer_use_keyboard(
        self: _HasNodeClient,
        *,
        text: str = "",
        key: str = "",
        keys: list[str] | tuple[str, ...] = (),
        delay_ms: int = 0,
        timeout_seconds: int = 30,
    ) -> None:
        self._node_client().computer_use_keyboard(
            text=text,
            key=key,
            keys=keys,
            delay_ms=delay_ms,
            rpc_timeout=timeout_seconds,
        )
