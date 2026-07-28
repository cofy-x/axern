"""HTTP worker runtime for Axern Python Functions."""

from __future__ import annotations

import argparse
import concurrent.futures
import dataclasses
import hashlib
import importlib
import json
import logging
import os
import sys
import tarfile
import tempfile
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable, Mapping
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from types import TracebackType
from typing import Any


logger = logging.getLogger("axern.function.worker")

DEFAULT_PORT = 8080
DEFAULT_MAX_BODY_BYTES = 32 * 1024 * 1024
DEFAULT_MAX_BUNDLE_BYTES = 64 * 1024 * 1024


@dataclasses.dataclass(frozen=True, slots=True)
class FunctionContext:
    function_id: str
    function_name: str
    namespace: str
    revision_id: str
    request_id: str
    invocation_id: str
    env: Mapping[str, str]
    state: Any
    deadline: float = 0.0
    log: Any = dataclasses.field(default_factory=lambda: logger)


@dataclasses.dataclass(frozen=True, slots=True)
class WorkerConfig:
    bundle_uri: str
    bundle_url: str
    bundle_token: str
    bundle_digest: str
    handler_ref: str
    initializer_ref: str
    function_id: str
    function_name: str
    namespace: str
    revision_id: str
    port: int = DEFAULT_PORT
    invoke_path: str = "/invoke"
    max_body_bytes: int = DEFAULT_MAX_BODY_BYTES
    max_bundle_bytes: int = DEFAULT_MAX_BUNDLE_BYTES

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> "WorkerConfig":
        values = dict(os.environ if env is None else env)
        return cls(
            bundle_uri=values.get("AXERN_FUNCTION_BUNDLE_URI", ""),
            bundle_url=values.get("AXERN_FUNCTION_BUNDLE_URL", ""),
            bundle_token=values.get("AXERN_FUNCTION_BUNDLE_TOKEN", ""),
            bundle_digest=values.get("AXERN_FUNCTION_BUNDLE_DIGEST", ""),
            handler_ref=values.get("AXERN_FUNCTION_HANDLER", ""),
            initializer_ref=values.get("AXERN_FUNCTION_INITIALIZER", ""),
            function_id=values.get("AXERN_FUNCTION_ID", ""),
            function_name=values.get("AXERN_FUNCTION_NAME", ""),
            namespace=values.get("AXERN_FUNCTION_NAMESPACE", ""),
            revision_id=values.get("AXERN_FUNCTION_REVISION_ID", ""),
            port=_env_int(values, "AXERN_FUNCTION_WORKER_PORT", DEFAULT_PORT),
            invoke_path=values.get("AXERN_FUNCTION_INVOKE_PATH", "/invoke") or "/invoke",
            max_body_bytes=_env_int(values, "AXERN_FUNCTION_MAX_BODY_BYTES", DEFAULT_MAX_BODY_BYTES),
            max_bundle_bytes=_env_int(values, "AXERN_FUNCTION_MAX_BUNDLE_BYTES", DEFAULT_MAX_BUNDLE_BYTES),
        )


