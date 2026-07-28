"""Configuration models for Axern tunnel connectors."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class _GatewayTransport:
    """Gateway transport inherited from the owning Axern client."""

    insecure: bool = False
    tls_ca_cert: str = ""
    tls_cert: str = ""
    tls_key: str = ""
    server_name: str = ""
    proxy_mode: str = "env"


@dataclass(frozen=True, slots=True)
class ConnectorConfig:
    """Local connector behavior for a tunnel client peer."""

    ping_interval_seconds: float = 15.0
    pong_timeout_seconds: float = 45.0
    max_streams: int = 256
    local_connect_timeout_seconds: float = 5.0
