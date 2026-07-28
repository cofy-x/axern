"""Synchronous sandbox browser helpers."""

from __future__ import annotations

from typing import Protocol

from axern_sdk.node import BrowserStatus, NodeSandboxClient


class _HasNodeClient(Protocol):
    def _node_client(self) -> NodeSandboxClient: ...


class SandboxBrowserMixin:
    """Browser operations for a started sandbox."""

    def browser_status(self: _HasNodeClient, *, timeout_seconds: int = 30) -> BrowserStatus:
        return self._node_client().browser_status(rpc_timeout=timeout_seconds)

    def browser_open(self: _HasNodeClient, url: str = "", *, timeout_seconds: int = 30) -> BrowserStatus:
        return self._node_client().browser_open(url, rpc_timeout=timeout_seconds)

    def browser_close(self: _HasNodeClient, *, timeout_seconds: int = 30) -> BrowserStatus:
        return self._node_client().browser_close(rpc_timeout=timeout_seconds)

    def browser_navigate(self: _HasNodeClient, url: str, *, timeout_seconds: int = 30) -> BrowserStatus:
        return self._node_client().browser_navigate(url, rpc_timeout=timeout_seconds)

    def browser_resize(self: _HasNodeClient, width: int, height: int, *, timeout_seconds: int = 30) -> BrowserStatus:
        return self._node_client().browser_resize(width, height, rpc_timeout=timeout_seconds)

    def browser_click(
        self: _HasNodeClient,
        x: int,
        y: int,
        *,
        button: str = "",
        timeout_seconds: int = 30,
    ) -> BrowserStatus:
        return self._node_client().browser_click(x, y, button=button, rpc_timeout=timeout_seconds)

    def browser_type(
        self: _HasNodeClient,
        text: str,
        *,
        delay_ms: int = 0,
        timeout_seconds: int = 30,
    ) -> BrowserStatus:
        return self._node_client().browser_type(text, delay_ms=delay_ms, rpc_timeout=timeout_seconds)

    def browser_wait(
        self: _HasNodeClient,
        *,
        timeout_ms: int = 0,
        timeout_seconds: int = 30,
    ) -> BrowserStatus:
        return self._node_client().browser_wait(timeout_ms=timeout_ms, rpc_timeout=timeout_seconds)