class FunctionWorker:
    def __init__(
        self,
        *,
        config: WorkerConfig,
        root: Path,
        handler: Callable[[Any, FunctionContext], Any],
        initializer: Callable[[FunctionContext], Any] | None = None,
    ) -> None:
        self.config = config
        self.root = root
        self.handler = handler
        self.state = None
        if initializer is not None:
            self.state = initializer(self._context(request_id="", invocation_id=""))

    @classmethod
    def load(cls, config: WorkerConfig) -> "FunctionWorker":
        if not config.handler_ref:
            raise RuntimeError("AXERN_FUNCTION_HANDLER is required")
        root = Path(tempfile.mkdtemp(prefix="axern-function-worker-"))
        payload = _read_bundle(config)
        _verify_digest(payload, config.bundle_digest)
        _extract_bundle(payload, root)
        source_root = (root / "src").resolve()
        if not source_root.is_dir():
            raise RuntimeError("function bundle is missing src/")
        sys.path.insert(0, str(source_root))
        handler = _load_callable(config.handler_ref)
        initializer = _load_callable(config.initializer_ref) if config.initializer_ref else None
        return cls(config=config, root=root, handler=handler, initializer=initializer)

    def invoke(self, body: bytes, headers: Mapping[str, str]) -> tuple[int, str, bytes]:
        event = _decode_event(body, _header_value(headers, "Content-Type"))
        timeout_seconds = _parse_timeout(headers)
        request_id = _header_value(headers, "X-Axern-Function-Request-Id")
        invocation_id = _header_value(headers, "X-Axern-Function-Invocation-Id")

        invocation_logger = _invocation_logger(
            self.config.function_name, request_id, invocation_id,
        )
        deadline = _monotonic_deadline(timeout_seconds)
        context = self._context(
            request_id=request_id,
            invocation_id=invocation_id,
            deadline=deadline,
            log=invocation_logger,
        )

        invocation_logger.info("invocation started")

        if timeout_seconds > 0:
            with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
                future = pool.submit(self.handler, event, context)
                try:
                    result = future.result(timeout=timeout_seconds)
                except concurrent.futures.TimeoutError:
                    invocation_logger.warning(
                        "invocation timed out after %ds", timeout_seconds,
                    )
                    future.cancel()
                    return (
                        HTTPStatus.GATEWAY_TIMEOUT,
                        "application/json",
                        json.dumps({"error": {"message": f"handler timed out after {timeout_seconds}s", "type": "TimeoutError"}}).encode("utf-8"),
                    )
        else:
            result = self.handler(event, context)

        invocation_logger.info("invocation succeeded")
        return HTTPStatus.OK, "application/json", json.dumps(result, separators=(",", ":")).encode("utf-8")

    def _context(
        self,
        *,
        request_id: str,
        invocation_id: str,
        deadline: float = 0.0,
        log: Any = None,
    ) -> FunctionContext:
        return FunctionContext(
            function_id=self.config.function_id,
            function_name=self.config.function_name,
            namespace=self.config.namespace,
            revision_id=self.config.revision_id,
            request_id=request_id,
            invocation_id=invocation_id,
            env=os.environ,
            state=self.state,
            deadline=deadline,
            log=log or logger,
        )


def serve(worker: FunctionWorker) -> None:
    handler = _handler(worker)
    server = ThreadingHTTPServer(("0.0.0.0", worker.config.port), handler)
    server.serve_forever()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.parse_args(argv)
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s %(name)s %(levelname)s %(message)s",
    )
    worker = FunctionWorker.load(WorkerConfig.from_env())
    logger.info("worker ready: function=%s handler=%s port=%d",
                worker.config.function_name, worker.config.handler_ref, worker.config.port)
    serve(worker)
    return 0


def _handler(worker: FunctionWorker) -> type[BaseHTTPRequestHandler]:
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            if self.path != "/healthz":
                self.send_error(HTTPStatus.NOT_FOUND)
                return
            self.send_response(HTTPStatus.OK)
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.end_headers()
            self.wfile.write(b"ok\n")

        def do_POST(self) -> None:  # noqa: N802
            if self.path != worker.config.invoke_path:
                self.send_error(HTTPStatus.NOT_FOUND)
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
            except ValueError:
                self._write_error(HTTPStatus.LENGTH_REQUIRED, "invalid content length")
                return
            if length > worker.config.max_body_bytes:
                self._write_error(HTTPStatus.REQUEST_ENTITY_TOO_LARGE, "request body too large")
                return
            try:
                status, content_type, payload = worker.invoke(self.rfile.read(length), dict(self.headers.items()))
            except Exception as exc:  # noqa: BLE001
                logger.exception("handler exception")
                self._write_exception(exc, sys.exc_info()[2])
                return
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.end_headers()
            self.wfile.write(payload)

        def log_message(self, format: str, *args: Any) -> None:
            return

        def _write_error(self, status: HTTPStatus, message: str) -> None:
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"error": {"message": message, "type": status.phrase}}).encode("utf-8"))

        def _write_exception(self, exc: BaseException, tb: TracebackType | None) -> None:
            self.send_response(HTTPStatus.INTERNAL_SERVER_ERROR)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(
                json.dumps(
                    {
                        "error": {
                            "message": str(exc),
                            "type": exc.__class__.__name__,
                            "stack_trace": "".join(traceback.format_exception(exc.__class__, exc, tb)),
                        }
                    }
                ).encode("utf-8")
            )

    return Handler


