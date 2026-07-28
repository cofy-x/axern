"""Compose E2E for NodeSandbox computer-use status and screenshot APIs."""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.parse

os.environ.setdefault("GRPC_VERBOSITY", "ERROR")
os.environ.setdefault("GLOG_minloglevel", "2")

from axern_sdk import (
    AxernClient,
    ComputerUseRegion,
    ComputerUseScreenshot,
    Sandbox,
)
from axern_sdk.errors import SandboxPreconditionError


SUPERVISORD_ARGV = [
    "/usr/bin/supervisord",
    "-c",
    "/etc/supervisor/conf.d/supervisord.conf",
]
PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--tls-ca-cert", required=True)
    parser.add_argument("--tls-cert", required=True)
    parser.add_argument("--tls-key", required=True)
    parser.add_argument("--runtime-class", default="runsc")
    parser.add_argument("--node-container", required=True)
    parser.add_argument("--desktop-template-id", default="desktop-base")
    parser.add_argument("--headless-template-id", default="server-base")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    client = AxernClient(
        args.endpoint,
        tls_ca_cert=args.tls_ca_cert,
        tls_cert=args.tls_cert,
        tls_key=args.tls_key,
    )
    phase = "desktop-start"
    desktop_service_id = ""
    headless_service_id = ""
    diagnostics = ""
    try:
        with Sandbox(
            client=client,
            template_id=args.desktop_template_id,
            runtime_class=args.runtime_class,
            argv=SUPERVISORD_ARGV,
            ready_timeout_seconds=180,
        ) as sandbox:
            desktop_service_id = sandbox.service_id
            phase = "desktop-status"
            status = wait_for_computer_use_ready(sandbox)
            if not status.available:
                raise SystemExit(
                    f"desktop computer_use status unavailable: {status.reason!r}"
                )
            if status.display != ":99":
                raise SystemExit(
                    f"desktop computer_use display = {status.display!r}, want ':99'"
                )
            if status.backend != "x11":
                raise SystemExit(
                    f"desktop computer_use backend = {status.backend!r}, want 'x11'"
                )
            diagnostics = sandboxd_diagnostics(sandbox)

            phase = "desktop-display"
            display = sandbox.computer_use_display(timeout_seconds=30)
            if display.width <= 0 or display.height <= 0:
                raise SystemExit(f"desktop display geometry is invalid: {display}")

            phase = "desktop-file-api"
            sandbox.write_text(
                "/tmp/axern-desktop-session-e2e.txt",
                "desktop-session-ok\n",
                timeout_seconds=30,
            )
            if (
                sandbox.read_text(
                    "/tmp/axern-desktop-session-e2e.txt",
                    timeout_seconds=30,
                )
                != "desktop-session-ok\n"
            ):
                raise SystemExit("desktop file API round-trip returned unexpected content")

            phase = "desktop-process-api"
            wait_for_process_api_ready(sandbox)

            phase = "desktop-screenshot"
            screenshot = wait_for_png_screenshot(sandbox)
            if screenshot.content_type != "image/png":
                raise SystemExit(
                    f"desktop screenshot content_type = {screenshot.content_type!r}, want image/png"
                )
            if not screenshot.data.startswith(PNG_SIGNATURE):
                raise SystemExit(
                    f"desktop screenshot is not PNG: {screenshot.data[:8]!r}"
                )

            phase = "desktop-region-screenshot"
            region_screenshot = sandbox.computer_use_screenshot(
                region=ComputerUseRegion(
                    x=0,
                    y=0,
                    width=min(display.width, 64),
                    height=min(display.height, 64),
                ),
                format="jpeg",
                quality=70,
                scale=1,
                timeout_seconds=30,
            )
            if region_screenshot.content_type != "image/jpeg" or not region_screenshot.data.startswith(
                b"\xff\xd8"
            ):
                raise SystemExit(
                    "desktop region screenshot is not JPEG: "
                    f"content_type={region_screenshot.content_type!r} head={region_screenshot.data[:2]!r}"
                )

            phase = "desktop-input"
            sandbox.computer_use_mouse(action="move", x=10, y=10, timeout_seconds=30)
            sandbox.computer_use_mouse(
                action="scroll",
                x=10,
                y=10,
                direction="down",
                amount=1,
                timeout_seconds=30,
            )
            sandbox.computer_use_keyboard(key="Escape", timeout_seconds=30)

            phase = "desktop-playwright"
            sandbox.exec(
                [
                    "/bin/sh",
                    "-lc",
                    "\n".join(
                        [
                            "python3 - <<'PY'",
                            "from playwright.sync_api import sync_playwright",
                            "",
                            "with sync_playwright() as p:",
                            "    browser = p.chromium.launch(",
                            "        headless=False,",
                            "        args=[",
                            "            '--no-sandbox',",
                            "            '--disable-dev-shm-usage',",
                            "            '--disable-gpu',",
                            "        ],",
                            "    )",
                            "    page = browser.new_page()",
                            "    page.goto('about:blank')",
                            "    browser.close()",
                            "PY",
                        ]
                    ),
                ],
                check=True,
                text=True,
                timeout_seconds=60,
            )

            phase = "desktop-browser-status"
            browser_status = sandbox.browser_status(timeout_seconds=30)
            if not browser_status.available:
                raise SystemExit(f"desktop browser status unavailable: {browser_status}")
            if "chrom" not in browser_status.command:
                raise SystemExit(
                    f"desktop browser command = {browser_status.command!r}, want chromium"
                )

            phase = "desktop-browser-open"
            browser_page = urllib.parse.quote(
                "<!doctype html>"
                "<html><body>"
                "<input id='q' autofocus style='font-size:32px;margin:40px' />"
                "<button style='font-size:32px'>Go</button>"
                "</body></html>",
                safe="",
            )
            browser_url = f"data:text/html,{browser_page}"
            browser_status = sandbox.browser_open(browser_url, timeout_seconds=30)
            if not browser_status.running:
                raise SystemExit(
                    f"desktop browser did not report running after open: {browser_status}"
                )
            if browser_status.url != browser_url:
                raise SystemExit(f"desktop browser url not tracked: {browser_status}")
            wait_for_png_screenshot(sandbox)

            phase = "desktop-browser-operations"
            browser_status = sandbox.browser_resize(1024, 768, timeout_seconds=30)
            if not browser_status.running:
                raise SystemExit(f"desktop browser resize stopped session: {browser_status}")
            sandbox.browser_click(120, 120, timeout_seconds=30)
            sandbox.browser_type("axern-browser-e2e", delay_ms=1, timeout_seconds=30)
            sandbox.browser_wait(timeout_ms=100, timeout_seconds=30)
            wait_for_png_screenshot(sandbox)

            phase = "desktop-browser-close"
            browser_status = sandbox.browser_close(timeout_seconds=30)
            if browser_status.running:
                raise SystemExit(
                    f"desktop browser still reported running after close: {browser_status}"
                )

        phase = "headless-start"
        with Sandbox(
            client=client,
            template_id=args.headless_template_id,
            runtime_class=args.runtime_class,
            argv=SUPERVISORD_ARGV,
            ready_timeout_seconds=180,
        ) as sandbox:
            headless_service_id = sandbox.service_id
            diagnostics = sandboxd_diagnostics(sandbox)
            phase = "headless-precondition"
            try:
                sandbox.computer_use_status(timeout_seconds=30)
            except SandboxPreconditionError as exc:
                assert_capability_precondition(exc, "computer_use")
            else:
                raise SystemExit(
                    "headless runtime unexpectedly exposed computer_use status"
                )
            try:
                sandbox.browser_status(timeout_seconds=30)
            except SandboxPreconditionError as exc:
                assert_capability_precondition(exc, "browser")
            else:
                raise SystemExit("headless runtime unexpectedly exposed browser status")

        print(
            "node_computer_use_e2e_ok=true "
            f"runtime_class={args.runtime_class} "
            f"desktop_service_id={desktop_service_id} headless_service_id={headless_service_id}"
        )
        return 0
    except BaseException as exc:
        log_e2e_failure(
            args,
            phase=phase,
            desktop_service_id=desktop_service_id,
            headless_service_id=headless_service_id,
            diagnostics=diagnostics,
            exc=exc,
        )
        raise
    finally:
        client.close()


