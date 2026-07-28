"""Explicit loader for Axern CLI contexts."""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class TLSContext:
    ca_cert: str
    cert: str
    key: str
    server_name: str = ""


@dataclass(frozen=True)
class AxernContext:
    name: str
    endpoint: str
    service_url: str
    ssh_endpoint: str
    ssh_identity_file: str
    tls: TLSContext
    proxy_mode: str


def load_context(path: str | Path, name: str = "") -> AxernContext:
    """Load one named context without implicitly reading the user directory."""

    config_path = Path(path).expanduser()
    raw = json.loads(config_path.read_text(encoding="utf-8"))
    if not isinstance(raw, dict):
        raise ValueError("Axern config must be an object")
    _reject_unknown(raw, {"current_context", "contexts", "agent_profiles"}, "config")
    context_name = name or _string(raw, "current_context")
    if not context_name:
        raise ValueError("Axern context name is required")
    contexts = raw.get("contexts")
    if not isinstance(contexts, dict) or context_name not in contexts:
        raise ValueError(f"Axern context {context_name!r} was not found")
    context = contexts[context_name]
    if not isinstance(context, dict):
        raise ValueError(f"Axern context {context_name!r} must be an object")
    _reject_unknown(context, {"endpoint", "service_url", "ssh_endpoint", "ssh_identity_file", "tls", "proxy_mode"}, "context")
    tls = context.get("tls")
    if not isinstance(tls, dict):
        raise ValueError("context.tls must be an object")
    _reject_unknown(tls, {"ca_cert", "cert", "key", "server_name"}, "context.tls")
    endpoint = _string(context, "endpoint")
    ca_cert = _string(tls, "ca_cert")
    cert = _string(tls, "cert")
    key = _string(tls, "key")
    if not endpoint or not ca_cert or not cert or not key:
        raise ValueError("context requires endpoint and tls.ca_cert, tls.cert, and tls.key")
    proxy_mode = _string(context, "proxy_mode") or "env"
    if proxy_mode not in {"env", "direct"}:
        raise ValueError("context.proxy_mode must be 'env' or 'direct'")
    return AxernContext(
        name=context_name,
        endpoint=endpoint,
        service_url=_string(context, "service_url"),
        ssh_endpoint=_string(context, "ssh_endpoint"),
        ssh_identity_file=_string(context, "ssh_identity_file"),
        tls=TLSContext(ca_cert=ca_cert, cert=cert, key=key, server_name=_string(tls, "server_name")),
        proxy_mode=proxy_mode,
    )


def _string(value: dict[str, Any], key: str) -> str:
    item = value.get(key, "")
    if not isinstance(item, str):
        raise ValueError(f"{key} must be a string")
    return item.strip()


def _reject_unknown(value: dict[str, Any], allowed: set[str], path: str) -> None:
    unknown = sorted(set(value) - allowed)
    if unknown:
        raise ValueError(f"{path} contains unknown field {unknown[0]!r}")