def _parse_timeout(headers: Mapping[str, str]) -> float:
    raw = _header_value(headers, "X-Axern-Function-Timeout")
    if not raw:
        return 0.0
    try:
        return float(raw)
    except ValueError:
        return 0.0


def _monotonic_deadline(timeout_seconds: float) -> float:
    if timeout_seconds <= 0:
        return 0.0
    return time.monotonic() + timeout_seconds


def _invocation_logger(
    function_name: str, request_id: str, invocation_id: str,
) -> logging.LoggerAdapter:
    child = logger.getChild("invoke")
    extra_parts = [f"fn={function_name}"]
    if request_id:
        extra_parts.append(f"req={request_id}")
    if invocation_id:
        extra_parts.append(f"inv={invocation_id}")
    prefix = " ".join(extra_parts)
    return logging.LoggerAdapter(child, {"prefix": prefix})


def _read_bundle(config: WorkerConfig) -> bytes:
    if config.bundle_url:
        request = urllib.request.Request(config.bundle_url)
        if config.bundle_token:
            request.add_header("Authorization", "Bearer " + config.bundle_token)
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return _read_capped(response, config.max_bundle_bytes)
        except urllib.error.URLError as exc:
            raise RuntimeError(f"download function bundle: {exc}") from exc
    parsed = urllib.parse.urlparse(config.bundle_uri)
    if parsed.scheme == "file":
        with Path(urllib.request.url2pathname(parsed.path)).open("rb") as handle:
            return _read_capped(handle, config.max_bundle_bytes)
    raise RuntimeError("AXERN_FUNCTION_BUNDLE_URL is required for non-file bundle sources")


def _read_capped(source: Any, limit: int) -> bytes:
    payload = source.read(limit + 1)
    if len(payload) > limit:
        raise RuntimeError(f"function bundle exceeds {limit} bytes")
    return payload


def _verify_digest(payload: bytes, expected: str) -> None:
    if not expected:
        return
    got = "sha256:" + hashlib.sha256(payload).hexdigest()
    if got != expected:
        raise RuntimeError(f"function bundle digest mismatch: got {got}, want {expected}")


def _extract_bundle(payload: bytes, root: Path) -> None:
    import io

    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:*") as archive:
        for member in archive.getmembers():
            target = (root / member.name).resolve()
            if root.resolve() not in (target, *target.parents):
                raise RuntimeError(f"function bundle contains unsafe path: {member.name}")
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            if not member.isfile():
                raise RuntimeError(f"function bundle contains unsupported member: {member.name}")
            target.parent.mkdir(parents=True, exist_ok=True)
            source = archive.extractfile(member)
            if source is None:
                raise RuntimeError(f"function bundle member is not readable: {member.name}")
            target.write_bytes(source.read())


def _load_callable(ref: str) -> Callable[..., Any]:
    module_name, _, attr = ref.rpartition(".")
    if not module_name or not attr:
        raise RuntimeError(f"invalid function reference: {ref}")
    module = importlib.import_module(module_name)
    value = getattr(module, attr)
    if not callable(value):
        raise RuntimeError(f"function reference is not callable: {ref}")
    return value


def _decode_event(body: bytes, content_type: str) -> Any:
    if not body:
        return {}
    if "json" in content_type.lower():
        return json.loads(body.decode("utf-8"))
    return body


def _header_value(headers: Mapping[str, str], name: str) -> str:
    for key, value in headers.items():
        if key.lower() == name.lower():
            return value
    return ""


def _env_int(env: Mapping[str, str], key: str, default: int) -> int:
    try:
        value = int(env.get(key, ""))
    except ValueError:
        return default
    return value if value > 0 else default


if __name__ == "__main__":
    raise SystemExit(main())