def assert_capability_precondition(exc: SandboxPreconditionError, capability_name: str) -> None:
    capability = exc.capability
    if capability is None:
        raise SystemExit(f"{capability_name} precondition missing structured capability detail: {exc}") from exc
    if capability.capability != capability_name:
        raise SystemExit(
            f"{capability_name} precondition capability={capability.capability!r}, want {capability_name!r}: {exc}"
        ) from exc
    if capability.provider != capability_name:
        raise SystemExit(
            f"{capability_name} precondition provider={capability.provider!r}, want {capability_name!r}: {exc}"
        ) from exc
    if capability.provider_state != "unavailable":
        raise SystemExit(
            f"{capability_name} precondition provider_state={capability.provider_state!r}, want 'unavailable': {exc}"
        ) from exc
    if not capability.reason:
        raise SystemExit(f"{capability_name} precondition missing reason: {exc}") from exc
    if not capability.missing_dependencies:
        raise SystemExit(f"{capability_name} precondition missing dependency detail: {exc}") from exc


def wait_for_png_screenshot(sandbox: Sandbox) -> ComputerUseScreenshot:
    deadline = time.monotonic() + 60
    last_error: BaseException | None = None
    while time.monotonic() < deadline:
        try:
            screenshot = sandbox.computer_use_screenshot(timeout_seconds=30)
            if screenshot.data.startswith(PNG_SIGNATURE):
                return screenshot
            last_error = RuntimeError(
                f"screenshot response is not PNG: {screenshot.data[:8]!r}"
            )
        except Exception as exc:
            last_error = exc
        time.sleep(1)
    raise TimeoutError(
        f"desktop screenshot did not become available: {last_error}"
    ) from last_error


