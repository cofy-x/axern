"""gRPC channel helpers shared by Axern SDK clients."""

from __future__ import annotations

import grpc

_GRPC_NO_PROXY_OPTIONS: tuple[tuple[str, object], ...] = (("grpc.enable_http_proxy", 0),)


def control_channel(
    target: str,
    *,
    tls_ca_cert: str | None = None,
    tls_cert: str | None = None,
    tls_key: str | None = None,
    tls_server_name: str | None = None,
    proxy_mode: str = "env",
) -> grpc.Channel:
    credentials = _credentials(tls_ca_cert, tls_cert, tls_key)
    options = _channel_options(proxy_mode, tls_server_name)
    target = normalize_target(target)
    if credentials is None:
        return grpc.insecure_channel(target, options=options)
    return grpc.secure_channel(target, credentials, options=options)


def async_control_channel(
    target: str,
    *,
    tls_ca_cert: str | None = None,
    tls_cert: str | None = None,
    tls_key: str | None = None,
    tls_server_name: str | None = None,
    proxy_mode: str = "env",
) -> grpc.aio.Channel:
    credentials = _credentials(tls_ca_cert, tls_cert, tls_key)
    options = _channel_options(proxy_mode, tls_server_name)
    target = normalize_target(target)
    if credentials is None:
        return grpc.aio.insecure_channel(target, options=options)
    return grpc.aio.secure_channel(target, credentials, options=options)


def relay_channel(
    target: str,
    *,
    insecure: bool = False,
    tls_ca_cert: str | None = None,
    tls_cert: str | None = None,
    tls_key: str | None = None,
    server_name: str | None = None,
    proxy_mode: str = "env",
) -> grpc.Channel:
    options = _channel_options(proxy_mode, server_name)
    target = normalize_target(target)
    if insecure:
        return grpc.insecure_channel(target, options=options)
    credentials = _credentials(tls_ca_cert, tls_cert, tls_key)
    if credentials is None:
        raise ValueError("tunnel relay TLS requires tls_ca_cert or insecure=True")
    return grpc.secure_channel(target, credentials, options=options)


def normalize_target(target: str) -> str:
    return target.removeprefix("http://").removeprefix("https://")


def _credentials(
    ca_path: str | None,
    cert_path: str | None,
    key_path: str | None,
) -> grpc.ChannelCredentials | None:
    ca_path = ca_path or ""
    cert_path = cert_path or ""
    key_path = key_path or ""
    if not ca_path and not cert_path and not key_path:
        return None
    if not ca_path or not cert_path or not key_path:
        raise ValueError("mTLS requires tls_ca_cert, tls_cert, and tls_key")
    with open(ca_path, "rb") as file:
        root_certificates = file.read()
    with open(cert_path, "rb") as file:
        private_cert_chain = file.read()
    with open(key_path, "rb") as file:
        private_key = file.read()
    return grpc.ssl_channel_credentials(
        root_certificates=root_certificates,
        private_key=private_key,
        certificate_chain=private_cert_chain,
    )


def _channel_options(proxy_mode: str, server_name: str | None = None) -> tuple[tuple[str, object], ...]:
    mode = proxy_mode.strip() or "env"
    if mode not in {"env", "direct"}:
        raise ValueError("proxy_mode must be 'env' or 'direct'")
    options: list[tuple[str, object]] = []
    if mode == "direct":
        options.extend(_GRPC_NO_PROXY_OPTIONS)
    if server_name:
        options.append(("grpc.ssl_target_name_override", server_name))
    return tuple(options)
