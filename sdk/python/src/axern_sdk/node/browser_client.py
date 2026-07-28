"""Node sandbox browser RPC mixin."""

from __future__ import annotations

from collections.abc import Callable
from typing import TYPE_CHECKING, Any

from axern.node.sandbox.v1 import node_pb2
from axern_sdk.node.models import BrowserStatus
from axern_sdk.node.protocol import browser_status


class NodeSandboxBrowserMixin:
    """Browser RPCs for ``NodeSandboxClient``."""

    if TYPE_CHECKING:
        _allocation_id: str

        def _call_unary(
            self,
            operation: str,
            method_name: str,
            request_factory: Callable[[], object],
            *,
            lease_ttl_seconds: int,
            rpc_timeout: float | None,
        ) -> Any: ...

    def browser_status(
        self,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> BrowserStatus:
        response = self._call_unary(
            "sandbox browser status",
            "BrowserStatus",
            lambda: node_pb2.BrowserStatusRequest(allocation_id=self._allocation_id),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return browser_status(response)

    def browser_open(
        self,
        url: str = "",
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> BrowserStatus:
        response = self._call_unary(
            "sandbox browser open",
            "BrowserOpen",
            lambda: node_pb2.BrowserOpenRequest(
                allocation_id=self._allocation_id,
                url=url,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return browser_status(response)

    def browser_close(
        self,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> BrowserStatus:
        response = self._call_unary(
            "sandbox browser close",
            "BrowserClose",
            lambda: node_pb2.BrowserCloseRequest(allocation_id=self._allocation_id),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return browser_status(response)

    def browser_navigate(
        self,
        url: str,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> BrowserStatus:
        response = self._call_unary(
            "sandbox browser navigate",
            "BrowserNavigate",
            lambda: node_pb2.BrowserNavigateRequest(
                allocation_id=self._allocation_id,
                url=url,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return browser_status(response)

    def browser_resize(
        self,
        width: int,
        height: int,
        *,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> BrowserStatus:
        response = self._call_unary(
            "sandbox browser resize",
            "BrowserResize",
            lambda: node_pb2.BrowserResizeRequest(
                allocation_id=self._allocation_id,
                width=width,
                height=height,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return browser_status(response)

    def browser_click(
        self,
        x: int,
        y: int,
        *,
        button: str = "",
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> BrowserStatus:
        response = self._call_unary(
            "sandbox browser click",
            "BrowserClick",
            lambda: node_pb2.BrowserClickRequest(
                allocation_id=self._allocation_id,
                x=x,
                y=y,
                button=button,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return browser_status(response)

    def browser_type(
        self,
        text: str,
        *,
        delay_ms: int = 0,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> BrowserStatus:
        response = self._call_unary(
            "sandbox browser type",
            "BrowserType",
            lambda: node_pb2.BrowserTypeRequest(
                allocation_id=self._allocation_id,
                text=text,
                delay_ms=delay_ms,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return browser_status(response)

    def browser_wait(
        self,
        *,
        timeout_ms: int = 0,
        lease_ttl_seconds: int = 60,
        rpc_timeout: float | None = None,
    ) -> BrowserStatus:
        response = self._call_unary(
            "sandbox browser wait",
            "BrowserWait",
            lambda: node_pb2.BrowserWaitRequest(
                allocation_id=self._allocation_id,
                timeout_ms=timeout_ms,
            ),
            lease_ttl_seconds=lease_ttl_seconds,
            rpc_timeout=rpc_timeout,
        )
        return browser_status(response)