def wait_for_computer_use_ready(sandbox: Sandbox):
    deadline = time.monotonic() + 60
    last_status = None
    while time.monotonic() < deadline:
        status = sandbox.computer_use_status(timeout_seconds=30)
        last_status = status
        if status.available and not status.dependencies:
            return status
        deps = {item.name: item for item in status.dependencies}
        if status.available and all(
            deps.get(name) is not None and deps[name].available
            for name in (
                "display_env",
                "screenshot_backend",
                "display_backend",
                "input_backend",
                "display_server",
            )
        ):
            return status
        time.sleep(1)
    details = ", ".join(
        f"{item.name}={item.available}:{item.reason}"
        for item in (last_status.dependencies if last_status else ())
    )
    raise TimeoutError(
        f"desktop computer_use dependencies did not become ready: {details}"
    )


def wait_for_process_api_ready(sandbox: Sandbox) -> None:
    deadline = time.monotonic() + 30
    last_stdout = ""
    last_stderr = ""
    last_exit_code: int | None = None
    while time.monotonic() < deadline:
        result = sandbox.exec(
            [
                "/bin/sh",
                "-lc",
                "read payload; printf 'stdout:%s:%s:%s' \"$(id -un)\" \"$(pwd)\" \"$payload\"; "
                "printf 'stderr:process-ready' >&2; exit 7",
            ],
            input=b"stdin-ok\n",
            check=False,
            text=True,
            timeout_seconds=30,
        )
        last_stdout = result.stdout
        last_stderr = result.stderr
        last_exit_code = result.exit_code
        _, _, stdout_tail = result.stdout.partition(":")
        user, _, rest = stdout_tail.partition(":")
        cwd, _, payload = rest.partition(":")
        if (
            result.exit_code == 7
            and user == "root"
            and cwd.startswith("/")
            and payload == "stdin-ok"
            and result.stderr == "stderr:process-ready"
        ):
            return
        time.sleep(1)
    raise TimeoutError(
        "desktop process API did not return expected output: "
        f"exit_code={last_exit_code} stdout={last_stdout!r} stderr={last_stderr!r}"
    )


def log_e2e_failure(
    args: argparse.Namespace,
    *,
    phase: str,
    desktop_service_id: str,
    headless_service_id: str,
    diagnostics: str,
    exc: BaseException,
) -> None:
    print(
        "node_computer_use_e2e_failed=true "
        f"runtime_class={args.runtime_class} phase={phase} "
        f"desktop_service_id={desktop_service_id or '-'} headless_service_id={headless_service_id or '-'} "
        f"node_container={args.node_container} "
        f"error_type={type(exc).__name__} error={exc}",
        file=sys.stderr,
    )
    if diagnostics:
        print("node_computer_use_e2e_sandboxd_diagnostics_begin", file=sys.stderr)
        print(diagnostics, file=sys.stderr)
        print("node_computer_use_e2e_sandboxd_diagnostics_end", file=sys.stderr)


def sandboxd_diagnostics(sandbox: Sandbox) -> str:
    script = r"""
import http.client
import json
import socket

class UnixHTTPConnection(http.client.HTTPConnection):
    def __init__(self, path):
        super().__init__("localhost")
        self.path = path

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(self.path)

def get(path):
    conn = UnixHTTPConnection("/run/axern/sandboxd.sock")
    conn.request("GET", path)
    resp = conn.getresponse()
    body = resp.read().decode("utf-8", "replace")
    conn.close()
    return {"status": resp.status, "body": json.loads(body)}

print(json.dumps({
    "status": get("/status"),
    "capabilities": get("/capabilities"),
}, sort_keys=True))
"""
    try:
        result = sandbox.exec(
            ["python3", "-c", script],
            check=False,
            text=True,
            timeout_seconds=30,
        )
        if result.exit_code == 0 and result.stdout.strip():
            return result.stdout.strip()
        return json.dumps(
            {
                "diagnostics_error": "sandboxd diagnostics command failed",
                "exit_code": result.exit_code,
                "stdout": result.stdout,
                "stderr": result.stderr,
            },
            sort_keys=True,
        )
    except Exception as exc:
        return f'{{"diagnostics_error":{json_string(str(exc))}}}'


def json_string(value: str) -> str:
    return json.dumps(value)


if __name__ == "__main__":
    sys.exit(main())
