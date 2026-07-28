"""Async sandbox browser helpers."""

from __future__ import annotations

from typing import Protocol

from axern_sdk.node import AsyncNodeSandboxClient, BrowserStatus


class _HasAsyncNodeClient(Protocol):
    def _node_client(self) -> AsyncNodeSandboxClient: ...


class AsyncSandboxBrowserMixin:
    """Async browser operations for a started sandbox."""

    async def browser_status(self: _HasAsyncNodeClient, *, timeout_seconds: int = 30) -> BrowserStatus:
        return await self._node_client().browser_status(rpc_timeout=timeout_seconds)

    async def browser_open(self: _HasAsyncNodeClient, url: str = "", *, timeout_seconds: int = 30) -> BrowserStatus:
        return await self._node_client().browser_open(url, rpc_timeout=timeout_seconds)

    async def browser_close(self: _HasAsyncNodeClient, *, timeout_seconds: int = 30) -> BrowserStatus:
        return await self._node_client().browser_close(rpc_timeout=timeout_seconds)

    async def browser_navigate(self: _HasAsyncNodeClient, url: str, *, timeout_seconds: int = 30) -> BrowserStatus:
        return await self._node_client().browser_navigate(url, rpc_timeout=timeout_seconds)

    async def browser_resize(
        self: _HasAsyncNodeClient,
        width: int,
        height: int,
        *,
        timeout_seconds: int = 30,
    ) -> BrowserStatus:
        return await self._node_client().browser_resize(width, height, rpc_timeout=timeout_seconds)

    async def browser_click(
        self: _HasAsyncNodeClient,
        x: int,
        y: int,
        *,
        button: str = "",
        timeout_seconds: int = 30,
    ) -> BrowserStatus:
        return await self._node_client().browser_click(x, y, button=button, rpc_timeout=timeout_seconds)

    async def browser_type(
        self: _HasAsyncNodeClient,
        text: str,
        *,
        delay_ms: int = 0,
        timeout_seconds: int = 30,
    ) -> BrowserStatus:
        return await self._node_client().browser_type(text, delay_ms=delay_ms, rpc_timeout=timeout_seconds)

    async def browser_wait(
        self: _HasAsyncNodeClient,
        *,
        timeout_ms: int = 0,
        timeout_seconds: int = 30,
    ) -> BrowserStatus:
        return await self._node_client().browser_wait(timeout_ms=timeout_ms, rpc_timeout=timeout_seconds)
