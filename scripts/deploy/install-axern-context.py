#!/usr/bin/env python3
"""Install or update an Axern CLI context in the local config file."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path


CONTEXT_KEYS = {
    "endpoint",
    "service_url",
    "ssh_endpoint",
    "ssh_identity_file",
    "tls",
    "proxy_mode",
}
TLS_KEYS = {"ca_cert", "cert", "key", "server_name"}


def is_current_context(value: object) -> bool:
    if not isinstance(value, dict) or not set(value).issubset(CONTEXT_KEYS):
        return False
    if not isinstance(value.get("endpoint"), str) or not value["endpoint"].strip():
        return False
    tls = value.get("tls")
    if not isinstance(tls, dict) or not set(tls).issubset(TLS_KEYS):
        return False
    if any(not isinstance(tls.get(key), str) or not tls[key].strip() for key in ("ca_cert", "cert", "key")):
        return False
    return value.get("proxy_mode", "env") in {"env", "direct"}


def normalized_config(value: object) -> dict[str, object]:
    if not isinstance(value, dict):
        return {}
    raw_contexts = value.get("contexts")
    contexts = {}
    if isinstance(raw_contexts, dict):
        contexts = {name: context for name, context in raw_contexts.items() if isinstance(name, str) and is_current_context(context)}

    data: dict[str, object] = {"contexts": contexts}
    current = value.get("current_context")
    if isinstance(current, str) and current in contexts:
        data["current_context"] = current
    if "agent_profiles" in value:
        data["agent_profiles"] = value["agent_profiles"]
    return data


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--context", required=True)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--tls-ca-cert", required=True)
    parser.add_argument("--tls-cert", required=True)
    parser.add_argument("--tls-key", required=True)
    parser.add_argument("--tls-server-name")
    parser.add_argument("--proxy-mode")
    parser.add_argument("--service-url")
    parser.add_argument("--ssh-endpoint")
    parser.add_argument("--ssh-identity-file")
    parser.add_argument("--current", action="store_true")
    args = parser.parse_args()

    config_path = Path(args.config).expanduser()
    if config_path.exists():
        data = normalized_config(json.loads(config_path.read_text()))
    else:
        data = normalized_config({})

    contexts = data.get("contexts")
    if not isinstance(contexts, dict):
        contexts = {}

    context = {
        "endpoint": args.endpoint,
        "tls": {
            "ca_cert": args.tls_ca_cert,
            "cert": args.tls_cert,
            "key": args.tls_key,
        },
        "proxy_mode": "env",
    }
    if args.tls_server_name:
        context["tls"]["server_name"] = args.tls_server_name
    if args.proxy_mode:
        proxy_mode = args.proxy_mode.strip()
        if proxy_mode not in {"env", "direct"}:
            raise SystemExit(f"invalid --proxy-mode {args.proxy_mode!r}; expected 'env' or 'direct'")
        context["proxy_mode"] = proxy_mode
    if args.service_url:
        context["service_url"] = args.service_url
    if args.ssh_endpoint:
        context["ssh_endpoint"] = args.ssh_endpoint
    if args.ssh_identity_file:
        context["ssh_identity_file"] = args.ssh_identity_file

    contexts[args.context] = context
    data["contexts"] = contexts
    if args.current or not data.get("current_context"):
        data["current_context"] = args.context

    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(json.dumps(data, indent=2) + "\n")
    os.chmod(config_path, 0o600)

    suffix = " (current)" if data.get("current_context") == args.context else ""
    print(f"Saved axern context {args.context}{suffix} in {config_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
